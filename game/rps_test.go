package game

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRPSBetOptionsAndSettlement(t *testing.T) {
	ps := players(2)
	a, b := ps[0], ps[1]

	// Mức cược không nằm trong danh sách cố định bị từ chối.
	if _, err := New("rps", a, json.RawMessage(`{"best_of":3,"bet":75}`), t0); err == nil {
		t.Fatal("invalid bet level should be rejected")
	}

	// Chơi vui (bet = 0) không dùng ví xu.
	free, err := New("rps", a, json.RawMessage(`{"best_of":1}`), t0)
	if err != nil {
		t.Fatal(err)
	}
	if free.Staked() {
		t.Fatal("free rps must not be staked")
	}
	if exact, amt := free.StakeRequirement(); exact || amt != 0 {
		t.Fatalf("free rps StakeRequirement = (%v, %d), want (false, 0)", exact, amt)
	}

	// Ván cược cố định 100 xu: thắng ăn trọn 200 xu.
	g, err := New("rps", a, json.RawMessage(`{"best_of":3,"bet":100}`), t0)
	if err != nil {
		t.Fatal(err)
	}
	if !g.Staked() {
		t.Fatal("bet rps must be staked")
	}
	if exact, amt := g.StakeRequirement(); !exact || amt != 100 {
		t.Fatalf("StakeRequirement = (%v, %d), want (true, 100)", exact, amt)
	}
	g.BuyIn(a.ID, 100)
	if st := g.StakeOf(a.ID); st.Chips != 100 {
		t.Fatalf("host lobby stake = %+v, want 100", st)
	}

	act(t, g, b, "join", nil)
	g.BuyIn(b.ID, 100)
	pub := g.View(0).State.(rpsPublic)
	if pub.Bet != 100 || pub.Pot != 200 {
		t.Fatalf("pub bet/pot = %d/%d, want 100/200", pub.Bet, pub.Pot)
	}

	// Đang Playing: không cho chủ phòng huỷ để né thua.
	if err := tryAct(g, a, "cancel", nil); err == nil {
		t.Fatal("cancel while playing must be blocked for rps")
	}

	// A thắng 2–0 -> A nhận 200, B nhận 0.
	act(t, g, a, "pick", map[string]string{"choice": "rock"})
	act(t, g, b, "pick", map[string]string{"choice": "scissors"})
	act(t, g, a, "pick", map[string]string{"choice": "paper"})
	act(t, g, b, "pick", map[string]string{"choice": "rock"})
	if g.Status() != Ended || len(g.Winners()) != 1 || g.Winners()[0] != a.ID {
		t.Fatalf("unexpected end: %s %v", g.Status(), g.Winners())
	}
	if gotA, gotB := g.StakeOf(a.ID).Chips, g.StakeOf(b.ID).Chips; gotA != 200 || gotB != 0 {
		t.Fatalf("win payout = (%d, %d), want (200, 0)", gotA, gotB)
	}

	// Chịu thua: đối thủ ăn trọn pot.
	gResign, _ := New("rps", a, json.RawMessage(`{"best_of":3,"bet":200}`), t0)
	gResign.BuyIn(a.ID, 200)
	act(t, gResign, b, "join", nil)
	gResign.BuyIn(b.ID, 200)
	act(t, gResign, a, "resign", nil)
	if gResign.StakeOf(a.ID).Chips != 0 || gResign.StakeOf(b.ID).Chips != 400 {
		t.Fatalf("resign payout = (%d, %d), want (0, 400)", gResign.StakeOf(a.ID).Chips, gResign.StakeOf(b.ID).Chips)
	}

	// Hoà do hết giờ cả hai không ra tay: hoàn tiền mỗi bên.
	gTimeout, _ := New("rps", a, json.RawMessage(`{"best_of":1,"bet":500}`), t0)
	gTimeout.BuyIn(a.ID, 500)
	act(t, gTimeout, b, "join", nil)
	gTimeout.BuyIn(b.ID, 500)
	if !gTimeout.Tick(t0.Add(rpsRoundTime+time.Second)) || gTimeout.Status() != Ended {
		t.Fatal("timeout should end game")
	}
	if gTimeout.StakeOf(a.ID).Chips != 500 || gTimeout.StakeOf(b.ID).Chips != 500 {
		t.Fatalf("timeout refund = (%d, %d), want (500, 500)", gTimeout.StakeOf(a.ID).Chips, gTimeout.StakeOf(b.ID).Chips)
	}

	// Huỷ ở phòng chờ & Abort giữa trận: hoàn tiền đầy đủ.
	gLobby, _ := New("rps", a, json.RawMessage(`{"best_of":3,"bet":50}`), t0)
	gLobby.BuyIn(a.ID, 50)
	act(t, gLobby, a, "cancel", nil)
	if gLobby.StakeOf(a.ID).Chips != 50 {
		t.Fatalf("lobby cancel refund = %d, want 50", gLobby.StakeOf(a.ID).Chips)
	}

	gAbort, _ := New("rps", a, json.RawMessage(`{"best_of":3,"bet":1000}`), t0)
	gAbort.BuyIn(a.ID, 1000)
	act(t, gAbort, b, "join", nil)
	gAbort.BuyIn(b.ID, 1000)
	gAbort.Abort("Tin nhắn bị xoá.")
	if gAbort.StakeOf(a.ID).Chips != 1000 || gAbort.StakeOf(b.ID).Chips != 1000 {
		t.Fatalf("abort refund = (%d, %d), want (1000, 1000)", gAbort.StakeOf(a.ID).Chips, gAbort.StakeOf(b.ID).Chips)
	}
}
