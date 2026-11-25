package controller_test

import (
	"testing"
)

type walletResp struct {
	UserID        int64 `json:"user_id"`
	Balance       int64 `json:"balance"`
	LockedBalance int64 `json:"locked_balance"`
	CanCheckin    bool  `json:"can_checkin"`
	CanBailout    bool  `json:"can_bailout"`
}

type walletActionResp struct {
	Wallet walletResp `json:"wallet"`
	Error  string     `json:"error"`
}

type walletTxListResp struct {
	Items []struct {
		ID           int64  `json:"id"`
		Type         string `json:"type"`
		Amount       int64  `json:"amount"`
		BalanceAfter int64  `json:"balance_after"`
		Note         string `json:"note"`
	} `json:"items"`
	HasMore bool `json:"has_more"`
}

func TestWalletEndpointsAndWebSocket(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")

	wa := e.dial(alice.Token)
	wb := e.dial(bob.Token)
	_ = wa

	// Check initial wallet
	var w walletResp
	if code := e.do("GET", "/api/wallet", alice.Token, nil, &w); code != 200 {
		t.Fatalf("get wallet: %d", code)
	}
	if w.Balance != 10000 || w.LockedBalance != 0 || !w.CanCheckin || w.CanBailout {
		t.Fatalf("unexpected initial wallet: %+v", w)
	}

	// Checkin +2000
	var actResp walletActionResp
	if code := e.do("POST", "/api/wallet/checkin", alice.Token, map[string]any{}, &actResp); code != 200 {
		t.Fatalf("checkin: %d %s", code, actResp.Error)
	}
	if actResp.Wallet.Balance != 12000 || actResp.Wallet.CanCheckin {
		t.Fatalf("after checkin: %+v", actResp.Wallet)
	}
	if code := e.do("POST", "/api/wallet/checkin", alice.Token, map[string]any{}, &actResp); code != 400 {
		t.Fatalf("duplicate checkin should be 400, got %d", code)
	}

	// Bailout not eligible when balance >= 500
	if code := e.do("POST", "/api/wallet/bailout", alice.Token, map[string]any{}, &actResp); code != 400 {
		t.Fatalf("bailout when rich should be 400, got %d", code)
	}

	// Transfer 11700 from Alice to Bob (Alice left with 300 -> eligible for bailout)
	if code := e.do("POST", "/api/wallet/transfer", alice.Token, map[string]any{
		"to_user_id": bob.User.ID,
		"amount":     11700,
		"note":       "trả kèo",
	}, &actResp); code != 200 {
		t.Fatalf("transfer: %d %s", code, actResp.Error)
	}
	if actResp.Wallet.Balance != 300 || !actResp.Wallet.CanBailout {
		t.Fatalf("alice after transfer: %+v", actResp.Wallet)
	}

	// Bob receives wallet.updated via WS
	ev := wb.waitFor("wallet.updated")
	bw := ev["wallet"].(map[string]any)
	if int64(bw["balance"].(float64)) != 21700 {
		t.Fatalf("expected bob balance 21700 via WS, got %v", bw["balance"])
	}

	// Alice claims bailout +1000
	if code := e.do("POST", "/api/wallet/bailout", alice.Token, map[string]any{}, &actResp); code != 200 {
		t.Fatalf("bailout: %d %s", code, actResp.Error)
	}
	if actResp.Wallet.Balance != 1300 {
		t.Fatalf("expected alice balance 1300 after bailout, got %d", actResp.Wallet.Balance)
	}

	// Transaction history check
	var txList walletTxListResp
	if code := e.do("GET", "/api/wallet/transactions?limit=2", alice.Token, nil, &txList); code != 200 {
		t.Fatalf("list transactions: %d", code)
	}
	if len(txList.Items) != 2 || !txList.HasMore {
		t.Fatalf("expected 2 items + has_more=true, got %+v", txList)
	}
}

func TestXiangqiBettingEndToEndAndLeaderboard(t *testing.T) {
	e := newEnv(t)
	alice := e.register("alice", "Alice")
	bob := e.register("bob", "Bob")
	carol := e.register("carol", "Carol")

	var ch channel
	e.do("POST", "/api/channels", alice.Token, map[string]any{"name": "co-tuong-cuoc"}, &ch)
	e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", bob.Token, nil, nil)
	e.do("POST", "/api/channels/"+itoa(ch.ID)+"/join", carol.Token, nil, nil)

	// Drain Carol's balance down to 500 so she cannot join a 2000 bet game
	if code := e.do("POST", "/api/wallet/transfer", carol.Token, map[string]any{
		"to_user_id": bob.User.ID,
		"amount":     9500,
	}, nil); code != 200 {
		t.Fatalf("drain carol failed: %d", code)
	}

	// 1. Create Xiangqi exceeding balance fails
	if code := e.do("POST", "/api/channels/"+itoa(ch.ID)+"/games", alice.Token, map[string]any{
		"kind":    "xiangqi",
		"options": map[string]any{"bet_amount": 50000, "first": "host"},
	}, nil); code != 400 {
		t.Fatalf("expected 400 creating bet exceeding balance, got %d", code)
	}

	// 2. Alice opens Xiangqi with 2000 Xu bet
	var m gameMsg
	if code := e.do("POST", "/api/channels/"+itoa(ch.ID)+"/games", alice.Token, map[string]any{
		"kind":    "xiangqi",
		"options": map[string]any{"bet_amount": 2000, "first": "host"},
	}, &m); code != 201 {
		t.Fatalf("create bet xiangqi: %d %s", code, m.Error)
	}

	var wa walletResp
	e.do("GET", "/api/wallet", alice.Token, nil, &wa)
	if wa.Balance != 8000 || wa.LockedBalance != 2000 {
		t.Fatalf("alice locked mismatch: %+v", wa)
	}

	// 3. Carol (500 Xu) cannot join 2000 Xu table
	if code := e.do("POST", "/api/messages/"+itoa(m.ID)+"/game", carol.Token, map[string]any{"action": "join"}, nil); code != 400 {
		t.Fatalf("expected 400 for broke carol joining 2000 bet, got %d", code)
	}

	// 4. Bob joins -> Playing, cancel during Playing rejected
	var g gameResp
	if code := e.do("POST", "/api/messages/"+itoa(m.ID)+"/game", bob.Token, map[string]any{"action": "join"}, &g); code != 200 {
		t.Fatalf("bob join failed: %d %s", code, g.Error)
	}
	if g.Status != "playing" {
		t.Fatalf("expected playing, got %s", g.Status)
	}
	if code := e.do("POST", "/api/messages/"+itoa(m.ID)+"/game", alice.Token, map[string]any{"action": "cancel"}, nil); code != 400 {
		t.Fatalf("cancel while playing must be 400, got %d", code)
	}

	// 5. Bob resigns -> Alice wins +4000 (balance 12000), Bob loses 2000 (balance 17500)
	if code := e.do("POST", "/api/messages/"+itoa(m.ID)+"/game", bob.Token, map[string]any{"action": "resign"}, &g); code != 200 {
		t.Fatalf("bob resign: %d %s", code, g.Error)
	}
	e.do("GET", "/api/wallet", alice.Token, nil, &wa)
	var wb walletResp
	e.do("GET", "/api/wallet", bob.Token, nil, &wb)
	if wa.Balance != 12000 || wa.LockedBalance != 0 {
		t.Fatalf("alice post-win wallet: %+v", wa)
	}
	if wb.Balance != 17500 || wb.LockedBalance != 0 {
		t.Fatalf("bob post-loss wallet: %+v", wb)
	}

	// 6. Match 2: Alice opens 1000 Xu bet in lobby, then cancels -> refunded
	var m2 gameMsg
	if code := e.do("POST", "/api/channels/"+itoa(ch.ID)+"/games", alice.Token, map[string]any{
		"kind":    "xiangqi",
		"options": map[string]any{"bet_amount": 1000},
	}, &m2); code != 201 {
		t.Fatalf("create match 2: %d", code)
	}
	if code := e.do("POST", "/api/messages/"+itoa(m2.ID)+"/game", alice.Token, map[string]any{"action": "cancel"}, nil); code != 200 {
		t.Fatalf("cancel lobby match 2: %d", code)
	}
	e.do("GET", "/api/wallet", alice.Token, nil, &wa)
	if wa.Balance != 12000 || wa.LockedBalance != 0 {
		t.Fatalf("alice post-cancel wallet: %+v", wa)
	}

	// 7. Match 3: Alice opens 1000 Xu bet, Bob joins, then draw_offer + draw_accept -> refunded
	var m3 gameMsg
	if code := e.do("POST", "/api/channels/"+itoa(ch.ID)+"/games", alice.Token, map[string]any{
		"kind":    "xiangqi",
		"options": map[string]any{"bet_amount": 1000},
	}, &m3); code != 201 {
		t.Fatalf("create match 3: %d", code)
	}
	e.do("POST", "/api/messages/"+itoa(m3.ID)+"/game", bob.Token, map[string]any{"action": "join"}, nil)
	e.do("POST", "/api/messages/"+itoa(m3.ID)+"/game", alice.Token, map[string]any{"action": "draw_offer"}, nil)
	e.do("POST", "/api/messages/"+itoa(m3.ID)+"/game", bob.Token, map[string]any{"action": "draw_accept"}, nil)
	e.do("GET", "/api/wallet", alice.Token, nil, &wa)
	e.do("GET", "/api/wallet", bob.Token, nil, &wb)
	if wa.Balance != 12000 || wb.Balance != 17500 {
		t.Fatalf("post-draw balances: alice=%d bob=%d", wa.Balance, wb.Balance)
	}

	// 8. Leaderboard includes net_profit and top_richest
	var lb struct {
		TopRichest []struct {
			UserID  int64 `json:"user_id"`
			Balance int64 `json:"balance"`
		} `json:"top_richest"`
		Members []struct {
			UserID    int64 `json:"user_id"`
			NetProfit int64 `json:"net_profit"`
		} `json:"members"`
	}
	if code := e.do("GET", "/api/games/leaderboard?kind=xiangqi", alice.Token, nil, &lb); code != 200 {
		t.Fatalf("leaderboard: %d", code)
	}
	if len(lb.TopRichest) == 0 || lb.TopRichest[0].UserID != bob.User.ID {
		t.Fatalf("expected bob richest in top_richest, got %+v", lb.TopRichest)
	}
	profitByUser := map[int64]int64{}
	for _, mem := range lb.Members {
		profitByUser[mem.UserID] = mem.NetProfit
	}
	if profitByUser[alice.User.ID] != 2000 || profitByUser[bob.User.ID] != -2000 {
		t.Fatalf("unexpected net_profit map: %+v", profitByUser)
	}
}
