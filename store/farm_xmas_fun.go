package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"time"

	"chat/model"
)

// Phần "cho vui" của sự kiện Giáng sinh (specs/FARM_SPEC.md §16): giờ vàng, cầu tuyết, quà Ông già Noel
// thả ngẫu nhiên, hộp quà bí ẩn + đồ trang trí hiếm, cây thông chung toàn server, bảng xếp hạng kẹo.

// xq: *sql.DB hoặc *sql.Tx (đọc nhiều dòng và gọi farmUser).
type xq interface {
	execQuerier
	queryer
}

// XmasPoke: tin báo riêng cho UserID (bị ném tuyết, có tất mới, tất được mở, quà ở vườn bị nhặt).
// Kind: snowball (kèm From để ném trả) | snowball_back | sock | sock_opened | drop_claimed.
type XmasPoke struct {
	UserID int64  `json:"-"`
	Kind   string `json:"kind"`
	Text   string `json:"text"`
	From   int64  `json:"from_id,omitempty"`
}

/* ---------- Giờ vàng ---------- */

var (
	XmasGoldenDay  = "2026-12-24"
	XmasGoldenFrom = 20 // giờ VN, [From, To)
	XmasGoldenTo   = 24
)

// xmasGolden: đang giờ vàng đêm Noel (mọi kẹo nhận được gấp đôi).
func xmasGolden(t time.Time) bool {
	v := t.In(vnZone)
	return vnDay(t) == XmasGoldenDay && v.Hour() >= XmasGoldenFrom && v.Hour() < XmasGoldenTo
}

/* ---------- Cầu tuyết ---------- */

const (
	XmasSnowballsPerDay = 10
	XmasSnowCooldown    = 30 * time.Minute // ném lại cùng một vườn
	XmasSnowHitChance   = 0.7
	XmasSnowHitCandy    = 1
	XmasSnowRevenge     = 2 // thêm khi ném trả người vừa ném trúng mình trong XmasSnowCooldown
	XmasSnowedFor       = 15 * time.Minute
	snowTryKey          = "snowball_try"
	snowQuestKey        = "snowball"
)

var (
	ErrXmasSnowSelf     = &FarmError{"Ném cầu tuyết vào vườn người khác nhé ⛄"}
	ErrXmasSnowDaily    = &FarmError{"Hôm nay bạn đã ném hết 10 quả cầu tuyết rồi."}
	ErrXmasSnowCooldown = &FarmError{"Vừa ném vườn này rồi, 30 phút nữa mới ném tiếp được."}
)

// FarmXmasSnowball: fromID ném cầu tuyết vào vườn toID. Trúng (70%): +1 kẹo, ném trả người vừa ném trúng
// mình thì +2 nữa; chủ vườn nhận nhật ký "snowball" và vườn bị phủ tuyết XmasSnowedFor. Trượt: mất lượt.
func (s *Store) FarmXmasSnowball(ctx context.Context, fromID, toID int64) (FarmResult, error) {
	res := FarmResult{OwnerID: toID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		if !xmasOn(op.day) {
			return ErrXmasOff
		}
		if fromID == toID {
			return ErrXmasSnowSelf
		}
		if _, err := farmUser(ctx, op.tx, toID); err != nil {
			return err
		}
		tries, err := counter(ctx, op.tx, fromID, op.day, snowTryKey)
		if err != nil {
			return err
		}
		if tries >= XmasSnowballsPerDay {
			return ErrXmasSnowDaily
		}
		now := op.unix()
		cd := int64(XmasSnowCooldown / time.Second)
		var recent, revenge int
		if err := op.tx.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(from_id = ? AND to_id = ?), 0),
			       COALESCE(SUM(from_id = ? AND to_id = ? AND hit = 1), 0)
			FROM farm_xmas_snowballs WHERE at > ?`,
			fromID, toID, toID, fromID, now-cd).Scan(&recent, &revenge); err != nil {
			return err
		}
		if recent > 0 {
			return ErrXmasSnowCooldown
		}
		if err := op.bump(fromID, snowTryKey, 1); err != nil {
			return err
		}
		hit := s.rand() < XmasSnowHitChance
		if _, err := op.tx.ExecContext(ctx, `INSERT INTO farm_xmas_snowballs (from_id, to_id, at, hit) VALUES (?, ?, ?, ?)`,
			fromID, toID, now, hit); err != nil {
			return err
		}
		me, err := farmUser(ctx, op.tx, fromID)
		if err != nil {
			return err
		}
		them, err := farmUser(ctx, op.tx, toID)
		if err != nil {
			return err
		}
		if !hit { // nhóm nhỏ: trượt cũng báo cả nhóm cho vui
			res.Gained = &Gained{Snow: "miss"}
			res.News = fmt.Sprintf("⛄ %s ném trượt vườn %s 😅", userLabel(me), userLabel(them))
			return nil
		}
		g := &Gained{Snow: "hit"}
		candy := int64(XmasSnowHitCandy)
		if revenge > 0 {
			candy += XmasSnowRevenge
			g.Snow = "revenge"
		}
		if g.Candy, err = op.addCandy(fromID, candy); err != nil {
			return err
		}
		if err := op.bump(fromID, snowQuestKey, 1); err != nil {
			return err
		}
		res.Gained = g
		if g.Snow == "revenge" { // người bị ném vừa ném mình: đang chờ hết 30 phút, không ném trả ngay được
			res.Pokes = []XmasPoke{{UserID: toID, Kind: "snowball_back", From: fromID,
				Text: fmt.Sprintf("⛄ %s ném trả trúng vườn bạn rồi! 😆", userLabel(me))}}
			res.News = fmt.Sprintf("⛄ %s ném trả trúng vườn %s! 😆", userLabel(me), userLabel(them))
		} else {
			res.News = fmt.Sprintf("⛄ %s ném cầu tuyết trúng vườn %s!", userLabel(me), userLabel(them))
			res.Pokes = []XmasPoke{{UserID: toID, Kind: "snowball", From: fromID,
				Text: fmt.Sprintf("⛄ %s ném cầu tuyết trúng vườn bạn! Ném trả trong 30 phút được thêm kẹo.", userLabel(me))}}
		}
		res.Log, err = op.log(toID, me, "snowball", "", 0)
		return err
	})
	return res, err
}

/* ---------- Quà Ông già Noel thả ngẫu nhiên ---------- */

const (
	XmasDropFor        = 10 * time.Minute
	XmasDropCandy      = 10 // người nhặt
	XmasDropXu         = 100
	XmasDropOwnerCandy = 3 // chủ vườn có quà (khi người khác nhặt)
)

// XmasDropHours: mỗi ngày sự kiện 4 lượt thả, mỗi lượt vào một phút ngẫu nhiên (cố định theo ngày) trong
// 2 giờ kể từ các mốc này (giờ VN).
var XmasDropHours = []int{9, 13, 17, 20}

var (
	ErrXmasNoDrop   = &FarmError{"Quà này không còn nữa."}
	ErrXmasDropGone = &FarmError{"Chậm chân rồi, đã có người nhặt mất quà 😢"}
)

type XmasDropView struct {
	ID        int64            `json:"id"`
	Owner     model.PublicUser `json:"owner"`
	ExpiresAt string           `json:"expires_at"`
}

// xmasDropTimes: giờ các lượt thả trong ngày day.
func xmasDropTimes(day string) []time.Time {
	d, err := time.ParseInLocation("2006-01-02", day, vnZone)
	if err != nil {
		return nil
	}
	out := make([]time.Time, len(XmasDropHours))
	for i, hr := range XmasDropHours {
		h := fnv.New32a()
		fmt.Fprintf(h, "xmas-drop|%s|%d", day, i)
		out[i] = d.Add(time.Duration(hr)*time.Hour + time.Duration(h.Sum32()%120)*time.Minute)
	}
	return out
}

// FarmXmasDropTick (gọi định kỳ): tới giờ một lượt thả mà chưa thả thì thả quà ở vườn một người chơi
// ngẫu nhiên (ưu tiên người có chơi hôm nay). Trả về quà vừa thả (nil nếu không có) để báo cả server.
func (s *Store) FarmXmasDropTick(ctx context.Context) (*XmasDropView, error) {
	now := s.now()
	day := vnDay(now)
	if !xmasOn(day) {
		return nil, nil
	}
	for i, at := range xmasDropTimes(day) {
		if now.Before(at) || !now.Before(at.Add(XmasDropFor)) {
			continue
		}
		slot := fmt.Sprintf("%s|%d", day, i)
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM farm_xmas_drops WHERE slot = ?`, slot).
			Scan(&exists); err != nil || exists > 0 {
			return nil, err
		}
		owner, err := s.pickDropOwner(ctx, day)
		if err != nil || owner.ID == 0 {
			return nil, err
		}
		exp := at.Add(XmasDropFor).Unix()
		r, err := s.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO farm_xmas_drops (slot, owner_id, at, expires_at) VALUES (?, ?, ?, ?)`,
			slot, owner.ID, now.Unix(), exp)
		if err != nil {
			return nil, err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return nil, nil // máy khác / lượt tick khác vừa thả
		}
		id, _ := r.LastInsertId()
		return &XmasDropView{ID: id, Owner: owner, ExpiresAt: rfc(exp)}, nil
	}
	return nil, nil
}

// pickDropOwner: người chơi thật ngẫu nhiên, ưu tiên người có hoạt động nông trại hôm nay.
func (s *Store) pickDropOwner(ctx context.Context, day string) (model.PublicUser, error) {
	for _, q := range []string{
		`SELECT DISTINCT user_id FROM farm_counters WHERE day = ?`,
		`SELECT user_id FROM farms WHERE ? <> ''`,
	} {
		rows, err := s.db.QueryContext(ctx, q, day)
		if err != nil {
			return model.PublicUser{}, err
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return model.PublicUser{}, err
			}
			ids = append(ids, id)
		}
		rows.Close()
		for len(ids) > 0 {
			j := s.randN(len(ids))
			u, err := farmUser(ctx, s.db, ids[j])
			if err == nil {
				return u, nil
			}
			if !errors.Is(err, ErrNotFound) {
				return model.PublicUser{}, err
			}
			ids = append(ids[:j], ids[j+1:]...)
		}
	}
	return model.PublicUser{}, nil
}

// FarmXmasClaimDrop: userID nhặt quà dropID (ai bấm trước được): +10 kẹo, +100 xu; chủ vườn +3 kẹo nếu
// người khác nhặt. Báo cả server ai đã nhặt.
func (s *Store) FarmXmasClaimDrop(ctx context.Context, userID, dropID int64) (FarmResult, error) {
	var res FarmResult
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		var owner, exp int64
		var claimed sql.NullInt64
		err := op.tx.QueryRowContext(ctx, `SELECT owner_id, expires_at, claimed_by FROM farm_xmas_drops WHERE id = ?`,
			dropID).Scan(&owner, &exp, &claimed)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && exp <= op.unix() && !claimed.Valid) {
			return ErrXmasNoDrop
		}
		if err != nil {
			return err
		}
		res.OwnerID = owner
		r, err := op.tx.ExecContext(ctx, `UPDATE farm_xmas_drops SET claimed_by = ? WHERE id = ? AND claimed_by IS NULL`,
			userID, dropID)
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return ErrXmasDropGone
		}
		g := &Gained{Xu: XmasDropXu}
		if g.Candy, err = op.addCandy(userID, XmasDropCandy); err != nil {
			return err
		}
		if err := op.earn(userID, XmasDropXu, "Quà Ông già Noel"); err != nil {
			return err
		}
		me, err := farmUser(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		if owner != userID {
			if _, err := op.addCandy(owner, XmasDropOwnerCandy); err != nil {
				return err
			}
			if res.Log, err = op.log(owner, me, "drop_claimed", "", 0); err != nil {
				return err
			}
			res.Pokes = []XmasPoke{{UserID: owner, Kind: "drop_claimed", From: userID,
				Text: fmt.Sprintf("🎅 %s đã nhặt quà Ông già Noel ở vườn bạn, bạn được %d kẹo.", userLabel(me), XmasDropOwnerCandy)}}
			o, err := farmUser(ctx, op.tx, owner)
			if err != nil {
				return err
			}
			res.News = fmt.Sprintf("🎅 %s đã nhặt được quà Ông già Noel ở vườn %s!", userLabel(me), userLabel(o))
		} else {
			res.News = fmt.Sprintf("🎅 %s đã tự nhặt quà Ông già Noel ở vườn mình!", userLabel(me))
		}
		res.Gained = g
		return nil
	})
	return res, err
}

// activeDrops: quà đang chờ nhặt (chưa ai nhặt, chưa hết hạn); ownerID > 0 thì chỉ vườn đó.
func activeDrops(ctx context.Context, q xq, ownerID, now int64) ([]XmasDropView, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, owner_id, expires_at FROM farm_xmas_drops
		WHERE claimed_by IS NULL AND expires_at > ? AND (? = 0 OR owner_id = ?) ORDER BY id`, now, ownerID, ownerID)
	if err != nil {
		return nil, err
	}
	type row struct{ id, owner, exp int64 }
	var rs []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.owner, &r.exp); err != nil {
			rows.Close()
			return nil, err
		}
		rs = append(rs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]XmasDropView, 0, len(rs))
	for _, r := range rs {
		u, err := farmUser(ctx, q, r.owner)
		if errors.Is(err, ErrNotFound) {
			continue
		} else if err != nil {
			return nil, err
		}
		out = append(out, XmasDropView{ID: r.id, Owner: u, ExpiresAt: rfc(r.exp)})
	}
	return out, nil
}

/* ---------- Quà dưới gốc cây thông (mỗi ngày một lần) ---------- */

const (
	XmasDailyGiftMin = 1
	XmasDailyGiftMax = 3
	xmasDailyGiftKey = "xmas_gift"
)

var ErrXmasGiftTaken = &FarmError{"Hôm nay bạn đã mở quà dưới gốc cây rồi, mai quay lại nhé 🎁"}

// FarmXmasDailyGift: mở hộp quà dưới gốc cây thông trong vườn mình: 1–3 kẹo ngẫu nhiên, mỗi ngày một lần.
func (s *Store) FarmXmasDailyGift(ctx context.Context, userID int64) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		if !xmasOn(op.day) {
			return ErrXmasOff
		}
		r, err := op.tx.ExecContext(ctx, `INSERT OR IGNORE INTO farm_claims (user_id, day, quest) VALUES (?, ?, ?)`,
			userID, op.day, xmasDailyGiftKey)
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return ErrXmasGiftTaken
		}
		g := &Gained{}
		base := int64(XmasDailyGiftMin + s.randN(XmasDailyGiftMax-XmasDailyGiftMin+1))
		if g.Candy, err = op.addCandy(userID, base); err != nil {
			return err
		}
		res.Gained = g
		return nil
	})
	return res, err
}

/* ---------- Hộp quà bí ẩn + đồ trang trí hiếm ---------- */

const XmasBoxCost = 12

// XmasDecor: đồ trang trí Noel hiếm, chỉ có từ hộp quà bí ẩn; hiện trên bản đồ vườn cho mọi người thấy.
type XmasDecor struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Emoji string `json:"emoji"`
}

var XmasDecors = []XmasDecor{
	{"reindeer", "Tuần lộc mũi đỏ", "🦌"},
	{"sleigh", "Xe trượt tuyết", "🛷"},
	{"santa", "Tượng Ông già Noel", "🎅"},
	{"snowman", "Người tuyết khổng lồ", "☃️"},
}

// XmasPrize: thứ vừa mở được từ hộp quà bí ẩn.
type XmasPrize struct {
	Kind  string `json:"kind"` // xu | candy | item | decor
	Xu    int64  `json:"xu,omitempty"`
	Candy int64  `json:"candy,omitempty"`
	Item  string `json:"item,omitempty"`
	Qty   int64  `json:"qty,omitempty"`
	Decor string `json:"decor,omitempty"`
	Rare  bool   `json:"rare,omitempty"`
}

// xmasBoxTable: (ngưỡng tích luỹ, phần thưởng). Decor chỉ khi còn món chưa có, hết thì thành 1000 xu.
var xmasBoxTable = []struct {
	Upto  float64
	Prize XmasPrize
}{
	{0.30, XmasPrize{Kind: "xu", Xu: 200}},
	{0.50, XmasPrize{Kind: "candy", Candy: 8}},
	{0.62, XmasPrize{Kind: "item", Item: "len", Qty: 3}},
	{0.74, XmasPrize{Kind: "item", Item: "nam", Qty: 2}},
	{0.86, XmasPrize{Kind: "item", Item: "ao_len", Qty: 1}},
	{0.94, XmasPrize{Kind: "xu", Xu: 600, Rare: true}},
	{1.01, XmasPrize{Kind: "decor", Rare: true}},
}

func loadDecor(ctx context.Context, q xq, userID int64) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT item FROM farm_xmas_decor WHERE user_id = ? ORDER BY item`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var it string
		if err := rows.Scan(&it); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// FarmXmasBox: đổi XmasBoxCost kẹo lấy một hộp quà bí ẩn. Trúng món hiếm thì báo cả server.
func (s *Store) FarmXmasBox(ctx context.Context, userID int64) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		if !xmasShopOpen(op.day) {
			return ErrXmasShopOff
		}
		if err := op.spendCandy(userID, XmasBoxCost); err != nil {
			return err
		}
		roll := s.rand()
		var p XmasPrize
		for _, e := range xmasBoxTable {
			if roll < e.Upto {
				p = e.Prize
				break
			}
		}
		if p.Kind == "decor" {
			owned, err := loadDecor(ctx, op.tx, userID)
			if err != nil {
				return err
			}
			has := map[string]bool{}
			for _, d := range owned {
				has[d] = true
			}
			var left []XmasDecor
			for _, d := range XmasDecors {
				if !has[d.ID] {
					left = append(left, d)
				}
			}
			if len(left) == 0 {
				p = XmasPrize{Kind: "xu", Xu: 1000, Rare: true}
			} else {
				d := left[s.randN(len(left))]
				p.Decor = d.ID
				if _, err := op.tx.ExecContext(ctx, `INSERT INTO farm_xmas_decor (user_id, item) VALUES (?, ?)`,
					userID, d.ID); err != nil {
					return err
				}
			}
		}
		if p.Kind == "item" {
			if room, err := op.storageRoom(userID); err != nil {
				return err
			} else if room < p.Qty { // kho đầy: đổi thành xu theo giá bán
				it, _ := itemByID(p.Item)
				p = XmasPrize{Kind: "xu", Xu: it.Price * p.Qty}
			}
		}
		g := &Gained{Candy: -XmasBoxCost, Prize: &p}
		switch p.Kind {
		case "xu":
			if err := op.earn(userID, p.Xu, "Hộp quà bí ẩn Noel"); err != nil {
				return err
			}
			g.Xu = p.Xu
		case "candy":
			got, err := op.addCandy(userID, p.Candy)
			if err != nil {
				return err
			}
			p.Candy = got
			g.Candy += got
		case "item":
			if err := op.addStock(userID, p.Item, p.Qty); err != nil {
				return err
			}
			g.Items = map[string]int64{p.Item: p.Qty}
		}
		if p.Kind == "decor" {
			me, err := farmUser(ctx, op.tx, userID)
			if err != nil {
				return err
			}
			for _, d := range XmasDecors {
				if d.ID == p.Decor {
					res.News = fmt.Sprintf("🎁 %s vừa mở hộp quà bí ẩn trúng %s %s!", userLabel(me), d.Emoji, d.Name)
				}
			}
		}
		res.Gained = g
		return nil
	})
	return res, err
}

/* ---------- Cây thông chung toàn server ---------- */

const (
	XmasTreeMaxQty     = 50
	XmasTreePerCandy   = 60  // góp 60 điểm (giá bán) được 1 kẹo
	XmasTreeGoalCandy  = 15  // mỗi mốc, người đã góp nhận
	XmasTreeCandyDay   = 20  // kẹo tối đa mỗi ngày từ góp cây (trước ưu đãi): kho to không đổi hết ra kẹo được
	XmasTreeMinShare   = 100 // góp ít nhất 100 điểm mới nhận thưởng mốc
	treeCandyKey       = "xmas_tree_candy"
	xmasTreeClaimDay   = "xmas"
	xmasTreeClaimQuest = "tree%d"
)

// XmasTreeGoals: các mốc điểm của cây thông chung.
var XmasTreeGoals = []int64{3000, 10000, 25000, 50000}

var (
	ErrXmasTreeBadQty  = &FarmError{"Mỗi lần góp 1–50 món."}
	ErrXmasTreeNoGoal  = &FarmError{"Cây thông chưa đạt mốc này."}
	ErrXmasTreeNoShare = &FarmError{"Góp ít nhất 100 điểm vào cây thông chung mới nhận được thưởng mốc."}
)

type XmasTreeView struct {
	Total   int64        `json:"total"`
	Mine    int64        `json:"mine"`
	Goals   []int64      `json:"goals"`
	Claimed []bool       `json:"claimed"`
	Reward  int64        `json:"reward"`
	Top     []XmasTopRow `json:"top"`
}

type XmasTopRow struct {
	User  model.PublicUser `json:"user"`
	Value int64            `json:"value"`
}

func xmasTreeTotal(ctx context.Context, q execQuerier) (int64, error) {
	var n int64
	err := q.QueryRowContext(ctx, `SELECT COALESCE(SUM(tree), 0) FROM farm_xmas`).Scan(&n)
	return n, err
}

func xmasTreeLevel(total int64) int {
	lv := 0
	for _, g := range XmasTreeGoals {
		if total >= g {
			lv++
		}
	}
	return lv
}

// FarmXmasTreeDonate: góp qty itemID vào cây thông chung: điểm = giá bán × qty, mỗi 60 điểm 1 kẹo (tối
// thiểu 1). Vượt mốc mới thì báo cả server.
func (s *Store) FarmXmasTreeDonate(ctx context.Context, userID int64, itemID string, qty int64) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		if !xmasOn(op.day) {
			return ErrXmasOff
		}
		it, ok := itemByID(itemID)
		if !ok {
			return ErrFarmUnknownItem
		}
		if qty < 1 || qty > XmasTreeMaxQty {
			return ErrXmasTreeBadQty
		}
		if ok, err := op.takeStock(userID, it.ID, qty); err != nil {
			return err
		} else if !ok {
			return farmErr("Trong kho không đủ %d %s để góp.", qty, it.Name)
		}
		before, err := xmasTreeTotal(ctx, op.tx)
		if err != nil {
			return err
		}
		pts := it.Price * qty
		if _, err := op.tx.ExecContext(ctx, `
			INSERT INTO farm_xmas (user_id, tree) VALUES (?, ?)
			ON CONFLICT (user_id) DO UPDATE SET tree = tree + excluded.tree`, userID, pts); err != nil {
			return err
		}
		g := &Gained{}
		got, err := counter(ctx, op.tx, userID, op.day, treeCandyKey)
		if err != nil {
			return err
		}
		// vẫn góp được điểm khi đã hết phần kẹo hôm nay
		if base := min(max(1, pts/XmasTreePerCandy), XmasTreeCandyDay-got); base > 0 {
			if err := op.bump(userID, treeCandyKey, base); err != nil {
				return err
			}
			if g.Candy, err = op.addCandy(userID, base); err != nil {
				return err
			}
		}
		if lv := xmasTreeLevel(before + pts); lv > xmasTreeLevel(before) {
			res.News = fmt.Sprintf("🎄 Cây thông chung đạt mốc %d/%d! Ai đã góp vào tab Noel nhận %d kẹo nhé.",
				lv, len(XmasTreeGoals), XmasTreeGoalCandy)
		}
		res.Gained = g
		return nil
	})
	return res, err
}

// FarmXmasTreeClaim: người đã góp nhận thưởng mốc goal (1-based) khi cây đạt mốc đó.
func (s *Store) FarmXmasTreeClaim(ctx context.Context, userID int64, goal int) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		if !xmasShopOpen(op.day) {
			return ErrXmasShopOff
		}
		total, err := xmasTreeTotal(ctx, op.tx)
		if err != nil {
			return err
		}
		if goal < 1 || goal > xmasTreeLevel(total) {
			return ErrXmasTreeNoGoal
		}
		var mine int64
		if err := op.tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(tree), 0) FROM farm_xmas WHERE user_id = ?`, userID).
			Scan(&mine); err != nil {
			return err
		}
		if mine < XmasTreeMinShare {
			return ErrXmasTreeNoShare
		}
		r, err := op.tx.ExecContext(ctx, `INSERT OR IGNORE INTO farm_claims (user_id, day, quest) VALUES (?, ?, ?)`,
			userID, xmasTreeClaimDay, fmt.Sprintf(xmasTreeClaimQuest, goal))
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return ErrFarmQuestClaimed
		}
		g := &Gained{}
		if g.Candy, err = op.addCandy(userID, XmasTreeGoalCandy); err != nil {
			return err
		}
		res.Gained = g
		return nil
	})
	return res, err
}

/* ---------- Bảng xếp hạng ---------- */

const XmasTopN = 10

// xmasTop: top theo cột col (earned | tree) của farm_xmas, chỉ người > 0.
func xmasTop(ctx context.Context, q xq, col string, n int) ([]XmasTopRow, error) {
	if col != "earned" && col != "tree" {
		return nil, fmt.Errorf("xmasTop: cột %q", col)
	}
	rows, err := q.QueryContext(ctx, `SELECT user_id, `+col+` FROM farm_xmas WHERE `+col+` > 0
		ORDER BY `+col+` DESC, user_id LIMIT ?`, n+5) // dư vài dòng phòng người đã rời đi
	if err != nil {
		return nil, err
	}
	type row struct{ id, v int64 }
	var rs []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.v); err != nil {
			rows.Close()
			return nil, err
		}
		rs = append(rs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []XmasTopRow{}
	for _, r := range rs {
		if len(out) >= n {
			break
		}
		u, err := farmUser(ctx, q, r.id)
		if errors.Is(err, ErrNotFound) {
			continue
		} else if err != nil {
			return nil, err
		}
		out = append(out, XmasTopRow{User: u, Value: r.v})
	}
	return out, nil
}

// FarmXmasTop: top XmasTopN người kiếm nhiều kẹo nhất (rỗng khi ngoài sự kiện / cửa hàng). Dùng cho bảng
// xếp hạng và huy hiệu 🎄 cạnh tên trong chat.
func (s *Store) FarmXmasTop(ctx context.Context) ([]XmasTopRow, error) {
	if !xmasShopOpen(vnDay(s.now())) {
		return []XmasTopRow{}, nil
	}
	return xmasTop(ctx, s.db, "earned", XmasTopN)
}

/* ---------- Xem ---------- */

// fillXmasFun: phần ai xem cũng thấy trong sự kiện (tuyết phủ, quà đang rơi, đồ trang trí, cây chung),
// và cho vườn người khác: XmasSnow (người xem ném cầu tuyết được không).
func (s *Store) fillXmasFun(ctx context.Context, v *FarmView, ownerID, viewerID int64, now time.Time) error {
	day := vnDay(now)
	// đồ trang trí Noel là chiến tích: giữ trên bản đồ cả sau sự kiện
	var err error
	if v.XmasDecor, err = loadDecor(ctx, s.db, ownerID); err != nil {
		return err
	}
	if !xmasShopOpen(day) {
		return nil
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM farm_xmas_socks WHERE to_id = ? AND opened = 0`, ownerID).
		Scan(&v.XmasSocks); err != nil {
		return err
	}
	if !xmasOn(day) {
		return nil
	}
	total, err := xmasTreeTotal(ctx, s.db)
	if err != nil {
		return err
	}
	v.XmasTreeLevel = xmasTreeLevel(total)
	drops, err := activeDrops(ctx, s.db, ownerID, now.Unix())
	if err != nil {
		return err
	}
	if len(drops) > 0 {
		v.XmasDrop = &drops[0]
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT from_id FROM farm_xmas_snowballs WHERE to_id = ? AND hit = 1 AND at > ? ORDER BY at DESC`,
		ownerID, now.Add(-XmasSnowedFor).Unix())
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if u, err := farmUser(ctx, s.db, id); err == nil {
			v.SnowedBy = append(v.SnowedBy, u)
		}
	}
	if v.Me {
		return nil
	}
	tries, err := counter(ctx, s.db, viewerID, day, snowTryKey)
	if err != nil {
		return err
	}
	var recent int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM farm_xmas_snowballs WHERE from_id = ? AND to_id = ? AND at > ?`,
		viewerID, ownerID, now.Add(-XmasSnowCooldown).Unix()).Scan(&recent); err != nil {
		return err
	}
	switch {
	case tries >= XmasSnowballsPerDay:
		v.XmasSnow = "none_left"
	case recent > 0:
		v.XmasSnow = "cooldown"
	default:
		v.XmasSnow = "can"
	}
	return nil
}

// fillXmasMine: phần vườn mình thêm vào XmasView: giờ vàng, lượt ném, quà đang rơi khắp server, cây
// chung, bảng xếp hạng, hộp bí ẩn.
func (s *Store) fillXmasMine(ctx context.Context, x *XmasView, ownerID int64, now time.Time) error {
	day := vnDay(now)
	x.Golden = xmasGolden(now)
	x.GoldenDay, x.GoldenFrom, x.GoldenTo = XmasGoldenDay, XmasGoldenFrom, XmasGoldenTo
	x.BoxCost, x.Decors = XmasBoxCost, XmasDecors
	x.NewbieLevel, x.NewbiePct, x.TreeCandyDay, x.TreeMinShare = XmasNewbieLevel, XmasNewbiePct, XmasTreeCandyDay, XmasTreeMinShare
	var err error
	if x.Newbie, err = xmasNewbie(ctx, s.db, ownerID); err != nil {
		return err
	}
	treeGot, err := counter(ctx, s.db, ownerID, day, treeCandyKey)
	if err != nil {
		return err
	}
	x.TreeCandyLeft = max(0, XmasTreeCandyDay-treeGot)
	if xmasOn(day) {
		claimed, err := claimedQuests(ctx, s.db, ownerID, day)
		if err != nil {
			return err
		}
		x.DailyGift = !claimed[xmasDailyGiftKey]
	}
	tries, err := counter(ctx, s.db, ownerID, day, snowTryKey)
	if err != nil {
		return err
	}
	x.SnowLeft = int(max(0, XmasSnowballsPerDay-tries))
	if x.Drops, err = activeDrops(ctx, s.db, 0, now.Unix()); err != nil {
		return err
	}
	t := XmasTreeView{Goals: XmasTreeGoals, Reward: XmasTreeGoalCandy, Claimed: make([]bool, len(XmasTreeGoals))}
	if t.Total, err = xmasTreeTotal(ctx, s.db); err != nil {
		return err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(tree), 0) FROM farm_xmas WHERE user_id = ?`, ownerID).
		Scan(&t.Mine); err != nil {
		return err
	}
	claimed, err := claimedQuests(ctx, s.db, ownerID, xmasTreeClaimDay)
	if err != nil {
		return err
	}
	for i := range t.Claimed {
		t.Claimed[i] = claimed[fmt.Sprintf(xmasTreeClaimQuest, i+1)]
	}
	if t.Top, err = xmasTop(ctx, s.db, "tree", 5); err != nil {
		return err
	}
	x.Tree = t
	if x.Top, err = xmasTop(ctx, s.db, "earned", XmasTopN); err != nil {
		return err
	}
	if err := s.fillXmasFinale(ctx, x, ownerID, now); err != nil {
		return err
	}
	x.ShopLeft = map[string]int{}
	for _, r := range XmasShop {
		if r.DailyMax > 0 {
			n, err := counter(ctx, s.db, ownerID, day, "xmas_buy_"+r.ID)
			if err != nil {
				return err
			}
			x.ShopLeft[r.ID] = max(0, r.DailyMax-int(n))
		}
	}
	if x.TreeVouchers, err = counter(ctx, s.db, ownerID, xmasVoucherDay, treeVoucherKey); err != nil {
		return err
	}
	return s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(earned), 0) FROM farm_xmas WHERE user_id = ?`, ownerID).
		Scan(&x.Earned)
}

// UserLabel: tên hiển thị của u (cho tin báo ở controller).
func UserLabel(u model.PublicUser) string { return userLabel(u) }
