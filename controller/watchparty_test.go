package controller_test

import (
	"testing"
)

type watchPartyItem struct {
	ID      int64  `json:"id"`
	VideoID string `json:"video_id"`
	Title   string `json:"title"`
}

type watchPartyState struct {
	ChannelID     int64            `json:"channel_id"`
	CurrentItem   *watchPartyItem  `json:"current_item"`
	Queue         []watchPartyItem `json:"queue"`
	IsPlaying     bool             `json:"is_playing"`
	ListenerCount int              `json:"listener_count"`
	Joined        bool             `json:"joined"`
	LoopMode      string           `json:"loop_mode"`
}

func TestWatchPartyJoinQueueAndSync(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")

	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]string{"name": "music-room"}, &ch)
	e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", bob.Token, nil, nil)

	wa := e.dial(alice.Token)
	wb := e.dial(bob.Token)
	wa.waitFor("ready")
	wb.waitFor("ready")

	base := "/api/channels/" + itoa(ch.ID) + "/watch-party"

	// 1. Invalid URL rejected
	if code := e.do("POST", base+"/items", alice.Token, map[string]any{"url": "https://example.com/song"}, nil); code != 400 {
		t.Fatalf("expected 400 for non-youtube URL, got %d", code)
	}

	// 2. Alice adds 2 YouTube songs; initially 0 listeners so not playing yet
	var add1 struct {
		Item  watchPartyItem  `json:"item"`
		State watchPartyState `json:"state"`
	}
	if code := e.do("POST", base+"/items", alice.Token, map[string]any{
		"url":   "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"title": "Never Gonna Give You Up",
	}, &add1); code != 201 {
		t.Fatalf("add item 1: %d", code)
	}
	if add1.State.CurrentItem == nil || add1.State.CurrentItem.VideoID != "dQw4w9WgXcQ" || add1.State.IsPlaying {
		t.Fatalf("expected item 1 paused until join, got %+v", add1.State)
	}

	var add2 struct {
		Item  watchPartyItem  `json:"item"`
		State watchPartyState `json:"state"`
	}
	if code := e.do("POST", base+"/items", bob.Token, map[string]any{
		"url":   "https://youtu.be/9bZkp7q19f0",
		"title": "Gangnam Style",
	}, &add2); code != 201 {
		t.Fatalf("add item 2: %d", code)
	}
	var add3 struct {
		Item  watchPartyItem  `json:"item"`
		State watchPartyState `json:"state"`
	}
	if code := e.do("POST", base+"/items", alice.Token, map[string]any{
		"url":   "https://youtu.be/kJQP7kiw5Fk",
		"title": "Despacito",
	}, &add3); code != 201 {
		t.Fatalf("add item 3: %d", code)
	}

	// 3. Control without Joining must be rejected!
	if code := e.do("POST", base+"/control", bob.Token, map[string]any{"action": "play"}, nil); code != 400 {
		t.Fatalf("control before join must be 400, got %d", code)
	}

	// 4. Bob joins via WS -> receives state with joined=true and playback starts
	wb.send(map[string]any{"type": "watchparty.join", "channel_id": ch.ID})
	evJoin := wb.waitFor("watchparty.state")
	stMap := evJoin["state"].(map[string]any)
	// First broadcast may be channel-wide (joined=false) followed by personal (joined=true), so check via GET /watch-party too
	_ = stMap
	var bobView watchPartyState
	if code := e.do("GET", base, bob.Token, nil, &bobView); code != 200 || !bobView.Joined || !bobView.IsPlaying || bobView.ListenerCount != 1 {
		t.Fatalf("expected bob joined and playing: code=%d view=%+v", code, bobView)
	}

	// 5. Alice (not joined yet) can reorder, edit, delete in queue!
	if code := e.do("PUT", base+"/queue/reorder", alice.Token, map[string]any{
		"item_ids": []int64{add3.Item.ID, add2.Item.ID},
	}, &bobView); code != 200 {
		t.Fatalf("reorder: %d", code)
	}
	if len(bobView.Queue) != 2 || bobView.Queue[0].ID != add3.Item.ID {
		t.Fatalf("expected add3 first in queue, got %+v", bobView.Queue)
	}

	if code := e.do("PATCH", base+"/items/"+itoa(add3.Item.ID), alice.Token, map[string]any{
		"title": "Despacito (Live)",
	}, &bobView); code != 200 || bobView.Queue[0].Title != "Despacito (Live)" {
		t.Fatalf("edit item: code=%d queue=%+v", code, bobView.Queue)
	}

	// 6. Bob (joined) reports song ended -> auto advances to Despacito (Live)
	wb.send(map[string]any{"type": "watchparty.ended", "channel_id": ch.ID, "item_id": add1.Item.ID})
	// Wait until GET shows CurrentItem == add3
	wb.waitFor("watchparty.state")
	var afterEnd watchPartyState
	e.do("GET", base, bob.Token, nil, &afterEnd)
	if afterEnd.CurrentItem == nil || afterEnd.CurrentItem.ID != add3.Item.ID {
		t.Fatalf("expected auto-advance to add3 (%d), got %+v", add3.Item.ID, afterEnd.CurrentItem)
	}

	// 7. Bob leaves -> listener_count drops to 0 and playback auto-pauses
	wb.send(map[string]any{"type": "watchparty.leave", "channel_id": ch.ID})
	wb.waitFor("watchparty.state")
	var afterLeave watchPartyState
	e.do("GET", base, bob.Token, nil, &afterLeave)
	if afterLeave.Joined || afterLeave.ListenerCount != 0 || afterLeave.IsPlaying {
		t.Fatalf("expected paused with 0 listeners after leave, got %+v", afterLeave)
	}
}
