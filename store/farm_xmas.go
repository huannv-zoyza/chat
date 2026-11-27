package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"chat/model"
)

// Sự kiện Giáng sinh (specs/FARM_SPEC.md §16), chạy XmasFrom..XmasTo (ngày giờ VN, tính cả hai đầu):
//   - Đơn của Ông già Noel: mỗi ngày 3 đơn riêng, giao đủ hàng được Kẹo Noel + XP; giao đủ cả 3 đơn
//     trong ngày được thưởng thêm theo chuỗi ngày liên tiếp. Kẹo đổi quà ở cửa hàng Noel (mở tới XmasShopTo).
//   - Tất Noel: mỗi ngày treo tối đa 3 tất (món trong kho) ở vườn người khác; chủ vườn mở từ hôm sau,
//     lúc đó mới biết ai tặng. Người treo và người mở đều được kẹo.
//   - Trong sự kiện không ai trộm được vườn ai (stealsPerDay = 0).

// Ngày sự kiện là var để thử nghiệm / đổi lịch không phải sửa logic.
var (
	XmasFrom   = "2026-12-20"
	XmasTo     = "2027-01-01"
	XmasShopTo = "2027-01-07" // còn một tuần để tiêu kẹo sau sự kiện
)

const (
	XmasOrders        = 3
	XmasOrderQtyMul   = 2  // đơn Noel nhiều gấp đôi đơn xe tải
	XmasCandyPerXu    = 60 // kẹo của đơn = ceil(giá trị đơn / 60), trong khoảng XmasOrderCandyMin..Max
	XmasOrderCandyMin = 4  // sàn cao, trần thấp: đơn người mới (hàng rẻ) không thua xa đơn người chơi lâu
	XmasOrderCandyMax = 8
	// Ưu đãi tân thủ: dưới cấp XmasNewbieLevel mọi kẹo nhận được +XmasNewbiePct% (làm tròn lên).
	XmasNewbieLevel = 6
	XmasNewbiePct   = 50
	XmasStreakCandy = 5 // giao đủ 3 đơn: thêm 5 kẹo × số ngày liên tiếp (tối đa XmasStreakMax)
	XmasStreakMax   = 7
	XmasSocksPerDay = 3
	XmasSockMaxQty  = 5
	XmasSockCandy   = 2 // mỗi tất: người treo được 2 kẹo lúc treo, người mở được 2 kẹo lúc mở
)

func xmasOn(day string) bool       { return day >= XmasFrom && day <= XmasTo }
func xmasShopOpen(day string) bool { return day >= XmasFrom && day <= XmasShopTo }

// stealsPerDay: lượt trộm mỗi ngày; mùa Giáng sinh thì 0 ("đêm an lành").
func stealsPerDay(day string) int64 {
	if xmasOn(day) {
		return 0
	}
	return FarmStealsPerDay
}

var (
	ErrXmasOff        = &FarmError{"Sự kiện Giáng sinh chưa mở hoặc đã kết thúc."}
	ErrXmasShopOff    = &FarmError{"Cửa hàng Noel đã đóng."}
	ErrXmasSteal      = &FarmError{"Mùa Giáng sinh an lành, không trộm vườn ai nhé 🎄"}
	ErrXmasOrderDone  = &FarmError{"Đơn này đã giao rồi."}
	ErrXmasCandy      = &FarmError{"Không đủ Kẹo Noel."}
	ErrXmasNoReward   = &FarmError{"Không có món quà này."}
	ErrXmasSockSelf   = &FarmError{"Treo tất ở vườn người khác nhé."}
	ErrXmasSockDaily  = &FarmError{"Hôm nay bạn đã treo đủ 3 chiếc tất rồi."}
	ErrXmasSockHung   = &FarmError{"Hôm nay bạn đã treo tất ở vườn này rồi."}
	ErrXmasNoSock     = &FarmError{"Chưa có tất nào mở được. Tất treo hôm nay thì mai mới mở nhé."}
	ErrXmasSockBadQty = &FarmError{"Mỗi chiếc tất bỏ được 1–5 món."}
)

// XmasReward: một món ở cửa hàng Noel: đổi Cost kẹo lấy Xu xu hoặc Qty đơn vị Item vào kho.
type XmasReward struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Emoji string `json:"emoji"`
	Cost  int64  `json:"cost"`
	Xu    int64  `json:"xu,omitempty"`
	Item  string `json:"item,omitempty"`
	Qty   int64  `json:"qty,omitempty"`
	// Kind khác rỗng: phiếu xây nông trại (expand: mở rộng đất giảm 50% | storage: nâng kho miễn phí |
	// tree: phiếu cây giống miễn phí, dùng ở lần trồng cây ăn quả kế tiếp).
	Kind string `json:"kind,omitempty"`
	Note string `json:"note,omitempty"`
	// DailyMax > 0: mỗi ngày đổi tối đa bấy nhiêu lần (túi xu: chặn lạm phát xu dùng chung với cả app).
	DailyMax int `json:"daily_max,omitempty"`
}

var XmasShop = []XmasReward{
	{ID: "xu", Name: "Túi 500 xu", Emoji: "💰", Cost: 15, Xu: 500, DailyMax: 2},
	{ID: "expand", Name: "Phiếu mở rộng đất -50%", Emoji: "🗺️", Cost: 30, Kind: "expand",
		Note: "Mở rộng lên bậc kế ngay, trả một nửa giá (vẫn cần đủ cấp)"},
	{ID: "storage", Name: "Phiếu nâng kho miễn phí", Emoji: "🏚️", Cost: 25, Kind: "storage",
		Note: "Nâng kho một bậc ngay, không tốn xu"},
	{ID: "tree", Name: "Phiếu cây giống miễn phí", Emoji: "🌳", Cost: 20, Kind: "tree",
		Note: "Lần trồng cây ăn quả kế tiếp không tốn xu"},
	{ID: "len", Name: "5 cuộn len", Emoji: "🧶", Cost: 15, Item: "len", Qty: 5},
	{ID: "nam", Name: "3 nấm truffle", Emoji: "🍄", Cost: 18, Item: "nam", Qty: 3},
	{ID: "ao_len", Name: "2 áo len", Emoji: "🧥", Cost: 25, Item: "ao_len", Qty: 2},
}

type XmasOrder struct {
	Slot       int         `json:"slot"`
	Items      []OrderItem `json:"items"`
	Candy      int64       `json:"candy"`
	Xu         int64       `json:"xu"`
	XP         int64       `json:"xp"`
	Done       bool        `json:"done"`
	CanDeliver bool        `json:"can_deliver"`
}

// XmasSock: một chiếc tất vừa mở (ai tặng, món gì).
type XmasSock struct {
	From model.PublicUser `json:"from"`
	Item string           `json:"item"`
	Qty  int64            `json:"qty"`
}

// XmasView: phần sự kiện trong FarmView của chính mình.
type XmasView struct {
	From         string       `json:"from"`
	To           string       `json:"to"`
	ShopTo       string       `json:"shop_to"`
	On           bool         `json:"on"` // đang trong sự kiện (đơn Noel, treo tất); false: chỉ còn cửa hàng / mở tất
	Candy        int64        `json:"candy"`
	Orders       []XmasOrder  `json:"orders"`
	Streak       int          `json:"streak"` // số ngày liên tiếp giao đủ 3 đơn (tới hôm nay nếu hôm nay đã đủ)
	StreakCandy  int64        `json:"streak_candy"`
	StreakMax    int          `json:"streak_max"`
	Shop         []XmasReward `json:"shop"`
	ShopOpen     bool         `json:"shop_open"`
	SocksLeft    int          `json:"socks_left"`    // tất còn treo được hôm nay
	SockMaxQty   int64        `json:"sock_max_qty"`  // món tối đa mỗi tất
	SocksReady   int          `json:"socks_ready"`   // tất người khác treo từ hôm trước, mở được
	SocksWaiting int          `json:"socks_waiting"` // tất người khác treo hôm nay, mai mới mở

	// farm_xmas_fun.go
	Golden     bool           `json:"golden"` // đang giờ vàng: kẹo x2
	GoldenDay  string         `json:"golden_day"`
	GoldenFrom int            `json:"golden_from"`
	GoldenTo   int            `json:"golden_to"`
	SnowLeft   int            `json:"snow_left"` // cầu tuyết còn ném được hôm nay
	Drops      []XmasDropView `json:"drops"`     // quà Ông già Noel đang chờ nhặt khắp server
	Tree       XmasTreeView   `json:"tree"`
	Top        []XmasTopRow   `json:"top"`    // top kẹo đã kiếm
	Earned     int64          `json:"earned"` // tổng kẹo mình đã kiếm
	BoxCost    int64          `json:"box_cost"`
	// Ưu đãi tân thủ / giới hạn kẹo góp cây (cân bằng người mới – người chơi lâu)
	Newbie        bool           `json:"newbie"`
	NewbieLevel   int            `json:"newbie_level"`
	NewbiePct     int            `json:"newbie_pct"`
	TreeCandyLeft int64          `json:"tree_candy_left"` // kẹo còn nhận được từ góp cây hôm nay
	TreeCandyDay  int64          `json:"tree_candy_day"`
	TreeMinShare  int64          `json:"tree_min_share"` // điểm góp tối thiểu để nhận thưởng mốc
	DailyGift     bool           `json:"daily_gift"`     // quà dưới gốc cây hôm nay chưa mở
	ShopLeft      map[string]int `json:"shop_left"`      // món giới hạn mỗi ngày: còn đổi được mấy lần
	TreeVouchers  int64          `json:"tree_vouchers"`  // phiếu cây giống đang có
	Stats         XmasStats      `json:"stats"`          // thống kê sự kiện (cả nhóm)
	// Cao trào (farm_xmas_finale.go)
	SpecialDecors []XmasDecor         `json:"special_decors"`
	Milestones    []XmasMilestoneView `json:"milestones"`
	Santa         XmasSantaView       `json:"santa"`
	Eve           XmasEveView         `json:"eve"`
	Decors        []XmasDecor         `json:"decors"`
}

/* ---------- Phiếu cây giống ---------- */

// Phiếu cây giống lưu trong farm_counters với "ngày" cố định (không hết hạn theo ngày).
const (
	xmasVoucherDay = "xmas"
	treeVoucherKey = "tree_voucher"
)

// useTreeVoucher dùng một phiếu cây giống nếu có (true: lần trồng này miễn phí).
func (op *farmOp) useTreeVoucher(userID int64) (bool, error) {
	r, err := op.tx.ExecContext(op.ctx, `UPDATE farm_counters SET n = n - 1 WHERE user_id = ? AND day = ? AND key = ? AND n > 0`,
		userID, xmasVoucherDay, treeVoucherKey)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n > 0, err
}

// XmasOrderXuPct: đơn Noel trả thêm xu = 40% đơn xe tải cùng hàng (để người chơi lâu giao đơn Noel không lỗ nhiều).
const XmasOrderXuPct = 40

func xmasOrderXu(items []OrderItem) int64 {
	var total int64
	for _, it := range items {
		if i, ok := itemByID(it.Item); ok {
			total += i.Price * it.Qty
		}
	}
	return (total*135*XmasOrderXuPct + 9999) / 10000
}

/* ---------- Kẹo ---------- */

func xmasCandy(ctx context.Context, q execQuerier, userID int64) (int64, error) {
	var n int64
	err := q.QueryRowContext(ctx, `SELECT candy FROM farm_xmas WHERE user_id = ?`, userID).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return n, err
}

// xmasNewbie: userID còn dưới cấp XmasNewbieLevel (được ưu đãi tân thủ).
func xmasNewbie(ctx context.Context, q execQuerier, userID int64) (bool, error) {
	f, err := readFarm(ctx, q, userID) // chưa có nông trại: XP 0, cấp 1
	return err == nil && levelFor(f.XP) < XmasNewbieLevel, err
}

// addCandy cộng n kẹo (tân thủ +50%, giờ vàng thì gấp đôi) vào ví kẹo và tổng đã kiếm; trả về số kẹo thật cộng.
func (op *farmOp) addCandy(userID, n int64) (int64, error) {
	if n <= 0 {
		return 0, nil
	}
	if newbie, err := xmasNewbie(op.ctx, op.tx, userID); err != nil {
		return 0, err
	} else if newbie {
		n += (n*XmasNewbiePct + 99) / 100
	}
	if xmasGolden(op.now) {
		n *= 2
	}
	_, err := op.tx.ExecContext(op.ctx, `
		INSERT INTO farm_xmas (user_id, candy, earned) VALUES (?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET candy = candy + excluded.candy, earned = earned + excluded.earned`,
		userID, n, n)
	return n, err
}

func (op *farmOp) spendCandy(userID, n int64) error {
	r, err := op.tx.ExecContext(op.ctx,
		`UPDATE farm_xmas SET candy = candy - ? WHERE user_id = ? AND candy >= ?`, n, userID, n)
	if err != nil {
		return err
	}
	if k, err := r.RowsAffected(); err != nil {
		return err
	} else if k == 0 {
		return ErrXmasCandy
	}
	return nil
}

/* ---------- Đơn của Ông già Noel ---------- */

type xmasOrderRow struct {
	Slot      int
	Items     []OrderItem
	Candy, XP int64
	Done      bool
}

func loadXmasOrders(ctx context.Context, q queryer, userID int64, day string) ([]xmasOrderRow, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT slot, items, candy, xp, done FROM farm_xmas_orders WHERE user_id = ? AND day = ? ORDER BY slot`,
		userID, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []xmasOrderRow
	for rows.Next() {
		var o xmasOrderRow
		var items string
		if err := rows.Scan(&o.Slot, &items, &o.Candy, &o.XP, &o.Done); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(items), &o.Items); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ensureXmasOrders: sinh 3 đơn Noel hôm nay cho userID nếu chưa có (cùng bể hàng với đơn xe tải, gấp
// XmasOrderQtyMul lần số lượng; thưởng kẹo thay cho xu).
func (s *Store) ensureXmasOrders(ctx context.Context, userID int64, day string) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM farm_xmas_orders WHERE user_id = ? AND day = ?`,
		userID, day).Scan(&n); err != nil || n >= XmasOrders {
		return err
	}
	_, err := s.farmTx(ctx, func(op *farmOp) error {
		level, built, fruits, err := op.orderPool(userID)
		if err != nil {
			return err
		}
		for slot := range XmasOrders {
			items, xu, xp := s.newOrder(day, level, built, fruits)
			for i := range items {
				items[i].Qty *= XmasOrderQtyMul
			}
			xu *= XmasOrderQtyMul
			candy := min(XmasOrderCandyMax, max(XmasOrderCandyMin, (xu+XmasCandyPerXu-1)/XmasCandyPerXu))
			raw, _ := json.Marshal(items)
			if _, err := op.tx.ExecContext(ctx, `
				INSERT OR IGNORE INTO farm_xmas_orders (user_id, day, slot, items, candy, xp) VALUES (?, ?, ?, ?, ?, ?)`,
				userID, day, slot, string(raw), candy, xp); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// xmasStreak: số ngày liên tiếp (lùi từ hôm nay, hoặc hôm qua nếu hôm nay chưa đủ) đã giao đủ 3 đơn Noel.
func xmasStreak(ctx context.Context, q queryer, userID int64, day string) (int, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT day FROM farm_xmas_orders WHERE user_id = ? GROUP BY day HAVING SUM(done) >= ?`, userID, XmasOrders)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	full := map[string]bool{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return 0, err
		}
		full[d] = true
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	d := day
	if !full[d] {
		d = prevDay(d)
	}
	n := 0
	for full[d] {
		n++
		d = prevDay(d)
	}
	return n, nil
}

// FarmXmasDeliver: giao đơn Noel slot hôm nay: trừ kho, cộng kẹo và XP; đủ 3 đơn thì thưởng chuỗi ngày.
// Tính vào nhiệm vụ "Giao 2 đơn hàng".
func (s *Store) FarmXmasDeliver(ctx context.Context, userID int64, slot int) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		if !xmasOn(op.day) {
			return ErrXmasOff
		}
		orders, err := loadXmasOrders(ctx, op.tx, userID, op.day)
		if err != nil {
			return err
		}
		var o *xmasOrderRow
		left := 0
		for i := range orders {
			if orders[i].Slot == slot {
				o = &orders[i]
			}
			if !orders[i].Done {
				left++
			}
		}
		if o == nil {
			return ErrFarmNoOrder
		}
		if o.Done {
			return ErrXmasOrderDone
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
		if _, err := op.tx.ExecContext(ctx,
			`UPDATE farm_xmas_orders SET done = 1 WHERE user_id = ? AND day = ? AND slot = ?`,
			userID, op.day, slot); err != nil {
			return err
		}
		candy := o.Candy
		if left == 1 && len(orders) >= XmasOrders { // vừa giao đơn cuối của ngày
			streak, err := xmasStreak(ctx, op.tx, userID, op.day)
			if err != nil {
				return err
			}
			candy += XmasStreakCandy * int64(min(streak, XmasStreakMax))
		}
		if candy, err = op.addCandy(userID, candy); err != nil {
			return err
		}
		if err := op.bump(userID, "order", 1); err != nil {
			return err
		}
		if err := op.xmasStat(userID, StatOrders); err != nil {
			return err
		}
		lv, err := op.addXP(userID, o.XP)
		if err != nil {
			return err
		}
		xu := xmasOrderXu(o.Items)
		if err := op.earn(userID, xu, "Đơn của Ông già Noel"); err != nil {
			return err
		}
		if err := op.bump(userID, "earn", xu); err != nil {
			return err
		}
		res.Gained = &Gained{Xu: xu, XP: o.XP, LevelUp: lv, Candy: candy}
		return nil
	})
	return res, err
}

// FarmXmasBuy: đổi kẹo lấy quà rewardID ở cửa hàng Noel.
func (s *Store) FarmXmasBuy(ctx context.Context, userID int64, rewardID string) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		if !xmasShopOpen(op.day) {
			return ErrXmasShopOff
		}
		var r *XmasReward
		for i := range XmasShop {
			if XmasShop[i].ID == rewardID {
				r = &XmasShop[i]
			}
		}
		if r == nil {
			return ErrXmasNoReward
		}
		buyKey := "xmas_buy_" + r.ID
		if r.DailyMax > 0 {
			n, err := counter(ctx, op.tx, userID, op.day, buyKey)
			if err != nil {
				return err
			}
			if n >= int64(r.DailyMax) {
				return farmErr("Hôm nay bạn đã đổi %s đủ %d lần rồi, mai đổi tiếp nhé.", r.Name, r.DailyMax)
			}
			if err := op.bump(userID, buyKey, 1); err != nil {
				return err
			}
		}
		switch r.Kind { // phiếu xây nông trại: làm ngay, lỗi (chưa đủ cấp, kho tối đa...) thì không mất kẹo
		case "expand":
			if err := op.expand(userID, 50); err != nil {
				return err
			}
		case "storage":
			if err := op.upgradeStorage(userID, 0); err != nil {
				return err
			}
		case "tree":
			if err := bump(ctx, op.tx, userID, xmasVoucherDay, treeVoucherKey, 1); err != nil {
				return err
			}
		}
		if r.Item != "" {
			if room, err := op.storageRoom(userID); err != nil {
				return err
			} else if room < r.Qty {
				return ErrFarmStorageFull
			}
		}
		if err := op.spendCandy(userID, r.Cost); err != nil {
			return err
		}
		g := &Gained{Candy: -r.Cost}
		if r.Xu > 0 {
			if err := op.earn(userID, r.Xu, "Quà Noel: "+r.Name); err != nil {
				return err
			}
			g.Xu = r.Xu
		}
		if r.Item != "" {
			if err := op.addStock(userID, r.Item, r.Qty); err != nil {
				return err
			}
			g.Items = map[string]int64{r.Item: r.Qty}
		}
		res.Gained = g
		return nil
	})
	return res, err
}

/* ---------- Tất Noel ---------- */

// FarmXmasSock: fromID treo một chiếc tất chứa qty itemID (lấy từ kho mình) ở vườn toID. Mỗi người
// tối đa XmasSocksPerDay tất mỗi ngày, mỗi vườn một tất mỗi ngày. Người treo nhận XmasSockCandy kẹo.
func (s *Store) FarmXmasSock(ctx context.Context, fromID, toID int64, itemID string, qty int64) (FarmResult, error) {
	res := FarmResult{OwnerID: toID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		if !xmasOn(op.day) {
			return ErrXmasOff
		}
		if fromID == toID {
			return ErrXmasSockSelf
		}
		if _, err := farmUser(ctx, op.tx, toID); err != nil {
			return err
		}
		it, ok := itemByID(itemID)
		if !ok {
			return ErrFarmUnknownItem
		}
		if qty < 1 || qty > XmasSockMaxQty {
			return ErrXmasSockBadQty
		}
		var hung, here int
		if err := op.tx.QueryRowContext(ctx, `
			SELECT COUNT(*), COALESCE(SUM(to_id = ?), 0) FROM farm_xmas_socks WHERE from_id = ? AND day = ?`,
			toID, fromID, op.day).Scan(&hung, &here); err != nil {
			return err
		}
		if here > 0 {
			return ErrXmasSockHung
		}
		if hung >= XmasSocksPerDay {
			return ErrXmasSockDaily
		}
		if ok, err := op.takeStock(fromID, it.ID, qty); err != nil {
			return err
		} else if !ok {
			return farmErr("Trong kho không đủ %d %s để bỏ vào tất.", qty, it.Name)
		}
		if _, err := op.tx.ExecContext(ctx,
			`INSERT INTO farm_xmas_socks (from_id, to_id, day, item, qty) VALUES (?, ?, ?, ?, ?)`,
			fromID, toID, op.day, it.ID, qty); err != nil {
			return err
		}
		if err := op.xmasStat(fromID, StatSocks); err != nil {
			return err
		}
		candy, err := op.addCandy(fromID, XmasSockCandy)
		if err != nil {
			return err
		}
		res.Gained = &Gained{Candy: candy}
		res.Pokes = []XmasPoke{{UserID: toID, Kind: "sock",
			Text: "🧦 Có ai đó vừa treo tất Noel ở vườn bạn 👀 Mai mới mở được, đoán xem ai tặng!"}}
		to, err := farmUser(ctx, op.tx, toID)
		if err != nil {
			return err
		}
		// cả nhóm biết có tất mới nhưng không biết ai treo
		res.News = fmt.Sprintf("🧦 Có ai đó vừa treo tất Noel ở vườn %s 👀", userLabel(to))
		return nil
	})
	return res, err
}

// FarmXmasOpenSocks: mở mọi chiếc tất người khác treo ở vườn userID từ hôm trước trở về trước: món vào
// kho (cần đủ chỗ cho tất cả), mỗi tất XmasSockCandy kẹo; người tặng nhận nhật ký "sock_opened".
// Mở được cả sau khi sự kiện kết thúc.
func (s *Store) FarmXmasOpenSocks(ctx context.Context, userID int64) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		type sockRow struct {
			ID, From, Qty int64
			Item          string
		}
		rows, err := op.tx.QueryContext(ctx,
			`SELECT id, from_id, item, qty FROM farm_xmas_socks WHERE to_id = ? AND opened = 0 AND day < ? ORDER BY id`,
			userID, op.day)
		if err != nil {
			return err
		}
		var socks []sockRow
		var total int64
		for rows.Next() {
			var r sockRow
			if err := rows.Scan(&r.ID, &r.From, &r.Item, &r.Qty); err != nil {
				rows.Close()
				return err
			}
			socks = append(socks, r)
			total += r.Qty
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(socks) == 0 {
			return ErrXmasNoSock
		}
		if room, err := op.storageRoom(userID); err != nil {
			return err
		} else if room < total {
			return farmErr("Kho cần trống %d chỗ để mở hết tất.", total)
		}
		me, err := farmUser(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		g := &Gained{Items: map[string]int64{}, Candy: XmasSockCandy * int64(len(socks))}
		for _, r := range socks {
			if err := op.addStock(userID, r.Item, r.Qty); err != nil {
				return err
			}
			if _, err := op.tx.ExecContext(ctx, `UPDATE farm_xmas_socks SET opened = 1 WHERE id = ?`, r.ID); err != nil {
				return err
			}
			g.Items[r.Item] += r.Qty
			from, err := farmUser(ctx, op.tx, r.From)
			if errors.Is(err, ErrNotFound) {
				from = model.PublicUser{ID: r.From} // người tặng đã rời đi: quà vẫn nhận
			} else if err != nil {
				return err
			} else if _, err := op.log(r.From, me, "sock_opened", r.Item, r.Qty); err != nil {
				return err
			} else {
				it, _ := itemByID(r.Item)
				res.Pokes = append(res.Pokes, XmasPoke{UserID: r.From, Kind: "sock_opened", From: userID,
					Text: fmt.Sprintf("🧦 %s đã mở tất bạn tặng: %d %s %s", userLabel(me), r.Qty, it.Emoji, it.Name)})
			}
			g.Socks = append(g.Socks, XmasSock{From: from, Item: r.Item, Qty: r.Qty})
		}
		if g.Candy, err = op.addCandy(userID, g.Candy); err != nil {
			return err
		}
		seen := map[int64]bool{}
		var names []string
		for _, x := range g.Socks {
			if !seen[x.From.ID] && x.From.Username != "" {
				seen[x.From.ID] = true
				names = append(names, userLabel(x.From))
			}
		}
		res.News = fmt.Sprintf("🧦 %s vừa mở %d chiếc tất Noel", userLabel(me), len(g.Socks))
		if len(names) > 0 {
			res.News += ", quà của " + strings.Join(names, ", ") + "!"
		}
		res.Gained = g
		return nil
	})
	return res, err
}

/* ---------- Xem ---------- */

// fillXmas thêm phần sự kiện vào FarmView: vườn mình thì XmasView (khi sự kiện / cửa hàng còn mở hoặc
// còn tất chưa mở); vườn người khác trong sự kiện thì XmasSock (người xem treo tất ở đây được không).
func (s *Store) fillXmas(ctx context.Context, v *FarmView, ownerID, viewerID int64, now time.Time) error {
	day := vnDay(now)
	if err := s.fillXmasFun(ctx, v, ownerID, viewerID, now); err != nil {
		return err
	}
	if !v.Me {
		if !xmasOn(day) {
			return nil
		}
		var hung, here int
		if err := s.db.QueryRowContext(ctx, `
			SELECT COUNT(*), COALESCE(SUM(to_id = ?), 0) FROM farm_xmas_socks WHERE from_id = ? AND day = ?`,
			ownerID, viewerID, day).Scan(&hung, &here); err != nil {
			return err
		}
		switch {
		case here > 0:
			v.XmasSock = "hung"
		case hung >= XmasSocksPerDay:
			v.XmasSock = "none_left"
		default:
			v.XmasSock = "can"
		}
		return nil
	}
	var ready, waiting int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(day < ?), 0), COALESCE(SUM(day >= ?), 0) FROM farm_xmas_socks WHERE to_id = ? AND opened = 0`,
		day, day, ownerID).Scan(&ready, &waiting); err != nil {
		return err
	}
	if !xmasShopOpen(day) && ready == 0 {
		// hết sự kiện vẫn hiện tab Noel nếu còn quà Ông già Noel bí mật chưa mở
		var santa int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM farm_xmas_santa WHERE target_id = ? AND opened = 0`,
			ownerID).Scan(&santa); err != nil {
			return err
		}
		if santa == 0 || now.Before(xmasEveTime()) { // santa đếm cả quà bù (người tặng quên gửi)
			return nil
		}
	}
	on := xmasOn(day)
	if on {
		if err := s.ensureXmasOrders(ctx, ownerID, day); err != nil {
			return err
		}
	}
	candy, err := xmasCandy(ctx, s.db, ownerID)
	if err != nil {
		return err
	}
	streak, err := xmasStreak(ctx, s.db, ownerID, day)
	if err != nil {
		return err
	}
	x := &XmasView{From: XmasFrom, To: XmasTo, ShopTo: XmasShopTo, On: on, Candy: candy, Orders: []XmasOrder{},
		Streak: streak, StreakCandy: XmasStreakCandy, StreakMax: XmasStreakMax, Shop: XmasShop,
		ShopOpen: xmasShopOpen(day), SockMaxQty: XmasSockMaxQty, SocksReady: ready, SocksWaiting: waiting}
	if on {
		orders, err := loadXmasOrders(ctx, s.db, ownerID, day)
		if err != nil {
			return err
		}
		var stock map[string]int64
		if v.Storage != nil {
			stock = v.Storage.Stock
		}
		for _, o := range orders {
			can := !o.Done
			for _, it := range o.Items {
				can = can && stock[it.Item] >= it.Qty
			}
			x.Orders = append(x.Orders, XmasOrder{Slot: o.Slot, Items: o.Items, Candy: o.Candy, Xu: xmasOrderXu(o.Items), XP: o.XP,
				Done: o.Done, CanDeliver: can})
		}
		var hung int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM farm_xmas_socks WHERE from_id = ? AND day = ?`,
			ownerID, day).Scan(&hung); err != nil {
			return err
		}
		x.SocksLeft = max(0, XmasSocksPerDay-hung)
	}
	if err := s.fillXmasMine(ctx, x, ownerID, now); err != nil {
		return err
	}
	v.Xmas = x
	return nil
}
