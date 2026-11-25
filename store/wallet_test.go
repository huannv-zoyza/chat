package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"chat/database"
	"chat/model"
	"chat/store"
)

func openTestStore(t *testing.T) (*store.Store, *sql.DB) {
	t.Helper()
	db, err := database.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(database.MigrationOpt{DB: db, Mode: database.ModeUp}); err != nil {
		t.Fatal(err)
	}
	return store.New(db), db
}

func assertLedgerInvariant(t *testing.T, db *sql.DB, userID int64) {
	t.Helper()
	var balance, sum int64
	if err := db.QueryRow(`SELECT balance FROM wallets WHERE user_id = ?`, userID).Scan(&balance); err != nil {
		t.Fatalf("read wallet %d: %v", userID, err)
	}
	if err := db.QueryRow(`SELECT COALESCE(SUM(amount), 0) FROM wallet_transactions WHERE user_id = ?`, userID).Scan(&sum); err != nil {
		t.Fatalf("sum ledger %d: %v", userID, err)
	}
	if balance != sum {
		t.Fatalf("invariant broken for user %d: wallet.balance=%d != sum(ledger)=%d", userID, balance, sum)
	}
}

func TestWalletInitialCheckinBailoutAndTransfer(t *testing.T) {
	st, db := openTestStore(t)
	ctx := context.Background()

	u1, err := st.CreateUser(ctx, "alice", "alice@example.com", "hash", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	u2, err := st.CreateUser(ctx, "bob", "bob@example.com", "hash", "Bob")
	if err != nil {
		t.Fatal(err)
	}

	w1, err := st.GetWallet(ctx, u1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w1.Balance != store.InitialWalletBalance || w1.LockedBalance != 0 || !w1.CanCheckin || w1.CanBailout {
		t.Fatalf("unexpected initial wallet: %+v", w1)
	}
	assertLedgerInvariant(t, db, u1.ID)

	// Daily checkin once per ICT day
	day1 := time.Date(2026, 11, 25, 2, 0, 0, 0, time.UTC) // 09:00 ICT
	w1, tx, err := st.ClaimDailyCheckin(ctx, u1.ID, day1)
	if err != nil {
		t.Fatalf("checkin day1: %v", err)
	}
	if w1.Balance != 12000 || tx.Amount != 2000 || w1.CanCheckin {
		t.Fatalf("unexpected after checkin: wallet=%+v tx=%+v", w1, tx)
	}
	if _, _, err := st.ClaimDailyCheckin(ctx, u1.ID, day1.Add(2*time.Hour)); !errors.Is(err, store.ErrCheckinAlreadyClaimed) {
		t.Fatalf("expected ErrCheckinAlreadyClaimed, got %v", err)
	}
	// Next ICT day succeeds
	day2 := day1.Add(24 * time.Hour)
	w1, _, err = st.ClaimDailyCheckin(ctx, u1.ID, day2)
	if err != nil || w1.Balance != 14000 {
		t.Fatalf("checkin day2: err=%v wallet=%+v", err, w1)
	}

	// Self-transfer & invalid amount
	if _, _, err := st.TransferCoins(ctx, u1.ID, u1.ID, 100, ""); !errors.Is(err, store.ErrSelfTransfer) {
		t.Fatalf("expected ErrSelfTransfer, got %v", err)
	}
	if _, _, err := st.TransferCoins(ctx, u1.ID, u2.ID, 0, ""); !errors.Is(err, store.ErrInvalidAmount) {
		t.Fatalf("expected ErrInvalidAmount, got %v", err)
	}
	if _, _, err := st.TransferCoins(ctx, u1.ID, 9999, 100, ""); !errors.Is(err, store.ErrRecipientNotFound) {
		t.Fatalf("expected ErrRecipientNotFound, got %v", err)
	}

	// Transfer almost everything from u1 to u2 to test bailout
	if _, _, err := st.TransferCoins(ctx, u1.ID, u2.ID, 13700, "all in"); err != nil {
		t.Fatalf("transfer: %v", err)
	}
	w1, _ = st.GetWallet(ctx, u1.ID)
	w2, _ := st.GetWallet(ctx, u2.ID)
	if w1.Balance != 300 || w2.Balance != 23700 {
		t.Fatalf("unexpected balances after transfer: w1=%d w2=%d", w1.Balance, w2.Balance)
	}
	assertLedgerInvariant(t, db, u1.ID)
	assertLedgerInvariant(t, db, u2.ID)

	// u2 has plenty -> bailout rejected
	if _, _, err := st.ClaimBailout(ctx, u2.ID, day2); !errors.Is(err, store.ErrBailoutNotEligible) {
		t.Fatalf("expected ErrBailoutNotEligible, got %v", err)
	}
	// u1 has 300 < 500 -> bailout succeeds (+1000 -> 1300)
	w1, _, err = st.ClaimBailout(ctx, u1.ID, day2)
	if err != nil || w1.Balance != 1300 {
		t.Fatalf("bailout 1: err=%v bal=%d", err, w1.Balance)
	}
	// Drain again 3 more times to test daily limit (max 3/day)
	for i := 2; i <= 4; i++ {
		w1, _ = st.GetWallet(ctx, u1.ID)
		if w1.Balance >= 500 {
			if _, _, err := st.TransferCoins(ctx, u1.ID, u2.ID, w1.Balance, "drain"); err != nil {
				t.Fatal(err)
			}
		}
		_, _, err = st.ClaimBailout(ctx, u1.ID, day2)
		if i <= 3 && err != nil {
			t.Fatalf("bailout %d unexpected err: %v", i, err)
		}
		if i == 4 && !errors.Is(err, store.ErrBailoutLimitReached) {
			t.Fatalf("expected ErrBailoutLimitReached on 4th, got %v", err)
		}
	}

	assertLedgerInvariant(t, db, u1.ID)
	assertLedgerInvariant(t, db, u2.ID)
}

func TestWalletGameBetEscrowWinDrawRefundAndIdempotency(t *testing.T) {
	st, db := openTestStore(t)
	ctx := context.Background()

	u1, _ := st.CreateUser(ctx, "red", "red@example.com", "hash", "Red")
	u2, _ := st.CreateUser(ctx, "black", "black@example.com", "hash", "Black")
	ch, err := st.CreateChannel(ctx, u1.ID, "co-tuong", "", false)
	if err != nil {
		t.Fatal(err)
	}

	// Insufficient bet fails atomically without leaving a message
	if _, _, err := st.CreateGameWithBet(ctx, ch.ID, u1.ID, model.Embed{Type: "game", ID: "xiangqi", Title: "Cờ tướng"}, "lobby", []byte(`{}`), 50000); !errors.Is(err, store.ErrInsufficientBalance) {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}

	// Match 1: Win (stake 2000)
	msg, wu1, err := st.CreateGameWithBet(ctx, ch.ID, u1.ID, model.Embed{Type: "game", ID: "xiangqi", Title: "Cờ tướng"}, "lobby", []byte(`{}`), 2000)
	if err != nil || wu1 == nil || wu1.Wallet.Balance != 8000 || wu1.Wallet.LockedBalance != 2000 {
		t.Fatalf("create with bet: err=%v wu=%+v", err, wu1)
	}
	wu2, err := st.LockGameBet(ctx, u2.ID, msg.ID, 2000)
	if err != nil || wu2.Wallet.Balance != 8000 || wu2.Wallet.LockedBalance != 2000 {
		t.Fatalf("lock bet u2: err=%v wu=%+v", err, wu2)
	}
	// Duplicate lock rejected
	if _, err := st.LockGameBet(ctx, u2.ID, msg.ID, 2000); !errors.Is(err, store.ErrBetAlreadyLocked) {
		t.Fatalf("expected ErrBetAlreadyLocked, got %v", err)
	}

	updates, err := st.SettleGameBets(ctx, msg.ID, []int64{u1.ID})
	if err != nil || len(updates) != 2 {
		t.Fatalf("settle win: err=%v updates=%+v", err, updates)
	}
	// Second settle is a no-op (idempotent)
	updates2, err := st.SettleGameBets(ctx, msg.ID, []int64{u1.ID})
	if err != nil || len(updates2) != 0 {
		t.Fatalf("double settle should be noop: err=%v updates=%+v", err, updates2)
	}

	w1, _ := st.GetWallet(ctx, u1.ID)
	w2, _ := st.GetWallet(ctx, u2.ID)
	if w1.Balance != 12000 || w1.LockedBalance != 0 || w2.Balance != 8000 || w2.LockedBalance != 0 {
		t.Fatalf("post-win balances mismatch: w1=%+v w2=%+v", w1, w2)
	}
	assertLedgerInvariant(t, db, u1.ID)
	assertLedgerInvariant(t, db, u2.ID)

	// Match 2: Draw (stake 1500)
	msg2, _, err := st.CreateGameWithBet(ctx, ch.ID, u1.ID, model.Embed{Type: "game", ID: "xiangqi", Title: "Cờ tướng"}, "lobby", []byte(`{}`), 1500)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.LockGameBet(ctx, u2.ID, msg2.ID, 1500); err != nil {
		t.Fatal(err)
	}
	if updates, err := st.SettleGameBets(ctx, msg2.ID, nil); err != nil || len(updates) != 2 {
		t.Fatalf("settle draw: err=%v len=%d", err, len(updates))
	}
	w1, _ = st.GetWallet(ctx, u1.ID)
	w2, _ = st.GetWallet(ctx, u2.ID)
	if w1.Balance != 12000 || w2.Balance != 8000 {
		t.Fatalf("post-draw balances mismatch: w1=%d w2=%d", w1.Balance, w2.Balance)
	}

	// Match 3: DeleteMessage refunds locked bet atomically
	msg3, _, err := st.CreateGameWithBet(ctx, ch.ID, u1.ID, model.Embed{Type: "game", ID: "xiangqi", Title: "Cờ tướng"}, "lobby", []byte(`{}`), 3000)
	if err != nil {
		t.Fatal(err)
	}
	w1Mid, _ := st.GetWallet(ctx, u1.ID)
	if w1Mid.Balance != 9000 || w1Mid.LockedBalance != 3000 {
		t.Fatalf("mid balance mismatch: %+v", w1Mid)
	}
	if _, _, err := st.DeleteMessage(ctx, msg3.ID); err != nil {
		t.Fatal(err)
	}
	w1End, _ := st.GetWallet(ctx, u1.ID)
	if w1End.Balance != 12000 || w1End.LockedBalance != 0 {
		t.Fatalf("delete message should refund bet: %+v", w1End)
	}

	assertLedgerInvariant(t, db, u1.ID)
	assertLedgerInvariant(t, db, u2.ID)
}

func TestWalletConcurrentTransfersNeverOverdraft(t *testing.T) {
	st, db := openTestStore(t)
	ctx := context.Background()

	u1, _ := st.CreateUser(ctx, "sender", "s@example.com", "hash", "Sender")
	u2, _ := st.CreateUser(ctx, "receiver", "r@example.com", "hash", "Receiver")

	// Send 10 concurrent transfers of 3000 each from a 10000 balance -> at most 3 can succeed
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := st.TransferCoins(ctx, u1.ID, u2.ID, 3000, "race"); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if successes != 3 {
		t.Fatalf("expected exactly 3 concurrent transfers to succeed, got %d", successes)
	}
	w1, _ := st.GetWallet(ctx, u1.ID)
	w2, _ := st.GetWallet(ctx, u2.ID)
	if w1.Balance != 1000 || w2.Balance != 19000 {
		t.Fatalf("unexpected final balances: w1=%d w2=%d", w1.Balance, w2.Balance)
	}
	assertLedgerInvariant(t, db, u1.ID)
	assertLedgerInvariant(t, db, u2.ID)
}
