// Package game: luật chơi các trò chơi nhỏ trong kênh chat (Ma Sói, Ai là gián điệp,
// cờ caro, oẳn tù tì). Không phụ thuộc HTTP/DB: controller giữ ván trong bộ nhớ,
// lưu Marshal() vào DB sau mỗi thay đổi và phát View() cho từng người.
//
// Mọi thay đổi đi qua Act (người chơi bấm) hoặc Tick (hết giờ). Hai hàm này không an toàn
// khi gọi đồng thời: người gọi phải khoá ván.
package game

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"time"
)

type Status string

const (
	Lobby   Status = "lobby"   // đang chờ người vào
	Playing Status = "playing" // đang chơi
	Ended   Status = "ended"   // đã xong hoặc bị huỷ
)

// Phòng chờ không ai bắt đầu sau chừng này thì tự huỷ.
const lobbyTimeout = 30 * time.Minute

// Player: người tham gia ván, tên chụp lại lúc vào (để nhật ký vẫn đúng khi đổi tên).
type Player struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Error: lỗi hiển thị thẳng cho người chơi (nước đi sai luật, chưa tới lượt...).
type Error string

func (e Error) Error() string { return string(e) }

// Base: phần chung của mọi trò.
type Base struct {
	Kind     string     `json:"kind"`
	HostID   int64      `json:"host_id"`
	Players  []Player   `json:"players"`
	Status   Status     `json:"status"`
	Deadline *time.Time `json:"deadline,omitempty"`
	Log      []string   `json:"log"`
	Result   string     `json:"result,omitempty"`
	Winners  []int64    `json:"winners,omitempty"`
	Version  int        `json:"version"`
}

func (b *Base) base() *Base { return b }

func (b *Base) isPlayer(id int64) bool { return b.index(id) >= 0 }

func (b *Base) index(id int64) int {
	for i, p := range b.Players {
		if p.ID == id {
			return i
		}
	}
	return -1
}

func (b *Base) name(id int64) string {
	if i := b.index(id); i >= 0 {
		return b.Players[i].Name
	}
	return "?"
}

func (b *Base) logf(format string, args ...any) {
	b.Log = append(b.Log, fmt.Sprintf(format, args...))
	if len(b.Log) > 200 {
		b.Log = b.Log[len(b.Log)-200:]
	}
}

func (b *Base) setDeadline(now time.Time, d time.Duration) {
	t := now.Add(d).UTC().Truncate(time.Second)
	b.Deadline = &t
}

func (b *Base) end(result string, winners []int64) {
	b.Status = Ended
	b.Deadline = nil
	b.Result = result
	b.Winners = winners
}

// rules: phần riêng của từng trò.
type rules interface {
	base() *Base
	limits() (min, max int)
	// autoStart: tự bắt đầu khi đủ max người (trò 2 người).
	autoStart() bool
	start(now time.Time) error
	act(p int64, action string, data json.RawMessage, now time.Time) error
	// tick: đã qua Deadline (đang chơi). Trả về false nếu không có gì thay đổi.
	tick(now time.Time) bool
	// view: trạng thái công khai và phần riêng của viewer (nil nếu không có).
	view(viewer int64) (state any, me any)
	// options: con trỏ tới tuỳ chọn của trò (nil nếu không có); normalize đưa về giá trị hợp lệ.
	options() any
	normalize()
}

// Info: thông tin hiển thị của một trò.
type Info struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
}

var titles = map[string]string{
	"station":  "Trạm Vũ Trụ",
	"werewolf": "Ma Sói",
	"spy":      "Ai là gián điệp",
	"caro":     "Cờ caro",
	"rps":      "Oẳn tù tì",
	"chess":    "Cờ vua",
	"xiangqi":  "Cờ tướng",
}

// Title: tên trò chơi để hiển thị ("" nếu không có trò này).
func Title(kind string) string { return titles[kind] }

func newRules(kind string) rules {
	switch kind {
	case "station":
		return &station{}
	case "werewolf":
		return &werewolf{}
	case "spy":
		return &spy{}
	case "caro":
		return &caro{}
	case "rps":
		return &rps{}
	case "chess":
		return &chess{}
	case "xiangqi":
		return &xiangqi{}
	}
	return nil
}

// Game: một ván chơi.
type Game struct {
	r rules
}

// New tạo ván mới, chủ phòng là người chơi đầu tiên. opts là JSON tuỳ chọn của từng trò (có thể rỗng).
func New(kind string, host Player, opts json.RawMessage, now time.Time) (*Game, error) {
	r := newRules(kind)
	if r == nil {
		return nil, Error("Không có trò chơi này.")
	}
	if o := r.options(); o != nil && len(opts) > 0 && string(opts) != "null" {
		if err := json.Unmarshal(opts, o); err != nil {
			return nil, Error("Tuỳ chọn không hợp lệ.")
		}
	}
	r.normalize()
	b := r.base()
	*b = Base{Kind: kind, HostID: host.ID, Players: []Player{host}, Status: Lobby, Log: []string{}, Version: 1}
	b.setDeadline(now, lobbyTimeout)
	return &Game{r: r}, nil
}

// Load đọc lại ván đã lưu bằng Marshal.
func Load(kind string, data []byte) (*Game, error) {
	r := newRules(kind)
	if r == nil {
		return nil, fmt.Errorf("game: unknown kind %q", kind)
	}
	if err := json.Unmarshal(data, r); err != nil {
		return nil, err
	}
	return &Game{r: r}, nil
}

func (g *Game) Marshal() ([]byte, error) { return json.Marshal(g.r) }

func (g *Game) Kind() string           { return g.r.base().Kind }
func (g *Game) Status() Status         { return g.r.base().Status }
func (g *Game) Deadline() *time.Time   { return g.r.base().Deadline }
func (g *Game) IsPlayer(id int64) bool { return g.r.base().isPlayer(id) }
func (g *Game) Version() int           { return g.r.base().Version }
func (g *Game) Players() []Player      { return append([]Player(nil), g.r.base().Players...) }
func (g *Game) Title() string          { return titles[g.Kind()] }
func (g *Game) Limits() (min, max int) { return g.r.limits() }
func (g *Game) Result() string         { return g.r.base().Result }
func (g *Game) Winners() []int64       { return append([]int64(nil), g.r.base().Winners...) }
func (g *Game) HostID() int64          { return g.r.base().HostID }
func (g *Game) touch()                 { g.r.base().Version++ }
func shuffle[T any](s []T)             { rand.Shuffle(len(s), func(i, j int) { s[i], s[j] = s[j], s[i] }) }
func pick[T any](s []T) T              { return s[rand.IntN(len(s))] }

// Act: p bấm một hành động. Hành động chung: join, leave, start, cancel; còn lại do từng trò xử lý.
// Lỗi kiểu Error là câu báo cho người chơi.
func (g *Game) Act(p Player, action string, data json.RawMessage, now time.Time) error {
	b := g.r.base()
	if b.Status == Ended {
		return Error("Ván này đã kết thúc.")
	}
	min, maxP := g.r.limits()
	switch action {
	case "join":
		if b.Status != Lobby {
			return Error("Ván đã bắt đầu, không vào được nữa.")
		}
		if b.isPlayer(p.ID) {
			return Error("Bạn đã ở trong phòng rồi.")
		}
		if len(b.Players) >= maxP {
			return Error(fmt.Sprintf("Phòng đã đủ %d người.", maxP))
		}
		b.Players = append(b.Players, p)
		if g.r.autoStart() && len(b.Players) == maxP {
			if err := g.begin(now); err != nil {
				return err
			}
		}
	case "leave":
		if b.Status != Lobby {
			return Error("Ván đã bắt đầu, không rời được.")
		}
		i := b.index(p.ID)
		if i < 0 {
			return Error("Bạn không ở trong phòng.")
		}
		if p.ID == b.HostID {
			return Error("Chủ phòng không rời được, hãy huỷ phòng.")
		}
		b.Players = append(b.Players[:i], b.Players[i+1:]...)
	case "start":
		if b.Status != Lobby {
			return Error("Ván đã bắt đầu rồi.")
		}
		if p.ID != b.HostID {
			return Error("Chỉ chủ phòng mới bắt đầu được.")
		}
		if len(b.Players) < min {
			return Error(fmt.Sprintf("Cần ít nhất %d người chơi.", min))
		}
		if err := g.begin(now); err != nil {
			return err
		}
	case "cancel":
		if p.ID != b.HostID {
			return Error("Chỉ chủ phòng mới huỷ được.")
		}
		b.logf("Chủ phòng đã huỷ ván.")
		b.end("Ván đã bị huỷ.", nil)
	default:
		if b.Status != Playing {
			return Error("Ván chưa bắt đầu.")
		}
		if !b.isPlayer(p.ID) {
			return Error("Bạn không chơi ván này.")
		}
		if err := g.r.act(p.ID, action, data, now); err != nil {
			return err
		}
	}
	g.touch()
	return nil
}

func (g *Game) begin(now time.Time) error {
	b := g.r.base()
	b.Status = Playing
	b.Deadline = nil
	if err := g.r.start(now); err != nil {
		b.Status = Lobby
		return err
	}
	return nil
}

// Tick xử lý hết giờ. Trả về true nếu ván thay đổi (cần lưu và phát lại).
func (g *Game) Tick(now time.Time) bool {
	b := g.r.base()
	if b.Status == Ended || b.Deadline == nil || now.Before(*b.Deadline) {
		return false
	}
	if b.Status == Lobby {
		b.logf("Hết thời gian chờ, phòng tự huỷ.")
		b.end("Không ai bắt đầu, phòng đã tự huỷ.", nil)
		g.touch()
		return true
	}
	if g.r.tick(now) {
		g.touch()
		return true
	}
	return false
}

// View: những gì viewer được thấy (viewer = 0: chỉ phần công khai).
type View struct {
	Kind       string     `json:"kind"`
	Title      string     `json:"title"`
	Status     Status     `json:"status"`
	HostID     int64      `json:"host_id"`
	Players    []Player   `json:"players"`
	MinPlayers int        `json:"min_players"`
	MaxPlayers int        `json:"max_players"`
	Deadline   *time.Time `json:"deadline,omitempty"`
	Log        []string   `json:"log"`
	Result     string     `json:"result,omitempty"`
	Winners    []int64    `json:"winners,omitempty"`
	Version    int        `json:"version"`
	State      any        `json:"state,omitempty"`
	Me         any        `json:"me,omitempty"`
}

func (g *Game) View(viewer int64) View {
	b := g.r.base()
	min, maxP := g.r.limits()
	v := View{
		Kind: b.Kind, Title: titles[b.Kind], Status: b.Status, HostID: b.HostID,
		Players: b.Players, MinPlayers: min, MaxPlayers: maxP, Deadline: b.Deadline,
		Log: b.Log, Result: b.Result, Winners: b.Winners, Version: b.Version,
	}
	if v.Players == nil {
		v.Players = []Player{}
	}
	if v.Log == nil {
		v.Log = []string{}
	}
	var me any
	v.State, me = g.r.view(viewer)
	if viewer != 0 && b.isPlayer(viewer) {
		v.Me = me
	}
	return v
}

// decode đọc data của một hành động vào dst.
func decode(data json.RawMessage, dst any) error {
	if len(data) == 0 {
		return Error("Thiếu dữ liệu.")
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return Error("Dữ liệu không hợp lệ.")
	}
	return nil
}

// tally đếm phiếu (0 = bỏ qua). Trả về người nhiều phiếu nhất nếu duy nhất và nhiều hơn số phiếu bỏ qua.
func tally(votes map[int64]int64) (target int64, count int, counts map[int64]int) {
	counts = map[int64]int{}
	for _, t := range votes {
		counts[t]++
	}
	best, tie := 0, false
	for t, c := range counts {
		if t == 0 {
			continue
		}
		switch {
		case c > best:
			best, target, tie = c, t, false
		case c == best:
			tie = true
		}
	}
	if tie || best == 0 || best <= counts[0] {
		return 0, best, counts
	}
	return target, best, counts
}

/* ---------- Trò thời gian thực (Trạm Vũ Trụ) ---------- */

// realtime: trò chạy liên tục, máy chủ gọi step nhiều lần mỗi giây và gửi frame riêng cho từng người.
type realtime interface {
	// step trả về true khi có thay đổi lớn (đổi pha, có người chết...) cần lưu và cập nhật thẻ trong kênh.
	step(now time.Time) bool
	frame(viewer int64, now time.Time) any
	move(p int64, x, y float64, now time.Time)
	mapData() any
}

// Realtime: trò cần vòng lặp nhanh và frame riêng (thay vì chỉ cập nhật khi có người bấm).
func (g *Game) Realtime() bool {
	_, ok := g.r.(realtime)
	return ok
}

// Step: chạy đồng hồ trò thời gian thực. true = có thay đổi lớn.
func (g *Game) Step(now time.Time) bool {
	rt, ok := g.r.(realtime)
	if !ok || g.Status() != Playing {
		return false
	}
	if rt.step(now) {
		g.touch()
		return true
	}
	return false
}

// Frame: những gì người chơi viewer thấy lúc này (nil nếu không phải người chơi).
func (g *Game) Frame(viewer int64, now time.Time) any {
	if rt, ok := g.r.(realtime); ok && g.Status() != Lobby {
		return rt.frame(viewer, now)
	}
	return nil
}

// Move: người chơi báo vị trí mới (máy chủ kiểm tra rồi mới nhận).
func (g *Game) Move(p int64, x, y float64, now time.Time) {
	if rt, ok := g.r.(realtime); ok {
		rt.move(p, x, y, now)
	}
}

// MapData: bản đồ tĩnh của trò thời gian thực.
func (g *Game) MapData() any {
	if rt, ok := g.r.(realtime); ok {
		return rt.mapData()
	}
	return nil
}
