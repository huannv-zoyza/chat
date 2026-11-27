package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"chat/model"
)

// Cao trào Giáng sinh (specs/FARM_SPEC.md §16c):
//   - Ông già Noel bí mật: đăng ký trước ngày XmasSantaDraw; tới ngày đó tự bốc thăm thành một vòng (mỗi người
//     tặng đúng một người, không ai tự tặng mình). Gửi quà (món trong kho) bất cứ lúc nào trước đêm Noel;
//     đêm Noel lộ ai tặng ai, người nhận mở quà.
//   - Mốc kẹo cá nhân: tổng kẹo đã kiếm đạt mốc thì nhận quà, mốc cuối là đồ trang trí độc quyền.
//   - Đêm Noel (XmasEveAt → hết ngày XmasEveTo): Ông già Noel ghé vườn mọi người, ai vào cũng nhận một
//     món quà lớn kèm chuông Đêm Thánh (đồ trang trí chỉ có đêm Noel); cả server được báo, kèm kết quả bí mật.

// Mốc thời gian là var để đổi lịch / chạy thử không phải sửa logic. XmasEveAt: "YYYY-MM-DDTHH:MM" giờ VN.
var (
	XmasSantaDraw = "2026-12-21"
	XmasEveAt     = "2026-12-24T20:00"
	XmasEveTo     = "2026-12-25"
)

const (
	XmasSantaMaxQty     = 10
	XmasSantaSendCandy  = 5 // người gửi quà bí mật
	XmasSantaOpenCandy  = 5 // người mở quà bí mật
	XmasEveCandy        = 20
	XmasEveXu           = 300
	xmasMetaDrawn       = "santa_drawn"
	xmasMetaEve         = "eve_announced"
	xmasEveClaimQuest   = "eve"
	xmasMilestoneQuest  = "ms%d"
	xmasSantaEveDecorID = "bell"
)

var (
	ErrXmasSantaClosed  = &FarmError{"Đêm Noel đã đến, hết đăng ký Ông già Noel bí mật rồi."}
	ErrXmasSantaFull    = &FarmError{"Mọi người đều đã gói quà xong, không chen vào vòng được nữa 😢"}
	ErrXmasSantaJoined  = &FarmError{"Bạn đã đăng ký rồi, chờ bốc thăm nhé."}
	ErrXmasSantaNoPair  = &FarmError{"Bạn chưa có người để tặng (chưa bốc thăm hoặc không tham gia)."}
	ErrXmasSantaSent    = &FarmError{"Bạn đã gửi quà bí mật rồi."}
	ErrXmasSantaLate    = &FarmError{"Đêm Noel đã đến, không gửi quà bí mật được nữa."}
	ErrXmasSantaNoGift  = &FarmError{"Chưa có quà Ông già Noel bí mật nào để mở."}
	ErrXmasSantaEarly   = &FarmError{"Quà Ông già Noel bí mật chỉ mở được từ đêm Noel."}
	ErrXmasSantaBadQty  = &FarmError{"Quà bí mật gồm 1–10 món."}
	ErrXmasEveNotYet    = &FarmError{"Đêm Noel chưa tới."}
	ErrXmasEveOver      = &FarmError{"Đêm Noel đã qua mất rồi."}
	ErrXmasMilestoneNot = &FarmError{"Chưa đạt mốc kẹo này."}
)

// XmasSpecialDecors: đồ trang trí độc quyền không có trong hộp bí ẩn (mốc kẹo cuối, quà đêm Noel).
var XmasSpecialDecors = []XmasDecor{
	{"star", "Ngôi sao Giáng sinh", "🌟"},
	{xmasSantaEveDecorID, "Chuông Đêm Thánh", "🔔"},
}

// XmasMilestone: mốc tổng kẹo đã kiếm (farm_xmas.earned) và quà.
type XmasMilestone struct {
	Need  int64     `json:"need"`
	Prize XmasPrize `json:"prize"`
}

var XmasMilestones = []XmasMilestone{
	{50, XmasPrize{Kind: "xu", Xu: 300}},
	{120, XmasPrize{Kind: "item", Item: "nam", Qty: 3}},
	{250, XmasPrize{Kind: "item", Item: "ao_len", Qty: 2}},
	{400, XmasPrize{Kind: "decor", Decor: "star", Rare: true}},
}

func xmasEveTime() time.Time {
	t, err := time.ParseInLocation("2006-01-02T15:04", XmasEveAt, vnZone)
	if err != nil {
		return time.Time{}
	}
	return t
}

// xmasEve: now trong đêm Noel (từ XmasEveAt tới hết ngày XmasEveTo).
func xmasEve(now time.Time) bool {
	return !now.Before(xmasEveTime()) && vnDay(now) <= XmasEveTo
}

func xmasMeta(ctx context.Context, q execQuerier, key string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM farm_xmas_meta WHERE key = ?`, key).Scan(&n)
	return n > 0, err
}

// setXmasMeta: đánh dấu key; false nếu đã có (máy khác / lượt khác vừa làm).
func setXmasMeta(ctx context.Context, q execQuerier, key string) (bool, error) {
	r, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO farm_xmas_meta (key) VALUES (?)`, key)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n > 0, err
}

// addDecor tặng đồ trang trí id (đã có thì thôi).
func (op *farmOp) addDecor(userID int64, id string) error {
	_, err := op.tx.ExecContext(op.ctx, `INSERT OR IGNORE INTO farm_xmas_decor (user_id, item) VALUES (?, ?)`, userID, id)
	return err
}

/* ---------- Ông già Noel bí mật ---------- */

// FarmXmasSantaJoin: đăng ký Ông già Noel bí mật, không giới hạn số người, tới trước đêm Noel. Trước ngày bốc
// thăm thì chờ bốc; sau đó thì vào vòng ngay (santaLateJoin).
func (s *Store) FarmXmasSantaJoin(ctx context.Context, userID int64) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		if !xmasOn(op.day) {
			return ErrXmasOff
		}
		if !op.now.Before(xmasEveTime()) {
			return ErrXmasSantaClosed
		}
		var joined int
		if err := op.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM farm_xmas_santa WHERE user_id = ?`, userID).
			Scan(&joined); err != nil {
			return err
		}
		if joined > 0 {
			return ErrXmasSantaJoined
		}
		me, err := farmUser(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		drawn, err := xmasMeta(ctx, op.tx, xmasMetaDrawn)
		if err != nil {
			return err
		}
		if !drawn {
			if _, err := op.tx.ExecContext(ctx, `INSERT INTO farm_xmas_santa (user_id) VALUES (?)`, userID); err != nil {
				return err
			}
			res.News = fmt.Sprintf("🎅 %s vừa đăng ký Ông già Noel bí mật! Bốc thăm ngày %s.", userLabel(me), viDay(XmasSantaDraw))
			return nil
		}
		paired, err := op.santaLateJoin(userID)
		if err != nil {
			return err
		}
		if paired {
			res.News = fmt.Sprintf("🎅 %s vừa vào vòng Ông già Noel bí mật! Ai đó trong nhóm sẽ tặng quà bí mật cho %s 🤫",
				userLabel(me), userLabel(me))
		} else {
			res.News = fmt.Sprintf("🎅 %s đăng ký Ông già Noel bí mật, đang chờ thêm một người nữa để ghép cặp!", userLabel(me))
		}
		return nil
	})
	return res, err
}

// santaLateJoin: thêm userID vào vòng đã bốc thăm. Có vòng thì chen vào sau một người tặng chưa gửi quà
// (A→B thành A→mới→B, không đổi người nhận của ai đã gói quà); chưa có vòng (lúc bốc thăm thiếu người) thì
// đủ 2 người chờ là xếp vòng cho họ. Trả về true nếu userID đã có người tặng / người nhận.
func (op *farmOp) santaLateJoin(userID int64) (bool, error) {
	ctx := op.ctx
	var inRing int
	if err := op.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM farm_xmas_santa WHERE target_id IS NOT NULL`).
		Scan(&inRing); err != nil {
		return false, err
	}
	if inRing >= 2 {
		rows, err := op.tx.QueryContext(ctx, `SELECT user_id, target_id FROM farm_xmas_santa
			WHERE target_id IS NOT NULL AND sent_at = 0 ORDER BY user_id`)
		if err != nil {
			return false, err
		}
		type pair struct{ from, to int64 }
		var free []pair
		for rows.Next() {
			var p pair
			if err := rows.Scan(&p.from, &p.to); err != nil {
				rows.Close()
				return false, err
			}
			free = append(free, p)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return false, err
		}
		if len(free) == 0 {
			return false, ErrXmasSantaFull
		}
		p := free[op.s.randN(len(free))]
		if _, err := op.tx.ExecContext(ctx, `INSERT INTO farm_xmas_santa (user_id, target_id) VALUES (?, ?)`, userID, p.to); err != nil {
			return false, err
		}
		_, err = op.tx.ExecContext(ctx, `UPDATE farm_xmas_santa SET target_id = ? WHERE user_id = ?`, userID, p.from)
		return true, err
	}
	// chưa có vòng: người chờ (chưa có người nhận) cùng người mới, đủ 2 thì xếp vòng ngẫu nhiên
	if _, err := op.tx.ExecContext(ctx, `INSERT INTO farm_xmas_santa (user_id) VALUES (?)`, userID); err != nil {
		return false, err
	}
	rows, err := op.tx.QueryContext(ctx, `SELECT user_id FROM farm_xmas_santa WHERE target_id IS NULL ORDER BY user_id`)
	if err != nil {
		return false, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return false, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(ids) < 2 {
		return false, err
	}
	return true, op.santaRing(ids)
}

// santaRing: xáo trộn ids rồi xếp vòng: người i tặng người i+1.
func (op *farmOp) santaRing(ids []int64) error {
	for i := len(ids) - 1; i > 0; i-- { // xáo trộn Fisher–Yates
		j := op.s.randN(i + 1)
		ids[i], ids[j] = ids[j], ids[i]
	}
	for i, id := range ids {
		if _, err := op.tx.ExecContext(op.ctx, `UPDATE farm_xmas_santa SET target_id = ? WHERE user_id = ?`,
			ids[(i+1)%len(ids)], id); err != nil {
			return err
		}
	}
	return nil
}

func viDay(day string) string {
	if t, err := time.Parse("2006-01-02", day); err == nil {
		return t.Format("02/01")
	}
	return day
}

// xmasSantaDraw: tới ngày bốc thăm mà chưa bốc thì xếp người tham gia thành một vòng ngẫu nhiên (i tặng
// i+1). Trả về tin báo (rỗng nếu không bốc lần này).
func (s *Store) xmasSantaDraw(ctx context.Context) (string, error) {
	day := vnDay(s.now())
	if !xmasOn(day) || day < XmasSantaDraw {
		return "", nil
	}
	if done, err := xmasMeta(ctx, s.db, xmasMetaDrawn); err != nil || done {
		return "", err
	}
	var news string
	_, err := s.farmTx(ctx, func(op *farmOp) error {
		if ok, err := setXmasMeta(ctx, op.tx, xmasMetaDrawn); err != nil || !ok {
			return err
		}
		rows, err := op.tx.QueryContext(ctx, `SELECT user_id FROM farm_xmas_santa ORDER BY user_id`)
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
		if err := rows.Err(); err != nil {
			return err
		}
		if len(ids) < 2 { // chưa đủ người: ai đăng ký sau sẽ được ghép (santaLateJoin)
			news = "🎅 Ông già Noel bí mật: chưa đủ người để bốc thăm, vào tab Noel đăng ký là được ghép cặp ngay!"
			return nil
		}
		if err := op.santaRing(ids); err != nil {
			return err
		}
		news = fmt.Sprintf("🎅 Đã bốc thăm Ông già Noel bí mật cho %d người! Vào tab Noel xem bạn tặng ai, nhớ giữ bí mật nhé 🤫", len(ids))
		return nil
	})
	return news, err
}

// FarmXmasSantaSend: gửi quà bí mật (qty itemID trong kho) cho người mình bốc được; trước đêm Noel, một lần.
func (s *Store) FarmXmasSantaSend(ctx context.Context, userID int64, itemID string, qty int64) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		if !xmasOn(op.day) {
			return ErrXmasOff
		}
		if !op.now.Before(xmasEveTime()) {
			return ErrXmasSantaLate
		}
		var target sql.NullInt64
		var sentAt int64
		err := op.tx.QueryRowContext(ctx, `SELECT target_id, sent_at FROM farm_xmas_santa WHERE user_id = ?`, userID).
			Scan(&target, &sentAt)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && !target.Valid) {
			return ErrXmasSantaNoPair
		}
		if err != nil {
			return err
		}
		if sentAt > 0 {
			return ErrXmasSantaSent
		}
		it, ok := itemByID(itemID)
		if !ok {
			return ErrFarmUnknownItem
		}
		if qty < 1 || qty > XmasSantaMaxQty {
			return ErrXmasSantaBadQty
		}
		if ok, err := op.takeStock(userID, it.ID, qty); err != nil {
			return err
		} else if !ok {
			return farmErr("Trong kho không đủ %d %s để gửi.", qty, it.Name)
		}
		if _, err := op.tx.ExecContext(ctx, `UPDATE farm_xmas_santa SET item = ?, qty = ?, sent_at = ? WHERE user_id = ?`,
			it.ID, qty, op.unix(), userID); err != nil {
			return err
		}
		g := &Gained{}
		if g.Candy, err = op.addCandy(userID, XmasSantaSendCandy); err != nil {
			return err
		}
		var sent, total int
		if err := op.tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(sent_at > 0), 0), COUNT(*) FROM farm_xmas_santa
			WHERE target_id IS NOT NULL`).Scan(&sent, &total); err != nil {
			return err
		}
		res.News = fmt.Sprintf("🎁 Một Ông già Noel bí mật vừa gói quà xong (%d/%d người đã gửi)", sent, total)
		res.Gained = g
		return nil
	})
	return res, err
}

// FarmXmasSantaOpen: từ đêm Noel, người nhận mở quà bí mật: món vào kho (cần đủ chỗ) + kẹo, lộ người tặng.
func (s *Store) FarmXmasSantaOpen(ctx context.Context, userID int64) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		if op.now.Before(xmasEveTime()) {
			return ErrXmasSantaEarly
		}
		var from, qty int64
		var item string
		err := op.tx.QueryRowContext(ctx, `
			SELECT user_id, item, qty FROM farm_xmas_santa WHERE target_id = ? AND sent_at > 0 AND opened = 0`, userID).
			Scan(&from, &item, &qty)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrXmasSantaNoGift
		}
		if err != nil {
			return err
		}
		if room, err := op.storageRoom(userID); err != nil {
			return err
		} else if room < qty {
			return farmErr("Kho cần trống %d chỗ để mở quà.", qty)
		}
		if err := op.addStock(userID, item, qty); err != nil {
			return err
		}
		if _, err := op.tx.ExecContext(ctx, `UPDATE farm_xmas_santa SET opened = 1 WHERE user_id = ?`, from); err != nil {
			return err
		}
		g := &Gained{Items: map[string]int64{item: qty}}
		if g.Candy, err = op.addCandy(userID, XmasSantaOpenCandy); err != nil {
			return err
		}
		me, err := farmUser(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		giver, err := farmUser(ctx, op.tx, from)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		g.Socks = []XmasSock{{From: giver, Item: item, Qty: qty}} // dùng chung khung "ai tặng gì" của tất
		it, _ := itemByID(item)
		if giver.Username != "" {
			res.News = fmt.Sprintf("🎅 %s mở quà Ông già Noel bí mật: %s tặng %d %s %s!", userLabel(me), userLabel(giver),
				qty, it.Emoji, it.Name)
		}
		res.Gained = g
		return nil
	})
	return res, err
}

/* ---------- Mốc kẹo cá nhân ---------- */

// FarmXmasMilestone: nhận quà mốc n (1-based) khi tổng kẹo đã kiếm đạt mốc.
func (s *Store) FarmXmasMilestone(ctx context.Context, userID int64, n int) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		if !xmasShopOpen(op.day) {
			return ErrXmasShopOff
		}
		if n < 1 || n > len(XmasMilestones) {
			return ErrXmasMilestoneNot
		}
		m := XmasMilestones[n-1]
		var earned int64
		if err := op.tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(earned), 0) FROM farm_xmas WHERE user_id = ?`, userID).
			Scan(&earned); err != nil {
			return err
		}
		if earned < m.Need {
			return ErrXmasMilestoneNot
		}
		if m.Prize.Kind == "item" {
			if room, err := op.storageRoom(userID); err != nil {
				return err
			} else if room < m.Prize.Qty {
				return ErrFarmStorageFull
			}
		}
		r, err := op.tx.ExecContext(ctx, `INSERT OR IGNORE INTO farm_claims (user_id, day, quest) VALUES (?, ?, ?)`,
			userID, xmasTreeClaimDay, fmt.Sprintf(xmasMilestoneQuest, n))
		if err != nil {
			return err
		}
		if k, _ := r.RowsAffected(); k == 0 {
			return ErrFarmQuestClaimed
		}
		p := m.Prize
		g := &Gained{Prize: &p}
		switch p.Kind {
		case "xu":
			if err := op.earn(userID, p.Xu, fmt.Sprintf("Mốc %d kẹo Noel", m.Need)); err != nil {
				return err
			}
			g.Xu = p.Xu
		case "item":
			if err := op.addStock(userID, p.Item, p.Qty); err != nil {
				return err
			}
			g.Items = map[string]int64{p.Item: p.Qty}
		case "decor":
			if err := op.addDecor(userID, p.Decor); err != nil {
				return err
			}
			me, err := farmUser(ctx, op.tx, userID)
			if err != nil {
				return err
			}
			res.News = fmt.Sprintf("🌟 %s đạt mốc %d kẹo Noel, nhận Ngôi sao Giáng sinh độc quyền!", userLabel(me), m.Need)
		}
		res.Gained = g
		return nil
	})
	return res, err
}

/* ---------- Đêm Noel ---------- */

// FarmXmasEveGift: trong đêm Noel, mỗi người nhận một lần quà Ông già Noel: kẹo, xu và Chuông Đêm Thánh.
func (s *Store) FarmXmasEveGift(ctx context.Context, userID int64) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		if op.now.Before(xmasEveTime()) {
			return ErrXmasEveNotYet
		}
		if !xmasEve(op.now) {
			return ErrXmasEveOver
		}
		r, err := op.tx.ExecContext(ctx, `INSERT OR IGNORE INTO farm_claims (user_id, day, quest) VALUES (?, ?, ?)`,
			userID, xmasTreeClaimDay, xmasEveClaimQuest)
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return ErrFarmQuestClaimed
		}
		g := &Gained{Xu: XmasEveXu, Prize: &XmasPrize{Kind: "decor", Decor: xmasSantaEveDecorID, Rare: true}}
		if g.Candy, err = op.addCandy(userID, XmasEveCandy); err != nil {
			return err
		}
		if err := op.earn(userID, XmasEveXu, "Quà đêm Noel"); err != nil {
			return err
		}
		if err := op.addDecor(userID, xmasSantaEveDecorID); err != nil {
			return err
		}
		res.Gained = g
		return nil
	})
	return res, err
}

// xmasEveAnnounce: lần đầu tới đêm Noel thì trả về tin báo cả server: Ông già Noel ghé vườn mọi người, kết quả
// Ông già Noel bí mật, top kẹo hiện tại.
func (s *Store) xmasEveAnnounce(ctx context.Context) ([]string, error) {
	if !xmasEve(s.now()) {
		return nil, nil
	}
	if ok, err := setXmasMeta(ctx, s.db, xmasMetaEve); err != nil || !ok {
		return nil, err
	}
	out := []string{"🎄 Đêm Noel đến rồi! Ông già Noel đã ghé vườn mọi người, vào nông trại nhận quà và mở quà bí mật nhé 🎅🔔"}
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, target_id FROM farm_xmas_santa WHERE target_id IS NOT NULL ORDER BY user_id`)
	if err != nil {
		return nil, err
	}
	type pair struct{ from, to int64 }
	var ps []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.from, &p.to); err != nil {
			rows.Close()
			return nil, err
		}
		ps = append(ps, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var parts []string
	for _, p := range ps {
		a, errA := farmUser(ctx, s.db, p.from)
		b, errB := farmUser(ctx, s.db, p.to)
		if errA == nil && errB == nil {
			parts = append(parts, userLabel(a)+" → "+userLabel(b))
		}
	}
	if len(parts) > 0 {
		out = append(out, "🤫 Lộ diện Ông già Noel bí mật: "+strings.Join(parts, ", "))
	}
	top, err := xmasTop(ctx, s.db, "earned", 3)
	if err != nil {
		return nil, err
	}
	if len(top) > 0 {
		medals := []string{"🥇", "🥈", "🥉"}
		var t []string
		for i, r := range top {
			t = append(t, fmt.Sprintf("%s %s (%d 🍬)", medals[i], userLabel(r.User), r.Value))
		}
		out = append(out, "🏆 Top kẹo Noel lúc này: "+strings.Join(t, " · "))
	}
	return out, nil
}

// FarmXmasFinaleTick (bộ hẹn giờ): bốc thăm Ông già Noel bí mật / báo đêm Noel khi tới giờ. Trả tin báo cả server.
func (s *Store) FarmXmasFinaleTick(ctx context.Context) ([]string, error) {
	var out []string
	news, err := s.xmasSantaDraw(ctx)
	if err != nil {
		return nil, err
	}
	if news != "" {
		out = append(out, news)
	}
	eve, err := s.xmasEveAnnounce(ctx)
	return append(out, eve...), err
}

/* ---------- Xem ---------- */

type XmasSantaView struct {
	DrawDay   string            `json:"draw_day"`
	CanJoin   bool              `json:"can_join"`
	Joined    bool              `json:"joined"`
	Drawn     bool              `json:"drawn"`
	People    int               `json:"people"` // người tham gia
	SentCount int               `json:"sent_count"`
	Target    *model.PublicUser `json:"target,omitempty"` // người mình tặng (sau bốc thăm)
	Sent      bool              `json:"sent"`
	SentItem  string            `json:"sent_item,omitempty"`
	SentQty   int64             `json:"sent_qty,omitempty"`
	GiftReady bool              `json:"gift_ready"` // có quà bí mật mở được (từ đêm Noel)
	MaxQty    int64             `json:"max_qty"`
}

type XmasMilestoneView struct {
	XmasMilestone
	Claimed bool `json:"claimed"`
}

type XmasEveView struct {
	At       string `json:"at"` // RFC3339
	To       string `json:"to"`
	Now      bool   `json:"now"`  // đang đêm Noel
	Gift     bool   `json:"gift"` // còn quà đêm Noel chưa nhận
	Candy    int64  `json:"candy"`
	Xu       int64  `json:"xu"`
	Revealed bool   `json:"revealed"`
}

func (s *Store) fillXmasFinale(ctx context.Context, x *XmasView, ownerID int64, now time.Time) error {
	day := vnDay(now)
	x.SpecialDecors = XmasSpecialDecors
	claimed, err := claimedQuests(ctx, s.db, ownerID, xmasTreeClaimDay)
	if err != nil {
		return err
	}
	x.Milestones = make([]XmasMilestoneView, len(XmasMilestones))
	for i, m := range XmasMilestones {
		x.Milestones[i] = XmasMilestoneView{m, claimed[fmt.Sprintf(xmasMilestoneQuest, i+1)]}
	}
	eve := xmasEveTime()
	x.Eve = XmasEveView{At: eve.UTC().Format(time.RFC3339), To: XmasEveTo, Now: xmasEve(now), Candy: XmasEveCandy,
		Xu: XmasEveXu, Revealed: !now.Before(eve)}
	x.Eve.Gift = x.Eve.Now && !claimed[xmasEveClaimQuest]

	sv := XmasSantaView{DrawDay: XmasSantaDraw, MaxQty: XmasSantaMaxQty}
	sv.CanJoin = xmasOn(day) && now.Before(eve)
	if sv.Drawn, err = xmasMeta(ctx, s.db, xmasMetaDrawn); err != nil {
		return err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(sent_at > 0), 0) FROM farm_xmas_santa`).
		Scan(&sv.People, &sv.SentCount); err != nil {
		return err
	}
	var target sql.NullInt64
	var sentAt int64
	err = s.db.QueryRowContext(ctx, `SELECT target_id, item, qty, sent_at FROM farm_xmas_santa WHERE user_id = ?`, ownerID).
		Scan(&target, &sv.SentItem, &sv.SentQty, &sentAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return err
	default:
		sv.Joined, sv.Sent = true, sentAt > 0
		sv.CanJoin = false
		if target.Valid {
			if u, err := farmUser(ctx, s.db, target.Int64); err == nil {
				sv.Target = &u
			}
		}
	}
	if !now.Before(eve) {
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM farm_xmas_santa WHERE target_id = ? AND sent_at > 0 AND opened = 0`,
			ownerID).Scan(&n); err != nil {
			return err
		}
		sv.GiftReady = n > 0
	}
	x.Santa = sv
	return nil
}
