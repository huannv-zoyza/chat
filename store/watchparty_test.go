package store_test

import (
	"context"
	"errors"
	"testing"

	"chat/store"
)

func TestWatchPartyQueueLifecycle(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)

	u1, _ := st.CreateUser(ctx, "alice", "a@wp.vn", "h", "Alice")
	u2, _ := st.CreateUser(ctx, "bob", "b@wp.vn", "h", "Bob")
	ch, _ := st.CreateChannel(ctx, u1.ID, "nghe-nhac", "", false)

	// 1. Add first item -> becomes CurrentItem
	it1, s1, err := st.AddWatchPartyItem(ctx, ch.ID, u1.ID, "dQw4w9WgXcQ", "https://youtu.be/dQw4w9WgXcQ", "Song 1", "Author 1", "thumb1", 212, true, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if s1.CurrentItem == nil || s1.CurrentItem.ID != it1.ID || !s1.IsPlaying || len(s1.Queue) != 0 {
		t.Fatalf("expected item 1 playing, got %+v", s1)
	}

	// 2. Add items 2 and 3 -> pushed to back of Queue in FIFO order
	it2, _, err := st.AddWatchPartyItem(ctx, ch.ID, u2.ID, "aaaaaaaaaaa", "https://youtu.be/aaaaaaaaaaa", "Song 2", "Author 2", "thumb2", 180, true, 2000)
	if err != nil {
		t.Fatal(err)
	}
	it3, s3, err := st.AddWatchPartyItem(ctx, ch.ID, u1.ID, "bbbbbbbbbbb", "https://youtu.be/bbbbbbbbbbb", "Song 3", "Author 3", "thumb3", 200, true, 3000)
	if err != nil {
		t.Fatal(err)
	}
	if len(s3.Queue) != 2 || s3.Queue[0].ID != it2.ID || s3.Queue[1].ID != it3.ID {
		t.Fatalf("expected FIFO queue [it2, it3], got %+v", s3.Queue)
	}

	// 3. Anyone can reorder: swap it3 before it2
	s4, err := st.ReorderWatchPartyQueue(ctx, ch.ID, []int64{it3.ID, it2.ID}, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if s4.Queue[0].ID != it3.ID || s4.Queue[1].ID != it2.ID {
		t.Fatalf("expected reordered [it3, it2], got %+v", s4.Queue)
	}

	// 4. Edit title of queued item
	newTitle := "Song 3 (Remix)"
	s5, err := st.UpdateWatchPartyItem(ctx, ch.ID, it3.ID, &newTitle, nil, nil, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if s5.Queue[0].Title != newTitle {
		t.Fatalf("expected updated title %q, got %q", newTitle, s5.Queue[0].Title)
	}

	// 5. Idempotent ended advance: first advances it1 -> it3, duplicate call for it1 is no-op
	adv, s6, err := st.AdvanceWatchPartyIfEnded(ctx, ch.ID, it1.ID, 6000, true)
	if err != nil || !adv || s6.CurrentItem == nil || s6.CurrentItem.ID != it3.ID {
		t.Fatalf("advance 1: adv=%v err=%v cur=%+v", adv, err, s6.CurrentItem)
	}
	advDup, _, err := st.AdvanceWatchPartyIfEnded(ctx, ch.ID, it1.ID, 6100, true)
	if err != nil || advDup {
		t.Fatalf("duplicate advance should be noop, got adv=%v err=%v", advDup, err)
	}

	// 6. Deleting current item (it3) auto-advances to it2
	s7, err := st.DeleteWatchPartyItem(ctx, ch.ID, it3.ID, 7000, true)
	if err != nil {
		t.Fatal(err)
	}
	if s7.CurrentItem == nil || s7.CurrentItem.ID != it2.ID || len(s7.Queue) != 0 {
		t.Fatalf("delete current should advance to it2, got %+v", s7)
	}

	// 7. Loop single repeats current item on end
	if _, err := st.ControlWatchParty(ctx, ch.ID, "loop", nil, "single", 7500, true); err != nil {
		t.Fatal(err)
	}
	advLoop, s8, err := st.AdvanceWatchPartyIfEnded(ctx, ch.ID, it2.ID, 8000, true)
	if err != nil || !advLoop || s8.CurrentItem == nil || s8.CurrentItem.ID != it2.ID {
		t.Fatalf("loop single failed: adv=%v cur=%+v", advLoop, s8.CurrentItem)
	}

	// 8. Pause on empty freezes elapsed position
	pos := 10.0
	if _, err := st.ControlWatchParty(ctx, ch.ID, "seek", &pos, "", 10000, true); err != nil {
		t.Fatal(err)
	}
	paused, s9, err := st.PauseWatchPartyIfPlaying(ctx, ch.ID, 15000)
	if err != nil || !paused || s9.IsPlaying || s9.AnchorPosSec < 14.9 || s9.AnchorPosSec > 15.1 {
		t.Fatalf("pause on empty failed: paused=%v state=%+v", paused, s9)
	}

	// 9. Clear all stops room
	s10, err := st.ClearWatchPartyQueue(ctx, ch.ID, true, 16000)
	if err != nil || s10.CurrentItem != nil || len(s10.Queue) != 0 {
		t.Fatalf("clear failed: err=%v state=%+v", err, s10)
	}
	if _, err := st.DeleteWatchPartyItem(ctx, ch.ID, 999999, 16500, false); !errors.Is(err, store.ErrWatchPartyItemNotFound) {
		t.Fatalf("expected ErrWatchPartyItemNotFound, got %v", err)
	}
}
