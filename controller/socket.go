package controller

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"unicode/utf8"

	"chat/auth"
	"chat/model"
	"chat/socket"
	"chat/store"

	"github.com/coder/websocket"
)

// Mã đóng kết nối riêng (4000–4999 dành cho ứng dụng).
// Client gặp 4001 thì không kết nối lại mà quay về màn đăng nhập.
const closeUnauthorized websocket.StatusCode = 4001

const maxMessageRunes = 4000

// Yêu cầu client gửi lên. Mọi yêu cầu đều có "type".
//
//	message.send  {channel_id, content, client_id, parent_id?}   gửi tin (parent_id: trả lời trong luồng)
//	typing        {channel_id}                       đang gõ (client tự giới hạn tần suất)
//	read          {channel_id, message_id}           đã đọc tới tin message_id
//	ping          {}                                 kiểm tra kết nối, server trả "pong"
type clientRequest struct {
	Type      string `json:"type"`
	ChannelID int64  `json:"channel_id"`
	MessageID int64  `json:"message_id"`
	Content   string `json:"content"`
	ClientID  string `json:"client_id"`
	ParentID  int64  `json:"parent_id"` // khác 0: trả lời trong luồng của tin này
}

// serveSocket: GET /ws?token=<token>
// Trình duyệt không cho đặt header Authorization khi mở websocket nên token đi qua query.
func (a *API) serveSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: a.originPatterns,
	})
	if err != nil {
		// Accept đã tự trả HTTP response lỗi, chỉ cần log.
		log.Printf("ws accept %s: %v", r.RemoteAddr, err)
		return
	}
	// Tin nhắn tối đa 4000 ký tự ~ 16KB UTF-8, cộng phần JSON.
	conn.SetReadLimit(32 << 10)

	ctx := r.Context()

	// Xác thực sau khi Accept để gửi được mã đóng 4001 (client đọc được mã này,
	// còn HTTP 401 trước khi upgrade thì trình duyệt giấu mất).
	token := r.URL.Query().Get("token")
	user, err := a.userFromToken(ctx, token)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			log.Printf("ws auth: %v", err)
		}
		conn.Close(closeUnauthorized, "unauthorized")
		return
	}

	channelIDs, err := a.store.ChannelIDsForUser(ctx, user.ID)
	if err != nil {
		log.Printf("ws load channels: %v", err)
		conn.Close(websocket.StatusInternalError, "internal error")
		return
	}

	client := socket.NewClient(user.ID, auth.HashToken(token), conn)
	firstConn, ok := a.hub.Add(client, channelIDs)
	if !ok {
		conn.Close(websocket.StatusGoingAway, "server shutting down")
		return
	}

	writerDone := make(chan struct{})
	go func() {
		client.WritePump(ctx)
		close(writerDone)
	}()

	a.hub.ToClient(client, mustJSON(readyEvent{Type: "ready", User: user, OnlineUserIDs: a.hub.OnlineUserIDs()}))
	if firstConn {
		a.hub.ToAll(mustJSON(presenceEvent{Type: "presence", UserID: user.ID, Online: true}))
	}

	defer func() {
		if lastConn := a.hub.Remove(client); lastConn {
			a.hub.ToAll(mustJSON(presenceEvent{Type: "presence", UserID: user.ID, Online: false}))
		}
		<-writerDone
		conn.Close(websocket.StatusNormalClosure, "")
	}()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if !isNormalClose(err) {
				log.Printf("ws read user=%d: %v", user.ID, err)
			}
			return
		}
		var req clientRequest
		if err := json.Unmarshal(data, &req); err != nil {
			a.hub.ToClient(client, mustJSON(errorEvent{Type: "error", Message: "Yêu cầu không phải JSON hợp lệ."}))
			continue
		}
		a.handleRequest(ctx, client, user, req)
	}
}

func (a *API) handleRequest(ctx context.Context, client *socket.Client, user model.User, req clientRequest) {
	reply := func(msg string) {
		a.hub.ToClient(client, mustJSON(errorEvent{Type: "error", Message: msg, ClientID: req.ClientID}))
	}

	switch req.Type {
	case "ping":
		a.hub.ToClient(client, []byte(`{"type":"pong"}`))

	case "message.send":
		content := strings.TrimSpace(req.Content)
		if content == "" {
			reply("Tin nhắn đang trống.")
			return
		}
		if utf8.RuneCountInString(content) > maxMessageRunes {
			reply("Tin nhắn dài tối đa 4000 ký tự.")
			return
		}
		if len(req.ClientID) > 64 {
			req.ClientID = req.ClientID[:64]
		}
		// Kiểm tra lại trong DB: Hub có thể chưa kịp cập nhật nếu vừa bị xoá khỏi kênh.
		member, err := a.store.IsMember(ctx, req.ChannelID, user.ID)
		if err != nil {
			log.Printf("ws is member: %v", err)
			reply("Server gặp lỗi, thử gửi lại.")
			return
		}
		if !member {
			reply("Bạn chưa tham gia kênh này.")
			return
		}
		var parentID *int64
		if req.ParentID > 0 {
			msg, err := a.validateParent(ctx, req.ChannelID, req.ParentID)
			if err != nil {
				log.Printf("ws validate parent: %v", err)
				reply("Server gặp lỗi, thử gửi lại.")
				return
			}
			if msg != "" {
				reply(msg)
				return
			}
			parentID = &req.ParentID
		}
		if _, err := a.createAndBroadcast(ctx, user, req.ChannelID, content, nil, req.ClientID, parentID); err != nil {
			log.Printf("ws create message: %v", err)
			reply("Server gặp lỗi, thử gửi lại.")
			return
		}

	case "typing":
		if a.hub.IsSubscribed(client, req.ChannelID) {
			a.hub.ToChannel(req.ChannelID, mustJSON(typingEvent{Type: "typing", ChannelID: req.ChannelID, UserID: user.ID}), client)
		}

	case "read":
		if req.MessageID > 0 && a.hub.IsSubscribed(client, req.ChannelID) {
			pos, moved, err := a.store.MarkRead(ctx, req.ChannelID, user.ID, req.MessageID)
			if err != nil {
				log.Printf("ws mark read: %v", err)
			} else if moved {
				a.hub.ToChannel(req.ChannelID, mustJSON(readUpdatedEvent{
					Type: "read.updated", ChannelID: req.ChannelID, UserID: user.ID, MessageID: pos,
				}), nil)
			}
		}

	default:
		reply("Không hiểu yêu cầu \"" + req.Type + "\".")
	}
}

func isNormalClose(err error) bool {
	switch websocket.CloseStatus(err) {
	case websocket.StatusNormalClosure, websocket.StatusGoingAway, websocket.StatusNoStatusRcvd, closeUnauthorized:
		return true
	}
	return errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) || errors.Is(err, context.Canceled)
}
