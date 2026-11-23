package controller_test

import "testing"

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
