package controller

import (
	"context"
	"encoding/json"
	"sort"
	"sync"

	"chat/model"
	"chat/socket"
)

// Thoại trong ván (Ma Sói): kênh thoại riêng của từng ván, chỉ người chơi vào được, tách hẳn
// khỏi cuộc gọi nhóm của kênh. Kiểu mesh chỉ có tiếng như gọi nhóm: người mới vào gửi offer cho
// mọi người đang có mặt, máy chủ chỉ chuyển tín hiệu giữa người chơi trong cùng ván.
// Ai nghe được ai do luật của trò quyết định (game.View(...).Voice, gửi kèm game.updated);
// máy người nói chỉ gửi tiếng tới những người được nghe, máy người nghe tắt tiếng người còn lại.
//
// Client gửi:  gamevoice.join {message_id} · gamevoice.leave {message_id}
//              gamevoice.signal {message_id, to, data}
// Máy chủ gửi: gamevoice.welcome {message_id, self, participants} (cho người vừa vào)
//              gamevoice.state {message_id, participants} (cho mọi người chơi của ván)
//              gamevoice.signal {message_id, from, from_user, data} · gamevoice.error {message_id, message}

type voicePeer struct {
	client *socket.Client
	user   model.PublicUser
}

type voiceRoom struct {
	messageID int64
	players   []int64               // người chơi của ván (để báo trạng thái)
	peers     map[string]*voicePeer // theo socket.Client.ID
}

type voiceRegistry struct {
	mu    sync.Mutex
	rooms map[int64]*voiceRoom
}

type voiceParticipant struct {
	PeerID string           `json:"peer_id"`
	User   model.PublicUser `json:"user"`
}

type voiceEvent struct {
	Type         string             `json:"type"`
	MessageID    int64              `json:"message_id"`
	Self         string             `json:"self,omitempty"`
	From         string             `json:"from,omitempty"`
	FromUser     *model.PublicUser  `json:"from_user,omitempty"`
	Participants []voiceParticipant `json:"participants"`
	Data         json.RawMessage    `json:"data,omitempty"`
	Message      string             `json:"message,omitempty"`
}

func (r *voiceRoom) participants() []voiceParticipant {
	out := []voiceParticipant{}
	for id, p := range r.peers {
		out = append(out, voiceParticipant{PeerID: id, User: p.user})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PeerID < out[j].PeerID })
	return out
}

// voicePeerOf: kết nối của userID trong phòng (nếu có).
func (r *voiceRoom) peerOf(userID int64) (string, bool) {
	for id, p := range r.peers {
		if p.user.ID == userID {
			return id, true
		}
	}
	return "", false
}

// stateTo: gửi danh sách người đang trong thoại cho mọi người chơi của ván.
func (a *API) voiceStateTo(messageID int64, players []int64, ps []voiceParticipant) {
	ev := mustJSON(voiceEvent{Type: "gamevoice.state", MessageID: messageID, Participants: ps})
	for _, id := range players {
		a.hub.ToUser(id, ev)
	}
}

func (a *API) handleGameVoice(ctx context.Context, client *socket.Client, user model.User, req clientRequest) {
	v := &a.voices
	fail := func(msg string) {
		a.hub.ToClient(client, mustJSON(voiceEvent{Type: "gamevoice.error", MessageID: req.MessageID, Message: msg}))
	}

	switch req.Type {
	case "gamevoice.join":
		s, err := a.gameSession(ctx, req.MessageID)
		if err != nil {
			fail("Không tìm thấy ván này.")
			return
		}
		s.mu.Lock()
		ok := !s.deleted && s.g.HasVoice() && s.g.IsPlayer(user.ID)
		var players []int64
		for _, p := range s.g.Players() {
			players = append(players, p.ID)
		}
		channelID := s.channelID
		s.mu.Unlock()
		if !ok || !a.hub.IsSubscribed(client, channelID) {
			fail("Chỉ người chơi của ván mới vào thoại được.")
			return
		}
		v.mu.Lock()
		if v.rooms == nil {
			v.rooms = map[int64]*voiceRoom{}
		}
		room := v.rooms[req.MessageID]
		if room == nil {
			room = &voiceRoom{messageID: req.MessageID, peers: map[string]*voicePeer{}}
			v.rooms[req.MessageID] = room
		}
		room.players = players
		if id, in := room.peerOf(user.ID); in && id != client.ID {
			v.mu.Unlock()
			fail("Bạn đang trong thoại của ván này trên thiết bị khác.")
			return
		}
		others := room.participants()
		room.peers[client.ID] = &voicePeer{client: client, user: user.Public()}
		ps := room.participants()
		v.mu.Unlock()

		a.hub.ToClient(client, mustJSON(voiceEvent{
			Type: "gamevoice.welcome", MessageID: req.MessageID, Self: client.ID, Participants: others,
		}))
		a.voiceStateTo(req.MessageID, players, ps)

	case "gamevoice.leave":
		a.leaveGameVoice(req.MessageID, client)

	case "gamevoice.signal":
		if len(req.Data) == 0 || len(req.Data) > 32<<10 {
			return
		}
		v.mu.Lock()
		var to *socket.Client
		var from *voicePeer
		if room := v.rooms[req.MessageID]; room != nil {
			from = room.peers[client.ID]
			if p := room.peers[req.To]; p != nil && from != nil {
				to = p.client
			}
		}
		v.mu.Unlock()
		if to != nil {
			u := from.user
			a.hub.ToClient(to, mustJSON(voiceEvent{
				Type: "gamevoice.signal", MessageID: req.MessageID, From: client.ID, FromUser: &u, Data: req.Data,
			}))
		}
	}
}

// leaveGameVoice: client rời thoại của ván messageID (0 = mọi ván, khi mất kết nối).
func (a *API) leaveGameVoice(messageID int64, client *socket.Client) {
	v := &a.voices
	type changed struct {
		id      int64
		players []int64
		ps      []voiceParticipant
	}
	var out []changed
	v.mu.Lock()
	for id, room := range v.rooms {
		if messageID != 0 && id != messageID {
			continue
		}
		if _, ok := room.peers[client.ID]; !ok {
			continue
		}
		delete(room.peers, client.ID)
		if len(room.peers) == 0 {
			delete(v.rooms, id)
		}
		out = append(out, changed{id, room.players, room.participants()})
	}
	v.mu.Unlock()
	for _, c := range out {
		a.voiceStateTo(c.id, c.players, c.ps)
	}
}

// syncGameVoice: sau mỗi thay đổi của ván, mời ra khỏi thoại người không còn là người chơi
// (rời phòng chờ) hoặc khi ván bị xoá. Gọi khi đang giữ s.mu.
func (a *API) syncGameVoice(s *gameSession) {
	v := &a.voices
	v.mu.Lock()
	room := v.rooms[s.messageID]
	if room == nil {
		v.mu.Unlock()
		return
	}
	var players []int64
	if !s.deleted {
		for _, p := range s.g.Players() {
			players = append(players, p.ID)
		}
	}
	var kicked []*socket.Client
	for id, p := range room.peers {
		if s.deleted || !s.g.IsPlayer(p.user.ID) {
			kicked = append(kicked, p.client)
			delete(room.peers, id)
		}
	}
	room.players = players
	if len(room.peers) == 0 {
		delete(v.rooms, s.messageID)
	}
	ps := room.participants()
	v.mu.Unlock()
	if len(kicked) == 0 {
		return
	}
	for _, c := range kicked {
		a.hub.ToClient(c, mustJSON(voiceEvent{Type: "gamevoice.error", MessageID: s.messageID, Message: "Bạn không còn trong ván này."}))
	}
	a.voiceStateTo(s.messageID, players, ps)
}

// dropGameVoice: tin chứa ván bị xoá, mời mọi người ra khỏi thoại.
func (a *API) dropGameVoice(messageID int64) {
	v := &a.voices
	v.mu.Lock()
	room := v.rooms[messageID]
	delete(v.rooms, messageID)
	v.mu.Unlock()
	if room == nil {
		return
	}
	for _, p := range room.peers {
		a.hub.ToClient(p.client, mustJSON(voiceEvent{Type: "gamevoice.error", MessageID: messageID, Message: "Ván đã bị xoá."}))
	}
}
