package controller_test

import (
	"context"
	"testing"
)

type walletView struct {
	Balance  int64 `json:"balance"`
	Debt     int64 `json:"debt"`
	CanClaim bool  `json:"can_claim"`
	CanLoan  bool  `json:"can_loan"`
	InTables int64 `json:"in_tables"`
}

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
	if w := e.wallet(alice.Token); w.Balance != 1000 || w.Debt != 0 || !w.CanClaim || w.CanLoan {
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

func TestRPSBettingMoneyFlow(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]string{"name": "oantuti"}, &ch)
	e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", bob.Token, nil, nil)
	wa := e.dial(alice.Token)
	wa.waitFor("ready")
	wb := e.dial(bob.Token)
	wb.waitFor("ready")

	createRPS := func(token string, bestOf int, bet int64) (int, int64) {
		var m struct{ ID int64 }
		code := e.do("POST", "/api/channels/"+itoa(ch.ID)+"/games", token, map[string]any{
			"kind":    "rps",
			"options": map[string]any{"best_of": bestOf, "bet": bet},
		}, &m)
		return code, m.ID
	}
	act := func(token string, id int64, action string, data any) int {
		body := map[string]any{"action": action}
		if data != nil {
			body["data"] = data
		}
		return e.do("POST", "/api/messages/"+itoa(id)+"/game", token, body, nil)
	}

	// Mức cược không hợp lệ bị từ chối.
	if code, _ := createRPS(alice.Token, 3, 75); code != 400 {
		t.Fatalf("invalid bet code = %d, want 400", code)
	}

	// Alice mở phòng cược 500 -> ví còn 500, in_tables = 500.
	code, g1 := createRPS(alice.Token, 1, 500)
	if code != 201 {
		t.Fatalf("create rps 500: %d", code)
	}
	if w := e.wallet(alice.Token); w.Balance != 500 || w.InTables != 500 {
		t.Fatalf("alice wallet after create: %+v", w)
	}
	// Alice mở thêm phòng cược 1000 khi chỉ còn 500 -> bị từ chối 400.
	if code, _ := createRPS(alice.Token, 1, 1000); code != 400 {
		t.Fatalf("create rps 1000 without enough balance: %d", code)
	}

	// Bob khoá 500 xu vào phòng riêng của mình -> Bob còn 500, rồi thử vào phòng 1000 của Alice (sau khi Alice huỷ g1 và mở 1000).
	if code := act(alice.Token, g1, "cancel", nil); code != 200 {
		t.Fatalf("cancel lobby g1: %d", code)
	}
	if w := e.wallet(alice.Token); w.Balance != 1000 || w.InTables != 0 {
		t.Fatalf("alice after lobby cancel: %+v", w)
	}
	_, gBob := createRPS(bob.Token, 1, 500)
	_, gAlice := createRPS(alice.Token, 1, 1000)

	// Bob chỉ còn 500 xu, bấm join phòng 1000 xu -> bị từ chối 400, ví Bob giữ nguyên 500.
	if code := act(bob.Token, gAlice, "join", nil); code != 400 {
		t.Fatalf("bob join 1000 with 500 balance: %d", code)
	}
	if w := e.wallet(bob.Token); w.Balance != 500 || w.InTables != 500 {
		t.Fatalf("bob wallet unchanged after rejected join: %+v", w)
	}

	// Bob huỷ phòng 500 -> đủ 1000 xu -> vào phòng 1000 của Alice thành công.
	act(bob.Token, gBob, "cancel", nil)
	if code := act(bob.Token, gAlice, "join", nil); code != 200 {
		t.Fatalf("bob join 1000: %d", code)
	}
	if a, b := e.wallet(alice.Token), e.wallet(bob.Token); a.Balance != 0 || b.Balance != 0 || a.InTables != 1000 || b.InTables != 1000 {
		t.Fatalf("wallets while playing: alice=%+v bob=%+v", a, b)
	}

	// Không cho chủ phòng huỷ ván khi đã đang Playing.
	if code := act(alice.Token, gAlice, "cancel", nil); code != 400 {
		t.Fatalf("cancel while playing: %d", code)
	}

	// Alice thắng 1-0 -> Alice nhận 2000 xu, Bob còn 0 xu.
	act(alice.Token, gAlice, "pick", map[string]string{"choice": "rock"})
	act(bob.Token, gAlice, "pick", map[string]string{"choice": "scissors"})
	if a, b := e.wallet(alice.Token), e.wallet(bob.Token); a.Balance != 2000 || b.Balance != 0 || a.InTables != 0 || b.InTables != 0 {
		t.Fatalf("wallets after alice win: alice=%+v bob=%+v", a, b)
	}

	// Chơi ván nữa (100 xu) rồi xoá tin nhắn giữa trận -> hoàn 100 xu cho cả hai.
	// Bob vay 500 trước để đủ 100 xu.
	if code := e.do("POST", "/api/wallet/loan", bob.Token, nil, nil); code != 200 {
		t.Fatalf("bob loan: %d", code)
	}
	_, gDel := createRPS(alice.Token, 3, 100)
	act(bob.Token, gDel, "join", nil)
	if code := e.do("DELETE", "/api/messages/"+itoa(gDel), alice.Token, nil, nil); code != 200 {
		t.Fatalf("delete rps message: %d", code)
	}
	if a, b := e.wallet(alice.Token), e.wallet(bob.Token); a.Balance != 2000 || b.Balance != 500 {
		t.Fatalf("wallets after delete refund: alice=%+v bob=%+v", a, b)
	}

	// Giả lập server restart khi có ván rps cược kết thúc chưa kịp trả tiền -> settlePendingCashouts trả bù 1 lần.
	_, gCrash := createRPS(alice.Token, 1, 200)
	row, err := e.st.GetGame(context.Background(), gCrash)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.SaveGame(context.Background(), gCrash, "ended", row.State); err != nil {
		t.Fatal(err)
	}
	e.api.SettlePendingCashoutsForTest(context.Background())
	e.api.SettlePendingCashoutsForTest(context.Background())
	if a := e.wallet(alice.Token); a.Balance != 2000 {
		t.Fatalf("alice after restart cashout = %d, want 2000", a.Balance)
	}
}
