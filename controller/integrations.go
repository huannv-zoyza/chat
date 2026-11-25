package controller

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"chat/auth"
	"chat/model"
	"chat/store"
)

// Tích hợp bot (giống Incoming / Outgoing Webhook của Slack):
//   - Chủ kênh tạo bot cho kênh. Bot là một tài khoản riêng tên "<tên>_bot" (người dùng không
//     đăng ký được tên đuôi "_bot"), được thêm vào kênh, không ai đăng nhập vào được.
//   - Webhook nhận tin: dịch vụ ngoài POST {"text": "..."} tới /api/hooks/<mã bí mật> là bot đăng
//     tin vào kênh. Mã chỉ hiện một lần lúc tạo / đổi mã, máy chủ chỉ giữ bản băm.
//   - Trả lời khi được nhắc (tuỳ chọn): ai "@tên_bot" trong kênh thì máy chủ POST tin đó tới
//     outgoing_url (ký HMAC-SHA256 bằng signing_secret); URL trả về {"text": "..."} thì bot đăng
//     câu trả lời ngay dưới tin đó.
// Tin nhắn riêng mã hoá đầu cuối nên không có bot.

const (
	botSuffix        = "_bot"
	botEmailDomain   = "@bot.invalid"
	maxIntegrations  = 20 // mỗi kênh
	maxBotNameRunes  = 40
	maxHookURLLen    = 500
	hookRatePerMin   = 30
	outgoingTimeout  = 10 * time.Second
	outgoingMaxReply = 64 << 10
)

// isBotUsername: tài khoản bot tích hợp (đuôi "_bot").
func isBotUsername(u string) bool { return strings.HasSuffix(u, botSuffix) }

// integrationView: thông tin bot trả cho client. Khoá ký chỉ chủ kênh thấy.
type integrationView struct {
	store.Integration
	HookPath string `json:"hook_path,omitempty"` // có khi vừa tạo / đổi mã: "/api/hooks/<mã>"
}

func viewIntegration(it store.Integration, owner bool, token string) integrationView {
	if !owner {
		it.SigningSecret = ""
		it.OutgoingURL = ""
	}
	v := integrationView{Integration: it}
	if token != "" {
		v.HookPath = "/api/hooks/" + token
	}
	return v
}

// validOutgoingURL: rỗng (tắt) hoặc http(s) có host. Cho phép địa chỉ trong mạng LAN:
// bot thường chạy trên máy trong cùng mạng; chỉ chủ kênh mới đặt được URL này.
func validOutgoingURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", true
	}
	if len(raw) > maxHookURLLen {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", false
	}
	return u.String(), true
}

// botUsernameFor: "CI Bot" -> "ci_bot", "Đơn hàng" -> "don_hang_bot".
func botUsernameFor(name string) string {
	var b strings.Builder
	lastUnderscore := true
	for _, r := range foldVN(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		case !lastUnderscore:
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	base := strings.Trim(b.String(), "_")
	base = strings.TrimSuffix(base, "_bot")
	base = strings.TrimSuffix(base, "bot")
	base = strings.Trim(base, "_")
	if base == "" {
		base = "app"
	}
	if max := 32 - len(botSuffix) - 3; len(base) > max { // chừa chỗ cho số đuôi khi trùng tên
		base = strings.Trim(base[:max], "_")
	}
	return base + botSuffix
}

/* ---------- GET /api/channels/{id}/integrations ---------- */

func (a *API) listIntegrations(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireMember(w, r, ch.ID) {
		return
	}
	list, err := a.store.ListIntegrations(r.Context(), ch.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	owner := ch.OwnerID == currentUser(r).ID
	out := make([]integrationView, len(list))
	for i, it := range list {
		out[i] = viewIntegration(it, owner, "")
	}
	writeJSON(w, http.StatusOK, out)
}

/* ---------- POST /api/channels/{id}/integrations ---------- */

func (a *API) createIntegration(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireOwner(w, r, ch) {
		return
	}
	var req struct {
		Name        string `json:"name"`
		OutgoingURL string `json:"outgoing_url"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	name := strings.Join(strings.Fields(req.Name), " ")
	if name == "" || utf8.RuneCountInString(name) > maxBotNameRunes {
		writeError(w, http.StatusBadRequest, "Tên bot từ 1 đến 40 ký tự.")
		return
	}
	outURL, ok := validOutgoingURL(req.OutgoingURL)
	if !ok {
		writeError(w, http.StatusBadRequest, "URL nhận tin phải bắt đầu bằng http:// hoặc https://.")
		return
	}
	ctx := r.Context()
	existing, err := a.store.ListIntegrations(ctx, ch.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if len(existing) >= maxIntegrations {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Mỗi kênh có tối đa %d bot.", maxIntegrations))
		return
	}

	bot, err := a.createBotUser(ctx, name)
	if err != nil {
		internalError(w, r, err)
		return
	}
	token, err := auth.NewToken()
	if err != nil {
		internalError(w, r, err)
		return
	}
	secret, err := auth.NewToken()
	if err != nil {
		internalError(w, r, err)
		return
	}
	it, err := a.store.CreateIntegration(ctx, ch.ID, bot.ID, currentUser(r).ID, auth.HashToken(token), secret, outURL)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if _, err := a.store.AddMember(ctx, ch.ID, bot.ID); err != nil {
		internalError(w, r, err)
		return
	}
	a.hub.ToChannel(ch.ID, mustJSON(memberJoinedEvent{Type: "member.joined", ChannelID: ch.ID, User: bot.Public()}), nil)
	writeJSON(w, http.StatusCreated, viewIntegration(it, true, token))
}

// createBotUser tạo tài khoản bot tên "<tên>_bot" (thêm số nếu trùng), mật khẩu ngẫu nhiên.
func (a *API) createBotUser(ctx context.Context, displayName string) (model.User, error) {
	base := botUsernameFor(displayName)
	pw, err := auth.NewToken()
	if err != nil {
		return model.User{}, err
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		return model.User{}, err
	}
	for i := 0; i < 50; i++ {
		username := base
		if i > 0 {
			username = strings.TrimSuffix(base, botSuffix) + strconv.Itoa(i+1) + botSuffix
		}
		u, err := a.store.CreateUser(ctx, username, username+botEmailDomain, hash, displayName)
		if errors.Is(err, store.ErrConflict) {
			continue
		}
		return u, err
	}
	return model.User{}, errors.New("không tìm được tên bot còn trống")
}

// loadIntegration: bot {iid} của kênh {id}, người gọi phải là chủ kênh.
func (a *API) loadIntegration(w http.ResponseWriter, r *http.Request) (model.Channel, store.Integration, bool) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireOwner(w, r, ch) {
		return ch, store.Integration{}, false
	}
	iid, err := strconv.ParseInt(r.PathValue("iid"), 10, 64)
	if err != nil || iid <= 0 {
		writeError(w, http.StatusNotFound, "Không tìm thấy bot.")
		return ch, store.Integration{}, false
	}
	it, err := a.store.GetIntegration(r.Context(), iid)
	if errors.Is(err, store.ErrNotFound) || (err == nil && it.ChannelID != ch.ID) {
		writeError(w, http.StatusNotFound, "Không tìm thấy bot.")
		return ch, it, false
	}
	if err != nil {
		internalError(w, r, err)
		return ch, it, false
	}
	return ch, it, true
}

/* ---------- PATCH /api/channels/{id}/integrations/{iid} ---------- */

func (a *API) updateIntegration(w http.ResponseWriter, r *http.Request) {
	_, it, ok := a.loadIntegration(w, r)
	if !ok {
		return
	}
	var req struct {
		OutgoingURL string `json:"outgoing_url"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	outURL, ok := validOutgoingURL(req.OutgoingURL)
	if !ok {
		writeError(w, http.StatusBadRequest, "URL nhận tin phải bắt đầu bằng http:// hoặc https://.")
		return
	}
	if err := a.store.UpdateIntegrationURL(r.Context(), it.ID, outURL); err != nil {
		internalError(w, r, err)
		return
	}
	it.OutgoingURL = outURL
	writeJSON(w, http.StatusOK, viewIntegration(it, true, ""))
}

/* ---------- POST /api/channels/{id}/integrations/{iid}/token ---------- */

// rotateIntegrationToken: đổi mã bí mật, link webhook cũ hết dùng được.
func (a *API) rotateIntegrationToken(w http.ResponseWriter, r *http.Request) {
	_, it, ok := a.loadIntegration(w, r)
	if !ok {
		return
	}
	token, err := auth.NewToken()
	if err != nil {
		internalError(w, r, err)
		return
	}
	if err := a.store.RotateIntegrationToken(r.Context(), it.ID, auth.HashToken(token)); err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, viewIntegration(it, true, token))
}

/* ---------- DELETE /api/channels/{id}/integrations/{iid} ---------- */

func (a *API) deleteIntegration(w http.ResponseWriter, r *http.Request) {
	ch, it, ok := a.loadIntegration(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if err := a.store.DeleteIntegration(ctx, it.ID); err != nil {
		internalError(w, r, err)
		return
	}
	if removed, err := a.store.RemoveMember(ctx, ch.ID, it.BotUserID); err != nil {
		internalError(w, r, err)
		return
	} else if removed {
		a.hub.ToChannel(ch.ID, mustJSON(memberLeftEvent{Type: "member.left", ChannelID: ch.ID, UserID: it.BotUserID}), nil)
	}
	w.WriteHeader(http.StatusNoContent)
}

/* ---------- POST /api/hooks/{token} (không cần đăng nhập) ---------- */

type hookPayload struct {
	Text    string `json:"text"`
	Content string `json:"content"` // tên khác của text
}

func (a *API) incomingHook(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		writeError(w, http.StatusNotFound, "Không tìm thấy webhook.")
		return
	}
	ctx := r.Context()
	it, err := a.store.IntegrationByToken(ctx, auth.HashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Không tìm thấy webhook (mã sai hoặc đã đổi).")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	if !a.hookLimiter().allow(it.ID) {
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("Bot gửi quá nhanh: tối đa %d tin mỗi phút.", hookRatePerMin))
		return
	}
	// nhận cả JSON có trường lạ (payload kiểu Slack có thêm username, icon_emoji…)
	var p hookPayload
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, `Body phải là JSON, ví dụ {"text": "Xin chào"}.`)
		return
	}
	text := strings.TrimSpace(p.Text)
	if text == "" {
		text = strings.TrimSpace(p.Content)
	}
	if text == "" {
		writeError(w, http.StatusBadRequest, `Thiếu nội dung: gửi {"text": "..."}.`)
		return
	}
	if utf8.RuneCountInString(text) > maxMessageRunes {
		writeError(w, http.StatusBadRequest, "Tin nhắn dài tối đa 4000 ký tự.")
		return
	}
	bot, err := a.store.GetUserByID(ctx, it.BotUserID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	msg, err := a.createAndBroadcast(ctx, bot, it.ChannelID, outgoing{Content: text})
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": msg.ID})
}

func (a *API) hookLimiter() *rateWindow { return a.hookRate }

/* ---------- Trả lời khi được nhắc tên ---------- */

// maybeBots: gọi sau khi một tin được tạo; tin có "@tên_bot" của bot có outgoing_url thì gửi
// tin đó cho bot ở goroutine riêng. Bot không kích hoạt bot (tránh hai bot gọi nhau mãi).
func (a *API) maybeBots(m model.Message, sender model.User) {
	if m.Cipher != "" || isBotUsername(sender.Username) || !strings.Contains(m.Content, botSuffix) {
		return
	}
	usernames, _ := parseMentions(m.Content)
	var named []string
	for _, u := range usernames {
		if isBotUsername(u) {
			named = append(named, u)
		}
	}
	if len(named) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), outgoingTimeout+5*time.Second)
		defer cancel()
		list, err := a.store.ListIntegrations(ctx, m.ChannelID)
		if err != nil {
			log.Printf("bot: liệt kê tích hợp kênh %d: %v", m.ChannelID, err)
			return
		}
		for _, it := range list {
			if it.OutgoingURL != "" && slices.Contains(named, it.BotUsername) {
				go a.callBot(it, m, sender)
			}
		}
	}()
}

type botEvent struct {
	Type      string           `json:"type"` // "mention"
	Timestamp int64            `json:"timestamp"`
	Channel   botEventChannel  `json:"channel"`
	Message   botEventMessage  `json:"message"`
	User      model.PublicUser `json:"user"`
	Bot       model.PublicUser `json:"bot"`
}

type botEventChannel struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type botEventMessage struct {
	ID       int64  `json:"id"`
	Text     string `json:"text"`
	ParentID *int64 `json:"parent_id"`
}

// signBotBody: chữ ký HMAC-SHA256 trên "<timestamp>.<body>" (giống Stripe/Slack), hex.
func signBotBody(secret string, ts int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", ts)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (a *API) callBot(it store.Integration, m model.Message, sender model.User) {
	ctx, cancel := context.WithTimeout(context.Background(), outgoingTimeout)
	defer cancel()
	ch, err := a.store.GetChannel(ctx, it.ChannelID)
	if err != nil {
		return
	}
	bot, err := a.store.GetUserByID(ctx, it.BotUserID)
	if err != nil {
		return
	}
	ts := time.Now().Unix()
	body, _ := json.Marshal(botEvent{
		Type: "mention", Timestamp: ts,
		Channel: botEventChannel{ID: ch.ID, Name: ch.Name},
		Message: botEventMessage{ID: m.ID, Text: m.Content, ParentID: m.ParentID},
		User:    sender.Public(), Bot: bot.Public(),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, it.OutgoingURL, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GoChat-Bot/1")
	req.Header.Set("X-GoChat-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-GoChat-Signature", signBotBody(it.SigningSecret, ts, body))
	res, err := a.botHTTP().Do(req)
	if err != nil {
		log.Printf("bot %s: gọi %s: %v", it.BotUsername, it.OutgoingURL, err)
		return
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		log.Printf("bot %s: %s trả về %d", it.BotUsername, it.OutgoingURL, res.StatusCode)
		return
	}
	var reply hookPayload
	if err := json.NewDecoder(io.LimitReader(res.Body, outgoingMaxReply)).Decode(&reply); err != nil {
		return // không trả lời gì (204, body rỗng…) là hợp lệ
	}
	text := strings.TrimSpace(reply.Text)
	if text == "" {
		text = strings.TrimSpace(reply.Content)
	}
	if text == "" {
		return
	}
	if utf8.RuneCountInString(text) > maxMessageRunes {
		text = string([]rune(text)[:maxMessageRunes])
	}
	if !a.hookLimiter().allow(it.ID) {
		return
	}
	if _, err := a.createAndBroadcast(ctx, bot, it.ChannelID, outgoing{Content: text, ParentID: m.ParentID, ReplyToID: &m.ID}); err != nil {
		log.Printf("bot %s: đăng trả lời: %v", it.BotUsername, err)
	}
}

func (a *API) botHTTP() *http.Client { return a.botClient }
