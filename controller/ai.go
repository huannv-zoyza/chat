package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"chat/ai"
	"chat/auth"
	"chat/model"
	"chat/store"
)

// Trợ lý AI trong kênh. Gõ "@ai câu hỏi" (kể cả trong luồng) thì bot "Trợ lý AI" trả lời ngay
// trong kênh, chữ hiện dần (message.updated); "@ai vẽ …" thì tạo ảnh và gửi kèm tin.
// Bot là một tài khoản thường tên "ai" (người dùng không đăng ký được tên 2 ký tự này).
// Tin nhắn riêng được mã hoá đầu cuối nên server không đọc được: AI chỉ hoạt động trong kênh.

const (
	aiBotUsername = "ai"
	// Nội dung tạm khi bot đang nghĩ (client hiện hiệu ứng "đang suy nghĩ").
	aiPending      = "…"
	aiHistoryLimit = 30
	aiHistoryChars = 12000
	aiFlushEvery   = 400 * time.Millisecond
)

// "@ai" đứng riêng (không phải một phần của @aixyz hay email).
var aiMentionRe = regexp.MustCompile(`(?i)(^|[^\p{L}\p{N}_.@])@ai\b`)

// Yêu cầu vẽ: "@ai vẽ …", "@ai tạo ảnh …", "@ai /img …", "@ai draw …"; thêm --ngang / --doc để đổi khung.
// (\b của Go chỉ hiểu chữ ASCII nên sau từ khoá phải là khoảng trắng / dấu câu / hết chuỗi.)
var aiDrawRe = regexp.MustCompile(`(?is)^\s*(?:/(?:ve|vẽ|img|anh|ảnh|image)|vẽ|ve|gen\s*(?:ảnh|anh|hình|hinh|image)?|tạo\s+(?:ảnh|hình)|tao\s+(?:anh|hinh)|làm\s+ảnh|lam\s+anh|draw|generate\s+(?:an?\s+)?image(?:\s+of)?)(?:[\s:,-]+|$)(.*)$`)

// aiLeadURLRe: người ta hay dán link ảnh trước rồi mới nhờ vẽ ("@ai <link> hãy gen ảnh …").
// Bỏ các URL đứng đầu đi rồi mới so khớp ý định, chứ không nới neo ^ (nới ra là "ai vẽ bìa?" cũng thành lệnh vẽ).
var aiLeadURLRe = regexp.MustCompile(`^\s*(?:https?://\S+\s*)+`)

// Có ảnh gốc kèm theo: các lời nhờ sửa / vẽ lại ảnh ("@ai biến ảnh này thành …", "@ai cho nhân vật này đội mũ").
var aiEditRe = regexp.MustCompile(`(?i)^\s*(?:biến|chuyển|sửa|chỉnh|đổi|làm|thêm|xoá|xóa|bỏ|cho|vẽ\s+lại|tạo\s+lại|phong\s+cách|style|make|turn|edit|redraw)(?:[\s:,-]|$)`)

const (
	maxAIRefs     = 3
	maxAIRefBytes = 8 << 20
)

type aiService struct {
	chat   *ai.Chat
	images *ai.Images
	search *ai.Search // nil: không tìm web
	bot    model.User
	sem    chan struct{}
	asks   *rateWindow
	draws  *rateWindow
}

// aiInfo gửi kèm "ready": client bật/tắt giao diện AI theo đó.
type aiInfo struct {
	BotID int64 `json:"bot_id"`
	Chat  bool  `json:"chat"`
	Image bool  `json:"image"`
	Edit  bool  `json:"edit"` // vẽ lại được từ ảnh gốc
}

func (a *API) aiInfo() *aiInfo {
	if a.ai == nil {
		return nil
	}
	return &aiInfo{BotID: a.ai.bot.ID, Chat: a.ai.chat != nil, Image: a.ai.images != nil,
		Edit: a.ai.images != nil && a.ai.images.CanEdit()}
}

// SetAI bật trợ lý AI (chat và/hoặc tạo ảnh) và tạo tài khoản bot nếu chưa có.
func (a *API) SetAI(ctx context.Context, chat *ai.Chat, images *ai.Images, search *ai.Search) error {
	if chat == nil && images == nil {
		return nil
	}
	bot, err := a.store.GetUserByLogin(ctx, aiBotUsername)
	if errors.Is(err, store.ErrNotFound) {
		token, terr := auth.NewToken()
		if terr != nil {
			return terr
		}
		hash, herr := auth.HashPassword(token) // mật khẩu ngẫu nhiên, không ai đăng nhập được
		if herr != nil {
			return herr
		}
		bot, err = a.store.CreateUser(ctx, aiBotUsername, "ai@bot.invalid", hash, "Trợ lý AI")
	}
	if err != nil {
		return fmt.Errorf("tài khoản bot AI: %w", err)
	}
	a.ai = &aiService{
		chat: chat, images: images, search: search, bot: bot,
		sem:   make(chan struct{}, 6),
		asks:  newRateWindow(8, time.Minute),
		draws: newRateWindow(4, time.Minute),
	}
	return nil
}

// rateWindow: tối đa n lần mỗi khoảng per cho mỗi người.
type rateWindow struct {
	mu   sync.Mutex
	n    int
	per  time.Duration
	hits map[int64][]time.Time
}

func newRateWindow(n int, per time.Duration) *rateWindow {
	return &rateWindow{n: n, per: per, hits: map[int64][]time.Time{}}
}

func (r *rateWindow) allow(id int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	list := r.hits[id][:0]
	for _, t := range r.hits[id] {
		if now.Sub(t) < r.per {
			list = append(list, t)
		}
	}
	if len(list) >= r.n {
		r.hits[id] = list
		return false
	}
	r.hits[id] = append(list, now)
	return true
}

// maybeAI: gọi sau khi một tin được tạo; tin có "@ai" thì bot xử lý ở goroutine riêng.
func (a *API) maybeAI(m model.Message, sender model.User) {
	if a.ai == nil || m.Cipher != "" || sender.ID == a.ai.bot.ID || !aiMentionRe.MatchString(m.Content) {
		return
	}
	go a.answerAI(m, sender)
}

func (a *API) answerAI(q model.Message, asker model.User) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	s := a.ai
	ch, err := a.store.GetChannel(ctx, q.ChannelID)
	if err != nil || ch.IsDM() {
		return
	}
	reply := func(content string) (model.Message, error) {
		return a.createAndBroadcast(ctx, s.bot, ch.ID, outgoing{Content: content, ParentID: q.ParentID, ReplyToID: &q.ID})
	}
	text := strings.TrimSpace(aiMentionRe.ReplaceAllString(q.Content, "$1"))
	refs := a.aiRefs(ctx, q)

	intent := aiLeadURLRe.ReplaceAllString(text, "")
	if m := aiDrawRe.FindStringSubmatch(intent); m != nil {
		a.drawAI(ctx, ch, q, asker, m[1], refs, reply)
		return
	}
	if len(refs) > 0 && aiEditRe.MatchString(intent) {
		a.drawAI(ctx, ch, q, asker, text, refs, reply)
		return
	}
	switch {
	case text == "":
		_, _ = reply("Chào " + displayNameOf(asker) + ", mình là Trợ lý AI. Gõ **@ai** kèm câu hỏi để hỏi, hoặc **@ai vẽ** kèm mô tả để mình tạo ảnh.")
		return
	case s.chat == nil:
		_, _ = reply("Máy chủ chưa bật hỏi đáp AI.")
		return
	case !s.asks.allow(asker.ID):
		_, _ = reply("Bạn hỏi nhanh quá, đợi một phút rồi hỏi tiếp nhé.")
		return
	}
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	default:
		_, _ = reply("Đang có nhiều người hỏi cùng lúc, thử lại sau ít giây nhé.")
		return
	}

	m, err := reply(aiPending)
	if err != nil {
		log.Printf("ai reply: %v", err)
		return
	}
	// tiến trình hiện trong tin của bot ("…" + dòng trạng thái) cho tới khi có chữ đầu tiên
	status := func(st string) {
		m.Content = aiPending + "\n" + st
		a.hub.ToChannel(ch.ID, mustJSON(messageUpdatedEvent{Type: "message.updated", Message: m}), nil)
	}
	live := &liveText{flush: func(t string) {
		m.Content = clip(t)
		a.hub.ToChannel(ch.ID, mustJSON(messageUpdatedEvent{Type: "message.updated", Message: m}), nil)
	}}
	// agent: model tự gọi công cụ để tra cứu; không chạy được (chưa trả chữ nào) thì dùng cách cũ
	res, got, err, ok := a.runAgent(ctx, ch, q, asker, text, refs, status, reply, live)
	if !ok {
		var msgs []ai.Message
		msgs, got = a.aiContext(ctx, ch, q, asker, text, status, false)
		status("Đang viết câu trả lời…")
		live.reset()
		res, err = s.chat.Stream(ctx, msgs, live.delta)
	}
	answer := strings.TrimSpace(live.text())
	switch {
	case err != nil && answer == "":
		log.Printf("ai: %s hỏi ở kênh %d: %v", asker.Username, ch.ID, err)
		answer = "⚠️ Chưa hỏi được AI: các dịch vụ AI đang lỗi hoặc hết lượt miễn phí. Thử lại sau ít phút nhé."
	case err != nil:
		log.Printf("ai: ngắt giữa chừng: %v", err)
		answer += "\n\n⚠️ Câu trả lời bị ngắt giữa chừng."
	case answer == "":
		answer = "Mình chưa nghĩ ra câu trả lời, bạn hỏi lại theo cách khác nhé."
	default:
		source := res.Provider + " · " + res.Model
		if g := got.String(); g != "" {
			source += " · " + g
		}
		answer += "\n\n_— " + source + "_"
		log.Printf("ai: trả lời %s ở kênh %d bằng %s/%s (%s, agent=%v)", asker.Username, ch.ID, res.Provider, res.Model, got, ok)
	}
	// Muse vừa viết vừa vẽ được: ảnh sinh ra ngay trong lượt trả lời thì đính vào tin luôn.
	// Các model API thuần không bao giờ điền res.Images nên nhánh này không ảnh hưởng gì tới chúng.
	if n := a.attachAIImages(ctx, m, res.Images, text); n > 0 {
		log.Printf("ai: đính %d ảnh %s tự vẽ vào tin ở kênh %d", n, res.Provider, ch.ID)
	}
	a.finishAI(ctx, m, clip(answer))
	go a.compactMemory(ch) // kênh dài ra: gộp tin cũ vào trí nhớ (chạy nền, không bắt người hỏi đợi)
}

// liveText: chữ đang stream vào tin của bot, đẩy cho cả kênh mỗi aiFlushEvery.
// reset: bỏ chữ của lượt trước (lượt đó hoá ra chỉ để gọi công cụ).
type liveText struct {
	buf   strings.Builder
	last  time.Time
	flush func(string)
}

func (l *liveText) delta(d string) {
	l.buf.WriteString(d)
	if time.Since(l.last) >= aiFlushEvery {
		l.last = time.Now()
		l.flush(l.buf.String())
	}
}

func (l *liveText) reset()       { l.buf.Reset() }
func (l *liveText) text() string { return l.buf.String() }
func (l *liveText) empty() bool  { return strings.TrimSpace(l.buf.String()) == "" }

// finishAI lưu nội dung cuối của tin bot và phát lại cho cả kênh.
func (a *API) finishAI(ctx context.Context, m model.Message, content string) {
	if err := a.store.SetContent(ctx, m.ID, content); err != nil {
		log.Printf("ai save: %v", err)
		return
	}
	full, err := a.store.GetMessage(ctx, m.ID)
	if err != nil {
		return
	}
	a.hub.ToChannel(full.ChannelID, mustJSON(messageUpdatedEvent{Type: "message.updated", Message: full}), nil)
}

func clip(s string) string {
	if utf8.RuneCountInString(s) <= maxMessageRunes {
		return s
	}
	r := []rune(s)
	return string(r[:maxMessageRunes-1]) + "…"
}

func displayNameOf(u model.User) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

func publicName(u *model.PublicUser) string {
	if u == nil {
		return "?"
	}
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

func shorten(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

/* ---------- Vẽ ảnh ---------- */

// aiRefs: ảnh gốc cho yêu cầu vẽ lại: ảnh gửi kèm tin, hoặc ảnh trong tin được trả lời trích dẫn.
func (a *API) aiRefs(ctx context.Context, q model.Message) []ai.Ref {
	atts := q.Attachments
	if len(atts) == 0 && q.ReplyToID != nil {
		if src, err := a.store.GetMessage(ctx, *q.ReplyToID); err == nil && src.ChannelID == q.ChannelID && src.Cipher == "" {
			atts = src.Attachments
		}
	}
	var refs []ai.Ref
	for _, at := range atts {
		switch at.ContentType {
		case "image/png", "image/jpeg", "image/webp":
		default:
			continue
		}
		if at.Size > maxAIRefBytes || !storageKeyRe.MatchString(at.StorageKey) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(a.uploadsDir, at.StorageKey))
		if err != nil {
			continue
		}
		refs = append(refs, ai.Ref{Data: data, Type: at.ContentType})
		if len(refs) == maxAIRefs {
			break
		}
	}
	return refs
}

// drawAI vẽ (hoặc vẽ lại từ refs) rồi gửi ảnh vào kênh. Trả về "" nếu đã gửi ảnh, không thì lý do
// (đã báo cho người dùng) để agent biết.
func (a *API) drawAI(ctx context.Context, ch model.Channel, q model.Message, asker model.User, prompt string, refs []ai.Ref,
	reply func(string) (model.Message, error)) string {
	s := a.ai
	fail := func(msg string) string {
		_, _ = reply(msg)
		return msg
	}
	w, h := 1024, 1024
	low := strings.ToLower(prompt)
	switch {
	case strings.Contains(low, "--ngang"):
		w, h = 1216, 832
	case strings.Contains(low, "--doc"), strings.Contains(low, "--dọc"):
		w, h = 832, 1216
	}
	for _, f := range []string{"--ngang", "--doc", "--dọc", "--vuong", "--vuông"} {
		prompt = strings.ReplaceAll(prompt, f, "")
		prompt = strings.ReplaceAll(prompt, strings.ToUpper(f), "")
	}
	prompt = strings.TrimSpace(prompt)
	switch {
	case s.images == nil:
		return fail("Máy chủ chưa bật tạo ảnh AI.")
	case len(refs) > 0 && !s.images.CanEdit():
		return fail("Máy chủ chưa bật vẽ lại từ ảnh gốc (cần key Pollinations hoặc model ảnh của OpenRouter). Bỏ ảnh đi thì mình vẽ mới theo mô tả được.")
	case prompt == "":
		return fail("Bạn muốn vẽ gì? Ví dụ: **@ai vẽ** chú mèo phi hành gia, phong cách màu nước")
	case utf8.RuneCountInString(prompt) > 800:
		return fail("Mô tả dài quá, gói gọn trong 800 ký tự nhé.")
	case !s.draws.allow(asker.ID):
		return fail("Bạn nhờ vẽ nhanh quá, đợi một phút rồi thử lại nhé.")
	}
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	default:
		return fail("Đang có nhiều người nhờ cùng lúc, thử lại sau ít giây nhé.")
	}
	what := "vẽ"
	if len(refs) > 0 {
		what = "vẽ lại từ ảnh gốc"
	}
	m, err := reply("🎨 Đang " + what + ": “" + prompt + "”…")
	if err != nil {
		return "không gửi được tin"
	}

	// Mô hình vẽ hiểu tiếng Anh tốt hơn: nhờ AI chat viết lại mô tả (lỗi thì dùng nguyên văn).
	english := prompt
	if s.chat != nil && !isASCII(prompt) { // agent đã viết sẵn mô tả tiếng Anh
		pctx, cancel := context.WithTimeout(ctx, 25*time.Second)
		sys := "You turn a user's image request (often in Vietnamese) into ONE concise English prompt for a text-to-image model. Keep the subject, style and any text the user wants shown; add a few concrete visual details. Reply with the prompt only, no quotes, no explanations."
		if len(refs) > 0 {
			sys = "The user attached an image and asks (often in Vietnamese) to edit or redraw it. Write ONE concise English instruction for an image-editing model: describe the desired result and style, and say what to keep from the original (for example the character's identity, face, pose or composition) unless the user wants it changed. Reply with the instruction only, no quotes, no explanations."
		}
		out, err := s.chat.Complete(pctx, []ai.Message{{Role: "system", Content: sys}, {Role: "user", Content: prompt}})
		cancel()
		if out = strings.Trim(strings.TrimSpace(out), `"'`); err == nil && out != "" && utf8.RuneCountInString(out) < 1200 {
			english = out
		}
	}

	var data []byte
	var provider string
	if len(refs) > 0 {
		data, provider, err = s.images.Edit(ctx, english, refs, w, h)
	} else {
		data, provider, err = s.images.Generate(ctx, english, w, h)
	}
	if err != nil {
		log.Printf("ai image: %v", err)
		why := "dịch vụ tạo ảnh đang lỗi hoặc quá tải. Thử lại sau nhé."
		if ai.OutOfCredit(err) {
			why = "tài khoản dịch vụ vẽ ảnh đã hết credit, nhờ admin nạp thêm nhé."
		}
		a.finishAI(ctx, m, "⚠️ Chưa vẽ được “"+prompt+"”: "+why)
		return why
	}
	att, err := a.saveBytes(data, "ai-"+fileSlug(prompt))
	if err != nil {
		log.Printf("ai image save: %v", err)
		a.finishAI(ctx, m, "⚠️ Vẽ xong nhưng không lưu được ảnh, thử lại nhé.")
		return "không lưu được ảnh"
	}
	if err := a.store.AddAttachments(ctx, m.ID, []model.Attachment{att}); err != nil {
		a.removeUpload(att.StorageKey)
		log.Printf("ai image attach: %v", err)
		return "không lưu được ảnh"
	}
	log.Printf("ai: vẽ cho %s ở kênh %d bằng %s (%d KB)", asker.Username, ch.ID, provider, len(data)/1024)
	a.finishAI(ctx, m, "🎨 “"+prompt+"”\n\n_— "+what+" bởi "+provider+"_")
	return ""
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// saveBytes lưu ảnh AI vào thư mục uploads như một file đính kèm.
func (a *API) saveBytes(data []byte, base string) (model.Attachment, error) {
	key, err := newStorageKey()
	if err != nil {
		return model.Attachment{}, err
	}
	ctype := http.DetectContentType(data)
	ext := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "image/gif": ".gif"}[ctype]
	if ext == "" {
		return model.Attachment{}, fmt.Errorf("loại ảnh không hỗ trợ: %s", ctype)
	}
	att := model.Attachment{StorageKey: key, FileName: base + ext, ContentType: ctype, Size: int64(len(data))}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		att.Width, att.Height = cfg.Width, cfg.Height
	}
	if err := os.WriteFile(filepath.Join(a.uploadsDir, key), data, 0o644); err != nil {
		return att, err
	}
	att.URL = store.AttachmentURL(key)
	return att, nil
}

// fileSlug: tên file gọn từ mô tả ("chú mèo" -> "chu-meo").
func fileSlug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if base, ok := slugBase[r]; ok {
			r = base
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= 40 {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "anh"
	}
	return out
}

var slugBase = func() map[rune]rune {
	m := map[rune]rune{}
	for base, chars := range map[rune]string{
		'a': "àáảãạăằắẳẵặâầấẩẫậ", 'e': "èéẻẽẹêềếểễệ", 'i': "ìíỉĩị", 'o': "òóỏõọôồốổỗộơờớởỡợ",
		'u': "ùúủũụưừứửữự", 'y': "ỳýỷỹỵ", 'd': "đ",
	} {
		for _, c := range chars {
			m[c] = base
		}
	}
	return m
}()

// attachAIImages lưu các ảnh nhà cung cấp tự sinh ra rồi gắn vào tin trả lời của bot.
// Trả về số ảnh đã gắn. Lỗi của một ảnh không làm hỏng câu trả lời.
func (a *API) attachAIImages(ctx context.Context, m model.Message, imgs []ai.Ref, prompt string) int {
	if len(imgs) == 0 {
		return 0
	}
	if len(imgs) > maxAIRefs {
		imgs = imgs[:maxAIRefs]
	}
	var atts []model.Attachment
	for _, im := range imgs {
		att, err := a.saveBytes(im.Data, "ai-"+fileSlug(prompt))
		if err != nil {
			log.Printf("ai image save: %v", err)
			continue
		}
		atts = append(atts, att)
	}
	if len(atts) == 0 {
		return 0
	}
	if err := a.store.AddAttachments(ctx, m.ID, atts); err != nil {
		for _, at := range atts {
			a.removeUpload(at.StorageKey)
		}
		log.Printf("ai image attach: %v", err)
		return 0
	}
	return len(atts)
}
