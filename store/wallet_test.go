package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"chat/database"
	"chat/model"
	"chat/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := database.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(database.MigrationOpt{DB: db, Mode: database.ModeUp}); err != nil {
		t.Fatal(err)
	}
	return store.New(db)
}

func TestWalletFlow(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	u, err := st.CreateUser(ctx, "alice", "a@x.vn", "h", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := st.CreateChannel(ctx, u.ID, "kenh", "", false)
	if err != nil {
		t.Fatal(err)
	}
	m, err := st.CreateGame(ctx, ch.ID, u.ID, model.Embed{Type: "game", ID: "baucua", Title: "x"}, "lobby", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	check := func(w store.Wallet, bal, debt int64) {
		t.Helper()
		if w.Balance != bal || w.Debt != debt {
			t.Fatalf("wallet = %d/%d, want %d/%d", w.Balance, w.Debt, bal, debt)
		}
	}

	w, _ := st.GetWallet(ctx, u.ID)
	check(w, 1000, 0)
	w, _ = st.ClaimDaily(ctx, u.ID, "2026-11-23")
	check(w, 1100, 0)
	if _, err := st.ClaimDaily(ctx, u.ID, "2026-11-23"); !errors.Is(err, store.ErrAlreadyClaimed) {
		t.Fatalf("claim twice: %v", err)
	}
	if _, err := st.TakeLoan(ctx, u.ID); !errors.Is(err, store.ErrLoanNotAllowed) {
		t.Fatalf("loan while rich: %v", err)
	}

	// apply lỗi: không đổi gì
	if _, err := st.BuyIn(ctx, m.ID, u.ID, 1, func(int64) (string, []byte, error) { return "", nil, errors.New("x") }); err == nil {
		t.Fatal("buyin with failing apply should fail")
	}
	w, _ = st.GetWallet(ctx, u.ID)
	check(w, 1100, 0)

	var got int64
	w, err = st.BuyIn(ctx, m.ID, u.ID, 1, func(a int64) (string, []byte, error) {
		got = a
		return "lobby", []byte(`{"x":1}`), nil
	})
	if err != nil || got != 1100 {
		t.Fatalf("buyin: %v amount %d", err, got)
	}
	check(w, 0, 0)
	if g, _ := st.GetGame(ctx, m.ID); string(g.State) != `{"x":1}` {
		t.Fatalf("game state not saved: %s", g.State)
	}

	w, _ = st.TakeLoan(ctx, u.ID)
	check(w, 500, 500)
	w, _ = st.RepayDebt(ctx, u.ID)
	check(w, 0, 0)
	if _, err := st.RepayDebt(ctx, u.ID); !errors.Is(err, store.ErrNothingToRepay) {
		t.Fatalf("repay without debt: %v", err)
	}

	if rows, _ := st.GamesNeedingCashout(ctx, "baucua"); len(rows) != 0 {
		t.Fatalf("lobby game should not need cashout: %v", rows)
	}
	if err := st.SaveGame(ctx, m.ID, "ended", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if rows, _ := st.GamesNeedingCashout(ctx, "baucua"); len(rows) != 1 {
		t.Fatalf("ended game should need cashout: %v", rows)
	}

	// trả chip đúng một lần
	pay := []store.Payout{{UserID: u.ID, Chips: 1500, Loans: 500}}
	ws, err := st.PayOut(ctx, m.ID, "cashout", 0, pay, "ended", []byte(`{}`))
	if err != nil || len(ws) != 1 {
		t.Fatalf("payout: %v %v", err, ws)
	}
	check(ws[0], 1500, 500)
	ws, _ = st.PayOut(ctx, m.ID, "cashout", 0, pay, "ended", []byte(`{}`))
	if len(ws) != 0 {
		t.Fatalf("second payout changed wallets: %v", ws)
	}
	w, _ = st.GetWallet(ctx, u.ID)
	check(w, 1500, 500)
	if rows, _ := st.GamesNeedingCashout(ctx, "baucua"); len(rows) != 0 {
		t.Fatalf("paid game still needs cashout: %v", rows)
	}

	rich, debtors, err := st.WalletBoard(ctx, ch.ID)
	if err != nil || len(rich) != 1 || len(debtors) != 1 || debtors[0].Debt != 500 {
		t.Fatalf("board: %v %v %v", err, rich, debtors)
	}
}

func TestFixedBetWalletFlow(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	alice, _ := st.CreateUser(ctx, "alice", "a@x.vn", "h", "Alice")
	bob, _ := st.CreateUser(ctx, "bob", "b@x.vn", "h", "Bob")
	ch, _ := st.CreateChannel(ctx, alice.ID, "kenh", "", false)
	_, _ = st.AddMember(ctx, ch.ID, bob.ID)

	// Không đủ xu tạo phòng (có 1000, đòi 1500) -> không tạo tin nhắn, không trừ ví.
	if _, _, err := st.CreateStakedGame(ctx, ch.ID, alice.ID, model.Embed{Type: "game", ID: "rps", Title: "Oẳn tù tì"}, 1500, func() (string, []byte, error) {
		return "lobby", []byte(`{"opts":{"best_of":3,"bet":1500}}`), nil
	}); !errors.Is(err, store.ErrInsufficientBalance) {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}
	if msgs, _, _ := st.ListMessages(ctx, ch.ID, 0, 10); len(msgs) != 0 {
		t.Fatalf("expected no message created, got %d", len(msgs))
	}

	// Tạo phòng cược 500 -> trừ đúng 500, còn lại 500 trong ví.
	m, wal, err := st.CreateStakedGame(ctx, ch.ID, alice.ID, model.Embed{Type: "game", ID: "rps", Title: "Oẳn tù tì"}, 500, func() (string, []byte, error) {
		return "lobby", []byte(`{"opts":{"best_of":3,"bet":500}}`), nil
	})
	if err != nil || wal.Balance != 500 {
		t.Fatalf("CreateStakedGame: wal=%+v err=%v", wal, err)
	}

	// Bob thử BuyInExact quá số dư -> ErrInsufficientBalance, state không đổi.
	if _, err := st.BuyInExact(ctx, m.ID, bob.ID, 1, 1500, func(int64) (string, []byte, error) {
		return "playing", []byte(`{"changed":true}`), nil
	}); !errors.Is(err, store.ErrInsufficientBalance) {
		t.Fatalf("expected ErrInsufficientBalance on BuyInExact, got %v", err)
	}
	if g, _ := st.GetGame(ctx, m.ID); string(g.State) != `{"opts":{"best_of":3,"bet":500}}` {
		t.Fatalf("game state mutated on failed BuyInExact: %s", g.State)
	}

	// Bob BuyInExact 500 thành công -> còn 500.
	walBob, err := st.BuyInExact(ctx, m.ID, bob.ID, 1, 500, func(amt int64) (string, []byte, error) {
		if amt != 500 {
			t.Fatalf("amt = %d, want 500", amt)
		}
		return "playing", []byte(`{"opts":{"best_of":3,"bet":500}}`), nil
	})
	if err != nil || walBob.Balance != 500 {
		t.Fatalf("BuyInExact: wal=%+v err=%v", walBob, err)
	}

	// Tạo thêm một ván rps miễn phí (bet=0) đã kết thúc -> GamesNeedingCashout không lấy ván này.
	mFree, _ := st.CreateGame(ctx, ch.ID, alice.ID, model.Embed{Type: "game", ID: "rps", Title: "Oẳn tù tì"}, "ended", []byte(`{"opts":{"best_of":3,"bet":0}}`))
	_ = mFree
	if err := st.SaveGame(ctx, m.ID, "ended", []byte(`{"opts":{"best_of":3,"bet":500}}`)); err != nil {
		t.Fatal(err)
	}
	rows, err := st.GamesNeedingCashout(ctx, "baucua", "rps")
	if err != nil || len(rows) != 1 || rows[0].MessageID != m.ID {
		t.Fatalf("GamesNeedingCashout = %+v (err=%v), want only %d", rows, err, m.ID)
	}
}
