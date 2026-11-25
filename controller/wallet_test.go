package controller_test

import (
	"context"
	"sync"
	"testing"
	"time"
)

type walletView struct {
	Balance         int64   `json:"balance"`
	Debt            int64   `json:"debt"`
	CanClaim        bool    `json:"can_claim"`
	CanLoan         bool    `json:"can_loan"`
	InTables        int64   `json:"in_tables"`
	LoanAvailableAt *string `json:"loan_available_at"`
	Streak          int     `json:"streak"`
}

// testClock: đồng hồ điều khiển được cho store (dùng chung cho request song song).
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (e *env) clock(start time.Time) *testClock {
	c := &testClock{now: start}
	e.st.SetClock(func() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now })
	return c
}

func (c *testClock) advance(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

func (e *env) wallet(token string) walletView {
	e.t.Helper()
	var w walletView
	if code := e.do("GET", "/api/wallet", token, nil, &w); code != 200 {
		e.t.Fatalf("get wallet: %d", code)
	}
	return w
}

func TestWalletAPI(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	if w := e.wallet(alice.Token); w.Balance != 1000 || w.Debt != 0 || !w.CanClaim || w.CanLoan ||
		w.Streak != 0 || w.LoanAvailableAt != nil {
		t.Fatalf("new wallet: %+v", w)
	}
	wa := e.dial(alice.Token)
	wa.waitFor("ready")
	if code := e.do("POST", "/api/wallet/claim", alice.Token, nil, nil); code != 200 {
		t.Fatalf("claim: %d", code)
	}
	if ev := wa.waitFor("wallet.updated"); ev["balance"] != float64(1100) {
		t.Fatalf("wallet.updated: %v", ev)
	}
	if code := e.do("POST", "/api/wallet/claim", alice.Token, nil, nil); code != 409 {
		t.Fatalf("claim twice: %d", code)
	}
	if w := e.wallet(alice.Token); w.Streak != 1 || w.CanClaim {
		t.Fatalf("after claim: %+v", w)
	}
	if code := e.do("POST", "/api/wallet/loan", alice.Token, nil, nil); code != 400 {
		t.Fatalf("loan while rich: %d", code)
	}

	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]string{"name": "song-bac"}, &ch)
	if code := e.do("GET", "/api/channels/"+itoa(ch.ID)+"/wallet-board", bob.Token, nil, nil); code != 403 {
		t.Fatalf("board for outsider: %d", code)
	}
	var board struct {
		Rich    []map[string]any `json:"rich"`
		Debtors []map[string]any `json:"debtors"`
	}
	if code := e.do("GET", "/api/channels/"+itoa(ch.ID)+"/wallet-board", alice.Token, nil, &board); code != 200 ||
		len(board.Rich) != 1 || board.Debtors == nil {
		t.Fatalf("board: %d %+v", code, board)
	}
}

func TestWalletLoanRulesAPI(t *testing.T) {
	e := newEnv(t)
	clk := e.clock(time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)) // 10:00 ngày 20/11 giờ VN
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	e.do("POST", "/api/wallet/transfer", alice.Token, map[string]any{"to_user_id": bob.User.ID, "amount": 950}, nil)
	if w := e.wallet(alice.Token); !w.CanLoan || w.LoanAvailableAt != nil {
		t.Fatalf("poor wallet: %+v", w)
	}
	var w walletView
	if code := e.do("POST", "/api/wallet/loan", alice.Token, nil, &w); code != 200 || w.Balance != 350 || w.Debt != 360 ||
		w.CanLoan || w.LoanAvailableAt == nil || *w.LoanAvailableAt != "2026-11-23T03:00:00Z" {
		t.Fatalf("loan: %d %+v", code, w)
	}
	var errResp struct{ Error string }
	if code := e.do("POST", "/api/wallet/loan", alice.Token, nil, &errResp); code != 400 ||
		errResp.Error != "Bạn còn nợ, trả hết rồi mới vay tiếp được." {
		t.Fatalf("loan in debt: %d %q", code, errResp.Error)
	}
	e.do("POST", "/api/wallet/transfer", bob.Token, map[string]any{"to_user_id": alice.User.ID, "amount": 100}, nil)
	e.do("POST", "/api/wallet/repay", alice.Token, nil, nil) // 90 / 0
	clk.advance(48 * time.Hour)
	if code := e.do("POST", "/api/wallet/loan", alice.Token, nil, &errResp); code != 400 ||
		errResp.Error != "Mỗi 3 ngày chỉ vay được một lần. Vay lại được lúc 10:00 ngày 23/11." {
		t.Fatalf("loan in cooldown: %d %q", code, errResp.Error)
	}
	clk.advance(24 * time.Hour)
	if w := e.wallet(alice.Token); !w.CanLoan || w.LoanAvailableAt != nil {
		t.Fatalf("after cooldown: %+v", w)
	}
	if code := e.do("POST", "/api/wallet/loan", alice.Token, nil, &w); code != 200 || w.Balance != 390 || w.Debt != 360 {
		t.Fatalf("loan after cooldown: %d %+v", code, w)
	}

	// điểm danh hai ngày liền: 100 rồi 120, đang nợ nên nửa số đó trả nợ
	if code := e.do("POST", "/api/wallet/claim", alice.Token, nil, &w); code != 200 || w.Balance != 440 || w.Debt != 310 || w.Streak != 1 {
		t.Fatalf("claim day 1: %d %+v", code, w)
	}
	clk.advance(24 * time.Hour)
	if w := e.wallet(alice.Token); w.Streak != 1 || !w.CanClaim {
		t.Fatalf("next day before claim: %+v", w)
	}
	if code := e.do("POST", "/api/wallet/claim", alice.Token, nil, &w); code != 200 || w.Balance != 500 || w.Debt != 250 || w.Streak != 2 {
		t.Fatalf("claim day 2: %d %+v", code, w)
	}
	clk.advance(48 * time.Hour)
	if w := e.wallet(alice.Token); w.Streak != 0 {
		t.Fatalf("broken streak: %+v", w)
	}
}

func TestBauCuaMoneyFlow(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]string{"name": "song-bac"}, &ch)
	e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", bob.Token, nil, nil)
	wb := e.dial(bob.Token)
	wb.waitFor("ready")

	open := func() int64 {
		t.Helper()
		var m struct{ ID int64 }
		if code := e.do("POST", "/api/channels/"+itoa(ch.ID)+"/games", alice.Token, map[string]any{"kind": "baucua"}, &m); code != 201 {
			t.Fatalf("create baucua: %d", code)
		}
		return m.ID
	}
	act := func(token string, id int64, action string, data any) int {
		body := map[string]any{"action": action}
		if data != nil {
			body["data"] = data
		}
		return e.do("POST", "/api/messages/"+itoa(id)+"/game", token, body, nil)
	}
	bal := func(token string, want int64) {
		t.Helper()
		if w := e.wallet(token); w.Balance != want {
			t.Fatalf("balance = %d, want %d (%+v)", w.Balance, want, w)
		}
	}

	// chủ phòng mang hết ví vào bàn; huỷ phòng thì hoàn lại
	g1 := open()
	if w := e.wallet(alice.Token); w.Balance != 0 || w.InTables != 1000 {
		t.Fatalf("host buy-in: %+v", w)
	}
	if code := act(bob.Token, g1, "join", nil); code != 200 {
		t.Fatalf("join: %d", code)
	}
	if ev := wb.waitFor("wallet.updated"); ev["balance"] != float64(0) {
		t.Fatalf("bob wallet after join: %v", ev)
	}
	if code := act(bob.Token, g1, "join", nil); code != 400 {
		t.Fatalf("join twice: %d", code)
	}
	bal(bob.Token, 0)
	if code := act(bob.Token, g1, "leave", nil); code != 200 {
		t.Fatalf("leave: %d", code)
	}
	bal(bob.Token, 1000)
	act(bob.Token, g1, "join", nil)
	bal(bob.Token, 0)
	act(alice.Token, g1, "cancel", nil)
	bal(alice.Token, 1000)
	bal(bob.Token, 1000)

	// xoá tin giữa lượt có cược: mọi chip về ví
	g2 := open()
	act(bob.Token, g2, "join", nil)
	act(alice.Token, g2, "start", nil)
	if code := act(bob.Token, g2, "bet", map[string]any{"symbol": "bau", "amount": 100}); code != 200 {
		t.Fatalf("bet: %d", code)
	}
	if code := e.do("DELETE", "/api/messages/"+itoa(g2), alice.Token, nil, nil); code != 200 {
		t.Fatalf("delete game message: %d", code)
	}
	bal(alice.Token, 1000)
	bal(bob.Token, 1000)

	// ví 0 xu vẫn vào bàn được, vay trong bàn; nợ vào ví khi hết ván
	g3 := open()
	g4 := open() // alice đã mang hết xu vào g3: vào g4 với 0 chip, bob làm cái
	act(bob.Token, g4, "join", nil)
	act(alice.Token, g4, "start", nil)
	if code := act(alice.Token, g4, "loan", nil); code != 200 {
		t.Fatalf("loan in table: %d", code)
	}
	act(alice.Token, g4, "cancel", nil)
	act(alice.Token, g3, "cancel", nil)
	if w := e.wallet(alice.Token); w.Balance != 1500 || w.Debt != 500 {
		t.Fatalf("alice after loan: %+v", w)
	}
	e.do("POST", "/api/wallet/repay", alice.Token, nil, nil)

	// ván đã kết thúc mà chưa trả chip (máy chủ tắt giữa chừng): khởi động trả bù đúng một lần
	g5 := open()
	row, err := e.st.GetGame(context.Background(), g5)
	if err != nil {
		t.Fatal(err)
	}
	e.st.SaveGame(context.Background(), g5, "ended", row.State)
	e.api.SettlePendingCashoutsForTest(context.Background())
	e.api.SettlePendingCashoutsForTest(context.Background())
	bal(alice.Token, 1000)

	// chơi được trong tin nhắn riêng
	var dm channel
	e.do("POST", "/api/dms", alice.Token, map[string]any{"user_id": bob.User.ID}, &dm)
	if code := e.do("POST", "/api/channels/"+itoa(dm.ID)+"/games", alice.Token, map[string]any{"kind": "baucua"}, nil); code != 201 {
		t.Fatalf("baucua in DM: %d", code)
	}
}

func TestBauCuaChipsSurviveChannelDeleteAndCancelledRequest(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	newTable := func(name string) (int64, int64) {
		var ch channel
		e.do("POST", "/api/channels", alice.Token, map[string]string{"name": name}, &ch)
		e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", bob.Token, nil, nil)
		var m struct{ ID int64 }
		e.do("POST", "/api/channels/"+itoa(ch.ID)+"/games", alice.Token, map[string]any{"kind": "baucua"}, &m)
		e.do("POST", "/api/messages/"+itoa(m.ID)+"/game", bob.Token, map[string]any{"action": "join"}, nil)
		return ch.ID, m.ID
	}

	// xoá kênh đang có bàn: chip phải về ví trước khi ván bị xoá theo kênh
	chID, _ := newTable("ban-1")
	if code := e.do("DELETE", "/api/channels/"+itoa(chID), alice.Token, nil, nil); code != 204 {
		t.Fatalf("delete channel: %d", code)
	}
	if a, b := e.wallet(alice.Token), e.wallet(bob.Token); a.Balance != 1000 || b.Balance != 1000 {
		t.Fatalf("after channel delete: alice %+v bob %+v", a, b)
	}

	// huỷ ván trong request đã bị huỷ (đóng tab): vẫn phải trả chip
	_, gID := newTable("ban-2")
	if err := e.api.CancelGameWithCancelledContextForTest(gID, alice.User.ID); err != nil {
		t.Fatal(err)
	}
	if a, b := e.wallet(alice.Token), e.wallet(bob.Token); a.Balance != 1000 || b.Balance != 1000 {
		t.Fatalf("after cancelled-context cancel: alice %+v bob %+v", a, b)
	}
}

func TestWalletTransferHistoryAndXiangqiBetting(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	carol := e.register("carol", "Carol")

	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]any{"name": "co-tuong-cuoc"}, &ch)
	e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", bob.Token, nil, nil)
	e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", carol.Token, nil, nil)

	wb := e.dial(bob.Token)
	wb.waitFor("ready")

	// Transfer 600 xu from Carol to Bob (Carol left with 400, Bob 1600)
	if code := e.do("POST", "/api/wallet/transfer", carol.Token, map[string]any{
		"to_user_id": bob.User.ID,
		"amount":     600,
		"note":       "trả kèo",
	}, nil); code != 200 {
		t.Fatalf("transfer: %d", code)
	}
	if ev := wb.waitFor("wallet.updated"); ev["balance"] != float64(1600) {
		t.Fatalf("expected bob WS balance 1600, got %v", ev)
	}

	// Self transfer & overdraft rejected
	if code := e.do("POST", "/api/wallet/transfer", carol.Token, map[string]any{
		"to_user_id": carol.User.ID,
		"amount":     100,
	}, nil); code != 400 {
		t.Fatalf("expected 400 self transfer, got %d", code)
	}
	if code := e.do("POST", "/api/wallet/transfer", carol.Token, map[string]any{
		"to_user_id": bob.User.ID,
		"amount":     5000,
	}, nil); code != 400 {
		t.Fatalf("expected 400 overdraft transfer, got %d", code)
	}

	// 1. Create Xiangqi exceeding balance fails
	if code := e.do("POST", "/api/channels/"+itoa(ch.ID)+"/games", alice.Token, map[string]any{
		"kind":    "xiangqi",
		"options": map[string]any{"bet_amount": 5000},
	}, nil); code != 400 {
		t.Fatalf("expected 400 creating bet exceeding balance, got %d", code)
	}

	// 2. Alice opens Xiangqi with 500 xu bet
	var m gameMsg
	if code := e.do("POST", "/api/channels/"+itoa(ch.ID)+"/games", alice.Token, map[string]any{
		"kind":    "xiangqi",
		"options": map[string]any{"bet_amount": 500, "first": "host"},
	}, &m); code != 201 {
		t.Fatalf("create bet xiangqi: %d %s", code, m.Error)
	}
	if w := e.wallet(alice.Token); w.Balance != 500 || w.InTables != 500 {
		t.Fatalf("alice locked mismatch: %+v", w)
	}

	// 3. Carol (400 xu) cannot join 500 xu table
	if code := e.do("POST", "/api/messages/"+itoa(m.ID)+"/game", carol.Token, map[string]any{"action": "join"}, nil); code != 400 {
		t.Fatalf("expected 400 for broke carol joining 500 bet, got %d", code)
	}

	// 4. Bob joins -> Playing, cancel during Playing rejected
	var g gameResp
	if code := e.do("POST", "/api/messages/"+itoa(m.ID)+"/game", bob.Token, map[string]any{"action": "join"}, &g); code != 200 || g.Status != "playing" {
		t.Fatalf("bob join: %d %+v", code, g)
	}
	if code := e.do("POST", "/api/messages/"+itoa(m.ID)+"/game", alice.Token, map[string]any{"action": "cancel"}, nil); code != 400 {
		t.Fatalf("cancel while playing must be 400, got %d", code)
	}

	// 5. Bob resigns -> Alice +1000 (1500 total), Bob 1100
	if code := e.do("POST", "/api/messages/"+itoa(m.ID)+"/game", bob.Token, map[string]any{"action": "resign"}, &g); code != 200 {
		t.Fatalf("bob resign: %d", code)
	}
	if wa := e.wallet(alice.Token); wa.Balance != 1500 || wa.InTables != 0 {
		t.Fatalf("alice post-win: %+v", wa)
	}
	if wbView := e.wallet(bob.Token); wbView.Balance != 1100 || wbView.InTables != 0 {
		t.Fatalf("bob post-loss: %+v", wbView)
	}

	// 6. Transaction history & Leaderboard net_profit
	var txList struct {
		Items []struct {
			Kind   string `json:"kind"`
			Amount int64  `json:"amount"`
		} `json:"items"`
	}
	if code := e.do("GET", "/api/wallet/transactions?limit=10", alice.Token, nil, &txList); code != 200 || len(txList.Items) < 2 {
		t.Fatalf("transactions: %d %+v", code, txList)
	}
	var lb struct {
		Members []struct {
			UserID    int64 `json:"user_id"`
			NetProfit int64 `json:"net_profit"`
		} `json:"members"`
	}
	if code := e.do("GET", "/api/games/leaderboard?kind=xiangqi", alice.Token, nil, &lb); code != 200 {
		t.Fatalf("leaderboard: %d", code)
	}
	profit := map[int64]int64{}
	for _, mem := range lb.Members {
		profit[mem.UserID] = mem.NetProfit
	}
	if profit[alice.User.ID] != 500 || profit[bob.User.ID] != -500 {
		t.Fatalf("unexpected net_profit: %+v", profit)
	}
}
