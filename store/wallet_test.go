package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
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

func TestWalletTransferHistoryAndXiangqiEscrow(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)

	u1, _ := st.CreateUser(ctx, "red", "red@x.vn", "h", "Red")
	u2, _ := st.CreateUser(ctx, "black", "black@x.vn", "h", "Black")
	ch, _ := st.CreateChannel(ctx, u1.ID, "co-tuong", "", false)

	// Transfer P2P + concurrent overdraft protection
	if _, _, _, err := st.TransferCoins(ctx, u1.ID, u1.ID, 50, ""); !errors.Is(err, store.ErrSelfTransfer) {
		t.Fatalf("expected ErrSelfTransfer, got %v", err)
	}
	if _, _, _, err := st.TransferCoins(ctx, u1.ID, u2.ID, 0, ""); !errors.Is(err, store.ErrInvalidAmount) {
		t.Fatalf("expected ErrInvalidAmount, got %v", err)
	}
	if _, _, _, err := st.TransferCoins(ctx, u1.ID, 99999, 50, ""); !errors.Is(err, store.ErrRecipientNotFound) {
		t.Fatalf("expected ErrRecipientNotFound, got %v", err)
	}

	// Concurrent transfers of 300 from 1000 balance -> max 3 succeed
	var wg sync.WaitGroup
	var mu sync.Mutex
	okCount := 0
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, _, err := st.TransferCoins(ctx, u1.ID, u2.ID, 300, "chuyển"); err == nil {
				mu.Lock()
				okCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if okCount != 3 {
		t.Fatalf("expected 3 concurrent transfers, got %d", okCount)
	}
	w1, _ := st.GetWallet(ctx, u1.ID)
	w2, _ := st.GetWallet(ctx, u2.ID)
	if w1.Balance != 100 || w2.Balance != 1900 {
		t.Fatalf("unexpected balances after concurrent transfer: w1=%d w2=%d", w1.Balance, w2.Balance)
	}

	// Xiangqi bet escrow: create fails if insufficient
	if _, _, err := st.CreateGameWithBet(ctx, ch.ID, u1.ID, model.Embed{Type: "game", ID: "xiangqi", Title: "Cờ tướng"}, "lobby", []byte(`{}`), 500); !errors.Is(err, store.ErrInsufficientBalance) {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}

	// Transfer back 900 so u1=1000, u2=1000
	if _, _, _, err := st.TransferCoins(ctx, u2.ID, u1.ID, 900, "hoàn"); err != nil {
		t.Fatal(err)
	}

	// Match 1: stake 400 -> u1 wins
	m1, wu1, err := st.CreateGameWithBet(ctx, ch.ID, u1.ID, model.Embed{Type: "game", ID: "xiangqi", Title: "Cờ tướng"}, "lobby", []byte(`{}`), 400)
	if err != nil || wu1 == nil || wu1.Balance != 600 {
		t.Fatalf("create bet: err=%v w=%+v", err, wu1)
	}
	if locked, _ := st.LockedXiangqiBets(ctx, u1.ID); locked != 400 {
		t.Fatalf("expected locked=400, got %d", locked)
	}
	wu2, err := st.LockGameBet(ctx, u2.ID, m1.ID, 400)
	if err != nil || wu2.Balance != 600 {
		t.Fatalf("lock bet u2: err=%v w=%+v", err, wu2)
	}
	if _, err := st.LockGameBet(ctx, u2.ID, m1.ID, 400); !errors.Is(err, store.ErrBetAlreadyLocked) {
		t.Fatalf("expected ErrBetAlreadyLocked, got %v", err)
	}
	ws, err := st.SettleGameBets(ctx, m1.ID, []int64{u1.ID})
	if err != nil || len(ws) != 2 {
		t.Fatalf("settle win: err=%v len=%d", err, len(ws))
	}
	// Idempotent double settle
	ws2, err := st.SettleGameBets(ctx, m1.ID, []int64{u1.ID})
	if err != nil || len(ws2) != 0 {
		t.Fatalf("double settle should be noop: err=%v len=%d", err, len(ws2))
	}
	w1, _ = st.GetWallet(ctx, u1.ID)
	w2, _ = st.GetWallet(ctx, u2.ID)
	if w1.Balance != 1400 || w2.Balance != 600 {
		t.Fatalf("post-win balances: w1=%d w2=%d", w1.Balance, w2.Balance)
	}

	// Match 2: DeleteMessage refunds locked bet
	m2, _, err := st.CreateGameWithBet(ctx, ch.ID, u1.ID, model.Embed{Type: "game", ID: "xiangqi", Title: "Cờ tướng"}, "lobby", []byte(`{}`), 500)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.DeleteMessage(ctx, m2.ID); err != nil {
		t.Fatal(err)
	}
	w1, _ = st.GetWallet(ctx, u1.ID)
	if w1.Balance != 1400 {
		t.Fatalf("expected refund on DeleteMessage, got %d", w1.Balance)
	}

	// History pagination check
	items, hasMore, err := st.ListWalletTransactions(ctx, u1.ID, "", 0, 3)
	if err != nil || len(items) != 3 || !hasMore {
		t.Fatalf("list transactions: err=%v len=%d hasMore=%v", err, len(items), hasMore)
	}
}
