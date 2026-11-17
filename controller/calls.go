package controller

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"sync"
	"time"

	"chat/model"
	"chat/socket"
)

// Gọi thoại / video 1-1 (WebRTC). Máy chủ chỉ làm "tổng đài":
//   - đổ chuông, nghe, từ chối, cúp máy;
//   - chuyển tiếp thông tin kết nối WebRTC (SDP, ICE) giữa đúng hai thiết bị.
// Âm thanh / hình ảnh đi thẳng giữa hai máy (hoặc qua TURN nếu có), mã hoá DTLS-SRTP.
//
// Client gửi:  call.invite {call_id, channel_id, video} · call.accept {call_id} · call.decline {call_id}
//              call.hangup {call_id} · call.signal {call_id, data}
// Máy chủ gửi: call.incoming {call_id, channel_id, from, video} · call.accepted {call_id}
//              call.signal {call_id, data} · call.ended {call_id, reason}

const ringTimeout = 45 * time.Second

var callIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// ICEServer: máy chủ STUN/TURN cho WebRTC (đúng dạng RTCIceServer của trình duyệt).
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// SetICEServers đặt danh sách STUN/TURN gửi cho client.
func (a *API) SetICEServers(list []ICEServer) { a.iceServers = list }

// GET /api/rtc-config
func (a *API) rtcConfig(w http.ResponseWriter, r *http.Request) {
	list := a.iceServers
	if list == nil {
		list = []ICEServer{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ice_servers": list})
}

type call struct {
	id        string
	channelID int64
	video     bool
	caller    *socket.Client
	from      model.User
	calleeID  int64
	callee    *socket.Client // thiết bị đã nghe máy (nil khi đang đổ chuông)
	answered  time.Time
	timer     *time.Timer
}

type callRegistry struct {
	mu   sync.Mutex
	byID map[string]*call
}

type callIncomingEvent struct {
	Type      string           `json:"type"`
	CallID    string           `json:"call_id"`
	ChannelID int64            `json:"channel_id"`
	From      model.PublicUser `json:"from"`
	Video     bool             `json:"video"`
}

type callEvent struct {
	Type   string          `json:"type"`
	CallID string          `json:"call_id"`
	Reason string          `json:"reason,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// busy: userID đang trong một cuộc gọi (đổ chuông hoặc đang nói).
func (reg *callRegistry) busy(userID int64) bool {
	for _, c := range reg.byID {
		if c.from.ID == userID || c.calleeID == userID {
			return true
		}
	}
	return false
}

func (a *API) handleCall(ctx context.Context, client *socket.Client, user model.User, req clientRequest) {
	send := func(c *socket.Client, ev callEvent) { a.hub.ToClient(c, mustJSON(ev)) }
	fail := func(reason string) {
		send(client, callEvent{Type: "call.ended", CallID: req.CallID, Reason: reason})
	}
	if !callIDRe.MatchString(req.CallID) {
		a.hub.ToClient(client, mustJSON(errorEvent{Type: "error", Message: "Mã cuộc gọi không hợp lệ."}))
		return
	}
	reg := &a.calls

	switch req.Type {
	case "call.invite":
		ch, err := a.store.GetChannel(ctx, req.ChannelID)
		if err != nil || !ch.IsDM() {
			fail("invalid")
			return
		}
		var peer int64
		member := false
		for _, u := range ch.DMMembers {
			if u.ID == user.ID {
				member = true
			} else {
				peer = u.ID
			}
		}
		if !member || peer == 0 {
			fail("invalid")
			return
		}
		reg.mu.Lock()
		if reg.byID == nil {
			reg.byID = map[string]*call{}
		}
		if _, dup := reg.byID[req.CallID]; dup || reg.busy(user.ID) {
			reg.mu.Unlock()
			fail("invalid")
			return
		}
		if reg.busy(peer) {
			reg.mu.Unlock()
			fail("busy")
			return
		}
		if !a.hub.IsOnline(peer) {
			reg.mu.Unlock()
			fail("offline")
			a.logCall(ch.ID, user, req.Video, "missed", 0)
			return
		}
		c := &call{id: req.CallID, channelID: ch.ID, video: req.Video, caller: client, from: user, calleeID: peer}
		c.timer = time.AfterFunc(ringTimeout, func() { a.endCall(c.id, "no_answer") })
		reg.byID[c.id] = c
		reg.mu.Unlock()
		a.hub.ToUser(peer, mustJSON(callIncomingEvent{
			Type: "call.incoming", CallID: c.id, ChannelID: ch.ID, From: user.Public(), Video: req.Video,
		}))

	case "call.accept":
		reg.mu.Lock()
		c := reg.byID[req.CallID]
		if c == nil || c.callee != nil || c.calleeID != user.ID {
			reg.mu.Unlock()
			fail("gone")
			return
		}
		c.callee = client
		c.answered = time.Now()
		c.timer.Stop()
		reg.mu.Unlock()
		send(c.caller, callEvent{Type: "call.accepted", CallID: c.id})
		// các thiết bị khác của người nghe ngừng đổ chuông
		a.hub.ToUserExcept(user.ID, mustJSON(callEvent{Type: "call.ended", CallID: c.id, Reason: "answered_elsewhere"}), client)

	case "call.decline":
		reg.mu.Lock()
		c := reg.byID[req.CallID]
		ok := c != nil && c.calleeID == user.ID && c.callee == nil
		reg.mu.Unlock()
		if ok {
			a.endCall(req.CallID, "declined")
		}

	case "call.hangup":
		reg.mu.Lock()
		c := reg.byID[req.CallID]
		ok := c != nil && (c.caller == client || c.callee == client)
		reg.mu.Unlock()
		if ok {
			a.endCall(req.CallID, "hangup")
		}

	case "call.signal":
		if len(req.Data) == 0 || len(req.Data) > 32<<10 {
			return
		}
		reg.mu.Lock()
		c := reg.byID[req.CallID]
		var to *socket.Client
		if c != nil && c.callee != nil {
			switch client {
			case c.caller:
				to = c.callee
			case c.callee:
				to = c.caller
			}
		}
		reg.mu.Unlock()
		if to != nil {
			send(to, callEvent{Type: "call.signal", CallID: req.CallID, Data: req.Data})
		}
	}
}

// endCall kết thúc cuộc gọi, báo cả hai phía và ghi nhật ký vào tin nhắn riêng.
func (a *API) endCall(id, reason string) {
	reg := &a.calls
	reg.mu.Lock()
	c := reg.byID[id]
	if c == nil {
		reg.mu.Unlock()
		return
	}
	delete(reg.byID, id)
	c.timer.Stop()
	reg.mu.Unlock()

	ev := mustJSON(callEvent{Type: "call.ended", CallID: id, Reason: reason})
	a.hub.ToClient(c.caller, ev)
	if c.callee != nil {
		a.hub.ToClient(c.callee, ev)
	} else {
		a.hub.ToUser(c.calleeID, ev) // dừng đổ chuông ở mọi thiết bị
	}

	switch {
	case !c.answered.IsZero():
		a.logCall(c.channelID, c.from, c.video, "completed", int(time.Since(c.answered).Seconds()))
	case reason == "declined":
		a.logCall(c.channelID, c.from, c.video, "declined", 0)
	default:
		a.logCall(c.channelID, c.from, c.video, "missed", 0)
	}
}

// clientGone: kết nối websocket đóng -> kết thúc cuộc gọi đang dùng kết nối đó.
func (reg *callRegistry) clientGone(a *API, client *socket.Client) {
	reg.mu.Lock()
	var ids []string
	for id, c := range reg.byID {
		if c.caller == client || c.callee == client {
			ids = append(ids, id)
		}
	}
	reg.mu.Unlock()
	for _, id := range ids {
		a.endCall(id, "disconnected")
	}
}

func (a *API) logCall(channelID int64, from model.User, video bool, outcome string, seconds int) {
	_, err := a.createAndBroadcast(context.Background(), from, channelID, outgoing{
		Embed: &model.Embed{Type: "call", ID: outcome, Video: video, Duration: seconds},
	})
	if err != nil {
		log.Printf("call log: %v", err)
	}
}
