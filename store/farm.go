package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"chat/model"
)

// Nông trại: trồng cây kiếm xu và XP, tưới / bắt sâu giúp hàng xóm, trộm rau, nhiệm vụ hằng ngày.
// Mọi luật chạy ở đây, mỗi hành động là một transaction (SQLite _txlock=immediate nên các hành động
// ghi nối tiếp nhau: không thu hoạch / trộm hai lần được). Trạng thái ô (đang lớn / chín / héo, tiến
// độ, sâu đã lộ) tính từ mốc thời gian lúc đọc. Thời điểm lưu bằng giây unix.

type Crop struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Emoji   string `json:"emoji"`
	Seed    int64  `json:"seed"`
	GrowSec int64  `json:"grow_sec"`
	Sell    int64  `json:"sell"`
	XP      int64  `json:"xp"`
	Level   int    `json:"level"`
}

// Crops: danh mục cây trồng (cố định).
var Crops = []Crop{
	{"rau_muong", "Rau muống", "🥬", 10, 5 * 60, 14, 2, 1},
	{"ca_rot", "Cà rốt", "🥕", 20, 15 * 60, 30, 4, 1},
	{"ca_chua", "Cà chua", "🍅", 35, 30 * 60, 56, 7, 2},
	{"ngo", "Ngô", "🌽", 50, 60 * 60, 85, 10, 3},
	{"lua", "Lúa", "🌾", 80, 2 * 3600, 145, 16, 4},
	{"dau_tay", "Dâu tây", "🍓", 120, 4 * 3600, 230, 25, 5},
	{"dua_hau", "Dưa hấu", "🍉", 200, 8 * 3600, 400, 40, 6},
	{"bi_ngo", "Bí ngô", "🎃", 300, 12 * 3600, 640, 60, 8},
}

func cropByID(id string) (Crop, bool) {
	for _, c := range Crops {
		if c.ID == id {
			return c, true
		}
	}
	return Crop{}, false
}

// LevelXP[n-1]: tổng XP cần để đạt cấp n (cấp 1..10).
var LevelXP = []int64{0, 30, 80, 160, 280, 450, 680, 1000, 1400, 1900}

const (
	FarmStartPlots   = 6
	FarmMaxPlots     = 20 // ô tối đa (bậc mở rộng cuối, xem ExpandPlots)
	FarmWitherAfter  = 12 * time.Hour
	FarmMaxWaters    = 3
	FarmMaxSteals    = 2 // mỗi vụ bị trộm tối đa
	FarmStealsPerDay = 5 // mỗi người trộm tối đa mỗi ngày (tính cả lần bị chó đuổi)
	FarmBugChance    = 0.25
	FarmDogChance    = 0.5
	FarmDogPrice     = 150
	FarmDogFine      = 10
	FarmDogDuration  = 24 * time.Hour
	FarmDogMax       = 72 * time.Hour
	FarmHelpWaterXu  = 2
	FarmHelpBugXu    = 3
	FarmHelpXP       = 1
	FarmChestXu      = 80
	FarmChestXP      = 20
	FarmLogKeep      = 50
)

func levelFor(xp int64) int {
	lv := 1
	for i := 1; i < len(LevelXP); i++ {
		if xp >= LevelXP[i] {
			lv = i + 1
		}
	}
	return lv
}

type questDef struct {
	ID, Title    string
	Goal, Xu, XP int64
}

// questDefs: kho nhiệm vụ hằng ngày; mỗi người mỗi ngày nhận 4 cái.
var questDefs = []questDef{
	{"harvest", "Thu hoạch 5 vụ", 5, 40, 10},
	{"plant", "Gieo 6 hạt", 6, 30, 8},
	{"plant_rare", "Trồng 2 cây từ cấp 3 trở lên", 2, 50, 12},
	{"water", "Tưới nước giúp 3 người hàng xóm", 3, 30, 8},
	{"bug", "Bắt 2 con sâu", 2, 30, 8},
	{"steal", "Trộm thành công 2 lần", 2, 25, 6},
	{"earn", "Bán hàng / giao đơn được 150 xu", 150, 50, 12},
	{"visit", "Ghé thăm 3 nông trại khác", 3, 20, 5},
	{"order", "Giao 2 đơn hàng", 2, 50, 12},
}

const (
	questsPerDay = 4
	chestQuestID = "chest"
	stealTryKey  = "steal_try" // bộ đếm lượt trộm trong ngày (không phải nhiệm vụ)
)

// dailyQuests: 4 nhiệm vụ của userID trong ngày day, chọn cố định theo băm (user, ngày, nhiệm vụ),
// giữ thứ tự trong kho.
func dailyQuests(userID int64, day string) []questDef {
	type ranked struct {
		i int
		h uint64
	}
	rs := make([]ranked, len(questDefs))
	for i, q := range questDefs {
		h := fnv.New64a()
		fmt.Fprintf(h, "%d|%s|%s", userID, day, q.ID)
		rs[i] = ranked{i, h.Sum64()}
	}
	sort.Slice(rs, func(a, b int) bool { return rs[a].h < rs[b].h })
	picked := make([]int, 0, questsPerDay)
	for _, r := range rs[:questsPerDay] {
		picked = append(picked, r.i)
	}
	sort.Ints(picked)
	out := make([]questDef, len(picked))
	for i, idx := range picked {
		out[i] = questDefs[idx]
	}
	return out
}

var vnZone = time.FixedZone("ICT", 7*3600)

// vnDay: ngày "YYYY-MM-DD" theo giờ VN.
func vnDay(t time.Time) string { return t.In(vnZone).Format("2006-01-02") }

/* ---------- Lỗi luật chơi ---------- */

// FarmError: vi phạm luật nông trại; Msg là câu tiếng Việt hiển thị thẳng cho người dùng.
type FarmError struct{ Msg string }

func (e *FarmError) Error() string { return e.Msg }

func farmErr(format string, a ...any) error { return &FarmError{Msg: fmt.Sprintf(format, a...)} }

var (
	ErrFarmBadPlot      = &FarmError{"Ô đất không hợp lệ."}
	ErrFarmUnknownCrop  = &FarmError{"Không có loại cây này."}
	ErrFarmPlotBusy     = &FarmError{"Ô này đang có cây rồi."}
	ErrFarmEmptyPlot    = &FarmError{"Ô này chưa trồng gì."}
	ErrFarmNotRipe      = &FarmError{"Cây chưa chín."}
	ErrFarmWithered     = &FarmError{"Cây đã héo, dọn ô rồi trồng lại nhé."}
	ErrFarmNothingRipe  = &FarmError{"Chưa có ô nào chín để thu hoạch."}
	ErrFarmNotWithered  = &FarmError{"Chỉ dọn được ô có cây đã héo."}
	ErrFarmNotGrowing   = &FarmError{"Chỉ tưới được cây đang lớn."}
	ErrFarmWaterMax     = &FarmError{"Vụ này đã được tưới đủ 3 lần."}
	ErrFarmWatered      = &FarmError{"Bạn đã tưới vụ này rồi."}
	ErrFarmNoBug        = &FarmError{"Cây này không có sâu."}
	ErrFarmOwnSteal     = &FarmError{"Không trộm được nông trại của chính mình."}
	ErrFarmStealMax     = &FarmError{"Vụ này đã bị trộm đủ 2 lần."}
	ErrFarmStolen       = &FarmError{"Bạn đã trộm vụ này rồi."}
	ErrFarmStealDaily   = &FarmError{"Hôm nay bạn đã trộm đủ 5 lần."}
	ErrFarmMaxPlots     = &FarmError{"Nông trại đã mở rộng tối đa."}
	ErrFarmDogMax       = &FarmError{"Chó đã canh nhà đủ 72 giờ rồi."}
	ErrFarmNoQuest      = &FarmError{"Hôm nay không có nhiệm vụ này."}
	ErrFarmQuestNotDone = &FarmError{"Nhiệm vụ chưa hoàn thành."}
	ErrFarmQuestClaimed = &FarmError{"Bạn đã nhận thưởng này rồi."}
)

/* ---------- Kiểu dữ liệu trả cho client ---------- */

type Plot struct {
	Index      int     `json:"index"`
	State      string  `json:"state"` // empty | growing | ripe | withered
	Crop       string  `json:"crop,omitempty"`
	PlantedAt  string  `json:"planted_at,omitempty"`
	ReadyAt    string  `json:"ready_at,omitempty"`
	WitherAt   string  `json:"wither_at,omitempty"`
	Progress   float64 `json:"progress"`
	Bug        bool    `json:"bug"`
	Waters     int64   `json:"waters"`
	Stolen     int64   `json:"stolen"`
	Steals     int64   `json:"steals"`
	CanWater   bool    `json:"can_water"`
	CanBug     bool    `json:"can_bug"`
	CanSteal   bool    `json:"can_steal"`
	CanHarvest bool    `json:"can_harvest"`
}

type NextPlot struct {
	Price int64 `json:"price"`
	Level int   `json:"level"`
}

type Quest struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Goal     int64  `json:"goal"`
	Progress int64  `json:"progress"`
	Xu       int64  `json:"xu"`
	XP       int64  `json:"xp"`
	Done     bool   `json:"done"`
	Claimed  bool   `json:"claimed"`
}

// Chest: rương thưởng ngày. Ready = đã xong cả 4 nhiệm vụ (kể cả khi đã nhận).
type Chest struct {
	Ready   bool  `json:"ready"`
	Claimed bool  `json:"claimed"`
	Xu      int64 `json:"xu"`
	XP      int64 `json:"xp"`
}

type LogEntry struct {
	At     time.Time        `json:"at"`
	Actor  model.PublicUser `json:"actor"`
	Action string           `json:"action"` // water | bug | steal | dog
	Crop   string           `json:"crop,omitempty"`
	Xu     int64            `json:"xu,omitempty"`
}

// Gained: kết quả hành động vừa làm. Xu là số xu kiếm được trước khi tự trừ nợ, không gồm thưởng
// lên cấp (20 × cấp mới, cộng riêng); âm khi bị chó đuổi. Items: số đơn vị vừa vào kho (thu hoạch,
// thu sản phẩm, trộm).
type Gained struct {
	Xu      int64            `json:"xu"`
	XP      int64            `json:"xp"`
	LevelUp int              `json:"level_up,omitempty"`
	Items   map[string]int64 `json:"items,omitempty"`
	Love    int64            `json:"love,omitempty"`    // điểm thân mật vừa cộng (kết đôi)
	LoveUp  int              `json:"love_up,omitempty"` // cấp thân mật mới nếu vừa lên cấp
}

type FarmView struct {
	User        model.PublicUser `json:"user"`
	Me          bool             `json:"me"`
	Level       int              `json:"level"`
	XP          int64            `json:"xp"`
	LevelXP     int64            `json:"level_xp"`
	NextLevelXP *int64           `json:"next_level_xp"`
	Plots       []Plot           `json:"plots"`
	PlotCount   int              `json:"plot_count"`
	MaxPlots    int              `json:"max_plots"`
	NextPlot    *NextPlot        `json:"next_plot"`
	DogUntil    *string          `json:"dog_until"`
	Crops       []Crop           `json:"crops"`
	Now         string           `json:"now"`
	Wallet      *Wallet          `json:"wallet,omitempty"`
	Quests      []Quest          `json:"quests,omitempty"`
	Chest       *Chest           `json:"chest,omitempty"`
	Log         *[]LogEntry      `json:"log,omitempty"`
	StealsLeft  int              `json:"steals_left"`
	Gained      *Gained          `json:"gained,omitempty"`
	Dog         *bool            `json:"dog,omitempty"`

	// Phần 2. items, storage, animal_kinds, orders chỉ có khi me; buildings, animals, expansion luôn có.
	Items       []Item         `json:"items,omitempty"`
	Storage     *StorageView   `json:"storage,omitempty"`
	Buildings   []BuildingView `json:"buildings"`
	AnimalKinds []AnimalKind   `json:"animal_kinds,omitempty"`
	Animals     []Animal       `json:"animals"`
	Orders      []Order        `json:"orders,omitempty"`
	Expansion   Expansion      `json:"expansion"`

	// Xưởng chế biến: jobs luôn có (lò đang nấu / đã xong), recipes chỉ khi me.
	Jobs    []Job    `json:"jobs"`
	Recipes []Recipe `json:"recipes,omitempty"`
	// Vùng cố định không xây đè được (chỉ khi me), để chọn chỗ đặt công trình.
	Blocked []Rect `json:"blocked,omitempty"`

	// Kết đôi (farm_couple.go): couple ai xem cũng thấy; couple_requests / couple_sent chỉ khi me;
	// couple_state chỉ khi xem vườn người khác: partner | can_propose | sent | incoming | "" (đã có đôi).
	Couple         *Couple         `json:"couple,omitempty"`
	CoupleRequests []CoupleRequest `json:"couple_requests,omitempty"`
	CoupleSent     *CoupleRequest  `json:"couple_sent,omitempty"`
	CoupleState    string          `json:"couple_state,omitempty"`
}

type Neighbor struct {
	User      model.PublicUser `json:"user"`
	Level     int              `json:"level"`
	XP        int64            `json:"xp"`
	Ripe      int              `json:"ripe"`
	Stealable int              `json:"stealable"`
	NeedsHelp int              `json:"needs_help"`
	Dog       bool             `json:"dog"`
	Partner   bool             `json:"partner,omitempty"` // người thương của người xem
}

type LeaderRow struct {
	User  model.PublicUser `json:"user"`
	Level int              `json:"level"`
	XP    int64            `json:"xp"`
}

// FarmResult: kết quả một hành động, để controller dựng FarmView và bắn sự kiện.
type FarmResult struct {
	OwnerID int64     // nông trại vừa bị tác động (trả FarmView của vườn này)
	Notify  int64     // người nhận farm.updated {log} nếu khác OwnerID (vd. tặng quà cho người thương)
	Gained  *Gained   // nil nếu hành động không đem lại gì
	Dog     *bool     // chỉ có khi trộm
	Log     *LogEntry // có khi người khác tác động lên nông trại của OwnerID
	Wallets []Wallet  // ví đã đổi
}

/* ---------- Đọc dữ liệu thô ---------- */

type farmRow struct {
	XP           int64
	Plots        int
	DogUntil     int64
	StorageLevel int
}

type plotRow struct {
	ID      int64
	Idx     int
	Crop    Crop
	Planted int64
	Ready   int64
	Bug     bool
	Waters  int64
	Stolen  int64
	Steals  int64
}

const (
	stateEmpty    = "empty"
	stateGrowing  = "growing"
	stateRipe     = "ripe"
	stateWithered = "withered"
)

func (p *plotRow) witherAt() int64 { return p.Ready + int64(FarmWitherAfter/time.Second) }

func (p *plotRow) state(now int64) string {
	switch {
	case p == nil:
		return stateEmpty
	case now >= p.witherAt():
		return stateWithered
	case now >= p.Ready:
		return stateRipe
	default:
		return stateGrowing
	}
}

func (p *plotRow) progress(now int64) float64 {
	if now >= p.Ready {
		return 1
	}
	total := p.Ready - p.Planted
	if total <= 0 {
		return 1
	}
	return min(max(float64(now-p.Planted)/float64(total), 0), 1)
}

// bugVisible: sâu lộ ra khi tiến độ từ 50% và cây chưa héo.
func (p *plotRow) bugVisible(now int64) bool {
	st := p.state(now)
	return p.Bug && (st == stateGrowing || st == stateRipe) && p.progress(now) >= 0.5
}

// farmable: người dùng thật (không phải bot tích hợp đuôi "_bot" hay trợ lý "ai").
func farmable(username string) bool {
	return username != "ai" && !strings.HasSuffix(username, "_bot")
}

const realUserFilter = `u.status = 'active' AND u.username <> 'ai' AND u.username NOT LIKE '%\_bot' ESCAPE '\'`

type queryer interface {
	execQuerier
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// farmUser: người dùng có nông trại (ErrNotFound nếu không có, bị khoá, hoặc là bot / AI).
func farmUser(ctx context.Context, q execQuerier, id int64) (model.PublicUser, error) {
	var u model.PublicUser
	var status string
	err := q.QueryRowContext(ctx,
		`SELECT id, username, display_name, avatar, status_text, status FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Username, &u.DisplayName, &u.Avatar, &u.StatusText, &status)
	if err != nil {
		return u, notFound(err)
	}
	if status != string(model.UserStatusActive) || !farmable(u.Username) {
		return u, ErrNotFound
	}
	return u, nil
}

func userLabel(u model.PublicUser) string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

func readFarm(ctx context.Context, q execQuerier, userID int64) (farmRow, error) {
	f := farmRow{Plots: FarmStartPlots}
	err := q.QueryRowContext(ctx, `SELECT xp, plots, dog_until, storage_level FROM farms WHERE user_id = ?`, userID).
		Scan(&f.XP, &f.Plots, &f.DogUntil, &f.StorageLevel)
	if errors.Is(err, sql.ErrNoRows) {
		return f, nil
	}
	return f, err
}

func ensureFarm(ctx context.Context, tx *sql.Tx, userID int64) (farmRow, error) {
	r, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO farms (user_id) VALUES (?)`, userID)
	if err != nil {
		return farmRow{}, err
	}
	// nông trại mới: tặng gói thức ăn khởi đầu để vật nuôi đầu tiên có cái ăn
	if n, _ := r.RowsAffected(); n == 1 && GiveStarterFeed {
		for _, f := range StarterFeed {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO farm_stock (user_id, item, qty) VALUES (?, ?, ?)
				ON CONFLICT (user_id, item) DO UPDATE SET qty = qty + excluded.qty`, userID, f.Item, f.Qty); err != nil {
				return farmRow{}, err
			}
		}
	}
	return readFarm(ctx, tx, userID)
}

const plotColumns = `id, idx, crop, planted_at, ready_at, bug, waters, stolen, steals`

func scanPlot(row scanner) (plotRow, error) {
	var p plotRow
	var crop string
	if err := row.Scan(&p.ID, &p.Idx, &crop, &p.Planted, &p.Ready, &p.Bug, &p.Waters, &p.Stolen, &p.Steals); err != nil {
		return p, err
	}
	p.Crop, _ = cropByID(crop)
	if p.Crop.ID == "" {
		p.Crop = Crop{ID: crop, Name: crop}
	}
	return p, nil
}

func loadPlot(ctx context.Context, q execQuerier, userID int64, idx int) (*plotRow, error) {
	p, err := scanPlot(q.QueryRowContext(ctx,
		`SELECT `+plotColumns+` FROM farm_plots WHERE user_id = ? AND idx = ?`, userID, idx))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func loadPlots(ctx context.Context, q queryer, userID int64) ([]plotRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+plotColumns+` FROM farm_plots WHERE user_id = ? ORDER BY idx`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []plotRow
	for rows.Next() {
		p, err := scanPlot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type actKey struct {
	plot   int64
	action string
}

// actorActs: các vụ (plot id) actorID đã tưới / trộm.
func actorActs(ctx context.Context, q queryer, actorID int64) (map[actKey]bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT plot_id, action FROM farm_crop_acts WHERE actor_id = ?`, actorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[actKey]bool{}
	for rows.Next() {
		var k actKey
		if err := rows.Scan(&k.plot, &k.action); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

func counter(ctx context.Context, q execQuerier, userID int64, day, key string) (int64, error) {
	var n int64
	err := q.QueryRowContext(ctx, `SELECT n FROM farm_counters WHERE user_id = ? AND day = ? AND key = ?`,
		userID, day, key).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return n, err
}

func bump(ctx context.Context, tx *sql.Tx, userID int64, day, key string, n int64) error {
	if n <= 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO farm_counters (user_id, day, key, n) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id, day, key) DO UPDATE SET n = n + excluded.n`, userID, day, key, n)
	return err
}

func rfc(sec int64) string { return time.Unix(sec, 0).UTC().Format(time.RFC3339) }

/* ---------- Một hành động (transaction) ---------- */

type farmOp struct {
	s       *Store
	ctx     context.Context
	tx      *sql.Tx
	now     time.Time
	day     string
	changed map[int64]bool
}

func (s *Store) farmTx(ctx context.Context, fn func(op *farmOp) error) ([]Wallet, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := s.now()
	op := &farmOp{s: s, ctx: ctx, tx: tx, now: now, day: vnDay(now), changed: map[int64]bool{}}
	if err := fn(op); err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(op.changed))
	for id := range op.changed {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	wallets := make([]Wallet, 0, len(ids))
	for _, id := range ids {
		w, err := loadWallet(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		wallets = append(wallets, w)
	}
	return wallets, tx.Commit()
}

func (op *farmOp) unix() int64 { return op.now.Unix() }

func (op *farmOp) earn(userID, amount int64, note string) error {
	if amount <= 0 {
		return nil
	}
	op.changed[userID] = true
	_, err := earn(op.ctx, op.tx, userID, "farm", amount, note)
	return err
}

func (op *farmOp) spend(userID, amount int64, note string) error {
	op.changed[userID] = true
	return spend(op.ctx, op.tx, userID, "farm", amount, note)
}

func (op *farmOp) bump(userID int64, key string, n int64) error {
	return bump(op.ctx, op.tx, userID, op.day, key, n)
}

// addXP cộng XP; lên cấp thì thưởng 20 × cấp mới cho mỗi cấp vượt qua. Trả về cấp mới nếu có lên cấp.
func (op *farmOp) addXP(userID, xp int64) (int, error) {
	f, err := ensureFarm(op.ctx, op.tx, userID)
	if err != nil || xp <= 0 {
		return 0, err
	}
	if _, err := op.tx.ExecContext(op.ctx, `UPDATE farms SET xp = xp + ? WHERE user_id = ?`, xp, userID); err != nil {
		return 0, err
	}
	from, to := levelFor(f.XP), levelFor(f.XP+xp)
	for lv := from + 1; lv <= to; lv++ {
		if err := op.earn(userID, int64(20*lv), fmt.Sprintf("Lên cấp %d nông trại", lv)); err != nil {
			return 0, err
		}
	}
	if to > from {
		return to, nil
	}
	return 0, nil
}

// log ghi nhật ký nông trại ownerID (giữ FarmLogKeep dòng mới nhất) và trả dòng vừa ghi.
func (op *farmOp) log(ownerID int64, actor model.PublicUser, action, crop string, xu int64) (*LogEntry, error) {
	if _, err := op.tx.ExecContext(op.ctx,
		`INSERT INTO farm_log (owner_id, actor_id, action, crop, xu, at) VALUES (?, ?, ?, ?, ?, ?)`,
		ownerID, actor.ID, action, crop, xu, op.unix()); err != nil {
		return nil, err
	}
	if _, err := op.tx.ExecContext(op.ctx, `
		DELETE FROM farm_log WHERE owner_id = ? AND id NOT IN
			(SELECT id FROM farm_log WHERE owner_id = ? ORDER BY id DESC LIMIT ?)`,
		ownerID, ownerID, FarmLogKeep); err != nil {
		return nil, err
	}
	return &LogEntry{At: time.Unix(op.unix(), 0).UTC(), Actor: actor, Action: action, Crop: crop, Xu: xu}, nil
}

// addAct ghi actor đã làm action trên vụ plotID; false nếu đã làm rồi.
func (op *farmOp) addAct(plotID, actorID int64, action string) (bool, error) {
	res, err := op.tx.ExecContext(op.ctx,
		`INSERT OR IGNORE INTO farm_crop_acts (plot_id, actor_id, action) VALUES (?, ?, ?)`, plotID, actorID, action)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (op *farmOp) hasAct(plotID, actorID int64, action string) (bool, error) {
	var one int
	err := op.tx.QueryRowContext(op.ctx,
		`SELECT 1 FROM farm_crop_acts WHERE plot_id = ? AND actor_id = ? AND action = ?`, plotID, actorID, action).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

/* ---------- Hành động của chủ nông trại ---------- */

// FarmPlant: gieo cropID vào ô idx của userID.
func (s *Store) FarmPlant(ctx context.Context, userID int64, idx int, cropID string) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		c, ok := cropByID(cropID)
		if !ok {
			return ErrFarmUnknownCrop
		}
		f, err := ensureFarm(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		if idx < 0 || idx >= f.Plots {
			return ErrFarmBadPlot
		}
		if levelFor(f.XP) < c.Level {
			return farmErr("Cần đạt cấp %d mới trồng được %s.", c.Level, c.Name)
		}
		p, err := loadPlot(ctx, op.tx, userID, idx)
		if err != nil {
			return err
		}
		if p != nil {
			if p.state(op.unix()) == stateWithered {
				return ErrFarmWithered
			}
			return ErrFarmPlotBusy
		}
		if err := op.spend(userID, c.Seed, "Mua hạt "+c.Name); err != nil {
			return err
		}
		bug := s.rand() < FarmBugChance
		if _, err := op.tx.ExecContext(ctx, `
			INSERT INTO farm_plots (user_id, idx, crop, planted_at, ready_at, bug) VALUES (?, ?, ?, ?, ?, ?)`,
			userID, idx, c.ID, op.unix(), op.unix()+c.GrowSec, bug); err != nil {
			return err
		}
		if err := op.bump(userID, "plant", 1); err != nil {
			return err
		}
		if c.Level >= 3 {
			return op.bump(userID, "plant_rare", 1)
		}
		return nil
	})
	return res, err
}

// harvestUnits: số đơn vị nông sản một vụ chín cho vào kho: 3 (còn sâu: 2) trừ phần bị trộm, tối thiểu 1.
func harvestUnits(p *plotRow) int64 {
	u := int64(FarmHarvestUnits)
	if p.Bug {
		u--
	}
	return max(u-p.Stolen, 1)
}

// FarmHarvest: thu hoạch ô idx (hoặc mọi ô chín nếu all) của userID. Nông sản vào kho (không trả xu),
// cộng XP của cây. Kho không đủ chỗ thì báo đầy; harvest all thu lần lượt các ô còn vừa kho.
func (s *Store) FarmHarvest(ctx context.Context, userID int64, idx int, all bool) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		now := op.unix()
		var ripe []plotRow
		if all {
			plots, err := loadPlots(ctx, op.tx, userID)
			if err != nil {
				return err
			}
			for _, p := range plots {
				if p.state(now) == stateRipe {
					ripe = append(ripe, p)
				}
			}
			if len(ripe) == 0 {
				return ErrFarmNothingRipe
			}
		} else {
			p, err := loadPlot(ctx, op.tx, userID, idx)
			if err != nil {
				return err
			}
			switch p.state(now) {
			case stateEmpty:
				return ErrFarmEmptyPlot
			case stateGrowing:
				return ErrFarmNotRipe
			case stateWithered:
				return ErrFarmWithered
			}
			ripe = append(ripe, *p)
		}
		room, err := op.storageRoom(userID)
		if err != nil {
			return err
		}
		var xp, n int64
		items := map[string]int64{}
		for i := range ripe {
			p := &ripe[i]
			u := harvestUnits(p)
			if u > room {
				continue
			}
			room -= u
			n++
			items[p.Crop.ID] += u
			xp += p.Crop.XP
			if _, err := op.tx.ExecContext(ctx, `DELETE FROM farm_plots WHERE id = ?`, p.ID); err != nil {
				return err
			}
			if err := op.addStock(userID, p.Crop.ID, u); err != nil {
				return err
			}
		}
		if n == 0 {
			return ErrFarmStorageFull
		}
		if err := op.bump(userID, "harvest", n); err != nil {
			return err
		}
		lv, err := op.addXP(userID, xp)
		if err != nil {
			return err
		}
		res.Gained = &Gained{XP: xp, LevelUp: lv, Items: items}
		return nil
	})
	return res, err
}

// FarmClear: dọn ô có cây đã héo.
func (s *Store) FarmClear(ctx context.Context, userID int64, idx int) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		p, err := loadPlot(ctx, op.tx, userID, idx)
		if err != nil {
			return err
		}
		switch p.state(op.unix()) {
		case stateEmpty:
			return ErrFarmEmptyPlot
		case stateWithered:
			_, err = op.tx.ExecContext(ctx, `DELETE FROM farm_plots WHERE id = ?`, p.ID)
			return err
		default:
			return ErrFarmNotWithered
		}
	})
	return res, err
}

// FarmExpand: mở rộng nông trại lên bậc kế (ExpandPlots). Người đã mua lẻ ô ở phần 1 đang ở bậc lớn
// nhất có số ô ≤ số ô đang có; mở rộng thì lên đúng số ô của bậc kế.
func (s *Store) FarmExpand(ctx context.Context, userID int64) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		f, err := ensureFarm(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		next := expansionOf(f.Plots).Next
		if next == nil {
			return ErrFarmMaxPlots
		}
		if levelFor(f.XP) < next.Level {
			return farmErr("Cần đạt cấp %d mới mở rộng được nông trại.", next.Level)
		}
		if err := op.spend(userID, next.Price, fmt.Sprintf("Mở rộng nông trại lên %d ô", next.Plots)); err != nil {
			return err
		}
		_, err = op.tx.ExecContext(ctx, `UPDATE farms SET plots = ? WHERE user_id = ?`, next.Plots, userID)
		return err
	})
	return res, err
}

// FarmBuyDog: thuê chó giữ nhà thêm 24h (cộng dồn từ lúc hết hạn hiện tại, tối đa 72h kể từ bây giờ).
func (s *Store) FarmBuyDog(ctx context.Context, userID int64) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		f, err := ensureFarm(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		now := op.unix()
		until := max(now, f.DogUntil) + int64(FarmDogDuration/time.Second)
		if until > now+int64(FarmDogMax/time.Second) {
			return ErrFarmDogMax
		}
		if err := op.spend(userID, FarmDogPrice, "Thuê chó giữ nhà"); err != nil {
			return err
		}
		_, err = op.tx.ExecContext(ctx, `UPDATE farms SET dog_until = ? WHERE user_id = ?`, until, userID)
		return err
	})
	return res, err
}

/* ---------- Hành động lên nông trại (của mình hoặc người khác) ---------- */

// FarmWater: actorID tưới ô idx của ownerID.
func (s *Store) FarmWater(ctx context.Context, actorID, ownerID int64, idx int) (FarmResult, error) {
	res := FarmResult{OwnerID: ownerID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		owner, err := farmUser(ctx, op.tx, ownerID)
		if err != nil {
			return err
		}
		p, err := loadPlot(ctx, op.tx, ownerID, idx)
		if err != nil {
			return err
		}
		now := op.unix()
		switch p.state(now) {
		case stateEmpty:
			return ErrFarmEmptyPlot
		case stateGrowing:
		default:
			return ErrFarmNotGrowing
		}
		if p.Waters >= FarmMaxWaters {
			return ErrFarmWaterMax
		}
		if ok, err := op.addAct(p.ID, actorID, "water"); err != nil {
			return err
		} else if !ok {
			return ErrFarmWatered
		}
		ready := max(now, p.Ready-p.Crop.GrowSec/10)
		if _, err := op.tx.ExecContext(ctx,
			`UPDATE farm_plots SET ready_at = ?, waters = waters + 1 WHERE id = ?`, ready, p.ID); err != nil {
			return err
		}
		if actorID == ownerID {
			return nil
		}
		if err := op.earn(actorID, FarmHelpWaterXu, "Tưới nước giúp "+userLabel(owner)); err != nil {
			return err
		}
		lv, err := op.addXP(actorID, FarmHelpXP)
		if err != nil {
			return err
		}
		if err := op.bump(actorID, "water", 1); err != nil {
			return err
		}
		res.Gained = &Gained{Xu: FarmHelpWaterXu, XP: FarmHelpXP, LevelUp: lv}
		love, up, err := op.addLove(actorID, ownerID, LoveWater)
		if err != nil {
			return err
		}
		gainLove(&res.Gained, love, up)
		actor, err := farmUser(ctx, op.tx, actorID)
		if err != nil {
			return err
		}
		res.Log, err = op.log(ownerID, actor, "water", p.Crop.ID, 0)
		return err
	})
	return res, err
}

// FarmBug: actorID bắt sâu ô idx của ownerID.
func (s *Store) FarmBug(ctx context.Context, actorID, ownerID int64, idx int) (FarmResult, error) {
	res := FarmResult{OwnerID: ownerID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		owner, err := farmUser(ctx, op.tx, ownerID)
		if err != nil {
			return err
		}
		p, err := loadPlot(ctx, op.tx, ownerID, idx)
		if err != nil {
			return err
		}
		now := op.unix()
		switch {
		case p == nil:
			return ErrFarmEmptyPlot
		case p.state(now) == stateWithered:
			return ErrFarmWithered
		case !p.bugVisible(now):
			return ErrFarmNoBug
		}
		if _, err := op.tx.ExecContext(ctx, `UPDATE farm_plots SET bug = 0 WHERE id = ?`, p.ID); err != nil {
			return err
		}
		if err := op.bump(actorID, "bug", 1); err != nil {
			return err
		}
		if actorID == ownerID {
			return nil
		}
		if err := op.earn(actorID, FarmHelpBugXu, "Bắt sâu giúp "+userLabel(owner)); err != nil {
			return err
		}
		lv, err := op.addXP(actorID, FarmHelpXP)
		if err != nil {
			return err
		}
		res.Gained = &Gained{Xu: FarmHelpBugXu, XP: FarmHelpXP, LevelUp: lv}
		love, up, err := op.addLove(actorID, ownerID, LoveBug)
		if err != nil {
			return err
		}
		gainLove(&res.Gained, love, up)
		actor, err := farmUser(ctx, op.tx, actorID)
		if err != nil {
			return err
		}
		res.Log, err = op.log(ownerID, actor, "bug", p.Crop.ID, 0)
		return err
	})
	return res, err
}

// FarmSteal: thiefID trộm ô idx của ownerID: lấy 1 đơn vị nông sản vào kho mình (kho đầy thì không
// trộm được). Chủ có chó còn hạn thì 50% bị chó đuổi.
func (s *Store) FarmSteal(ctx context.Context, thiefID, ownerID int64, idx int) (FarmResult, error) {
	res := FarmResult{OwnerID: ownerID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		if thiefID == ownerID {
			return ErrFarmOwnSteal
		}
		if ok, err := partners(ctx, op.tx, thiefID, ownerID); err != nil {
			return err
		} else if ok {
			return ErrFarmStealPartner
		}
		owner, err := farmUser(ctx, op.tx, ownerID)
		if err != nil {
			return err
		}
		thief, err := farmUser(ctx, op.tx, thiefID)
		if err != nil {
			return err
		}
		p, err := loadPlot(ctx, op.tx, ownerID, idx)
		if err != nil {
			return err
		}
		now := op.unix()
		switch p.state(now) {
		case stateEmpty:
			return ErrFarmEmptyPlot
		case stateGrowing:
			return ErrFarmNotRipe
		case stateWithered:
			return ErrFarmWithered
		}
		if p.Steals >= FarmMaxSteals {
			return ErrFarmStealMax
		}
		if done, err := op.hasAct(p.ID, thiefID, "steal"); err != nil {
			return err
		} else if done {
			return ErrFarmStolen
		}
		tries, err := counter(ctx, op.tx, thiefID, op.day, stealTryKey)
		if err != nil {
			return err
		}
		if tries >= FarmStealsPerDay {
			return ErrFarmStealDaily
		}
		if room, err := op.storageRoom(thiefID); err != nil {
			return err
		} else if room < 1 {
			return ErrFarmStorageFull
		}
		if _, err := op.addAct(p.ID, thiefID, "steal"); err != nil {
			return err
		}
		if err := op.bump(thiefID, stealTryKey, 1); err != nil {
			return err
		}
		f, err := ensureFarm(ctx, op.tx, ownerID)
		if err != nil {
			return err
		}
		chased := f.DogUntil > now && s.rand() < FarmDogChance
		res.Dog = &chased
		if chased {
			w, err := loadWallet(ctx, op.tx, thiefID)
			if err != nil {
				return err
			}
			fine := min(int64(FarmDogFine), w.Balance)
			if fine > 0 {
				op.changed[thiefID] = true
				if err := addTxNote(ctx, op.tx, thiefID, "farm", -fine, 0,
					"Bị chó nhà "+userLabel(owner)+" đuổi"); err != nil {
					return err
				}
				if err := op.earn(ownerID, fine, "Chó đuổi "+userLabel(thief)); err != nil {
					return err
				}
			}
			res.Gained = &Gained{Xu: -fine}
			res.Log, err = op.log(ownerID, thief, "dog", p.Crop.ID, fine)
			return err
		}
		if _, err := op.tx.ExecContext(ctx,
			`UPDATE farm_plots SET stolen = stolen + 1, steals = steals + 1 WHERE id = ?`, p.ID); err != nil {
			return err
		}
		if err := op.addStock(thiefID, p.Crop.ID, 1); err != nil {
			return err
		}
		if err := op.bump(thiefID, "steal", 1); err != nil {
			return err
		}
		res.Gained = &Gained{Items: map[string]int64{p.Crop.ID: 1}}
		res.Log, err = op.log(ownerID, thief, "steal", p.Crop.ID, 0)
		return err
	})
	return res, err
}

/* ---------- Nhiệm vụ ---------- */

// FarmClaimQuest: nhận thưởng nhiệm vụ questID hôm nay ("chest" = rương thưởng ngày).
func (s *Store) FarmClaimQuest(ctx context.Context, userID int64, questID string) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		quests, err := questViews(ctx, op.tx, userID, op.day)
		if err != nil {
			return err
		}
		var xu, xp int64
		var note string
		if questID == chestQuestID {
			for _, q := range quests {
				if !q.Done {
					return ErrFarmQuestNotDone
				}
			}
			xu, xp, note = FarmChestXu, FarmChestXP, "Rương thưởng ngày"
		} else {
			var found *Quest
			for i := range quests {
				if quests[i].ID == questID {
					found = &quests[i]
				}
			}
			if found == nil {
				return ErrFarmNoQuest
			}
			if !found.Done {
				return ErrFarmQuestNotDone
			}
			xu, xp, note = found.Xu, found.XP, "Nhiệm vụ: "+found.Title
		}
		r, err := op.tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO farm_claims (user_id, day, quest) VALUES (?, ?, ?)`, userID, op.day, questID)
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return ErrFarmQuestClaimed
		}
		if err := op.earn(userID, xu, note); err != nil {
			return err
		}
		lv, err := op.addXP(userID, xp)
		if err != nil {
			return err
		}
		res.Gained = &Gained{Xu: xu, XP: xp, LevelUp: lv}
		return nil
	})
	return res, err
}

func questViews(ctx context.Context, q queryer, userID int64, day string) ([]Quest, error) {
	counts := map[string]int64{}
	rows, err := q.QueryContext(ctx, `SELECT key, n FROM farm_counters WHERE user_id = ? AND day = ?`, userID, day)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k string
		var n int64
		if err := rows.Scan(&k, &n); err != nil {
			rows.Close()
			return nil, err
		}
		counts[k] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	claimed, err := claimedQuests(ctx, q, userID, day)
	if err != nil {
		return nil, err
	}
	defs := dailyQuests(userID, day)
	out := make([]Quest, len(defs))
	for i, d := range defs {
		n := counts[d.ID]
		out[i] = Quest{ID: d.ID, Title: d.Title, Goal: d.Goal, Progress: min(n, d.Goal), Xu: d.Xu, XP: d.XP,
			Done: n >= d.Goal, Claimed: claimed[d.ID]}
	}
	return out, nil
}

func claimedQuests(ctx context.Context, q queryer, userID int64, day string) (map[string]bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT quest FROM farm_claims WHERE user_id = ? AND day = ?`, userID, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

/* ---------- Xem ---------- */

// FarmVisit: viewerID ghé nông trại ownerID (tính nhiệm vụ "visit" với mỗi nông trại khác một lần/ngày).
func (s *Store) FarmVisit(ctx context.Context, viewerID, ownerID int64) (FarmView, error) {
	if viewerID != ownerID {
		if _, err := farmUser(ctx, s.db, ownerID); err != nil {
			return FarmView{}, err
		}
		if _, err := s.farmTx(ctx, func(op *farmOp) error {
			r, err := op.tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO farm_visits (user_id, day, owner_id) VALUES (?, ?, ?)`, viewerID, op.day, ownerID)
			if err != nil {
				return err
			}
			if n, _ := r.RowsAffected(); n > 0 {
				if _, _, err := op.addLove(viewerID, ownerID, LoveVisit); err != nil {
					return err
				}
				return op.bump(viewerID, "visit", 1)
			}
			return nil
		}); err != nil {
			return FarmView{}, err
		}
	}
	return s.GetFarm(ctx, ownerID, viewerID)
}

// GetFarm: nông trại ownerID dưới góc nhìn viewerID (chính chủ thì kèm ví, nhiệm vụ, nhật ký).
func (s *Store) GetFarm(ctx context.Context, ownerID, viewerID int64) (FarmView, error) {
	me := ownerID == viewerID
	owner, err := farmUser(ctx, s.db, ownerID)
	if err != nil {
		return FarmView{}, err
	}
	if me { // ô đơn hàng đã tới giờ thì sinh đơn mới trước khi đọc
		if err := s.refillOrders(ctx, ownerID); err != nil {
			return FarmView{}, err
		}
	}
	nowT := s.now()
	now, day := nowT.Unix(), vnDay(nowT)
	f, err := readFarm(ctx, s.db, ownerID)
	if err != nil {
		return FarmView{}, err
	}
	rows, err := loadPlots(ctx, s.db, ownerID)
	if err != nil {
		return FarmView{}, err
	}
	acts, err := actorActs(ctx, s.db, viewerID)
	if err != nil {
		return FarmView{}, err
	}
	tries, err := counter(ctx, s.db, viewerID, day, stealTryKey)
	if err != nil {
		return FarmView{}, err
	}
	lv := levelFor(f.XP)
	v := FarmView{
		User: owner, Me: me, Level: lv, XP: f.XP, LevelXP: LevelXP[lv-1],
		PlotCount: f.Plots, MaxPlots: FarmMaxPlots, Crops: Crops,
		Now:        rfc(now),
		StealsLeft: int(max(0, FarmStealsPerDay-tries)),
	}
	if lv < len(LevelXP) {
		next := LevelXP[lv]
		v.NextLevelXP = &next
	}
	v.Expansion = expansionOf(f.Plots)
	if n := v.Expansion.Next; n != nil {
		v.NextPlot = &NextPlot{Price: n.Price, Level: n.Level}
	}
	if f.DogUntil > now {
		d := rfc(f.DogUntil)
		v.DogUntil = &d
	}
	byIdx := map[int]*plotRow{}
	for i := range rows {
		byIdx[rows[i].Idx] = &rows[i]
	}
	v.Plots = make([]Plot, f.Plots)
	for i := range v.Plots {
		v.Plots[i] = buildPlot(i, byIdx[i], now, me, acts, v.StealsLeft)
	}
	if err := s.fillFarmV2(ctx, &v, ownerID, f, now); err != nil {
		return FarmView{}, err
	}
	if err := s.fillCouple(ctx, &v, ownerID, viewerID, day); err != nil {
		return FarmView{}, err
	}
	if v.CoupleState == "partner" { // không trộm vườn người thương
		for i := range v.Plots {
			v.Plots[i].CanSteal = false
		}
	}
	if !me {
		return v, nil
	}
	w, err := s.GetWallet(ctx, ownerID)
	if err != nil {
		return FarmView{}, err
	}
	v.Wallet = &w
	if v.Quests, err = questViews(ctx, s.db, ownerID, day); err != nil {
		return FarmView{}, err
	}
	claimed, err := claimedQuests(ctx, s.db, ownerID, day)
	if err != nil {
		return FarmView{}, err
	}
	allDone := true
	for _, q := range v.Quests {
		allDone = allDone && q.Done
	}
	v.Chest = &Chest{Ready: allDone, Claimed: claimed[chestQuestID], Xu: FarmChestXu, XP: FarmChestXP}
	log, err := s.farmLog(ctx, ownerID)
	if err != nil {
		return FarmView{}, err
	}
	v.Log = &log
	return v, nil
}

func buildPlot(idx int, p *plotRow, now int64, me bool, acts map[actKey]bool, stealsLeft int) Plot {
	out := Plot{Index: idx, State: p.state(now)}
	if p == nil {
		return out
	}
	out.Crop = p.Crop.ID
	out.PlantedAt, out.ReadyAt, out.WitherAt = rfc(p.Planted), rfc(p.Ready), rfc(p.witherAt())
	out.Progress = p.progress(now)
	out.Bug = p.bugVisible(now)
	out.Waters, out.Stolen, out.Steals = p.Waters, p.Stolen, p.Steals
	out.CanWater = out.State == stateGrowing && p.Waters < FarmMaxWaters && !acts[actKey{p.ID, "water"}]
	out.CanBug = out.Bug
	out.CanSteal = !me && out.State == stateRipe && p.Steals < FarmMaxSteals && stealsLeft > 0 &&
		!acts[actKey{p.ID, "steal"}]
	out.CanHarvest = me && out.State == stateRipe
	return out
}

func (s *Store) farmLog(ctx context.Context, ownerID int64) ([]LogEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.at, l.action, l.crop, l.xu, u.id, u.username, u.display_name, u.avatar, u.status_text
		FROM farm_log l JOIN users u ON u.id = l.actor_id
		WHERE l.owner_id = ? ORDER BY l.id DESC LIMIT ?`, ownerID, FarmLogKeep)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LogEntry{}
	for rows.Next() {
		var e LogEntry
		var at int64
		if err := rows.Scan(&at, &e.Action, &e.Crop, &e.Xu,
			&e.Actor.ID, &e.Actor.Username, &e.Actor.DisplayName, &e.Actor.Avatar, &e.Actor.StatusText); err != nil {
			return nil, err
		}
		e.At = time.Unix(at, 0).UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

// realUsers: mọi người dùng thật đang hoạt động (không bot, không AI) kèm XP và hạn chó.
func (s *Store) realUsers(ctx context.Context, limit int) ([]model.PublicUser, []farmRow, error) {
	q := `
		SELECT u.id, u.username, u.display_name, u.avatar, u.status_text,
		       COALESCE(f.xp, 0), COALESCE(f.plots, ?), COALESCE(f.dog_until, 0)
		FROM users u LEFT JOIN farms f ON f.user_id = u.id
		WHERE ` + realUserFilter + `
		ORDER BY COALESCE(f.xp, 0) DESC, u.id`
	args := []any{FarmStartPlots}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var users []model.PublicUser
	var farms []farmRow
	for rows.Next() {
		var u model.PublicUser
		var f farmRow
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Avatar, &u.StatusText,
			&f.XP, &f.Plots, &f.DogUntil); err != nil {
			return nil, nil, err
		}
		users = append(users, u)
		farms = append(farms, f)
	}
	return users, farms, rows.Err()
}

// FarmNeighbors: mọi người dùng thật trừ viewerID, kèm số ô chín / trộm được / cần giúp (theo góc nhìn
// viewerID). Xếp: nhiều việc làm được (trộm + giúp) trước, rồi XP cao trước.
func (s *Store) FarmNeighbors(ctx context.Context, viewerID int64) ([]Neighbor, error) {
	users, farms, err := s.realUsers(ctx, 0)
	if err != nil {
		return nil, err
	}
	nowT := s.now()
	now := nowT.Unix()
	acts, err := actorActs(ctx, s.db, viewerID)
	if err != nil {
		return nil, err
	}
	tries, err := counter(ctx, s.db, viewerID, vnDay(nowT), stealTryKey)
	if err != nil {
		return nil, err
	}
	stealsLeft := int(max(0, FarmStealsPerDay-tries))
	couple, coupled, err := coupleOf(ctx, s.db, viewerID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, `+plotColumns+` FROM farm_plots`)
	if err != nil {
		return nil, err
	}
	plots := map[int64][]plotRow{}
	for rows.Next() {
		var uid int64
		var p plotRow
		var crop string
		if err := rows.Scan(&uid, &p.ID, &p.Idx, &crop, &p.Planted, &p.Ready, &p.Bug, &p.Waters, &p.Stolen, &p.Steals); err != nil {
			rows.Close()
			return nil, err
		}
		p.Crop, _ = cropByID(crop)
		plots[uid] = append(plots[uid], p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []Neighbor{}
	for i, u := range users {
		if u.ID == viewerID {
			continue
		}
		n := Neighbor{User: u, Level: levelFor(farms[i].XP), XP: farms[i].XP, Dog: farms[i].DogUntil > now,
			Partner: coupled && couple.Partner == u.ID}
		for j := range plots[u.ID] {
			p := &plots[u.ID][j]
			if p.Idx >= farms[i].Plots {
				continue
			}
			v := buildPlot(p.Idx, p, now, false, acts, stealsLeft)
			if v.State == stateRipe {
				n.Ripe++
			}
			if v.CanSteal && !n.Partner {
				n.Stealable++
			}
			if v.CanWater || v.CanBug {
				n.NeedsHelp++
			}
		}
		out = append(out, n)
	}
	sort.SliceStable(out, func(a, b int) bool {
		ja, jb := out[a].Stealable+out[a].NeedsHelp, out[b].Stealable+out[b].NeedsHelp
		if ja != jb {
			return ja > jb
		}
		return out[a].XP > out[b].XP
	})
	return out, nil
}

// FarmLeaderboard: top 20 người dùng thật theo XP nông trại.
func (s *Store) FarmLeaderboard(ctx context.Context) ([]LeaderRow, error) {
	users, farms, err := s.realUsers(ctx, 20)
	if err != nil {
		return nil, err
	}
	out := make([]LeaderRow, len(users))
	for i, u := range users {
		out[i] = LeaderRow{User: u, Level: levelFor(farms[i].XP), XP: farms[i].XP}
	}
	return out, nil
}
