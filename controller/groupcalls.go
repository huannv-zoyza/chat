package controller

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"sync"
	"time"

	"chat/model"
	"chat/socket"
)

// Gọi nhóm trong kênh (không phải tin nhắn riêng), tối đa maxGroupPeers người.
// Kiểu "mesh": mỗi máy nối WebRTC thẳng với từng máy còn lại; máy chủ chỉ chuyển tín hiệu.
// Người mới vào gửi offer cho tất cả người đang có mặt (nên không bao giờ hai bên cùng gửi offer).
//
// Client gửi:  groupcall.join {channel_id, video} · groupcall.leave {channel_id}
//              groupcall.signal {channel_id, to, data}
// Máy chủ gửi: groupcall.state {channel_id, started_at, participants} (cho cả kênh, kể cả khi kết thúc)
//              groupcall.started {channel_id, by, video} (mời người trong kênh tham gia)
//              groupcall.welcome {channel_id, self, participants} (cho người vừa vào)
//              groupcall.signal {channel_id, from, data} · groupcall.error {channel_id, message}

const maxGroupPeers = 8

type groupPeer struct {
	client *socket.Client
	user   model.PublicUser
	video  bool
}

type groupRoom struct {
	channelID int64
	startedAt time.Time
	startedBy model.User
	video     bool
	peers     map[string]*groupPeer // theo socket.Client.ID
	maxSeen   int                   // số người đông nhất (ghi vào nhật ký)
}

type groupRegistry struct {
	mu    sync.Mutex
	rooms map[int64]*groupRoom
}

type groupParticipant struct {
	PeerID string           `json:"peer_id"`
	User   model.PublicUser `json:"user"`
	Video  bool             `json:"video"`
}

type groupCallState struct {
	ChannelID    int64              `json:"channel_id"`
	StartedAt    *time.Time         `json:"started_at,omitempty"`
	Participants []groupParticipant `json:"participants"`
}

func (r *groupRoom) state() groupCallState {
	s := groupCallState{ChannelID: r.channelID, StartedAt: &r.startedAt, Participants: []groupParticipant{}}
	for id, p := range r.peers {
		s.Participants = append(s.Participants, groupParticipant{PeerID: id, User: p.user, Video: p.video})
	}
	sort.Slice(s.Participants, func(i, j int) bool { return s.Participants[i].PeerID < s.Participants[j].PeerID })
	return s
}

// statesFor: cuộc gọi nhóm đang diễn ra trong các kênh channelIDs (gửi kèm sự kiện "ready").
func (g *groupRegistry) statesFor(channelIDs []int64) []groupCallState {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := []groupCallState{}
	for _, id := range channelIDs {
		if r := g.rooms[id]; r != nil {
			out = append(out, r.state())
		}
	}
	return out
}

// inCall: userID đang ở trong một cuộc gọi nhóm nào đó.
func (g *groupRegistry) inCall(userID int64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, r := range g.rooms {
		for _, p := range r.peers {
			if p.user.ID == userID {
				return true
			}
		}
	}
	return false
}

type groupEvent struct {
	Type         string             `json:"type"`
	ChannelID    int64              `json:"channel_id"`
	Self         string             `json:"self,omitempty"`
	From         string             `json:"from,omitempty"`
	FromUser     *model.PublicUser  `json:"from_user,omitempty"`
	By           *model.PublicUser  `json:"by,omitempty"`
	Video        bool               `json:"video,omitempty"`
	Participants []groupParticipant `json:"participants,omitempty"`
	Data         json.RawMessage    `json:"data,omitempty"`
	Message      string             `json:"message,omitempty"`
	// welcome cho người mở cuộc gọi: số người đang online trong kênh đã được mời.
	Invited *int `json:"invited,omitempty"`
}

type groupStateEvent struct {
	Type string `json:"type"`
	groupCallState
}

func (a *API) handleGroupCall(ctx context.Context, client *socket.Client, user model.User, req clientRequest) {
	g := &a.groups
	fail := func(msg string) {
		a.hub.ToClient(client, mustJSON(groupEvent{Type: "groupcall.error", ChannelID: req.ChannelID, Message: msg}))
	}

	switch req.Type {
	case "groupcall.join":
		ch, err := a.store.GetChannel(ctx, req.ChannelID)
		if err != nil || ch.IsDM() || !a.hub.IsSubscribed(client, ch.ID) {
			fail("Không tham gia được cuộc gọi trong kênh này.")
			return
		}
		a.calls.mu.Lock()
		busy1 := a.calls.busy(user.ID)
		a.calls.mu.Unlock()
		if busy1 {
			fail("Bạn đang trong một cuộc gọi khác.")
			return
		}
		g.mu.Lock()
		if g.rooms == nil {
			g.rooms = map[int64]*groupRoom{}
		}
		for _, r := range g.rooms {
			for id, p := range r.peers {
				if p.user.ID == user.ID && (r.channelID != ch.ID || id != client.ID) {
					g.mu.Unlock()
					fail("Bạn đang ở trong một cuộc gọi khác (có thể trên thiết bị khác).")
					return
				}
			}
		}
		room := g.rooms[ch.ID]
		created := room == nil
		if created {
			room = &groupRoom{channelID: ch.ID, startedAt: time.Now(), startedBy: user, video: req.Video, peers: map[string]*groupPeer{}}
			g.rooms[ch.ID] = room
		}
		if _, already := room.peers[client.ID]; !already && len(room.peers) >= maxGroupPeers {
			g.mu.Unlock()
			fail("Cuộc gọi đã đủ 8 người.")
			return
		}
		others := room.state().Participants
		room.peers[client.ID] = &groupPeer{client: client, user: user.Public(), video: req.Video}
		if n := len(room.peers); n > room.maxSeen {
			room.maxSeen = n
		}
		st := room.state()
		g.mu.Unlock()

		var invited *int
		if created {
			n := a.hub.ChannelOnlineUsers(ch.ID, user.ID)
			invited = &n
			log.Printf("groupcall: %s mở cuộc gọi ở kênh %d, mời %d người đang online", user.Username, ch.ID, n)
		} else {
			log.Printf("groupcall: %s vào kênh %d (%d người)", user.Username, ch.ID, len(st.Participants))
		}
		a.hub.ToClient(client, mustJSON(groupEvent{
			Type: "groupcall.welcome", ChannelID: ch.ID, Self: client.ID, Participants: others, Invited: invited,
		}))
		a.hub.ToChannel(ch.ID, mustJSON(groupStateEvent{Type: "groupcall.state", groupCallState: st}), nil)
		if created {
			by := user.Public()
			a.hub.ToChannel(ch.ID, mustJSON(groupEvent{
				Type: "groupcall.started", ChannelID: ch.ID, By: &by, Video: req.Video,
			}), client)
		}

	case "groupcall.leave":
		a.leaveGroup(req.ChannelID, client)

	case "groupcall.signal":
		if len(req.Data) == 0 || len(req.Data) > 32<<10 {
			return
		}
		g.mu.Lock()
		room := g.rooms[req.ChannelID]
		var to *socket.Client
		var from *groupPeer
		if room != nil {
			from = room.peers[client.ID]
			if p := room.peers[req.To]; p != nil && from != nil {
				to = p.client
			}
		}
		g.mu.Unlock()
		if to != nil {
			u := from.user
			a.hub.ToClient(to, mustJSON(groupEvent{
				Type: "groupcall.signal", ChannelID: req.ChannelID, From: client.ID, FromUser: &u, Data: req.Data,
			}))
		}
	}
}

// dropFromGroupCall đưa mọi kết nối của userID ra khỏi cuộc gọi nhóm của kênh (khi bị mời ra khỏi kênh).
func (a *API) dropFromGroupCall(channelID, userID int64) {
	a.groups.mu.Lock()
	var clients []*socket.Client
	if room := a.groups.rooms[channelID]; room != nil {
		for _, p := range room.peers {
			if p.user.ID == userID {
				clients = append(clients, p.client)
			}
		}
	}
	a.groups.mu.Unlock()
	for _, c := range clients {
		a.leaveGroup(channelID, c)
	}
}

// leaveGroup: client rời cuộc gọi nhóm ở channelID (0 = mọi cuộc gọi của client đó).
func (a *API) leaveGroup(channelID int64, client *socket.Client) {
	g := &a.groups
	g.mu.Lock()
	type ended struct {
		room  *groupRoom
		state groupCallState
		empty bool
	}
	var changed []ended
	for id, room := range g.rooms {
		if channelID != 0 && id != channelID {
			continue
		}
		if _, ok := room.peers[client.ID]; !ok {
			continue
		}
		delete(room.peers, client.ID)
		e := ended{room: room, state: room.state(), empty: len(room.peers) == 0}
		if e.empty {
			delete(g.rooms, id)
			e.state.StartedAt = nil
		}
		changed = append(changed, e)
	}
	g.mu.Unlock()

	for _, e := range changed {
		a.hub.ToChannel(e.room.channelID, mustJSON(groupStateEvent{Type: "groupcall.state", groupCallState: e.state}), nil)
		if e.empty {
			secs := int(time.Since(e.room.startedAt).Seconds())
			log.Printf("groupcall: kênh %d kết thúc sau %ds, đông nhất %d người", e.room.channelID, secs, e.room.maxSeen)
			_, err := a.createAndBroadcast(context.Background(), e.room.startedBy, e.room.channelID, outgoing{
				Embed: &model.Embed{Type: "call", ID: "group", Video: e.room.video, Duration: secs, Participants: e.room.maxSeen},
			})
			if err != nil {
				log.Printf("groupcall log: %v", err)
			}
		}
	}
}
