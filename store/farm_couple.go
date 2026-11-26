package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"chat/model"
)

// Kết đôi trong nông trại (specs/FARM_SPEC.md §11). Hai người độc thân ngỏ lời / nhận lời thành một
// cặp; chăm vườn cho nhau (tưới, bắt sâu), ghé thăm, tặng nông sản thì tăng điểm thân mật. Lên cấp
// thân mật thì cả hai cùng nhận xu. Không trộm được vườn của người thương.

const (
	LoveWater       = 2  // tưới giúp người thương
	LoveBug         = 3  // bắt sâu giúp người thương
	LoveVisit       = 2  // ghé vườn người thương (lần đầu mỗi ngày)
	LoveGiftMax     = 10 // điểm tối đa một lần tặng quà
	FarmGiftsPerDay = 3
	FarmGiftMaxQty  = 10
	loveGiftKey     = "gift"
)

// LoveLevels[n-1]: điểm thân mật cần để đạt cấp n; LoveNames[n-1]: tên cấp.
var (
	LoveLevels = []int64{0, 20, 60, 120, 200, 320, 480, 700, 1000}
	LoveNames  = []string{"Mới quen", "Cảm nắng", "Hẹn hò", "Thân thiết", "Yêu nhau", "Gắn bó", "Tri kỷ",
		"Vợ chồng son", "Bách niên giai lão"}
)

// loveReward: xu mỗi người nhận khi cặp lên cấp thân mật lv.
func loveReward(lv int) int64 { return int64(30 * lv) }

func loveLevel(love int64) int {
	lv := 1
	for i := range LoveLevels {
		if love >= LoveLevels[i] {
			lv = i + 1
		}
	}
	return lv
}

var (
	ErrFarmCoupleSelf     = &FarmError{"Không kết đôi với chính mình được."}
	ErrFarmCoupleTaken    = &FarmError{"Người này đã có đôi rồi."}
	ErrFarmCoupleHave     = &FarmError{"Bạn đang có đôi rồi, chung thuỷ chút nào 💕"}
	ErrFarmCoupleNone     = &FarmError{"Bạn chưa có người thương."}
	ErrFarmCoupleNoAsk    = &FarmError{"Lời ngỏ này không còn nữa."}
	ErrFarmStealPartner   = &FarmError{"Ai nỡ trộm vườn người thương 💕"}
	ErrFarmGiftDaily      = &FarmError{"Hôm nay bạn đã tặng quà đủ 3 lần rồi."}
	ErrFarmGiftPartnerFul = &FarmError{"Kho của người thương đầy rồi, tặng ít thôi."}
)

// Couple: cặp đôi của chủ vườn (ai xem cũng thấy). GiftsLeft chỉ có khi chủ vườn xem.
type Couple struct {
	Partner   model.PublicUser `json:"partner"`
	Love      int64            `json:"love"`
	Level     int              `json:"level"`
	LevelName string           `json:"level_name"`
	LevelLove int64            `json:"level_love"`
	NextLove  *int64           `json:"next_love"`
	Since     string           `json:"since"`
	GiftsLeft *int             `json:"gifts_left,omitempty"`
}

// CoupleRequest: một lời ngỏ kết đôi (đến mình, hoặc mình gửi đi).
type CoupleRequest struct {
	User model.PublicUser `json:"user"`
	At   string           `json:"at"`
}

type coupleRow struct {
	Partner int64
	Love    int64
	Since   int64
}

func pairOf(a, b int64) (int64, int64) {
	if a < b {
		return a, b
	}
	return b, a
}

// coupleOf: cặp của userID (ok=false nếu độc thân).
func coupleOf(ctx context.Context, q execQuerier, userID int64) (coupleRow, bool, error) {
	var c coupleRow
	err := q.QueryRowContext(ctx, `
		SELECT CASE WHEN user_a = ? THEN user_b ELSE user_a END, love, since
		FROM farm_couples WHERE user_a = ? OR user_b = ?`, userID, userID, userID).Scan(&c.Partner, &c.Love, &c.Since)
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	return c, err == nil, err
}

// partners: a và b có phải một cặp không.
func partners(ctx context.Context, q execQuerier, a, b int64) (bool, error) {
	c, ok, err := coupleOf(ctx, q, a)
	return ok && c.Partner == b, err
}

// addLove cộng n điểm thân mật cho cặp a–b (nếu là một cặp); lên cấp thì cả hai nhận loveReward. Trả
// về điểm vừa cộng và cấp mới (0 nếu không lên cấp / không phải cặp).
func (op *farmOp) addLove(a, b int64, n int64) (int64, int, error) {
	c, ok, err := coupleOf(op.ctx, op.tx, a)
	if err != nil || !ok || c.Partner != b || n <= 0 {
		return 0, 0, err
	}
	x, y := pairOf(a, b)
	if _, err := op.tx.ExecContext(op.ctx, `UPDATE farm_couples SET love = love + ? WHERE user_a = ? AND user_b = ?`,
		n, x, y); err != nil {
		return 0, 0, err
	}
	from, to := loveLevel(c.Love), loveLevel(c.Love+n)
	if to <= from {
		return n, 0, nil
	}
	ua, err := farmUser(op.ctx, op.tx, a)
	if err != nil {
		return 0, 0, err
	}
	ub, err := farmUser(op.ctx, op.tx, b)
	if err != nil {
		return 0, 0, err
	}
	for lv := from + 1; lv <= to; lv++ {
		name := LoveNames[lv-1]
		if err := op.earn(a, loveReward(lv), fmt.Sprintf("Thân mật cấp %d (%s) với %s", lv, name, userLabel(ub))); err != nil {
			return 0, 0, err
		}
		if err := op.earn(b, loveReward(lv), fmt.Sprintf("Thân mật cấp %d (%s) với %s", lv, name, userLabel(ua))); err != nil {
			return 0, 0, err
		}
	}
	return n, to, nil
}

// gainLove: thêm điểm thân mật vào Gained (tạo Gained nếu chưa có).
func gainLove(g **Gained, n int64, up int) {
	if n <= 0 {
		return
	}
	if *g == nil {
		*g = &Gained{}
	}
	(*g).Love += n
	if up > 0 {
		(*g).LoveUp = up
	}
}

// FarmPropose: fromID ngỏ lời kết đôi với toID. Người kia đã ngỏ lời với mình trước thì thành đôi luôn.
func (s *Store) FarmPropose(ctx context.Context, fromID, toID int64) (FarmResult, error) {
	res := FarmResult{OwnerID: toID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		if fromID == toID {
			return ErrFarmCoupleSelf
		}
		me, err := farmUser(ctx, op.tx, fromID)
		if err != nil {
			return err
		}
		if _, err := farmUser(ctx, op.tx, toID); err != nil {
			return err
		}
		if _, ok, err := coupleOf(ctx, op.tx, fromID); err != nil {
			return err
		} else if ok {
			return ErrFarmCoupleHave
		}
		if _, ok, err := coupleOf(ctx, op.tx, toID); err != nil {
			return err
		} else if ok {
			return ErrFarmCoupleTaken
		}
		var mutual int
		if err := op.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM farm_couple_requests WHERE from_id = ? AND to_id = ?`,
			toID, fromID).Scan(&mutual); err != nil {
			return err
		}
		if mutual > 0 {
			if err := op.makeCouple(fromID, toID); err != nil {
				return err
			}
			res.Log, err = op.log(toID, me, "couple", "", 0)
			return err
		}
		if _, err := op.tx.ExecContext(ctx, `DELETE FROM farm_couple_requests WHERE from_id = ?`, fromID); err != nil {
			return err
		}
		if _, err := op.tx.ExecContext(ctx, `INSERT INTO farm_couple_requests (from_id, to_id, at) VALUES (?, ?, ?)`,
			fromID, toID, op.unix()); err != nil {
			return err
		}
		res.Log, err = op.log(toID, me, "propose", "", 0)
		return err
	})
	return res, err
}

// makeCouple: a và b thành một cặp (cả hai đang độc thân); xoá mọi lời ngỏ liên quan tới hai người.
func (op *farmOp) makeCouple(a, b int64) error {
	x, y := pairOf(a, b)
	if _, err := op.tx.ExecContext(op.ctx, `INSERT INTO farm_couples (user_a, user_b, love, since) VALUES (?, ?, 0, ?)`,
		x, y, op.unix()); err != nil {
		return err
	}
	_, err := op.tx.ExecContext(op.ctx,
		`DELETE FROM farm_couple_requests WHERE from_id IN (?, ?) OR to_id IN (?, ?)`, a, b, a, b)
	return err
}

// FarmCoupleRespond: meID trả lời lời ngỏ của fromID (accept: thành đôi; không: bỏ lời ngỏ).
func (s *Store) FarmCoupleRespond(ctx context.Context, meID, fromID int64, accept bool) (FarmResult, error) {
	res := FarmResult{OwnerID: meID, Notify: fromID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		r, err := op.tx.ExecContext(ctx, `DELETE FROM farm_couple_requests WHERE from_id = ? AND to_id = ?`, fromID, meID)
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return ErrFarmCoupleNoAsk
		}
		if !accept {
			return nil
		}
		if _, ok, err := coupleOf(ctx, op.tx, meID); err != nil {
			return err
		} else if ok {
			return ErrFarmCoupleHave
		}
		if _, ok, err := coupleOf(ctx, op.tx, fromID); err != nil {
			return err
		} else if ok {
			return ErrFarmCoupleTaken
		}
		if err := op.makeCouple(meID, fromID); err != nil {
			return err
		}
		me, err := farmUser(ctx, op.tx, meID)
		if err != nil {
			return err
		}
		res.Log, err = op.log(fromID, me, "couple", "", 0)
		return err
	})
	return res, err
}

// FarmCancelPropose: meID rút lại lời ngỏ đang chờ của mình.
func (s *Store) FarmCancelPropose(ctx context.Context, meID int64) (FarmResult, error) {
	res := FarmResult{OwnerID: meID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		_, err := op.tx.ExecContext(ctx, `DELETE FROM farm_couple_requests WHERE from_id = ?`, meID)
		return err
	})
	return res, err
}

// FarmBreakUp: meID chia tay; điểm thân mật mất hết.
func (s *Store) FarmBreakUp(ctx context.Context, meID int64) (FarmResult, error) {
	res := FarmResult{OwnerID: meID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		c, ok, err := coupleOf(ctx, op.tx, meID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrFarmCoupleNone
		}
		x, y := pairOf(meID, c.Partner)
		if _, err := op.tx.ExecContext(ctx, `DELETE FROM farm_couples WHERE user_a = ? AND user_b = ?`, x, y); err != nil {
			return err
		}
		me, err := farmUser(ctx, op.tx, meID)
		if err != nil {
			return err
		}
		res.Notify = c.Partner
		res.Log, err = op.log(c.Partner, me, "breakup", "", 0)
		return err
	})
	return res, err
}

// FarmGift: meID tặng qty đơn vị itemID trong kho mình cho người thương (tối đa FarmGiftsPerDay lần
// mỗi ngày, FarmGiftMaxQty đơn vị mỗi lần). Điểm thân mật = giá trị quà / 20 (1..LoveGiftMax).
func (s *Store) FarmGift(ctx context.Context, meID int64, itemID string, qty int64) (FarmResult, error) {
	res := FarmResult{OwnerID: meID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		c, ok, err := coupleOf(ctx, op.tx, meID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrFarmCoupleNone
		}
		it, ok := itemByID(itemID)
		if !ok {
			return ErrFarmUnknownItem
		}
		if qty <= 0 || qty > FarmGiftMaxQty {
			return ErrFarmBadQty
		}
		n, err := counter(ctx, op.tx, meID, op.day, loveGiftKey)
		if err != nil {
			return err
		}
		if n >= FarmGiftsPerDay {
			return ErrFarmGiftDaily
		}
		if room, err := op.storageRoom(c.Partner); err != nil {
			return err
		} else if room < qty {
			return ErrFarmGiftPartnerFul
		}
		if ok, err := op.takeStock(meID, it.ID, qty); err != nil {
			return err
		} else if !ok {
			return farmErr("Trong kho không đủ %d %s để tặng.", qty, it.Name)
		}
		if err := op.addStock(c.Partner, it.ID, qty); err != nil {
			return err
		}
		if err := op.bump(meID, loveGiftKey, 1); err != nil {
			return err
		}
		love, up, err := op.addLove(meID, c.Partner, min(LoveGiftMax, max(1, (it.Price*qty+19)/20)))
		if err != nil {
			return err
		}
		gainLove(&res.Gained, love, up)
		me, err := farmUser(ctx, op.tx, meID)
		if err != nil {
			return err
		}
		res.Notify = c.Partner
		res.Log, err = op.log(c.Partner, me, "gift", it.ID, qty)
		return err
	})
	return res, err
}

// fillCouple thêm cặp đôi của ownerID vào FarmView; chủ vườn xem thì kèm lời ngỏ đến / đi và số lần
// tặng quà còn lại; người khác xem thì kèm CoupleState (quan hệ của người xem với chủ vườn).
func (s *Store) fillCouple(ctx context.Context, v *FarmView, ownerID, viewerID int64, day string) error {
	c, ok, err := coupleOf(ctx, s.db, ownerID)
	if err != nil {
		return err
	}
	if ok {
		p, err := farmUser(ctx, s.db, c.Partner)
		if err == nil {
			lv := loveLevel(c.Love)
			cv := &Couple{Partner: p, Love: c.Love, Level: lv, LevelName: LoveNames[lv-1], LevelLove: LoveLevels[lv-1],
				Since: rfc(c.Since)}
			if lv < len(LoveLevels) {
				next := LoveLevels[lv]
				cv.NextLove = &next
			}
			if v.Me {
				n, err := counter(ctx, s.db, ownerID, day, loveGiftKey)
				if err != nil {
					return err
				}
				left := int(max(0, FarmGiftsPerDay-n))
				cv.GiftsLeft = &left
			}
			v.Couple = cv
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	if v.Me {
		in, err := coupleRequests(ctx, s.db, `to_id`, `from_id`, ownerID)
		if err != nil {
			return err
		}
		v.CoupleRequests = in
		out, err := coupleRequests(ctx, s.db, `from_id`, `to_id`, ownerID)
		if err != nil {
			return err
		}
		if len(out) > 0 {
			v.CoupleSent = &out[0]
		}
		return nil
	}
	// người xem với chủ vườn
	switch {
	case ok && c.Partner == viewerID:
		v.CoupleState = "partner"
	default:
		_, mine, err := coupleOf(ctx, s.db, viewerID)
		if err != nil {
			return err
		}
		if ok || mine {
			return nil // một trong hai đã có đôi
		}
		var sent, got int
		if err := s.db.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(from_id = ?), 0), COALESCE(SUM(from_id = ?), 0) FROM farm_couple_requests
			WHERE (from_id = ? AND to_id = ?) OR (from_id = ? AND to_id = ?)`,
			viewerID, ownerID, viewerID, ownerID, ownerID, viewerID).Scan(&sent, &got); err != nil {
			return err
		}
		switch {
		case got > 0:
			v.CoupleState = "incoming"
		case sent > 0:
			v.CoupleState = "sent"
		default:
			v.CoupleState = "can_propose"
		}
	}
	return nil
}

// coupleRequests: lời ngỏ có cột col = userID, kèm người ở cột other (mới nhất trước).
func coupleRequests(ctx context.Context, q queryer, col, other string, userID int64) ([]CoupleRequest, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT r.at, u.id, u.username, u.display_name, u.avatar, u.status_text
		FROM farm_couple_requests r JOIN users u ON u.id = r.`+other+`
		WHERE r.`+col+` = ? AND `+realUserFilter+` ORDER BY r.at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CoupleRequest{}
	for rows.Next() {
		var r CoupleRequest
		var at int64
		if err := rows.Scan(&at, &r.User.ID, &r.User.Username, &r.User.DisplayName, &r.User.Avatar, &r.User.StatusText); err != nil {
			return nil, err
		}
		r.At = time.Unix(at, 0).UTC().Format(time.RFC3339)
		out = append(out, r)
	}
	return out, rows.Err()
}
