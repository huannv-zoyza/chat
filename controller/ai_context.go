package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"chat/ai"
	"chat/model"
)

// Ngữ cảnh cho câu hỏi @ai: một bước "lập kế hoạch" nhanh quyết định cần đọc gì (bao nhiêu giờ
// lịch sử, tìm lại tin cũ, tìm web, xem ảnh), rồi gom đủ: tin nhắn trong khoảng thời gian đó
// (cả trả lời trong luồng), mô tả ảnh (lưu lại để dùng lần sau), nội dung file chữ, kết quả tìm
// trong kênh, kết quả web kèm nội dung trang.

const (
	aiRangeLimit    = 600    // tin tối đa khi đọc theo khoảng thời gian
	aiHistoryBudget = 90_000 // ký tự lịch sử tối đa
	aiLineMax       = 500    // ký tự tối đa mỗi tin
	aiMaxCaptions   = 6      // ảnh trong lịch sử được xem mỗi lần
	aiFileChars     = 20_000
	aiWebPages      = 4
	aiPageChars     = 3500
)

type aiPlan struct {
	HistoryHours float64 `json:"history_hours"`
	SearchChat   string  `json:"search_chat"`
	WebSearch    string  `json:"web_search"`
	LookAtImages bool    `json:"look_at_images"`
}

// aiImage: một ảnh đính kèm (đủ để đọc file và lưu mô tả).
type aiImage struct {
	Key, Name, Type string
	Size            int64
}

// aiGathered: tóm tắt những gì đã đọc (ghi ở cuối câu trả lời).
type aiGathered struct {
	Messages int
	Since    time.Time
	Images   int
	Files    int
	Web      int
}

func (g aiGathered) String() string {
	var parts []string
	if g.Messages > 0 {
		s := fmt.Sprintf("đọc %d tin", g.Messages)
		if !g.Since.IsZero() {
			s += " từ " + g.Since.Local().Format("15:04 02/01")
		}
		parts = append(parts, s)
	}
	if g.Images > 0 {
		parts = append(parts, fmt.Sprintf("%d ảnh", g.Images))
	}
	if g.Files > 0 {
		parts = append(parts, fmt.Sprintf("%d file", g.Files))
	}
	if g.Web > 0 {
		parts = append(parts, fmt.Sprintf("%d nguồn web", g.Web))
	}
	return strings.Join(parts, ", ")
}

/* ---------- Lập kế hoạch ---------- */

func (a *API) planAI(ctx context.Context, ch model.Channel, text string, hasImages bool) aiPlan {
	guess := guessPlan(text, time.Now())
	if a.ai.chat == nil {
		return guess
	}
	now := time.Now()
	sys := fmt.Sprintf(`You decide what context a chat assistant needs before answering a question asked in the team chat channel #%s.
Current local time: %s (%s).
Reply with ONLY a JSON object, no other text:
{"history_hours": number, "search_chat": string, "web_search": string, "look_at_images": boolean}
- history_hours: how many hours of this channel's recent messages to read. 0 = only the last few messages (normal questions).
  Summaries or questions about past discussion need a range: "1 ngày"/"24h" = 24, "hôm nay"/"sáng nay" = hours since 00:00 today,
  "hôm qua" = hours since 00:00 yesterday, "tuần này" = hours since Monday 00:00, "tuần qua"/"1 tuần" = 168, "tháng này" = since day 1.
  "tóm tắt" with no time given = 24. Maximum 744.
- search_chat: a few Vietnamese keywords to search OLDER messages of this channel when the question refers to something
  said before that may be older than the range (e.g. "hôm trước ai nói link deploy"), else "".
- web_search: a concise web search query (in the best language for the topic) when the answer needs up-to-date or external
  information (news, prices, exchange rates, weather, sports, releases, facts you may not know, a website/link), else "".
- look_at_images: true if answering needs to see images posted in the channel (e.g. "ảnh vừa gửi là gì", summaries mentioning pictures).%s`,
		ch.Name, now.Format("15:04 Monday 02/01/2006"), now.Format("-07:00"),
		map[bool]string{true: "\nThe question itself has images attached; they will be described automatically.", false: ""}[hasImages])
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := a.ai.chat.Complete(pctx, []ai.Message{{Role: "system", Content: sys}, {Role: "user", Content: text}})
	if err != nil {
		return guess
	}
	var p aiPlan
	if i, j := strings.Index(out, "{"), strings.LastIndex(out, "}"); i >= 0 && j > i {
		if json.Unmarshal([]byte(out[i:j+1]), &p) != nil {
			return guess
		}
	} else {
		return guess
	}
	// kế hoạch bỏ sót khoảng thời gian của một câu hỏi về cuộc trò chuyện: tin vào luật đoán
	// ("giá vàng hôm nay" có chữ "hôm nay" nhưng không cần đọc lịch sử kênh)
	if p.HistoryHours <= 0 && guess.HistoryHours > 0 && aboutChatRe.MatchString(strings.ToLower(text)) {
		p.HistoryHours = guess.HistoryHours
	}
	if math.IsNaN(p.HistoryHours) || p.HistoryHours < 0 {
		p.HistoryHours = 0
	}
	p.HistoryHours = math.Min(p.HistoryHours, 744)
	p.SearchChat = shorten(p.SearchChat, 100)
	p.WebSearch = shorten(p.WebSearch, 200)
	return p
}

var (
	rangeRe     = regexp.MustCompile(`(\d+(?:[.,]\d+)?)\s*(phút|phut|giờ|gio|tiếng|tieng|h|ngày|ngay|tuần|tuan|tháng|thang)\b`)
	aboutChatRe = regexp.MustCompile(`tóm tắt|tom tat|summar|mọi người|kênh|nhóm|ai nói|ai đã|nói gì|bàn|thảo luận|trao đổi|chat|tin nhắn|cuộc họp|đã chốt|quyết định|việc gì|làm gì`)
	webRe       = regexp.MustCompile(`(?i)(tìm trên mạng|tra google|google|search|tin tức|tin mới|giá vàng|giá xăng|tỷ giá|ty gia|thời tiết|thoi tiet|mới nhất|hiện nay|bây giờ là|kết quả trận|lịch thi đấu|link|website|trang web)`)
)

// guessPlan: luật đoán đơn giản khi không gọi được bước lập kế hoạch.
func guessPlan(text string, now time.Time) aiPlan {
	t := strings.ToLower(text)
	var p aiPlan
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch {
	case strings.Contains(t, "hôm qua") || strings.Contains(t, "hom qua"):
		p.HistoryHours = now.Sub(midnight.AddDate(0, 0, -1)).Hours()
	case strings.Contains(t, "hôm nay") || strings.Contains(t, "sáng nay") || strings.Contains(t, "chiều nay") || strings.Contains(t, "hom nay"):
		p.HistoryHours = math.Max(1, now.Sub(midnight).Hours())
	case strings.Contains(t, "tuần này"):
		wd := (int(now.Weekday()) + 6) % 7 // thứ Hai = 0
		p.HistoryHours = now.Sub(midnight.AddDate(0, 0, -wd)).Hours()
	case strings.Contains(t, "tuần qua") || strings.Contains(t, "tuần trước"):
		p.HistoryHours = 168
	case strings.Contains(t, "tháng này"):
		p.HistoryHours = now.Sub(time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())).Hours()
	}
	if m := rangeRe.FindStringSubmatch(t); m != nil && p.HistoryHours == 0 {
		n, _ := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64)
		unit := map[string]float64{"phút": 1.0 / 60, "phut": 1.0 / 60, "giờ": 1, "gio": 1, "tiếng": 1, "tieng": 1, "h": 1,
			"ngày": 24, "ngay": 24, "tuần": 168, "tuan": 168, "tháng": 720, "thang": 720}[m[2]]
		p.HistoryHours = n * unit
	}
	if p.HistoryHours == 0 && (strings.Contains(t, "tóm tắt") || strings.Contains(t, "tom tat") || strings.Contains(t, "summar")) {
		p.HistoryHours = 24
	}
	p.HistoryHours = math.Min(p.HistoryHours, 744)
	if webRe.MatchString(t) {
		p.WebSearch = shorten(text, 200)
	}
	if strings.Contains(t, "ảnh") || strings.Contains(t, "hình") {
		p.LookAtImages = true
	}
	return p
}

/* ---------- Gom ngữ cảnh ---------- */

// aiContext dựng hội thoại gửi cho AI: lời dặn + các khối ngữ cảnh + câu hỏi.
func (a *API) aiContext(ctx context.Context, ch model.Channel, q model.Message, asker model.User, text string,
	status func(string)) ([]ai.Message, aiGathered) {
	var got aiGathered
	qImages, qFiles := a.questionAttachments(ctx, q)
	plan := a.planAI(ctx, ch, text, len(qImages) > 0)
	now := time.Now()

	// 1. lịch sử
	var history []model.Message
	dropped := 0
	if q.ParentID != nil {
		status("Đang đọc cả luồng trả lời…")
		if parent, err := a.store.GetMessage(ctx, *q.ParentID); err == nil {
			history = append(history, parent)
		}
		if replies, err := a.store.ListThread(ctx, *q.ParentID); err == nil {
			for _, r := range replies {
				if r.ID < q.ID {
					history = append(history, r)
				}
			}
		}
	}
	if plan.HistoryHours > 0 {
		got.Since = now.Add(-time.Duration(plan.HistoryHours * float64(time.Hour)))
		status(fmt.Sprintf("Đang đọc tin nhắn trong %s qua…", humanHours(plan.HistoryHours)))
		if list, d, err := a.store.MessagesSince(ctx, ch.ID, got.Since, q.ID, aiRangeLimit); err == nil {
			history, dropped = mergeByID(history, list), d
		}
	} else if q.ParentID == nil {
		if list, _, err := a.store.ListMessages(ctx, ch.ID, q.ID, 40); err == nil {
			history = list
		}
	}

	// 2. ảnh: ảnh gửi kèm câu hỏi luôn xem; ảnh trong lịch sử xem khi cần (mô tả cũ đã lưu thì dùng lại)
	captions := map[string]string{}
	var toSee []aiImage
	for _, im := range qImages {
		toSee = append(toSee, im)
	}
	var histImages []aiImage
	for _, m := range history {
		for _, at := range m.Attachments {
			if im, ok := imageOf(at); ok {
				histImages = append(histImages, im)
			}
		}
	}
	for _, im := range histImages {
		if c, _ := a.store.Caption(ctx, im.Key); c != "" {
			captions[im.Key] = c
		}
	}
	if plan.LookAtImages {
		n := 0
		for i := len(histImages) - 1; i >= 0 && n < aiMaxCaptions; i-- { // mới nhất trước
			if captions[histImages[i].Key] == "" {
				toSee = append(toSee, histImages[i])
				n++
			}
		}
	}
	if len(toSee) > 0 && a.ai.chat != nil && a.ai.chat.CanSee() {
		status(fmt.Sprintf("Đang xem %d ảnh…", len(toSee)))
		for k, v := range a.describeImages(ctx, toSee) {
			captions[k] = v
		}
	}
	got.Images = len(captions)

	// 3. lịch sử thành văn bản (giữ phần mới nhất trong giới hạn)
	lines := transcript(history, a.ai.bot.ID, captions)
	total := 0
	start := len(lines)
	for start > 0 && total+len(lines[start-1]) < aiHistoryBudget {
		start--
		total += len(lines[start])
	}
	dropped += countMessages(lines[:start])
	lines = lines[start:]
	got.Messages = countMessages(lines)

	// 4. tìm lại tin cũ trong kênh (chỉ kênh này: câu trả lời hiện ở đây, không được lộ kênh khác)
	var chatHits []string
	if plan.SearchChat != "" {
		status("Đang tìm tin cũ: «" + plan.SearchChat + "»…")
		if hits, err := a.store.SearchMessages(ctx, asker.ID, plan.SearchChat, ch.ID, q.ID, 15); err == nil {
			for i := len(hits) - 1; i >= 0; i-- {
				if l := transcriptLine(hits[i], a.ai.bot.ID, captions, true); l != "" {
					chatHits = append(chatHits, l)
				}
			}
		}
	}

	// 5. file chữ gửi kèm câu hỏi
	var files []string
	for _, f := range qFiles {
		status("Đang đọc file " + f.FileName + "…")
		if body := a.readTextFile(f); body != "" {
			files = append(files, fmt.Sprintf("### File %s\n%s", f.FileName, body))
			got.Files++
		} else {
			files = append(files, fmt.Sprintf("### File %s (%s, %d KB): chưa đọc được loại file này", f.FileName, f.ContentType, f.Size/1024))
		}
	}

	// 6. tìm web
	var web []string
	if plan.WebSearch != "" && a.ai.search != nil {
		status("Đang tìm trên web: «" + plan.WebSearch + "»…")
		if results, _, err := a.ai.search.Search(ctx, plan.WebSearch, 6); err == nil {
			a.fetchPages(ctx, results, status)
			for i, r := range results {
				b := fmt.Sprintf("[%d] %s — %s\n%s", i+1, r.Title, r.URL, r.Snippet)
				if r.Content != "" {
					b += "\nNội dung trang: " + r.Content
				}
				web = append(web, b)
			}
			got.Web = len(results)
		}
	}

	// lời dặn + ngữ cảnh
	where := "kênh #" + ch.Name
	if q.ParentID != nil {
		where += " (trong một luồng trả lời)"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, `Bạn là "Trợ lý AI" trong Go Chat, ứng dụng chat nhóm nội bộ. Mọi người gọi bạn bằng @ai trong %s.
- Trả lời bằng ngôn ngữ người hỏi dùng (thường là tiếng Việt), thân thiện, đi thẳng vào ý chính; ngắn gọn trừ khi được nhờ viết dài hoặc chi tiết.
- Định dạng bằng Markdown đơn giản: **đậm**, *nghiêng*, danh sách, `+"`code`"+` và khối code. Ưu tiên danh sách; chỉ dùng bảng Markdown khi so sánh nhiều cột thật sự cần (khung chat hẹp). Không dùng HTML.
- Dựa vào các phần ngữ cảnh bên dưới. Khi tóm tắt: bao quát cả khoảng thời gian được đọc, gom theo chủ đề, nêu ai nói gì / quyết định gì / việc còn dang dở, kèm giờ khi hữu ích.
- Dùng kết quả web thì ghi nguồn bằng link Markdown, ví dụ [tên trang](url). Không bịa thông tin; không chắc thì nói rõ.
- QUAN TRỌNG với giá cả, tỷ giá, tỷ số, số liệu, ngày giờ sự kiện: chỉ nêu con số xuất hiện NGUYÊN VĂN trong phần "Kết quả tìm web" và ghi nguồn ngay cạnh. Tuyệt đối không dùng số nhớ từ trước (thường đã cũ). Nếu kết quả web không có số liệu, nói thẳng là chưa đọc được số liệu (trang có thể tải bằng JavaScript) và đưa link để người hỏi tự xem.
- Mô tả ảnh do một model khác viết; nói về ảnh thì dựa vào đó.
- Bây giờ là %s. Tính thứ / ngày cẩn thận theo mốc này.`, where, now.Format("15:04 Monday 02/01/2006"))
	if len(lines) > 0 {
		head := fmt.Sprintf("\n\n## Tin nhắn trong %s", where)
		if !got.Since.IsZero() {
			head += fmt.Sprintf(" từ %s đến giờ", got.Since.Local().Format("15:04 02/01"))
		} else {
			head += " (gần nhất)"
		}
		head += fmt.Sprintf(" — %d tin", got.Messages)
		if dropped > 0 {
			head += fmt.Sprintf(", đã bỏ bớt %d tin cũ hơn vì quá dài", dropped)
		}
		sb.WriteString(head + ":\n" + strings.Join(lines, "\n"))
	} else if plan.HistoryHours > 0 {
		sb.WriteString(fmt.Sprintf("\n\n## Không có tin nào trong %s qua ở %s.", humanHours(plan.HistoryHours), where))
	}
	if len(chatHits) > 0 {
		sb.WriteString("\n\n## Tin cũ hơn trong kênh khớp «" + plan.SearchChat + "»:\n" + strings.Join(chatHits, "\n"))
	}
	if len(web) > 0 {
		sb.WriteString("\n\n## Kết quả tìm web cho «" + plan.WebSearch + "»:\n" + strings.Join(web, "\n\n"))
	}
	if len(files) > 0 {
		sb.WriteString("\n\n## File người hỏi gửi kèm / trích dẫn:\n" + strings.Join(files, "\n\n"))
	}
	sb.WriteString(fmt.Sprintf("\n\n(Tính năng của bạn: đọc tin nhắn theo khoảng thời gian, xem ảnh, đọc file chữ, tìm web%s. Người dùng có thể gõ \"@ai vẽ …\" để tạo ảnh hoặc gửi kèm ảnh để vẽ lại.)",
		map[bool]string{true: "", false: " (đang tắt)"}[a.ai.search != nil]))

	ask := displayNameOf(asker) + " hỏi: " + text
	var attached []string
	for _, im := range qImages {
		if c := captions[im.Key]; c != "" {
			attached = append(attached, fmt.Sprintf("- Ảnh %s: %s", im.Name, c))
		} else {
			attached = append(attached, fmt.Sprintf("- Ảnh %s (chưa xem được)", im.Name))
		}
	}
	if len(attached) > 0 {
		ask += "\n(Ảnh gửi kèm câu hỏi:\n" + strings.Join(attached, "\n") + ")"
	}
	if q.ReplyTo != nil && !q.ReplyTo.Deleted && q.ReplyTo.Content != "" {
		who := "?"
		if u, err := a.store.GetUserByID(ctx, q.ReplyTo.UserID); err == nil {
			who = displayNameOf(u)
		}
		ask = fmt.Sprintf("(Đang trả lời tin của %s: «%s»)\n%s", who, shorten(q.ReplyTo.Content, 1500), ask)
	}
	return []ai.Message{{Role: "system", Content: sb.String()}, {Role: "user", Content: ask}}, got
}

func humanHours(h float64) string {
	switch {
	case h < 1:
		return fmt.Sprintf("%.0f phút", h*60)
	case h < 48:
		return fmt.Sprintf("%.0f giờ", h)
	default:
		return fmt.Sprintf("%.0f ngày", h/24)
	}
}

func mergeByID(a, b []model.Message) []model.Message {
	seen := map[int64]bool{}
	var out []model.Message
	for _, list := range [][]model.Message{a, b} {
		for _, m := range list {
			if !seen[m.ID] {
				seen[m.ID] = true
				out = append(out, m)
			}
		}
	}
	// id tăng dần = thứ tự thời gian
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].ID < out[j-1].ID; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// transcript: các dòng lịch sử, chèn dòng ngày khi sang ngày mới. Dòng ngày không tính là tin.
func transcript(msgs []model.Message, botID int64, captions map[string]string) []string {
	var out []string
	day := ""
	for _, m := range msgs {
		l := transcriptLine(m, botID, captions, false)
		if l == "" {
			continue
		}
		if d := m.CreatedAt.Local().Format("Monday 02/01/2006"); d != day {
			day = d
			out = append(out, "--- "+d+" ---")
		}
		out = append(out, l)
	}
	return out
}

func countMessages(lines []string) int {
	n := 0
	for _, l := range lines {
		if !strings.HasPrefix(l, "--- ") {
			n++
		}
	}
	return n
}

// transcriptLine: "[15:04] Tên: nội dung [ảnh …] [file …]"; trả lời trong luồng thụt vào.
func transcriptLine(m model.Message, botID int64, captions map[string]string, withDate bool) string {
	if m.DeletedAt != nil || m.Cipher != "" {
		return ""
	}
	name := publicName(m.User)
	if m.UserID == botID {
		name = "Trợ lý AI"
	}
	var parts []string
	if c := strings.TrimSpace(m.Content); c != "" && !strings.HasPrefix(c, aiPending) {
		if m.UserID == botID {
			c = aiSourceRe.ReplaceAllString(c, "")
		}
		parts = append(parts, shorten(c, aiLineMax))
	}
	if m.Poll != nil {
		opts := make([]string, 0, len(m.Poll.Options))
		for _, o := range m.Poll.Options {
			opts = append(opts, fmt.Sprintf("%s (%d phiếu)", o.Text, o.Votes))
		}
		parts = append(parts, "[bình chọn: "+m.Poll.Question+" — "+strings.Join(opts, ", ")+"]")
	}
	if e := m.Embed; e != nil {
		switch e.Type {
		case "sticker", "gif":
			parts = append(parts, "[gửi "+e.Type+"]")
		case "call":
			parts = append(parts, "[cuộc gọi]")
		case "game":
			parts = append(parts, "[mở trò chơi "+e.Title+"]")
		case "share":
			parts = append(parts, fmt.Sprintf("[chia sẻ tin của %s ở #%s: %s]", e.Author, e.Channel, shorten(e.Text, 200)))
		}
	}
	for _, at := range m.Attachments {
		if im, ok := imageOf(at); ok {
			if c := captions[im.Key]; c != "" {
				parts = append(parts, fmt.Sprintf("[ảnh %s: %s]", at.FileName, c))
			} else {
				parts = append(parts, "[ảnh "+at.FileName+"]")
			}
		} else {
			parts = append(parts, fmt.Sprintf("[file %s, %d KB]", at.FileName, max(1, at.Size/1024)))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	when := m.CreatedAt.Local().Format("15:04")
	if withDate {
		when = m.CreatedAt.Local().Format("15:04 02/01")
	}
	indent := ""
	if m.ParentID != nil {
		indent = "  ↳ "
	}
	return fmt.Sprintf("%s[%s] %s: %s", indent, when, name, strings.Join(parts, " "))
}

var numberRe = regexp.MustCompile(`\d[\d.,]{2,}`)

var aiSourceRe = regexp.MustCompile(`\n*_— [^\n]*_\s*$`)

func imageOf(at model.Attachment) (aiImage, bool) {
	switch at.ContentType {
	case "image/png", "image/jpeg", "image/webp":
		return aiImage{Key: at.StorageKey, Name: at.FileName, Type: at.ContentType, Size: at.Size}, true
	}
	return aiImage{}, false
}

// questionAttachments: ảnh và file của tin hỏi, hoặc của tin được trả lời trích dẫn.
func (a *API) questionAttachments(ctx context.Context, q model.Message) ([]aiImage, []model.Attachment) {
	atts := q.Attachments
	if q.ReplyToID != nil {
		if src, err := a.store.GetMessage(ctx, *q.ReplyToID); err == nil && src.ChannelID == q.ChannelID && src.Cipher == "" {
			atts = append(append([]model.Attachment{}, atts...), src.Attachments...)
		}
	}
	var images []aiImage
	var files []model.Attachment
	for _, at := range atts {
		if im, ok := imageOf(at); ok {
			if len(images) < maxAIRefs && at.Size <= maxAIRefBytes {
				images = append(images, im)
			}
		} else if len(files) < 3 {
			files = append(files, at)
		}
	}
	return images, files
}

// describeImages: mô tả các ảnh (song song, tối đa 3 cùng lúc) và lưu lại mô tả.
func (a *API) describeImages(ctx context.Context, images []aiImage) map[string]string {
	out := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 3)
	for _, im := range images {
		if c, _ := a.store.Caption(ctx, im.Key); c != "" {
			out[im.Key] = c
			continue
		}
		if !storageKeyRe.MatchString(im.Key) {
			continue
		}
		wg.Add(1)
		go func(im aiImage) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			data, err := os.ReadFile(filepath.Join(a.uploadsDir, im.Key))
			if err != nil {
				return
			}
			dctx, cancel := context.WithTimeout(ctx, 45*time.Second)
			defer cancel()
			text, _, err := a.ai.chat.Describe(dctx, "Mô tả bức ảnh này bằng tiếng Việt trong tối đa 120 từ: có ai / cái gì, đang làm gì, bối cảnh, màu sắc nổi bật, và chép lại nguyên văn mọi chữ đọc được trong ảnh. Chỉ trả lời phần mô tả.",
				[]ai.Ref{{Data: data, Type: im.Type}})
			if err != nil || text == "" {
				return
			}
			text = shorten(text, 1200)
			_ = a.store.SaveCaption(ctx, im.Key, text)
			mu.Lock()
			out[im.Key] = text
			mu.Unlock()
		}(im)
	}
	wg.Wait()
	return out
}

// Loại file đọc được dạng chữ.
var textExt = map[string]bool{
	".txt": true, ".md": true, ".csv": true, ".tsv": true, ".json": true, ".log": true, ".xml": true, ".yaml": true, ".yml": true,
	".html": true, ".htm": true, ".go": true, ".js": true, ".ts": true, ".tsx": true, ".jsx": true, ".py": true, ".java": true,
	".kt": true, ".php": true, ".rb": true, ".rs": true, ".c": true, ".h": true, ".cpp": true, ".cs": true, ".sql": true,
	".sh": true, ".ini": true, ".toml": true, ".env.example": true, ".css": true, ".scss": true, ".vue": true, ".swift": true,
}

func (a *API) readTextFile(at model.Attachment) string {
	ext := strings.ToLower(filepath.Ext(at.FileName))
	if !(strings.HasPrefix(at.ContentType, "text/") || at.ContentType == "application/json" || textExt[ext]) || at.Size > 2<<20 ||
		!storageKeyRe.MatchString(at.StorageKey) {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(a.uploadsDir, at.StorageKey))
	if err != nil || !utf8.Valid(b) {
		return ""
	}
	s := string(b)
	if utf8.RuneCountInString(s) > aiFileChars {
		s = string([]rune(s)[:aiFileChars]) + "\n… (đã cắt bớt)"
	}
	return "```\n" + s + "\n```"
}

// fetchPages: tải nội dung các trang đầu (dịch vụ chưa trả sẵn) qua client chặn địa chỉ nội bộ.
func (a *API) fetchPages(ctx context.Context, results []ai.SearchResult, status func(string)) {
	var idx []int
	for i := range results {
		if results[i].Content == "" && len(idx) < aiWebPages {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 {
		return
	}
	status(fmt.Sprintf("Đang đọc %d trang web…", len(idx)))
	var wg sync.WaitGroup
	for _, i := range idx {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pctx, cancel := context.WithTimeout(ctx, 12*time.Second)
			defer cancel()
			text, err := ai.PageText(pctx, a.unfurlClient, results[i].URL, aiPageChars)
			switch {
			case err != nil:
			case len(numberRe.FindAllString(text, -1)) < 3:
				// toàn menu, số liệu nạp bằng JavaScript: báo rõ để AI không đoán
				results[i].Content = "(không đọc được nội dung chính của trang, có thể trang tải số liệu bằng JavaScript)"
			default:
				results[i].Content = text
			}
		}(i)
	}
	wg.Wait()
}
