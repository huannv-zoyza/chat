package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"chat/database"
	"chat/model"
	"chat/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	store.GiveStarterFeed = false // kho bắt đầu trống, các con số trong test mới rõ ràng
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
	check(w, 300, 360)
	w, _ = st.RepayDebt(ctx, u.ID)
	check(w, 0, 60)
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
	check(ws[0], 1500, 560)
	ws, _ = st.PayOut(ctx, m.ID, "cashout", 0, pay, "ended", []byte(`{}`))
	if len(ws) != 0 {
		t.Fatalf("second payout changed wallets: %v", ws)
	}
	w, _ = st.GetWallet(ctx, u.ID)
	check(w, 1500, 560)
	if rows, _ := st.GamesNeedingCashout(ctx, "baucua"); len(rows) != 0 {
		t.Fatalf("paid game still needs cashout: %v", rows)
	}

	rich, debtors, err := st.WalletBoard(ctx, ch.ID)
	if err != nil || len(rich) != 1 || len(debtors) != 1 || debtors[0].Debt != 560 {
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

// Đang nợ ngân hàng thì không chuyển xu được (chặn vay rồi chuyển cho bạn để "in tiền").
func TestTransferBlockedWhileInDebt(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	u1, _ := st.CreateUser(ctx, "debtor", "debtor@x.vn", "h", "Debtor")
	u2, _ := st.CreateUser(ctx, "friend", "friend@x.vn", "h", "Friend")

	if _, _, _, err := st.TransferCoins(ctx, u1.ID, u2.ID, store.WalletStart-50, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.TakeLoan(ctx, u1.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := st.TransferCoins(ctx, u1.ID, u2.ID, 10, ""); !errors.Is(err, store.ErrTransferInDebt) {
		t.Fatalf("expected ErrTransferInDebt, got %v", err)
	}
	// vay 300 nhưng nợ 360: bạn gửi thêm xu mới trả hết được
	if _, _, _, err := st.TransferCoins(ctx, u2.ID, u1.ID, 100, ""); err != nil {
		t.Fatal(err)
	}
	if w, err := st.RepayDebt(ctx, u1.ID); err != nil || w.Debt != 0 {
		t.Fatalf("repay: %v %+v", err, w)
	}
	if _, _, _, err := st.TransferCoins(ctx, u1.ID, u2.ID, 10, ""); err != nil {
		t.Fatalf("transfer after repaying: %v", err)
	}
}

// Vay: chỉ khi hết nợ và dưới 100 xu, +300 / nợ 360, 3 ngày một lần.
func TestLoanRulesAndCooldown(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	now := time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)
	st.SetClock(func() time.Time { return now })
	u, _ := st.CreateUser(ctx, "poor", "poor@x.vn", "h", "Poor")
	friend, _ := st.CreateUser(ctx, "rich", "rich@x.vn", "h", "Rich")

	if _, err := st.TakeLoan(ctx, u.ID); !errors.Is(err, store.ErrLoanNotAllowed) {
		t.Fatalf("loan with 1000 xu: %v", err)
	}
	st.TransferCoins(ctx, u.ID, friend.ID, 950, "")
	if at, _ := st.LoanAvailableAt(ctx, u.ID); at != nil {
		t.Fatalf("never borrowed but must wait until %v", at)
	}
	w, err := st.TakeLoan(ctx, u.ID)
	if err != nil || w.Balance != 350 || w.Debt != 360 {
		t.Fatalf("loan: %v %+v", err, w)
	}
	// còn nợ: không vay tiếp được (kể cả khi đã tiêu hết)
	st.TransferCoins(ctx, friend.ID, u.ID, 10, "")
	if _, err := st.TakeLoan(ctx, u.ID); !errors.Is(err, store.ErrLoanInDebt) {
		t.Fatalf("loan while in debt: %v", err)
	}
	// trả hết nợ, tiêu cho dưới 100: vẫn phải chờ 72h kể từ lần vay trước
	st.TransferCoins(ctx, friend.ID, u.ID, 100, "")
	if w, _ := st.RepayDebt(ctx, u.ID); w.Debt != 0 || w.Balance != 100 {
		t.Fatalf("after repay: %+v", w)
	}
	// số dư 100 là chưa dưới 100
	if _, err := st.TakeLoan(ctx, u.ID); !errors.Is(err, store.ErrLoanNotAllowed) {
		t.Fatalf("loan at 100: %v", err)
	}
	// hết nợ nên chuyển xu được: đẩy số dư xuống 50
	if _, _, _, err := st.TransferCoins(ctx, u.ID, friend.ID, 50, ""); err != nil {
		t.Fatal(err)
	}
	now = now.Add(71 * time.Hour)
	_, err = st.TakeLoan(ctx, u.ID)
	var cd *store.LoanCooldownError
	if !errors.Is(err, store.ErrLoanCooldown) || !errors.As(err, &cd) {
		t.Fatalf("loan during cooldown: %v", err)
	}
	want := time.Date(2026, 11, 23, 3, 0, 0, 0, time.UTC)
	if !cd.Until.Equal(want) {
		t.Fatalf("cooldown until %v, want %v", cd.Until, want)
	}
	if at, _ := st.LoanAvailableAt(ctx, u.ID); at == nil || !at.Equal(want) {
		t.Fatalf("LoanAvailableAt = %v", at)
	}
	now = now.Add(time.Hour)
	if at, _ := st.LoanAvailableAt(ctx, u.ID); at != nil {
		t.Fatalf("cooldown over but LoanAvailableAt = %v", at)
	}
	if w, err := st.TakeLoan(ctx, u.ID); err != nil || w.Balance != 350 || w.Debt != 360 {
		t.Fatalf("loan after cooldown: %v %+v", err, w)
	}
}

// Điểm danh liên tục: thưởng tăng theo chuỗi, đứt thì về 1; đang nợ thì nửa thưởng trả nợ.
func TestDailyStreakAndAutoRepay(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	u, _ := st.CreateUser(ctx, "daily", "daily@x.vn", "h", "Daily")
	day := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	bal := int64(store.WalletStart)
	for i, want := range []int64{100, 120, 140, 160, 180, 200, 300, 100, 120} {
		today := day.AddDate(0, 0, i).Format("2006-01-02")
		w, err := st.ClaimDaily(ctx, u.ID, today)
		if err != nil {
			t.Fatal(err)
		}
		bal += want
		if w.Balance != bal || w.Streak != i+1 || w.StreakOn(today) != i+1 {
			t.Fatalf("day %d: %+v, want balance %d", i+1, w, bal)
		}
	}
	// hôm sau vẫn thấy chuỗi (chưa điểm danh), bỏ một ngày thì đứt
	w, _ := st.GetWallet(ctx, u.ID)
	if w.StreakOn("2026-11-10") != 9 || w.StreakOn("2026-11-11") != 0 {
		t.Fatalf("streak view: %d %d", w.StreakOn("2026-11-10"), w.StreakOn("2026-11-11"))
	}
	w, _ = st.ClaimDaily(ctx, u.ID, "2026-11-11")
	if w.Streak != 1 || w.Balance != bal+100 {
		t.Fatalf("broken streak: %+v", w)
	}
	bal += 100

	// đang nợ: điểm danh 120 xu thì 60 tự trả nợ
	v, _ := st.CreateUser(ctx, "debtor2", "d2@x.vn", "h", "D2")
	st.TransferCoins(ctx, v.ID, u.ID, 950, "")
	st.TakeLoan(ctx, v.ID) // 350 / 360
	st.ClaimDaily(ctx, v.ID, "2026-11-11")
	w, _ = st.ClaimDaily(ctx, v.ID, "2026-11-12") // ngày 2: 120
	// ngày 1: +100, trả 50 -> 400/310; ngày 2: +120, trả 60 -> 460/250
	if w.Balance != 460 || w.Debt != 250 {
		t.Fatalf("auto repay: %+v", w)
	}
	items, _, _ := st.ListWalletTransactions(ctx, v.ID, "auto_repay", 0, 10)
	if len(items) != 2 || items[0].Amount != -60 || items[0].DebtDelta != -60 {
		t.Fatalf("auto_repay rows: %+v", items)
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
