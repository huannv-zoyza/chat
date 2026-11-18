package controller

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"chat/game"
	"chat/model"
	"chat/socket"
	"chat/store"
)

// Trò chơi trong kênh. Mỗi ván là một tin nhắn (embed type "game"); máy chủ làm quản trò:
// giữ ván trong bộ nhớ, lưu xuống bảng games sau mỗi thay đổi, tự chuyển pha khi hết giờ.
//
//	POST /api/channels/{id}/games  {kind, options?, client_id}  tạo phòng (mình là chủ phòng)
//	POST /api/messages/{id}/game   {action, data?}              join | leave | start | cancel | hành động của trò
//
// Sự kiện game.updated {channel_id, message_id, game}: phát cho cả kênh phần công khai
// (không có "me"), rồi gửi riêng từng người chơi bản có "me" (vai, từ khoá... của người đó).

type gameSession struct {
	mu        sync.Mutex
	messageID int64
	channelID int64
	g         *game.Game
	deleted   bool
	// Trò thời gian thực: vòng lặp đang chạy, và các kết nối đang mở màn hình chơi (nhận frame).
	running  bool
	watchers map[*socket.Client]bool
}

type gameRegistry struct {
	mu       sync.Mutex
	sessions map[int64]*gameSession
}

type gameUpdatedEvent struct {
	Type      string          `json:"type"`
	ChannelID int64           `json:"channel_id"`
	MessageID int64           `json:"message_id"`
	Game      json.RawMessage `json:"game"`
}

func (r *gameRegistry) put(s *gameSession) *gameSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessions == nil {
		r.sessions = map[int64]*gameSession{}
	}
	if cur := r.sessions[s.messageID]; cur != nil {
		return cur
	}
	r.sessions[s.messageID] = s
	return s
}

// drop: tin chứa ván bị xoá.
func (r *gameRegistry) drop(messageID int64) {
	r.mu.Lock()
	s := r.sessions[messageID]
	delete(r.sessions, messageID)
	r.mu.Unlock()
	if s != nil {
		s.mu.Lock()
		s.deleted = true
		s.mu.Unlock()
	}
}

// session: ván của tin messageID (nạp từ DB nếu chưa có trong bộ nhớ).
func (a *API) gameSession(ctx context.Context, messageID int64) (*gameSession, error) {
	a.games.mu.Lock()
	s := a.games.sessions[messageID]
	a.games.mu.Unlock()
	if s != nil {
		return s, nil
	}
	row, err := a.store.GetGame(ctx, messageID)
	if err != nil {
		return nil, err
	}
	g, err := game.Load(row.Kind, row.State)
	if err != nil {
		return nil, err
	}
	return a.games.put(&gameSession{messageID: messageID, channelID: row.ChannelID, g: g}), nil
}

func viewJSON(g *game.Game, viewer int64) json.RawMessage {
	b, err := json.Marshal(g.View(viewer))
	if err != nil {
		log.Printf("game view: %v", err)
		return nil
	}
	return b
}

// saveAndBroadcast: gọi khi đang giữ s.mu.
func (a *API) saveAndBroadcast(ctx context.Context, s *gameSession) {
	state, err := s.g.Marshal()
	if err == nil {
		err = a.store.SaveGame(ctx, s.messageID, string(s.g.Status()), state)
	}
	if err != nil {
		log.Printf("save game %d: %v", s.messageID, err)
	}
	a.hub.ToChannel(s.channelID, mustJSON(gameUpdatedEvent{
		Type: "game.updated", ChannelID: s.channelID, MessageID: s.messageID, Game: viewJSON(s.g, 0),
	}), nil)
	for _, p := range s.g.Players() {
		a.hub.ToUser(p.ID, mustJSON(gameUpdatedEvent{
			Type: "game.updated", ChannelID: s.channelID, MessageID: s.messageID, Game: viewJSON(s.g, p.ID),
		}))
	}
}

func asPlayer(u model.User) game.Player {
	name := u.DisplayName
	if name == "" {
		name = u.Username
	}
	return game.Player{ID: u.ID, Name: name}
}

/* ---------- POST /api/channels/{id}/games ---------- */

type createGameRequest struct {
	Kind     string          `json:"kind"`
	Options  json.RawMessage `json:"options"`
	ClientID string          `json:"client_id"`
}

func (a *API) createGame(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireMember(w, r, ch.ID) {
		return
	}
	var req createGameRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	title := game.Title(req.Kind)
	if title == "" {
		writeError(w, http.StatusBadRequest, "Không có trò chơi này.")
		return
	}
	if ch.IsDM() && req.Kind != "caro" && req.Kind != "rps" {
		writeError(w, http.StatusBadRequest, "Trò này cần nhiều người, hãy mở trong kênh.")
		return
	}
	me := currentUser(r)
	now := time.Now()
	g, err := game.New(req.Kind, asPlayer(me), req.Options, now)
	if err != nil {
		writeGameError(w, r, err)
		return
	}
	state, err := g.Marshal()
	if err != nil {
		internalError(w, r, err)
		return
	}
	m, err := a.store.CreateGame(r.Context(), ch.ID, me.ID, model.Embed{Type: "game", ID: req.Kind, Title: title}, string(g.Status()), state)
	if err != nil {
		internalError(w, r, err)
		return
	}
	s := a.games.put(&gameSession{messageID: m.ID, channelID: ch.ID, g: g})
	_, _, _ = a.store.MarkRead(r.Context(), ch.ID, me.ID, m.ID)
	if len(req.ClientID) > 64 {
		req.ClientID = req.ClientID[:64]
	}
	s.mu.Lock()
	m.Game = viewJSON(g, 0)
	a.hub.ToChannel(ch.ID, mustJSON(messageNewEvent{Type: "message.new", Message: m, ClientID: req.ClientID}), nil)
	m.Game = viewJSON(g, me.ID)
	s.mu.Unlock()
	log.Printf("game: %s mở %s ở kênh %d", me.Username, req.Kind, ch.ID)
	writeJSON(w, http.StatusCreated, m)
}

/* ---------- POST /api/messages/{id}/game ---------- */

type gameActionRequest struct {
	Action string          `json:"action"`
	Data   json.RawMessage `json:"data"`
}

func (a *API) gameAction(w http.ResponseWriter, r *http.Request) {
	msg, ok := a.loadMessage(w, r)
	if !ok {
		return
	}
	var req gameActionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if msg.DeletedAt != nil || msg.Embed == nil || msg.Embed.Type != "game" {
		writeError(w, http.StatusNotFound, "Không tìm thấy ván chơi.")
		return
	}
	s, err := a.gameSession(r.Context(), msg.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Không tìm thấy ván chơi.")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	me := currentUser(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleted {
		writeError(w, http.StatusNotFound, "Không tìm thấy ván chơi.")
		return
	}
	wasPlaying := s.g.Status() == game.Playing
	if err := s.g.Act(asPlayer(me), req.Action, req.Data, time.Now()); err != nil {
		writeGameError(w, r, err)
		return
	}
	if s.g.Realtime() && wasPlaying && s.g.Status() == game.Playing {
		// trò thời gian thực: vòng lặp gửi frame ngay lượt sau, thay đổi lớn thì nó lưu và cập nhật thẻ
		a.sendFrames(s, time.Now())
	} else {
		a.saveAndBroadcast(r.Context(), s)
	}
	a.ensureLoop(s)
	writeJSON(w, http.StatusOK, json.RawMessage(viewJSON(s.g, me.ID)))
}

func writeGameError(w http.ResponseWriter, r *http.Request, err error) {
	var ge game.Error
	if errors.As(err, &ge) {
		writeError(w, http.StatusBadRequest, ge.Error())
		return
	}
	internalError(w, r, err)
}

// fillGames điền ván chơi (theo góc nhìn của userID) vào các tin chứa ván.
func (a *API) fillGames(ctx context.Context, userID int64, msgs []model.Message) {
	for i := range msgs {
		m := &msgs[i]
		if m.DeletedAt != nil || m.Embed == nil || m.Embed.Type != "game" {
			continue
		}
		s, err := a.gameSession(ctx, m.ID)
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				log.Printf("load game %d: %v", m.ID, err)
			}
			continue
		}
		s.mu.Lock()
		m.Game = viewJSON(s.g, userID)
		s.mu.Unlock()
	}
}

// fillForViewer: phần riêng của người xem trong các tin (lựa chọn bình chọn, ván chơi).
func (a *API) fillForViewer(ctx context.Context, userID int64, msgs []model.Message) error {
	if err := a.fillMyVotes(ctx, userID, msgs); err != nil {
		return err
	}
	a.fillGames(ctx, userID, msgs)
	return nil
}

// RunGames nạp lại các ván đang dở và mỗi giây xử lý những ván hết giờ (chuyển pha, xử thua...).
func (a *API) RunGames(ctx context.Context) {
	rows, err := a.store.ActiveGames(ctx)
	if err != nil {
		log.Printf("load games: %v", err)
	}
	for _, row := range rows {
		g, err := game.Load(row.Kind, row.State)
		if err != nil {
			log.Printf("load game %d: %v", row.MessageID, err)
			continue
		}
		a.ensureLoopLocked(a.games.put(&gameSession{messageID: row.MessageID, channelID: row.ChannelID, g: g}))
	}
	if len(rows) > 0 {
		log.Printf("game: chạy tiếp %d ván đang dở", len(rows))
	}
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			a.tickGames(ctx, now)
		}
	}
}

func (a *API) tickGames(ctx context.Context, now time.Time) {
	a.games.mu.Lock()
	list := make([]*gameSession, 0, len(a.games.sessions))
	for _, s := range a.games.sessions {
		list = append(list, s)
	}
	a.games.mu.Unlock()
	for _, s := range list {
		s.mu.Lock()
		if !s.deleted && s.g.Tick(now) {
			a.saveAndBroadcast(ctx, s)
		}
		ended := s.g.Status() == game.Ended
		s.mu.Unlock()
		if ended {
			// ván xong không cần đồng hồ nữa; lần sau có người xem thì nạp lại từ DB
			a.games.mu.Lock()
			if a.games.sessions[s.messageID] == s {
				delete(a.games.sessions, s.messageID)
			}
			a.games.mu.Unlock()
		}
	}
}

/* ---------- Trò thời gian thực (Trạm Vũ Trụ) ---------- */

// Client gửi qua websocket:
//
//	game.watch   {message_id}               mở màn hình chơi: nhận game.map rồi game.frame ~15 lần/giây
//	game.unwatch {message_id}               đóng màn hình chơi
//	game.pos     {message_id, data:{x, y}}  vị trí mới của mình (máy chủ kiểm tra tốc độ, tường)
//
// Hành động (làm nhiệm vụ, giết, báo xác, bỏ phiếu...) vẫn đi qua POST /api/messages/{id}/game.

const frameEvery = time.Second / 15

type gameMapEvent struct {
	Type      string `json:"type"`
	MessageID int64  `json:"message_id"`
	Map       any    `json:"map"`
}

type gameFrameEvent struct {
	Type      string `json:"type"`
	MessageID int64  `json:"message_id"`
	Frame     any    `json:"frame"`
}

// ensureLoop chạy vòng lặp của ván thời gian thực nếu đang chơi mà chưa chạy.
func (a *API) ensureLoop(s *gameSession) {
	if !s.g.Realtime() || s.g.Status() != game.Playing || s.running || s.deleted {
		return
	}
	s.running = true
	go a.runLoop(s)
}

// ensureLoopLocked: như ensureLoop nhưng tự khoá s.mu (ensureLoop đòi người gọi giữ sẵn).
func (a *API) ensureLoopLocked(s *gameSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a.ensureLoop(s)
}

func (a *API) runLoop(s *gameSession) {
	t := time.NewTicker(frameEvery)
	defer t.Stop()
	for now := range t.C {
		s.mu.Lock()
		if s.deleted || s.g.Status() != game.Playing {
			a.sendFrames(s, now) // màn kết thúc
			s.running = false
			s.mu.Unlock()
			return
		}
		if s.g.Step(now) {
			a.saveAndBroadcast(context.Background(), s)
		}
		a.sendFrames(s, now)
		s.mu.Unlock()
	}
}

// sendFrames: gọi khi giữ s.mu.
func (a *API) sendFrames(s *gameSession, now time.Time) {
	for c := range s.watchers {
		if !a.hub.Connected(c) {
			delete(s.watchers, c)
			continue
		}
		if f := s.g.Frame(c.UserID, now); f != nil {
			a.hub.ToClient(c, mustJSON(gameFrameEvent{Type: "game.frame", MessageID: s.messageID, Frame: f}))
		}
	}
}

func (a *API) handleGameSocket(ctx context.Context, client *socket.Client, user model.User, req clientRequest) {
	s, err := a.gameSession(ctx, req.MessageID)
	if err != nil {
		return
	}
	if req.Type != "game.unwatch" && !a.hub.IsSubscribed(client, s.channelID) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleted || !s.g.Realtime() || !s.g.IsPlayer(user.ID) {
		return
	}
	switch req.Type {
	case "game.watch":
		if s.watchers == nil {
			s.watchers = map[*socket.Client]bool{}
		}
		s.watchers[client] = true
		a.hub.ToClient(client, mustJSON(gameMapEvent{Type: "game.map", MessageID: s.messageID, Map: s.g.MapData()}))
		if f := s.g.Frame(user.ID, time.Now()); f != nil {
			a.hub.ToClient(client, mustJSON(gameFrameEvent{Type: "game.frame", MessageID: s.messageID, Frame: f}))
		}
		a.ensureLoop(s)
	case "game.unwatch":
		delete(s.watchers, client)
	case "game.pos":
		var d struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
		}
		if json.Unmarshal(req.Data, &d) == nil {
			s.g.Move(user.ID, d.X, d.Y, time.Now())
		}
	}
}
