package controller

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"chat/model"
	"chat/store"
)

// Chia sẻ tin nhắn sang kênh / tin nhắn riêng khác dưới dạng thẻ liên kết (embed type "share",
// id = id tin gốc), và link …/m/<id> mở thẳng tới tin. Ai mở được tin gốc là do quyền kênh gốc:
// thành viên xem đầy đủ; kênh công khai thì người ngoài được mời tham gia; kênh riêng tư / tin
// nhắn riêng thì người ngoài không thấy gì.

const shareTextMax = 300

/* ---------- GET /api/messages/{id}/link ---------- */

type linkChannel struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	IsPrivate   bool   `json:"is_private"`
	MemberCount int    `json:"member_count,omitempty"`
}

type linkInfo struct {
	Channel linkChannel `json:"channel"`
	// Member: người xem ở trong kênh gốc (khi đó có Message đầy đủ).
	Member  bool           `json:"member"`
	Message *model.Message `json:"message,omitempty"`
}

func (a *API) messageLink(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "Không tìm thấy tin nhắn.")
		return
	}
	msg, err := a.store.GetMessage(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && msg.DeletedAt != nil) {
		writeError(w, http.StatusNotFound, "Tin nhắn không còn tồn tại.")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	ch, err := a.store.GetChannel(r.Context(), msg.ChannelID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	me := currentUser(r)
	member, err := a.store.IsMember(r.Context(), ch.ID, me.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	info := linkInfo{Channel: linkChannel{ID: ch.ID, Name: ch.Name, Kind: ch.Kind, IsPrivate: ch.IsPrivate}, Member: member}
	switch {
	case member:
		msgs := []model.Message{msg}
		if err := a.fillForViewer(r.Context(), me.ID, msgs); err != nil {
			internalError(w, r, err)
			return
		}
		info.Message = &msgs[0]
	case ch.IsDM():
		writeError(w, http.StatusNotFound, "Không tìm thấy tin nhắn.")
		return
	case ch.IsPrivate:
		writeError(w, http.StatusForbidden, "Tin nhắn nằm trong một kênh riêng tư mà bạn chưa tham gia.")
		return
	default:
		// kênh công khai: cho biết tên kênh để mời tham gia, không lộ nội dung
		info.Channel.MemberCount = ch.MemberCount
		if info.Channel.MemberCount == 0 {
			if members, err := a.store.ListMembers(r.Context(), ch.ID); err == nil {
				info.Channel.MemberCount = len(members)
			}
		}
	}
	writeJSON(w, http.StatusOK, info)
}

/* ---------- Thẻ chia sẻ ---------- */

// buildShareEmbed dựng thẻ chia sẻ cho tin rawID (người gửi phải xem được tin gốc).
// Bản tóm tắt lấy từ DB, không tin dữ liệu client gửi. Trả về câu báo lỗi cho người dùng.
func (a *API) buildShareEmbed(ctx context.Context, sender model.User, rawID string) (*model.Embed, string, error) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id <= 0 {
		return nil, "Tin được chia sẻ không hợp lệ.", nil
	}
	src, err := a.store.GetMessage(ctx, id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && src.DeletedAt != nil) {
		return nil, "Tin được chia sẻ không còn tồn tại.", nil
	}
	if err != nil {
		return nil, "", err
	}
	// chia sẻ lại một thẻ chia sẻ: trỏ thẳng tới tin gốc
	if src.Embed != nil && src.Embed.Type == "share" && src.Cipher == "" {
		if inner, err := strconv.ParseInt(src.Embed.ID, 10, 64); err == nil {
			if orig, err := a.store.GetMessage(ctx, inner); err == nil && orig.DeletedAt == nil {
				src = orig
			}
		}
	}
	ch, err := a.store.GetChannel(ctx, src.ChannelID)
	if err != nil {
		return nil, "", err
	}
	member, err := a.store.IsMember(ctx, ch.ID, sender.ID)
	if err != nil {
		return nil, "", err
	}
	if !member {
		return nil, "Bạn không xem được tin này nên không chia sẻ được.", nil
	}
	if ch.IsDM() || src.Cipher != "" {
		return nil, "Tin nhắn riêng chỉ chia sẻ được dạng bản sao.", nil
	}
	e := shareSnapshot(src, a.aiBotID())
	e.Channel, e.ChannelID = ch.Name, ch.ID
	return e, "", nil
}

func (a *API) aiBotID() int64 {
	if a.ai == nil {
		return 0
	}
	return a.ai.bot.ID
}

// shareSnapshot: loại tin, nội dung tóm tắt, ảnh xem trước (nếu có) của tin gốc.
func shareSnapshot(m model.Message, botID int64) *model.Embed {
	e := &model.Embed{Type: "share", ID: strconv.FormatInt(m.ID, 10), Kind: "message", Author: publicName(m.User)}
	text := strings.TrimSpace(m.Content)
	if m.UserID == botID && botID != 0 {
		e.Author = "Trợ lý AI"
		text = strings.TrimSpace(aiSourceRe.ReplaceAllString(text, ""))
	}
	switch {
	case m.Poll != nil:
		e.Kind, e.Title, text = "poll", "Bình chọn", m.Poll.Question
	case m.Embed != nil && m.Embed.Type == "game":
		e.Kind, e.Title = "game", m.Embed.Title
		text = "Phòng chơi " + m.Embed.Title
	case m.Embed != nil && m.Embed.Type == "call":
		e.Kind, e.Title, text = "call", "Cuộc gọi", "Nhật ký cuộc gọi"
	case m.Embed != nil && m.Embed.Type == "sticker":
		e.Kind, e.Title = "sticker", "Sticker"
	case m.Embed != nil && m.Embed.Type == "gif":
		e.Kind, e.Title, e.Preview = "gif", "GIF", m.Embed.Preview
		if e.Preview == "" {
			e.Preview = m.Embed.URL
		}
	}
	for _, at := range m.Attachments {
		if _, ok := imageOf(at); ok && e.Preview == "" {
			e.Preview = at.URL
			if e.Kind == "message" && text == "" {
				e.Kind = "image"
			}
		}
	}
	if text == "" && len(m.Attachments) > 0 {
		names := make([]string, 0, len(m.Attachments))
		for _, at := range m.Attachments {
			names = append(names, at.FileName)
		}
		text = strings.Join(names, ", ")
		if e.Kind == "message" {
			e.Kind = "file"
		}
	}
	if utf8.RuneCountInString(text) > shareTextMax {
		text = string([]rune(text)[:shareTextMax]) + "…"
	}
	e.Text = text
	return e
}
