package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"chat/database"
	"chat/game"
	"chat/model"
	"chat/socket"
	"chat/store"
)

// playedXiangqi: ván cờ tướng 3 nước giữa a (Đỏ) và b, dạng đã lưu.
func playedXiangqi(t *testing.T, a, b model.User) []byte {
	t.Helper()
	now := time.Now()
	pa, pb := game.Player{ID: a.ID, Name: a.DisplayName}, game.Player{ID: b.ID, Name: b.DisplayName}
	g, err := game.New("xiangqi", pa, json.RawMessage(`{"first":"host"}`), now)
	if err != nil {
		t.Fatal(err)
	}
	must := func(p game.Player, action string, data string) {
		t.Helper()
		var raw json.RawMessage
		if data != "" {
			raw = json.RawMessage(data)
		}
		if err := g.Act(p, action, raw, now); err != nil {
			t.Fatalf("%s %s: %v", p.Name, action, err)
		}
	}
	must(pb, "join", "")
	must(pa, "move", `{"from":"h2","to":"e2"}`)
	must(pb, "move", `{"from":"h9","to":"g7"}`)
	must(pa, "move", `{"from":"e2","to":"e6"}`)
	state, err := g.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestAIGameTools(t *testing.T) {
	ctx := context.Background()
	db, err := database.InitDB(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(database.MigrationOpt{DB: db, Mode: database.ModeUp}); err != nil {
		t.Fatal(err)
	}
	st := store.New(db)
	hub := socket.NewHub()
	t.Cleanup(hub.Shutdown)
	api := NewAPI(st, hub, nil, t.TempDir())

	user := func(name string) model.User {
		u, err := st.CreateUser(ctx, name, name+"@t.local", "x", strings.ToUpper(name[:1])+name[1:])
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	alice, bob, carol := user("alice"), user("bob"), user("carol")
	here, _ := st.CreateChannel(ctx, alice.ID, "here", "", false)
	public, _ := st.CreateChannel(ctx, carol.ID, "public", "", false)
	secret, _ := st.CreateChannel(ctx, carol.ID, "secret", "", true)
	dm, _, err := st.OpenDM(ctx, bob.ID, carol.ID)
	if err != nil {
		t.Fatal(err)
	}

	newGame := func(ch model.Channel, a, b model.User) int64 {
		m, err := st.CreateGame(ctx, ch.ID, a.ID, model.Embed{Type: "game", ID: "xiangqi", Title: "Cờ tướng"}, "playing", playedXiangqi(t, a, b))
		if err != nil {
			t.Fatal(err)
		}
		return m.ID
	}
	gHere := newGame(here, alice, bob)
	gPublic := newGame(public, carol, bob)
	gSecret := newGame(secret, carol, bob)
	gDM := newGame(dm, bob, carol)

	r := &agentRun{a: api, ch: here, asker: alice, status: func(string) {}}
	has := func(out string, id int64) bool { return strings.Contains(out, fmt.Sprintf("#%d ", id)) }

	// danh sách: kênh đang hỏi + kênh công khai, không có kênh riêng tư / tin nhắn riêng
	out := r.listGames(ctx, "", "", "", "", 0)
	if !has(out, gHere) || !has(out, gPublic) || has(out, gSecret) || has(out, gDM) {
		t.Fatalf("visibility:\n%s", out)
	}
	if !strings.Contains(out, "2 nước") || !strings.Contains(out, "#public") {
		t.Fatalf("summary line:\n%s", out)
	}
	// lọc theo người chơi / đối thủ
	out = r.listGames(ctx, "carol", "", "xiangqi", "playing", 5)
	if has(out, gHere) || !has(out, gPublic) {
		t.Fatalf("filter player:\n%s", out)
	}
	out = r.listGames(ctx, "tôi", "bob", "", "", 0)
	if !has(out, gHere) || has(out, gPublic) {
		t.Fatalf("filter me vs bob:\n%s", out)
	}
	if out := r.listGames(ctx, "", "", "bowling", "", 0); !strings.HasPrefix(out, "Lỗi") {
		t.Fatalf("bad kind: %s", out)
	}

	// nước đi
	out = r.gameMoves(ctx, float64(gPublic))
	for _, want := range []string{"Carol (Đỏ) vs Bob (Đen)", "1. Đỏ: Pháo h2→e2 | Đen: Mã h9→g7", "2. Đỏ: Pháo e2→e6 ăn Tốt", "Thế cờ hiện tại", "Lượt đi: Đen"} {
		if !strings.Contains(out, want) {
			t.Fatalf("moves missing %q:\n%s", want, out)
		}
	}
	for _, id := range []int64{gSecret, gDM} {
		if out := r.gameMoves(ctx, float64(id)); !strings.Contains(out, "không xem được") {
			t.Fatalf("private game %d readable:\n%s", id, out)
		}
	}
	if out := r.gameMoves(ctx, 999999); !strings.Contains(out, "Không có ván") {
		t.Fatalf("missing game: %s", out)
	}
}
