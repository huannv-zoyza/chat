package controller

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"chat/model"
	"chat/socket"
	"chat/store"
)

type wpRoom struct {
	listeners          map[*socket.Client]model.PublicUser
	pausedBecauseEmpty bool
	pausedManually     bool
}

type watchPartyRegistry struct {
	mu    sync.Mutex
	rooms map[int64]*wpRoom
}

func (r *watchPartyRegistry) roomLocked(channelID int64) *wpRoom {
	if r.rooms == nil {
		r.rooms = map[int64]*wpRoom{}
	}
	rm := r.rooms[channelID]
	if rm == nil {
		rm = &wpRoom{listeners: map[*socket.Client]model.PublicUser{}}
		r.rooms[channelID] = rm
	}
	return rm
}

func (r *watchPartyRegistry) listenersOf(channelID int64) ([]model.PublicUser, map[int64]bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rm := r.rooms[channelID]
	if rm == nil || len(rm.listeners) == 0 {
		return []model.PublicUser{}, map[int64]bool{}
	}
	byID := map[int64]model.PublicUser{}
	for _, u := range rm.listeners {
		byID[u.ID] = u
	}
	out := make([]model.PublicUser, 0, len(byID))
	ids := make(map[int64]bool, len(byID))
	for id, u := range byID {
		out = append(out, u)
		ids[id] = true
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, ids
}

func (r *watchPartyRegistry) isUserJoined(channelID, userID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	rm := r.rooms[channelID]
	if rm == nil {
		return false
	}
	for _, u := range rm.listeners {
		if u.ID == userID {
			return true
		}
	}
	return false
}

func (r *watchPartyRegistry) isClientJoined(channelID int64, c *socket.Client) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	rm := r.rooms[channelID]
	if rm == nil {
		return false
	}
	_, ok := rm.listeners[c]
	return ok
}

type watchPartyStateEvent struct {
	Type  string                `json:"type"`
	State model.WatchPartyState `json:"state"`
}

type watchPartyErrorEvent struct {
	Type      string `json:"type"`
	ChannelID int64  `json:"channel_id"`
	Message   string `json:"message"`
}

func (a *API) decorateWatchPartyState(st model.WatchPartyState, viewerID int64) (model.WatchPartyState, map[int64]bool) {
	listeners, joinedIDs := a.watchParties.listenersOf(st.ChannelID)
	st.Listeners = listeners
	st.ListenerCount = len(listeners)
	st.Joined = viewerID != 0 && joinedIDs[viewerID]
	return st, joinedIDs
}

func (a *API) broadcastWatchPartyState(channelID int64, st model.WatchPartyState) {
	pub, joinedIDs := a.decorateWatchPartyState(st, 0)
	a.hub.ToChannel(channelID, mustJSON(watchPartyStateEvent{Type: "watchparty.state", State: pub}), nil)
	for uid := range joinedIDs {
		priv := pub
		priv.Joined = true
		a.hub.ToUser(uid, mustJSON(watchPartyStateEvent{Type: "watchparty.state", State: priv}))
	}
}

func (a *API) activeWatchPartiesFor(ctx context.Context, channelIDs []int64, userID int64) []model.WatchPartyState {
	nowMs := time.Now().UnixMilli()
	var out []model.WatchPartyState
	for _, cid := range channelIDs {
		st, err := a.store.GetWatchParty(ctx, cid, nowMs)
		if err != nil {
			continue
		}
		dec, _ := a.decorateWatchPartyState(st, userID)
		if dec.CurrentItem != nil || len(dec.Queue) > 0 || dec.ListenerCount > 0 {
			out = append(out, dec)
		}
	}
	return out
}

type resolvedYouTube struct {
	VideoID string
	URL     string
	Title   string
	Author  string
	Thumb   string
}

func (a *API) resolveYouTube(ctx context.Context, rawURL string) (resolvedYouTube, error) {
	rawURL = strings.TrimSpace(rawURL)
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return resolvedYouTube{}, errors.New("Link YouTube không hợp lệ.")
	}
	id := youtubeID(u)
	if id == "" {
		return resolvedYouTube{}, errors.New("Chỉ hỗ trợ link video hoặc nhạc YouTube.")
	}
	canonical := "https://www.youtube.com/watch?v=" + id
	res := resolvedYouTube{
		VideoID: id,
		URL:     canonical,
		Title:   "YouTube Video (" + id + ")",
		Thumb:   "https://i.ytimg.com/vi/" + id + "/hqdefault.jpg",
	}

	if cached, ok := a.unfurls.get(rawURL); ok && cached.Kind == "youtube" && cached.Title != "" {
		res.Title = cached.Title
		res.Author = cached.Description
		if cached.Image != "" {
			res.Thumb = cached.Image
		}
		return res, nil
	}

	if a.unfurlClient != nil {
		oCtx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
		defer cancel()
		var oe struct {
			Title      string `json:"title"`
			AuthorName string `json:"author_name"`
		}
		q := "https://www.youtube.com/oembed?format=json&url=" + url.QueryEscape(canonical)
		if err := a.getJSON(oCtx, q, &oe); err == nil && strings.TrimSpace(oe.Title) != "" {
			res.Title = truncateRunes(strings.TrimSpace(oe.Title), 140)
			res.Author = truncateRunes(strings.TrimSpace(oe.AuthorName), 80)
		}
	}
	return res, nil
}

/* ---------- REST Endpoints ---------- */

func (a *API) getWatchParty(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireMember(w, r, ch.ID) {
		return
	}
	if ch.IsDM() {
		writeError(w, http.StatusBadRequest, "Phòng nghe chung chỉ hỗ trợ trong kênh.")
		return
	}
	st, err := a.store.GetWatchParty(r.Context(), ch.ID, time.Now().UnixMilli())
	if err != nil {
		internalError(w, r, err)
		return
	}
	dec, _ := a.decorateWatchPartyState(st, currentUser(r).ID)
	writeJSON(w, http.StatusOK, dec)
}

type addWatchPartyItemReq struct {
	URL            string `json:"url"`
	Title          string `json:"title"`
	AnnounceInChat bool   `json:"announce_in_chat"`
}

func (a *API) addWatchPartyItem(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireMember(w, r, ch.ID) {
		return
	}
	if ch.IsDM() {
		writeError(w, http.StatusBadRequest, "Phòng nghe chung chỉ hỗ trợ trong kênh.")
		return
	}
	var req addWatchPartyItemReq
	if !decodeJSON(w, r, &req) {
		return
	}
	yt, err := a.resolveYouTube(r.Context(), req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = yt.Title
	}
	if utf8.RuneCountInString(title) > 140 {
		title = truncateRunes(title, 140)
	}

	me := currentUser(r)
	_, joinedIDs := a.watchParties.listenersOf(ch.ID)
	hasListeners := len(joinedIDs) > 0
	item, st, err := a.store.AddWatchPartyItem(r.Context(), ch.ID, me.ID, yt.VideoID, yt.URL, title, yt.Author, yt.Thumb, 0, hasListeners, time.Now().UnixMilli())
	if err != nil {
		internalError(w, r, err)
		return
	}
	a.broadcastWatchPartyState(ch.ID, st)

	if req.AnnounceInChat {
		_, _ = a.createAndBroadcast(r.Context(), me, ch.ID, outgoing{
			Content: "🎵 Đã thêm vào phòng nghe chung: " + item.Title + " — " + yt.URL,
		})
	}

	dec, _ := a.decorateWatchPartyState(st, me.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"item": item, "state": dec})
}

type updateWatchPartyItemReq struct {
	Title       *string `json:"title"`
	URL         *string `json:"url"`
	DurationSec *int    `json:"duration_sec"`
}

func (a *API) updateWatchPartyItem(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireMember(w, r, ch.ID) {
		return
	}
	itemID, err := strconv.ParseInt(r.PathValue("itemId"), 10, 64)
	if err != nil || itemID <= 0 {
		writeError(w, http.StatusBadRequest, "ID bài hát không hợp lệ.")
		return
	}
	var req updateWatchPartyItemReq
	if !decodeJSON(w, r, &req) {
		return
	}
	var titlePtr *string
	if req.Title != nil {
		t := truncateRunes(strings.TrimSpace(*req.Title), 140)
		if t == "" {
			writeError(w, http.StatusBadRequest, "Tiêu đề bài hát không được để trống.")
			return
		}
		titlePtr = &t
	}
	var newVid *struct{ VideoID, URL, Title, Author, Thumb string }
	if req.URL != nil && strings.TrimSpace(*req.URL) != "" {
		yt, err := a.resolveYouTube(r.Context(), *req.URL)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		newVid = &struct{ VideoID, URL, Title, Author, Thumb string }{
			VideoID: yt.VideoID, URL: yt.URL, Title: yt.Title, Author: yt.Author, Thumb: yt.Thumb,
		}
	}
	st, err := a.store.UpdateWatchPartyItem(r.Context(), ch.ID, itemID, titlePtr, newVid, req.DurationSec, time.Now().UnixMilli())
	if errors.Is(err, store.ErrWatchPartyItemNotFound) {
		writeError(w, http.StatusNotFound, "Không tìm thấy bài hát trong hàng đợi.")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	a.broadcastWatchPartyState(ch.ID, st)
	dec, _ := a.decorateWatchPartyState(st, currentUser(r).ID)
	writeJSON(w, http.StatusOK, dec)
}

func (a *API) deleteWatchPartyItem(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireMember(w, r, ch.ID) {
		return
	}
	itemID, err := strconv.ParseInt(r.PathValue("itemId"), 10, 64)
	if err != nil || itemID <= 0 {
		writeError(w, http.StatusBadRequest, "ID bài hát không hợp lệ.")
		return
	}
	_, joinedIDs := a.watchParties.listenersOf(ch.ID)
	st, err := a.store.DeleteWatchPartyItem(r.Context(), ch.ID, itemID, time.Now().UnixMilli(), len(joinedIDs) > 0)
	if errors.Is(err, store.ErrWatchPartyItemNotFound) {
		writeError(w, http.StatusNotFound, "Không tìm thấy bài hát trong hàng đợi.")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	a.broadcastWatchPartyState(ch.ID, st)
	dec, _ := a.decorateWatchPartyState(st, currentUser(r).ID)
	writeJSON(w, http.StatusOK, dec)
}

type reorderWatchPartyReq struct {
	ItemIDs []int64 `json:"item_ids"`
}

func (a *API) reorderWatchPartyQueue(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireMember(w, r, ch.ID) {
		return
	}
	var req reorderWatchPartyReq
	if !decodeJSON(w, r, &req) {
		return
	}
	st, err := a.store.ReorderWatchPartyQueue(r.Context(), ch.ID, req.ItemIDs, time.Now().UnixMilli())
	if err != nil {
		internalError(w, r, err)
		return
	}
	a.broadcastWatchPartyState(ch.ID, st)
	dec, _ := a.decorateWatchPartyState(st, currentUser(r).ID)
	writeJSON(w, http.StatusOK, dec)
}

func (a *API) clearWatchPartyQueue(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireMember(w, r, ch.ID) {
		return
	}
	includeCurrent := r.URL.Query().Get("include_current") == "true" || r.URL.Query().Get("include_current") == "1"
	st, err := a.store.ClearWatchPartyQueue(r.Context(), ch.ID, includeCurrent, time.Now().UnixMilli())
	if err != nil {
		internalError(w, r, err)
		return
	}
	a.broadcastWatchPartyState(ch.ID, st)
	dec, _ := a.decorateWatchPartyState(st, currentUser(r).ID)
	writeJSON(w, http.StatusOK, dec)
}

type controlWatchPartyReq struct {
	Action      string   `json:"action"`
	PositionSec *float64 `json:"position_sec"`
	LoopMode    string   `json:"loop_mode"`
}

func (a *API) controlWatchParty(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.loadChannel(w, r)
	if !ok || !a.requireMember(w, r, ch.ID) {
		return
	}
	me := currentUser(r)
	if !a.watchParties.isUserJoined(ch.ID, me.ID) {
		writeError(w, http.StatusBadRequest, "Bạn cần nhấn Join phòng nghe chung trước khi điều khiển phát nhạc.")
		return
	}
	var req controlWatchPartyReq
	if !decodeJSON(w, r, &req) {
		return
	}
	a.watchParties.mu.Lock()
	rm := a.watchParties.roomLocked(ch.ID)
	rm.pausedBecauseEmpty = false
	if req.Action == "pause" {
		rm.pausedManually = true
	} else if req.Action == "play" || req.Action == "next" || req.Action == "prev" {
		rm.pausedManually = false
	}
	a.watchParties.mu.Unlock()

	_, joinedIDs := a.watchParties.listenersOf(ch.ID)
	st, err := a.store.ControlWatchParty(r.Context(), ch.ID, req.Action, req.PositionSec, req.LoopMode, time.Now().UnixMilli(), len(joinedIDs) > 0)
	if errors.Is(err, store.ErrInvalidWatchPartyAct) {
		writeError(w, http.StatusBadRequest, "Lệnh điều khiển không hợp lệ.")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	a.broadcastWatchPartyState(ch.ID, st)
	dec, _ := a.decorateWatchPartyState(st, me.ID)
	writeJSON(w, http.StatusOK, dec)
}

/* ---------- WebSocket Handlers ---------- */

func (a *API) leaveWatchPartyClient(ctx context.Context, client *socket.Client, channelID int64) {
	var left bool
	var becameEmpty bool
	a.watchParties.mu.Lock()
	rm := a.watchParties.rooms[channelID]
	if rm != nil {
		if _, ok := rm.listeners[client]; ok {
			delete(rm.listeners, client)
			left = true
			becameEmpty = len(rm.listeners) == 0
		}
	}
	a.watchParties.mu.Unlock()

	if !left {
		return
	}
	nowMs := time.Now().UnixMilli()
	var st model.WatchPartyState
	var err error
	if becameEmpty {
		var paused bool
		paused, st, err = a.store.PauseWatchPartyIfPlaying(ctx, channelID, nowMs)
		if err == nil && paused {
			a.watchParties.mu.Lock()
			if rm := a.watchParties.rooms[channelID]; rm != nil && len(rm.listeners) == 0 {
				rm.pausedBecauseEmpty = true
			}
			a.watchParties.mu.Unlock()
		}
	} else {
		st, err = a.store.GetWatchParty(ctx, channelID, nowMs)
	}
	if err != nil {
		if !strings.Contains(err.Error(), "database is closed") {
			log.Printf("watchparty leave ch=%d: %v", channelID, err)
		}
		return
	}
	a.broadcastWatchPartyState(channelID, st)
}

func (a *API) watchPartyClientGone(client *socket.Client) {
	a.watchParties.mu.Lock()
	var channels []int64
	for cid, rm := range a.watchParties.rooms {
		if _, ok := rm.listeners[client]; ok {
			channels = append(channels, cid)
		}
	}
	a.watchParties.mu.Unlock()
	for _, cid := range channels {
		a.leaveWatchPartyClient(context.Background(), client, cid)
	}
}

func (a *API) handleWatchPartySocket(ctx context.Context, client *socket.Client, user model.User, req clientRequest) {
	fail := func(msg string) {
		a.hub.ToClient(client, mustJSON(watchPartyErrorEvent{
			Type: "watchparty.error", ChannelID: req.ChannelID, Message: msg,
		}))
	}
	if req.ChannelID <= 0 {
		fail("Thiếu channel_id.")
		return
	}
	member, err := a.store.IsMember(ctx, req.ChannelID, user.ID)
	if err != nil || !member {
		fail("Bạn không thuộc kênh này.")
		return
	}

	switch req.Type {
	case "watchparty.join":
		ch, err := a.store.GetChannel(ctx, req.ChannelID)
		if err != nil || ch.IsDM() {
			fail("Phòng nghe chung chỉ hỗ trợ trong kênh.")
			return
		}
		// Rời phòng nghe chung ở các kênh khác nếu client đang tham gia
		a.watchParties.mu.Lock()
		var otherChannels []int64
		for cid, rm := range a.watchParties.rooms {
			if cid != req.ChannelID {
				if _, ok := rm.listeners[client]; ok {
					otherChannels = append(otherChannels, cid)
				}
			}
		}
		a.watchParties.mu.Unlock()
		for _, ocid := range otherChannels {
			a.leaveWatchPartyClient(ctx, client, ocid)
		}

		var shouldResume bool
		var pausedManually bool
		a.watchParties.mu.Lock()
		rm := a.watchParties.roomLocked(req.ChannelID)
		rm.listeners[client] = user.Public()
		if rm.pausedBecauseEmpty {
			shouldResume = true
			rm.pausedBecauseEmpty = false
		}
		pausedManually = rm.pausedManually
		a.watchParties.mu.Unlock()

		nowMs := time.Now().UnixMilli()
		var st model.WatchPartyState
		if shouldResume {
			st, err = a.store.ControlWatchParty(ctx, req.ChannelID, "play", nil, "", nowMs, true)
		} else {
			st, err = a.store.GetWatchParty(ctx, req.ChannelID, nowMs)
			// Nếu phòng đang có bài nhưng đang dừng và không phải bị Pause thủ công -> tự phát khi có người Join
			if err == nil && st.CurrentItem != nil && !st.IsPlaying && !pausedManually {
				st, err = a.store.ControlWatchParty(ctx, req.ChannelID, "play", nil, "", nowMs, true)
			}
		}
		if err != nil {
			fail("Không thể tải trạng thái phòng nghe chung.")
			return
		}
		a.broadcastWatchPartyState(req.ChannelID, st)

	case "watchparty.leave":
		a.leaveWatchPartyClient(ctx, client, req.ChannelID)

	case "watchparty.ended":
		if !a.watchParties.isClientJoined(req.ChannelID, client) || req.ItemID <= 0 {
			return
		}
		_, joinedIDs := a.watchParties.listenersOf(req.ChannelID)
		adv, st, err := a.store.AdvanceWatchPartyIfEnded(ctx, req.ChannelID, req.ItemID, time.Now().UnixMilli(), len(joinedIDs) > 0)
		if err == nil && adv {
			a.broadcastWatchPartyState(req.ChannelID, st)
		}

	case "watchparty.sync_duration":
		if !a.watchParties.isClientJoined(req.ChannelID, client) || req.ItemID <= 0 || req.DurationSec <= 0 {
			return
		}
		dur := req.DurationSec
		st, err := a.store.UpdateWatchPartyItem(ctx, req.ChannelID, req.ItemID, nil, nil, &dur, time.Now().UnixMilli())
		if err == nil {
			a.broadcastWatchPartyState(req.ChannelID, st)
		}
	}
}
