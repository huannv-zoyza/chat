package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// Nông trại phần 2 (specs/FARM_SPEC.md §4–8): kho, chăn nuôi, đơn hàng xe tải, mở rộng đất theo bậc.
// Cùng khuôn với phần 1: mỗi hành động là một farmTx, mọi kiểm tra (sức chứa kho, đủ hàng, đủ xu...)
// nằm trong transaction đó.

/* ---------- Danh mục ---------- */

// Item: một món vào được kho (nông sản hoặc sản phẩm chăn nuôi). Price là giá bán một đơn vị.
type Item struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Emoji string `json:"emoji"`
	Price int64  `json:"price"`
	Kind  string `json:"kind"` // crop | product | goods (món chế biến, xem farm_craft.go)
	Level int    `json:"level"`
	// BuyPrice: giá mua một đơn vị ở chợ (chỉ nông sản, để có thức ăn cho vật nuôi). Đắt hơn giá bán
	// 50% để không mua đi bán lại kiếm lời được. 0: không bán ở chợ.
	BuyPrice int64 `json:"buy_price,omitempty"`
}

type AnimalKind struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Emoji    string `json:"emoji"`
	Building string `json:"building"` // coop | pasture
	Price    int64  `json:"price"`
	Level    int    `json:"level"`
	Feed     string `json:"feed"` // id nông sản trong kho
	FeedQty  int64  `json:"feed_qty"`
	TimeSec  int64  `json:"time_sec"`
	Product  string `json:"product"` // id sản phẩm (Item kind "product")
	XP       int64  `json:"xp"`
}

// AnimalKinds: danh mục vật nuôi (cố định).
var AnimalKinds = []AnimalKind{
	{"ga", "Gà", "🐓", "coop", 80, 2, "rau_muong", 1, 20 * 60, "trung", 2}, // 🐓: 🐔 ở font Noto chỉ vẽ cái đầu
	{"bo", "Bò", "🐄", "pasture", 300, 3, "ngo", 1, 60 * 60, "sua", 6},
	{"de", "Dê", "🐐", "pasture", 400, 5, "ca_chua", 1, 90 * 60, "pho_mai", 8},
	{"cuu", "Cừu", "🐑", "pasture", 450, 4, "ca_rot", 2, 2 * 3600, "len", 10},
	{"heo", "Heo", "🐖", "pasture", 650, 6, "dau_tay", 1, 3 * 3600, "nam", 15},
}

// products: sản phẩm chăn nuôi, cùng thứ tự với AnimalKinds; cấp mở = cấp của con vật làm ra nó.
var products = []struct {
	ID, Name, Emoji string
	Price           int64
}{
	{"trung", "Trứng", "🥚", 22},
	{"sua", "Sữa", "🥛", 75},
	{"pho_mai", "Phô mai", "🧀", 95},
	{"len", "Len", "🧶", 110},
	{"nam", "Nấm truffle", "🍄", 220},
}

// Items: mọi món vào được kho: nông sản (giá đơn vị = ceil(sell / 3)), sản phẩm chăn nuôi, rồi món
// chế biến ở xưởng.
var Items = func() []Item {
	out := make([]Item, 0, len(Crops)+len(products)+len(Recipes))
	for _, c := range Crops {
		price := (c.Sell + FarmHarvestUnits - 1) / FarmHarvestUnits
		out = append(out, Item{c.ID, c.Name, c.Emoji, price, "crop", c.Level, (price*3 + 1) / 2})
	}
	for i, p := range products {
		out = append(out, Item{p.ID, p.Name, p.Emoji, p.Price, "product", AnimalKinds[i].Level, 0})
	}
	for _, r := range Recipes {
		out = append(out, Item{r.ID, r.Name, r.Emoji, r.Price, "goods", r.Level, 0})
	}
	return out
}()

func itemByID(id string) (Item, bool) {
	for _, it := range Items {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

func animalKindByID(id string) (AnimalKind, bool) {
	for _, k := range AnimalKinds {
		if k.ID == id {
			return k, true
		}
	}
	return AnimalKind{}, false
}

func itemName(id string) string {
	if it, ok := itemByID(id); ok {
		return it.Name
	}
	return id
}

type buildingDef struct {
	ID, Name     string
	Price        int64
	Level        int // cấp nông trại cần để xây
	Cap          int // sức chứa cơ bản (xưởng: số lò)
	UpgradeCap   int // sức chứa sau nâng cấp
	UpgradePrice int64
	// Size khác 0: công trình tự do, đặt ở chỗ tuỳ chọn trên bản đồ (farm_place.go); Default là chỗ
	// khi không chọn. Size 0: cố định chỗ (chuồng có hàng rào vẽ sẵn).
	Size    Size
	Default Spot
}

var buildingDefs = []buildingDef{
	{ID: "coop", Name: "Chuồng gà", Price: 200, Level: 2, Cap: 4, UpgradeCap: 8, UpgradePrice: 400},
	{ID: "pasture", Name: "Đồng cỏ", Price: 600, Level: 3, Cap: 3, UpgradeCap: 6, UpgradePrice: 900},
	{ID: kitchenID, Name: "Xưởng chế biến", Price: 500, Level: 4, Cap: 2, UpgradeCap: 4, UpgradePrice: 1200,
		Size: Size{3, 2}, Default: Spot{12, 12}},
}

func buildingByID(id string) (buildingDef, bool) {
	for _, b := range buildingDefs {
		if b.ID == id {
			return b, true
		}
	}
	return buildingDef{}, false
}

// capAt: sức chứa chuồng ở level (0 = chưa xây, 1 = cơ bản, 2 = đã nâng cấp).
func (b buildingDef) capAt(level int) int {
	if level >= 2 {
		return b.UpgradeCap
	}
	return b.Cap
}

const (
	FarmHarvestUnits      = 3 // đơn vị nông sản mỗi vụ (còn sâu: 2)
	FarmStorageBase       = 60
	FarmStorageStep       = 30
	FarmOrderSlots        = 3
	FarmOrderAfterDeliver = 5 * time.Minute
	FarmOrderAfterDiscard = 15 * time.Minute
)

var (
	// StoragePrices[k]: giá nâng cấp kho lần thứ k+1 (mỗi lần +30, tối đa 210).
	StoragePrices = []int64{300, 600, 1000, 1600, 2400}
	// Mở rộng đất theo bậc: số ô mỗi bậc, giá và cấp cần để lên bậc kế.
	ExpandPlots  = []int{6, 9, 12, 16, 20}
	ExpandPrices = []int64{400, 1000, 2200, 4500}
	ExpandLevels = []int{2, 4, 6, 8}
)

func storageCap(level int) int64 { return FarmStorageBase + FarmStorageStep*int64(level) }

/* ---------- Lỗi ---------- */

var (
	ErrFarmStorageFull    = &FarmError{"Kho đầy, bán bớt hoặc nâng cấp kho."}
	ErrFarmStorageMax     = &FarmError{"Kho đã nâng cấp tối đa."}
	ErrFarmUnknownItem    = &FarmError{"Không có món hàng này."}
	ErrFarmBadQty         = &FarmError{"Số lượng không hợp lệ."}
	ErrFarmStockEmpty     = &FarmError{"Kho trống, không có gì để bán."}
	ErrFarmUnknownBuild   = &FarmError{"Không có công trình này."}
	ErrFarmUnknownAnimal  = &FarmError{"Không có loại vật nuôi này."}
	ErrFarmNoAnimal       = &FarmError{"Không tìm thấy con vật này."}
	ErrFarmFed            = &FarmError{"Con vật này đã được cho ăn rồi."}
	ErrFarmNoHungry       = &FarmError{"Không có con nào đang đói."}
	ErrFarmNotProduced    = &FarmError{"Con vật chưa có sản phẩm."}
	ErrFarmNothingCollect = &FarmError{"Chưa có sản phẩm nào để thu."}
	ErrFarmNoOrder        = &FarmError{"Không tìm thấy đơn hàng này."}
	ErrFarmOrderShort     = &FarmError{"Chưa đủ hàng để giao đơn này."}
	ErrFarmNothingTend    = &FarmError{"Chuồng này không có gì để thu hay cho ăn."}
	ErrFarmNoXuFeed       = &FarmError{"Không đủ xu mua thức ăn cho vật nuôi."}
)

/* ---------- Kiểu dữ liệu trả cho client ---------- */

type PriceTag struct {
	Price int64 `json:"price"`
}

type StorageView struct {
	Cap         int64            `json:"cap"`
	Used        int64            `json:"used"`
	NextUpgrade *PriceTag        `json:"next_upgrade"` // null khi đã tối đa
	Stock       map[string]int64 `json:"stock"`
}

// BuildingView: Level là cấp nông trại cần để xây; Cap là sức chứa hiện tại (chưa xây: sức chứa cơ bản);
// UpgradePrice chỉ có khi đã xây và chưa nâng cấp.
type BuildingView struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Built        bool   `json:"built"`
	Cap          int    `json:"cap"`
	Price        int64  `json:"price"`
	Level        int    `json:"level"`
	UpgradePrice *int64 `json:"upgrade_price"`
	// Công trình tự do: số ô chiếm và chỗ đang đặt (chưa xây: chỗ mặc định).
	Size *Size `json:"size,omitempty"`
	Spot *Spot `json:"spot,omitempty"`
}

type Animal struct {
	ID      int64  `json:"id"`
	Kind    string `json:"kind"`
	State   string `json:"state"` // hungry | producing | ready
	ReadyAt string `json:"ready_at,omitempty"`
	CanFeed bool   `json:"can_feed"`
}

type OrderItem struct {
	Item string `json:"item"`
	Qty  int64  `json:"qty"`
}

// Order: một ô đơn hàng. Ô có đơn: id, items, xu, xp, can_deliver. Ô trống: next_at.
type Order struct {
	Slot       int         `json:"slot"`
	ID         int64       `json:"id,omitempty"`
	Items      []OrderItem `json:"items,omitempty"`
	Xu         int64       `json:"xu,omitempty"`
	XP         int64       `json:"xp,omitempty"`
	NextAt     string      `json:"next_at,omitempty"`
	CanDeliver *bool       `json:"can_deliver,omitempty"`
}

type ExpansionStep struct {
	Plots int   `json:"plots"`
	Price int64 `json:"price"`
	Level int   `json:"level"`
}

// Expansion: Tier tính từ 1 (bậc 1 = 6 ô); Plots là số ô đang có (người mua lẻ ở phần 1 có thể lệch bậc).
type Expansion struct {
	Tier  int            `json:"tier"`
	Plots int            `json:"plots"`
	Next  *ExpansionStep `json:"next"`
}

func expansionOf(plots int) Expansion {
	t := 0
	for i, n := range ExpandPlots {
		if plots >= n {
			t = i
		}
	}
	e := Expansion{Tier: t + 1, Plots: plots}
	if t+1 < len(ExpandPlots) {
		e.Next = &ExpansionStep{Plots: ExpandPlots[t+1], Price: ExpandPrices[t], Level: ExpandLevels[t]}
	}
	return e
}

/* ---------- Kho ---------- */

func loadStock(ctx context.Context, q queryer, userID int64) (map[string]int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT item, qty FROM farm_stock WHERE user_id = ? AND qty > 0`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var it string
		var n int64
		if err := rows.Scan(&it, &n); err != nil {
			return nil, err
		}
		out[it] = n
	}
	return out, rows.Err()
}

func stockUsed(stock map[string]int64) int64 {
	var n int64
	for _, q := range stock {
		n += q
	}
	return n
}

// storageRoom: số đơn vị kho của userID còn chứa thêm được.
func (op *farmOp) storageRoom(userID int64) (int64, error) {
	f, err := ensureFarm(op.ctx, op.tx, userID)
	if err != nil {
		return 0, err
	}
	var used int64
	if err := op.tx.QueryRowContext(op.ctx, `SELECT COALESCE(SUM(qty), 0) FROM farm_stock WHERE user_id = ?`, userID).
		Scan(&used); err != nil {
		return 0, err
	}
	return max(storageCap(f.StorageLevel)-used, 0), nil
}

// addStock cộng qty đơn vị item vào kho userID (người gọi đã kiểm tra sức chứa trong cùng transaction).
func (op *farmOp) addStock(userID int64, item string, qty int64) error {
	_, err := op.tx.ExecContext(op.ctx, `
		INSERT INTO farm_stock (user_id, item, qty) VALUES (?, ?, ?)
		ON CONFLICT (user_id, item) DO UPDATE SET qty = qty + excluded.qty`, userID, item, qty)
	return err
}

// takeStock trừ qty đơn vị item khỏi kho userID; false nếu không đủ (kho không đổi).
func (op *farmOp) takeStock(userID int64, item string, qty int64) (bool, error) {
	r, err := op.tx.ExecContext(op.ctx,
		`UPDATE farm_stock SET qty = qty - ? WHERE user_id = ? AND item = ? AND qty >= ?`, qty, userID, item, qty)
	if err != nil {
		return false, err
	}
	if n, err := r.RowsAffected(); err != nil || n == 0 {
		return false, err
	}
	_, err = op.tx.ExecContext(op.ctx, `DELETE FROM farm_stock WHERE user_id = ? AND item = ? AND qty = 0`, userID, item)
	return true, err
}

// FarmSell: bán qty đơn vị item trong kho (hoặc bán sạch kho nếu all). Tiền qua earn (đang nợ thì
// tự trừ 50%), tính vào nhiệm vụ "earn".
func (s *Store) FarmSell(ctx context.Context, userID int64, itemID string, qty int64, all bool) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		var total int64
		var note string
		if all {
			stock, err := loadStock(ctx, op.tx, userID)
			if err != nil {
				return err
			}
			for _, it := range Items {
				n := stock[it.ID]
				if n <= 0 {
					continue
				}
				if _, err := op.takeStock(userID, it.ID, n); err != nil {
					return err
				}
				total += it.Price * n
			}
			if total == 0 {
				return ErrFarmStockEmpty
			}
			note = "Bán hết hàng trong kho"
		} else {
			it, ok := itemByID(itemID)
			if !ok {
				return ErrFarmUnknownItem
			}
			if qty <= 0 {
				return ErrFarmBadQty
			}
			if ok, err := op.takeStock(userID, it.ID, qty); err != nil {
				return err
			} else if !ok {
				return farmErr("Trong kho không đủ %d %s để bán.", qty, it.Name)
			}
			total = it.Price * qty
			note = fmt.Sprintf("Bán %d %s", qty, it.Name)
		}
		if err := op.earn(userID, total, note); err != nil {
			return err
		}
		if err := op.bump(userID, "earn", total); err != nil {
			return err
		}
		res.Gained = &Gained{Xu: total}
		return nil
	})
	return res, err
}

// GiveStarterFeed: tặng StarterFeed khi tạo nông trại. Test tắt đi để kho bắt đầu trống.
var GiveStarterFeed = true

// StarterFeed: gói thức ăn khởi đầu (mỗi nông trại nhận một lần): đủ cho gà, bò, cừu, dê ăn vài lượt.
var StarterFeed = []struct {
	Item string
	Qty  int64
}{{"rau_muong", 6}, {"ngo", 3}, {"ca_rot", 4}, {"ca_chua", 3}}

// FarmBuyItem: mua nông sản ở chợ (để làm thức ăn), giá BuyPrice, cần đủ cấp và còn chỗ trong kho.
func (s *Store) FarmBuyItem(ctx context.Context, userID int64, itemID string, qty int64) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		it, ok := itemByID(itemID)
		if !ok || it.BuyPrice <= 0 {
			return ErrFarmUnknownItem
		}
		if qty <= 0 || qty > 50 {
			return ErrFarmBadQty
		}
		f, err := ensureFarm(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		if lv := levelFor(f.XP); lv < it.Level {
			return farmErr("Cần đạt cấp %d mới mua được %s.", it.Level, it.Name)
		}
		room, err := op.storageRoom(userID)
		if err != nil {
			return err
		}
		if room < qty {
			return ErrFarmStorageFull
		}
		if err := op.spend(userID, it.BuyPrice*qty, fmt.Sprintf("Mua %d %s ở chợ", qty, it.Name)); err != nil {
			return err
		}
		if err := op.addStock(userID, it.ID, qty); err != nil {
			return err
		}
		res.Gained = &Gained{Items: map[string]int64{it.ID: qty}}
		return nil
	})
	return res, err
}

// FarmUpgradeStorage: nâng sức chứa kho thêm FarmStorageStep.
func (s *Store) FarmUpgradeStorage(ctx context.Context, userID int64) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		f, err := ensureFarm(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		if f.StorageLevel >= len(StoragePrices) {
			return ErrFarmStorageMax
		}
		if err := op.spend(userID, StoragePrices[f.StorageLevel],
			fmt.Sprintf("Nâng cấp kho lên %d chỗ", storageCap(f.StorageLevel+1))); err != nil {
			return err
		}
		_, err = op.tx.ExecContext(ctx, `UPDATE farms SET storage_level = storage_level + 1 WHERE user_id = ?`, userID)
		return err
	})
	return res, err
}

/* ---------- Chuồng trại và vật nuôi ---------- */

func loadBuildings(ctx context.Context, q queryer, userID int64) (map[string]int, error) {
	rows, err := q.QueryContext(ctx, `SELECT building, level FROM farm_buildings WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var lv int
		if err := rows.Scan(&id, &lv); err != nil {
			return nil, err
		}
		out[id] = lv
	}
	return out, rows.Err()
}

// FarmBuild: xây công trình id ở chỗ mặc định.
func (s *Store) FarmBuild(ctx context.Context, userID int64, id string) (FarmResult, error) {
	return s.FarmBuildAt(ctx, userID, id, nil)
}

// FarmBuildAt: xây công trình id (coop | pasture | kitchen); công trình tự do đặt ở at (nil: chỗ mặc
// định), chỗ phải hợp lệ (farm_place.go).
func (s *Store) FarmBuildAt(ctx context.Context, userID int64, id string, at *Spot) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		b, ok := buildingByID(id)
		if !ok {
			return ErrFarmUnknownBuild
		}
		f, err := ensureFarm(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		built, err := loadBuildings(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		if built[b.ID] > 0 {
			return farmErr("Bạn đã xây %s rồi.", b.Name)
		}
		if levelFor(f.XP) < b.Level {
			return farmErr("Cần đạt cấp %d mới xây được %s.", b.Level, b.Name)
		}
		spot, err := op.placeSpot(userID, b, at)
		if err != nil {
			return err
		}
		if err := op.spend(userID, b.Price, "Xây "+b.Name); err != nil {
			return err
		}
		var x, y any
		if spot != nil {
			x, y = spot.X, spot.Y
		}
		_, err = op.tx.ExecContext(ctx, `INSERT INTO farm_buildings (user_id, building, level, x, y) VALUES (?, ?, 1, ?, ?)`,
			userID, b.ID, x, y)
		return err
	})
	return res, err
}

// FarmUpgradeBuilding: nâng cấp chuồng id (gấp đôi sức chứa, một lần).
func (s *Store) FarmUpgradeBuilding(ctx context.Context, userID int64, id string) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		b, ok := buildingByID(id)
		if !ok {
			return ErrFarmUnknownBuild
		}
		built, err := loadBuildings(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		switch lv := built[b.ID]; {
		case lv == 0:
			return farmErr("Chưa xây %s.", b.Name)
		case lv >= 2:
			return farmErr("%s đã nâng cấp tối đa.", b.Name)
		}
		if err := op.spend(userID, b.UpgradePrice, "Nâng cấp "+b.Name); err != nil {
			return err
		}
		_, err = op.tx.ExecContext(ctx, `UPDATE farm_buildings SET level = 2 WHERE user_id = ? AND building = ?`, userID, b.ID)
		return err
	})
	return res, err
}

type animalRow struct {
	ID    int64
	Kind  AnimalKind
	Ready int64 // 0: đói
}

const (
	animalHungry    = "hungry"
	animalProducing = "producing"
	animalReady     = "ready"
)

func (a *animalRow) state(now int64) string {
	switch {
	case a.Ready == 0:
		return animalHungry
	case now < a.Ready:
		return animalProducing
	default:
		return animalReady
	}
}

func loadAnimals(ctx context.Context, q queryer, userID int64) ([]animalRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, kind, ready_at FROM farm_animals WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []animalRow
	for rows.Next() {
		var a animalRow
		var kind string
		if err := rows.Scan(&a.ID, &kind, &a.Ready); err != nil {
			return nil, err
		}
		var ok bool
		if a.Kind, ok = animalKindByID(kind); !ok {
			continue
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// FarmBuyAnimal: mua một con kind (mới mua thì đói). Cần đủ cấp, đã xây chuồng và chuồng còn chỗ.
func (s *Store) FarmBuyAnimal(ctx context.Context, userID int64, kind string) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		k, ok := animalKindByID(kind)
		if !ok {
			return ErrFarmUnknownAnimal
		}
		f, err := ensureFarm(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		if levelFor(f.XP) < k.Level {
			return farmErr("Cần đạt cấp %d mới nuôi được %s.", k.Level, k.Name)
		}
		b, _ := buildingByID(k.Building)
		built, err := loadBuildings(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		lv := built[b.ID]
		if lv == 0 {
			return farmErr("Cần xây %s trước khi nuôi %s.", b.Name, k.Name)
		}
		animals, err := loadAnimals(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		n := 0
		for _, a := range animals {
			if a.Kind.Building == b.ID {
				n++
			}
		}
		if n >= b.capAt(lv) {
			if lv < 2 {
				return farmErr("%s đã đầy (%d con), nâng cấp để nuôi thêm.", b.Name, n)
			}
			return farmErr("%s đã đầy (%d con).", b.Name, n)
		}
		if err := op.spend(userID, k.Price, "Mua "+k.Name); err != nil {
			return err
		}
		_, err = op.tx.ExecContext(ctx,
			`INSERT INTO farm_animals (user_id, kind, ready_at, created_at) VALUES (?, ?, 0, ?)`, userID, k.ID, op.unix())
		return err
	})
	return res, err
}

// startProducing: con vật a vừa được cho ăn, bắt đầu làm sản phẩm (chín sau Kind.TimeSec).
func (op *farmOp) startProducing(a *animalRow, now int64) error {
	a.Ready = now + a.Kind.TimeSec
	_, err := op.tx.ExecContext(op.ctx, `UPDATE farm_animals SET ready_at = ? WHERE id = ?`, a.Ready, a.ID)
	return err
}

// collectAnimals: thu sản phẩm của các con đã chín trong ready (người gọi đã cắt theo sức chứa kho):
// sản phẩm vào kho, con vật đói lại, cộng XP.
func (op *farmOp) collectAnimals(userID int64, ready []*animalRow) (*Gained, error) {
	var xp int64
	items := map[string]int64{}
	for _, a := range ready {
		if _, err := op.tx.ExecContext(op.ctx, `UPDATE farm_animals SET ready_at = 0 WHERE id = ?`, a.ID); err != nil {
			return nil, err
		}
		a.Ready = 0
		if err := op.addStock(userID, a.Kind.Product, 1); err != nil {
			return nil, err
		}
		items[a.Kind.Product]++
		xp += a.Kind.XP
	}
	lv, err := op.addXP(userID, xp)
	if err != nil {
		return nil, err
	}
	return &Gained{XP: xp, LevelUp: lv, Items: items}, nil
}

func missingFeed(a *animalRow) error {
	return farmErr("Thiếu %s trong kho để cho %s ăn.", itemName(a.Kind.Feed), a.Kind.Name)
}

// FarmFeed: cho con vật id ăn (hoặc mọi con đang đói mà đủ thức ăn nếu all), thức ăn lấy từ kho.
func (s *Store) FarmFeed(ctx context.Context, userID, id int64, all bool) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		now := op.unix()
		animals, err := loadAnimals(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		feed := func(a *animalRow) (bool, error) {
			ok, err := op.takeStock(userID, a.Kind.Feed, a.Kind.FeedQty)
			if err != nil || !ok {
				return false, err
			}
			return true, op.startProducing(a, now)
		}
		if !all {
			for i := range animals {
				a := &animals[i]
				if a.ID != id {
					continue
				}
				if a.state(now) != animalHungry {
					return ErrFarmFed
				}
				if ok, err := feed(a); err != nil {
					return err
				} else if !ok {
					return missingFeed(a)
				}
				return nil
			}
			return ErrFarmNoAnimal
		}
		var firstMissing error
		fed := 0
		for i := range animals {
			a := &animals[i]
			if a.state(now) != animalHungry {
				continue
			}
			ok, err := feed(a)
			if err != nil {
				return err
			}
			if ok {
				fed++
			} else if firstMissing == nil {
				firstMissing = missingFeed(a)
			}
		}
		if fed == 0 {
			if firstMissing != nil {
				return firstMissing
			}
			return ErrFarmNoHungry
		}
		return nil
	})
	return res, err
}

// FarmCollect: thu sản phẩm của con vật id (hoặc mọi con đã có sản phẩm nếu all, lần lượt tới khi kho
// đầy). Mỗi con cho 1 sản phẩm vào kho và XP của nó, rồi đói lại.
func (s *Store) FarmCollect(ctx context.Context, userID, id int64, all bool) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		now := op.unix()
		animals, err := loadAnimals(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		var ready []*animalRow
		for i := range animals {
			a := &animals[i]
			if all && a.state(now) == animalReady {
				ready = append(ready, a)
			} else if !all && a.ID == id {
				if a.state(now) != animalReady {
					return ErrFarmNotProduced
				}
				ready = append(ready, a)
			}
		}
		if len(ready) == 0 {
			if all {
				return ErrFarmNothingCollect
			}
			return ErrFarmNoAnimal
		}
		room, err := op.storageRoom(userID)
		if err != nil {
			return err
		}
		if room < 1 {
			return ErrFarmStorageFull
		}
		ready = ready[:min(int64(len(ready)), room)]
		g, err := op.collectAnimals(userID, ready)
		if err != nil {
			return err
		}
		res.Gained = g
		return nil
	})
	return res, err
}

// FarmTend: chăm nhanh cả một chuồng (coop | pasture) trong một transaction: thu mọi sản phẩm đã chín
// (cắt theo sức chứa kho), rồi cho ăn mọi con đang đói, kể cả con vừa thu. Thức ăn lấy từ kho, thiếu
// thì tự mua ở chợ (không qua kho, nên kho đầy vẫn mua được) nếu đủ cấp và đủ xu; con nào không mua
// đủ thì vẫn đói và không bị trừ kho. Chỉ báo lỗi khi không làm được gì.
func (s *Store) FarmTend(ctx context.Context, userID int64, building string) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		if b, ok := buildingByID(building); !ok || b.ID == "kitchen" {
			return ErrFarmUnknownBuild
		}
		now := op.unix()
		all, err := loadAnimals(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		var animals []*animalRow
		for i := range all {
			if all[i].Kind.Building == building {
				animals = append(animals, &all[i])
			}
		}
		var ready []*animalRow
		hungry0 := 0
		for _, a := range animals {
			switch a.state(now) {
			case animalReady:
				ready = append(ready, a)
			case animalHungry:
				hungry0++
			}
		}
		if len(ready) == 0 && hungry0 == 0 {
			return ErrFarmNothingTend
		}
		room, err := op.storageRoom(userID)
		if err != nil {
			return err
		}
		if room < 1 && hungry0 == 0 {
			return ErrFarmStorageFull
		}
		g := &Gained{}
		if n := min(int64(len(ready)), room); n > 0 {
			if g, err = op.collectAnimals(userID, ready[:n]); err != nil {
				return err
			}
		}
		f, err := ensureFarm(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		level := levelFor(f.XP)
		// spend chỉ nhận khi số dư >= số tiền (không cho nợ thêm): lấy số dư một lần, tự theo dõi.
		w, err := loadWallet(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		budget := w.Balance
		stock := map[string]int64{}
		rows, err := op.tx.QueryContext(ctx, `SELECT item, qty FROM farm_stock WHERE user_id = ?`, userID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var item string
			var qty int64
			if err := rows.Scan(&item, &qty); err != nil {
				rows.Close()
				return err
			}
			stock[item] = qty
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()

		bought := map[string]int64{}
		var spent int64
		var blocked error // lý do đầu tiên một con không được cho ăn vì cấp
		for _, a := range animals {
			if a.Ready != 0 {
				continue
			}
			k := a.Kind
			take := min(stock[k.Feed], k.FeedQty)
			buy := k.FeedQty - take
			if buy > 0 {
				it, ok := itemByID(k.Feed)
				if !ok || it.BuyPrice <= 0 {
					continue
				}
				if level < it.Level {
					if blocked == nil {
						blocked = farmErr("Cần đạt cấp %d mới mua được %s.", it.Level, it.Name)
					}
					continue
				}
				cost := it.BuyPrice * buy
				if cost > budget {
					continue
				}
				budget -= cost
				spent += cost
				bought[k.Feed] += buy
			}
			if take > 0 {
				if ok, err := op.takeStock(userID, k.Feed, take); err != nil {
					return err
				} else if !ok {
					return missingFeed(a)
				}
				stock[k.Feed] -= take
			}
			if err := op.startProducing(a, now); err != nil {
				return err
			}
			g.Fed++
		}
		for id, qty := range bought {
			it, _ := itemByID(id)
			if err := op.spend(userID, it.BuyPrice*qty, fmt.Sprintf("Mua %d %s cho vật nuôi", qty, it.Name)); err != nil {
				return err
			}
		}
		for _, a := range animals {
			if a.Ready == 0 {
				g.Hungry++
			}
		}
		if g.Fed == 0 && len(g.Items) == 0 {
			if blocked != nil {
				return blocked
			}
			return ErrFarmNoXuFeed
		}
		if len(bought) > 0 {
			g.Bought = bought
			g.Xu = -spent
		}
		res.Gained = g
		return nil
	})
	return res, err
}

/* ---------- Đơn hàng (xe tải) ---------- */

type orderRow struct {
	ID     int64
	Slot   int
	Items  []OrderItem // rỗng: ô trống
	Xu, XP int64
	NextAt int64
}

func loadOrders(ctx context.Context, q queryer, userID int64) (map[int]orderRow, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, slot, items, xu, xp, next_at FROM farm_orders WHERE user_id = ? ORDER BY slot`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]orderRow{}
	for rows.Next() {
		var o orderRow
		var items string
		if err := rows.Scan(&o.ID, &o.Slot, &items, &o.Xu, &o.XP, &o.NextAt); err != nil {
			return nil, err
		}
		if items != "" {
			if err := json.Unmarshal([]byte(items), &o.Items); err != nil {
				return nil, err
			}
		}
		out[o.Slot] = o
	}
	return out, rows.Err()
}

// randN: số nguyên ngẫu nhiên trong [0, n) từ nguồn ngẫu nhiên của store.
func (s *Store) randN(n int) int {
	return min(max(int(s.rand()*float64(n)), 0), n-1)
}

// newOrder: đơn 1–3 loại hàng đã mở khoá ở cấp level (nông sản theo cấp cây, sản phẩm theo cấp con
// vật, món chế biến theo cấp món nếu goods = đã có xưởng), mỗi loại 1–4 đơn vị (món chế biến: một
// loại mỗi đơn, 1–2 món). Thưởng xu = ceil(1.35 × tổng giá đơn vị), XP = ceil(xu / 10).
func (s *Store) newOrder(level int, goods bool) ([]OrderItem, int64, int64) {
	var pool []Item
	for _, it := range Items {
		if it.Level <= level && (it.Kind != "goods" || goods) {
			pool = append(pool, it)
		}
	}
	n := min(1+s.randN(3), len(pool))
	var total int64
	out := make([]OrderItem, 0, n)
	for range n {
		j := s.randN(len(pool))
		it := pool[j]
		pool = append(pool[:j:j], pool[j+1:]...)
		qty := int64(1 + s.randN(4))
		if it.Kind == "goods" {
			qty = int64(1 + s.randN(2))
			pool = slices.DeleteFunc(pool, func(x Item) bool { return x.Kind == "goods" })
		}
		out = append(out, OrderItem{Item: it.ID, Qty: qty})
		total += it.Price * qty
	}
	xu := (total*135 + 99) / 100
	return out, xu, (xu + 9) / 10
}

// fillOrders: ô đơn nào chưa có hoặc đã tới giờ có đơn mới thì sinh đơn mới (một dòng mới, id mới).
func (op *farmOp) fillOrders(userID int64) error {
	f, err := ensureFarm(op.ctx, op.tx, userID)
	if err != nil {
		return err
	}
	orders, err := loadOrders(op.ctx, op.tx, userID)
	if err != nil {
		return err
	}
	built, err := loadBuildings(op.ctx, op.tx, userID)
	if err != nil {
		return err
	}
	for slot := range FarmOrderSlots {
		o, ok := orders[slot]
		if ok && (len(o.Items) > 0 || o.NextAt > op.unix()) {
			continue
		}
		if ok {
			if _, err := op.tx.ExecContext(op.ctx, `DELETE FROM farm_orders WHERE id = ?`, o.ID); err != nil {
				return err
			}
		}
		items, xu, xp := op.s.newOrder(levelFor(f.XP), built[kitchenID] > 0)
		raw, _ := json.Marshal(items)
		if _, err := op.tx.ExecContext(op.ctx,
			`INSERT INTO farm_orders (user_id, slot, items, xu, xp) VALUES (?, ?, ?, ?, ?)`,
			userID, slot, string(raw), xu, xp); err != nil {
			return err
		}
	}
	return nil
}

// refillOrders: sinh đơn cho các ô trống đã tới giờ (chỉ mở transaction khi cần).
func (s *Store) refillOrders(ctx context.Context, userID int64) error {
	var live int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM farm_orders WHERE user_id = ? AND (items <> '' OR next_at > ?)`,
		userID, s.now().Unix()).Scan(&live); err != nil {
		return err
	}
	if live >= FarmOrderSlots {
		return nil
	}
	_, err := s.farmTx(ctx, func(op *farmOp) error { return op.fillOrders(userID) })
	return err
}

func (op *farmOp) loadOrder(userID, orderID int64) (orderRow, error) {
	orders, err := loadOrders(op.ctx, op.tx, userID)
	if err != nil {
		return orderRow{}, err
	}
	for _, o := range orders {
		if o.ID == orderID && len(o.Items) > 0 {
			return o, nil
		}
	}
	return orderRow{}, ErrFarmNoOrder
}

func (op *farmOp) emptyOrderSlot(orderID int64, after time.Duration) error {
	_, err := op.tx.ExecContext(op.ctx, `UPDATE farm_orders SET items = '', xu = 0, xp = 0, next_at = ? WHERE id = ?`,
		op.now.Add(after).Unix(), orderID)
	return err
}

// FarmDeliverOrder: giao đơn orderID: đủ hàng thì trừ kho, cộng xu (qua earn) và XP; ô đơn trống, có
// đơn mới sau FarmOrderAfterDeliver.
func (s *Store) FarmDeliverOrder(ctx context.Context, userID, orderID int64) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		o, err := op.loadOrder(userID, orderID)
		if err != nil {
			return err
		}
		stock, err := loadStock(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		for _, it := range o.Items {
			if stock[it.Item] < it.Qty {
				return ErrFarmOrderShort
			}
		}
		for _, it := range o.Items {
			if ok, err := op.takeStock(userID, it.Item, it.Qty); err != nil {
				return err
			} else if !ok {
				return ErrFarmOrderShort
			}
		}
		if err := op.emptyOrderSlot(o.ID, FarmOrderAfterDeliver); err != nil {
			return err
		}
		if err := op.earn(userID, o.Xu, "Giao đơn hàng"); err != nil {
			return err
		}
		if err := op.bump(userID, "earn", o.Xu); err != nil {
			return err
		}
		if err := op.bump(userID, "order", 1); err != nil {
			return err
		}
		lv, err := op.addXP(userID, o.XP)
		if err != nil {
			return err
		}
		res.Gained = &Gained{Xu: o.Xu, XP: o.XP, LevelUp: lv}
		return nil
	})
	return res, err
}

// FarmDiscardOrder: huỷ đơn orderID; ô trống, có đơn mới sau FarmOrderAfterDiscard.
func (s *Store) FarmDiscardOrder(ctx context.Context, userID, orderID int64) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		o, err := op.loadOrder(userID, orderID)
		if err != nil {
			return err
		}
		return op.emptyOrderSlot(o.ID, FarmOrderAfterDiscard)
	})
	return res, err
}

/* ---------- Xem ---------- */

// fillFarmV2 thêm phần 2 vào FarmView: chuồng, vật nuôi, mở rộng (ai xem cũng thấy); danh mục, kho,
// đơn hàng (chỉ chủ).
func (s *Store) fillFarmV2(ctx context.Context, v *FarmView, ownerID int64, f farmRow, now int64) error {
	built, err := loadBuildings(ctx, s.db, ownerID)
	if err != nil {
		return err
	}
	spots, err := loadSpots(ctx, s.db, ownerID)
	if err != nil {
		return err
	}
	v.Buildings = make([]BuildingView, len(buildingDefs))
	for i, b := range buildingDefs {
		lv := built[b.ID]
		bv := BuildingView{ID: b.ID, Name: b.Name, Built: lv > 0, Cap: b.capAt(lv), Price: b.Price, Level: b.Level}
		if b.movable() {
			size, spot := b.Size, b.Default
			if sp, ok := spots[b.ID]; ok {
				spot = sp
			}
			bv.Size, bv.Spot = &size, &spot
		}
		if lv == 1 {
			p := b.UpgradePrice
			bv.UpgradePrice = &p
		}
		v.Buildings[i] = bv
	}
	animals, err := loadAnimals(ctx, s.db, ownerID)
	if err != nil {
		return err
	}
	var stock map[string]int64
	if v.Me {
		if stock, err = loadStock(ctx, s.db, ownerID); err != nil {
			return err
		}
	}
	v.Animals = make([]Animal, len(animals))
	for i := range animals {
		a := &animals[i]
		av := Animal{ID: a.ID, Kind: a.Kind.ID, State: a.state(now)}
		if a.Ready > 0 {
			av.ReadyAt = rfc(a.Ready)
		}
		av.CanFeed = v.Me && av.State == animalHungry && stock[a.Kind.Feed] >= a.Kind.FeedQty
		v.Animals[i] = av
	}
	if err := s.fillKitchen(ctx, v, ownerID, now); err != nil {
		return err
	}
	if !v.Me {
		return nil
	}
	v.Items, v.AnimalKinds, v.Blocked = Items, AnimalKinds, FarmBlocked
	v.Storage = &StorageView{Cap: storageCap(f.StorageLevel), Used: stockUsed(stock), Stock: stock}
	if f.StorageLevel < len(StoragePrices) {
		v.Storage.NextUpgrade = &PriceTag{Price: StoragePrices[f.StorageLevel]}
	}
	orders, err := loadOrders(ctx, s.db, ownerID)
	if err != nil {
		return err
	}
	v.Orders = make([]Order, FarmOrderSlots)
	for slot := range FarmOrderSlots {
		ov := Order{Slot: slot}
		o, ok := orders[slot]
		switch {
		case ok && len(o.Items) > 0:
			can := true
			for _, it := range o.Items {
				can = can && stock[it.Item] >= it.Qty
			}
			ov.ID, ov.Items, ov.Xu, ov.XP, ov.CanDeliver = o.ID, o.Items, o.Xu, o.XP, &can
		case ok:
			ov.NextAt = rfc(o.NextAt)
		default:
			// chưa sinh (vừa có người xoá giữa chừng): đơn mới có ngay khi tải lại
			ov.NextAt = rfc(now)
		}
		v.Orders[slot] = ov
	}
	return nil
}
