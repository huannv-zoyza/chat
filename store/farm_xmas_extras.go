package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Bổ sung Giáng sinh (specs/FARM_SPEC.md §16d): thống kê hoạt động theo ngày (biết phần nào hút người chơi để
// chỉnh giữa chừng) và lưu tin Noel để người đang offline vào lại vẫn xem được.

/* ---------- Thống kê ---------- */

// Khoá thống kê (một lần hành động = +1).
const (
	StatOrders    = "orders"    // giao đơn Ông già Noel
	StatSnowballs = "snowballs" // ném cầu tuyết (cả trượt)
	StatSocks     = "socks"     // treo tất
	StatDrops     = "drops"     // nhặt quà Ông già Noel rơi
	StatBoxes     = "boxes"     // mở hộp quà bí ẩn
	StatTree      = "tree"      // góp cây thông chung
	StatSanta     = "santa"     // gửi quà bí mật
	StatGifts     = "gifts"     // mở quà dưới gốc cây
)

// xmasStat: +1 hoạt động key hôm nay và ghi userID là người chơi sự kiện hôm nay.
func (op *farmOp) xmasStat(userID int64, key string) error {
	if _, err := op.tx.ExecContext(op.ctx, `
		INSERT INTO farm_xmas_stats (day, key, n) VALUES (?, ?, 1)
		ON CONFLICT (day, key) DO UPDATE SET n = n + 1`, op.day, key); err != nil {
		return err
	}
	_, err := op.tx.ExecContext(op.ctx, `INSERT OR IGNORE INTO farm_xmas_active (day, user_id) VALUES (?, ?)`, op.day, userID)
	return err
}

// XmasStats: hôm nay và cả sự kiện: số người chơi sự kiện và số lần mỗi hoạt động.
type XmasStats struct {
	Today        map[string]int64 `json:"today"`
	Total        map[string]int64 `json:"total"`
	PlayersToday int64            `json:"players_today"`
	Players      int64            `json:"players"` // người chơi khác nhau cả sự kiện
}

func xmasStats(ctx context.Context, q xq, day string) (XmasStats, error) {
	st := XmasStats{Today: map[string]int64{}, Total: map[string]int64{}}
	rows, err := q.QueryContext(ctx, `SELECT day, key, n FROM farm_xmas_stats`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var d, k string
		var n int64
		if err := rows.Scan(&d, &k, &n); err != nil {
			rows.Close()
			return st, err
		}
		st.Total[k] += n
		if d == day {
			st.Today[k] = n
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return st, err
	}
	err = q.QueryRowContext(ctx, `SELECT COALESCE(SUM(day = ?), 0), COUNT(DISTINCT user_id) FROM farm_xmas_active`, day).
		Scan(&st.PlayersToday, &st.Players)
	return st, err
}

/* ---------- Tin Noel cho người offline ---------- */

const (
	xmasNewsKeep = 300            // giữ bấy nhiêu tin gần nhất
	xmasNewsAge  = 48 * time.Hour // chỉ cho xem lại tin trong 2 ngày
	xmasInboxMax = 20
)

// XmasNewsItem: một tin Noel đã báo (tin chung hoặc tin riêng của người xem).
type XmasNewsItem struct {
	ID   int64  `json:"id"`
	At   string `json:"at"`
	Kind string `json:"kind"`
	Text string `json:"text"`
	From int64  `json:"from_id,omitempty"`
}

// AddXmasNews lưu tin vừa báo: userID 0 là tin chung (skip: những người đã có tin riêng / tự làm), khác 0 là tin
// riêng. Trả id tin (client đánh dấu đã xem khi nhận trực tiếp).
func (s *Store) AddXmasNews(ctx context.Context, userID int64, kind, text string, fromID int64, skip []int64) (int64, error) {
	var sk strings.Builder
	if len(skip) > 0 {
		sk.WriteString(",")
		for _, id := range skip {
			fmt.Fprintf(&sk, "%d,", id)
		}
	}
	var uid any
	if userID != 0 {
		uid = userID
	}
	r, err := s.db.ExecContext(ctx, `INSERT INTO farm_xmas_news (at, user_id, kind, text, from_id, skip) VALUES (?, ?, ?, ?, ?, ?)`,
		s.now().Unix(), uid, kind, text, fromID, sk.String())
	if err != nil {
		return 0, err
	}
	id, err := r.LastInsertId()
	if err != nil {
		return 0, err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM farm_xmas_news WHERE id NOT IN (SELECT id FROM farm_xmas_news ORDER BY id DESC LIMIT ?)`,
		xmasNewsKeep)
	return id, err
}

// XmasInbox: tin Noel của userID có id > since trong 2 ngày qua (mới nhất trước, tối đa 20), kèm id lớn nhất
// hiện có để client đánh dấu đã xem. since < 0: chỉ trả id lớn nhất (lần đầu mở app, không dội tin cũ).
func (s *Store) XmasInbox(ctx context.Context, userID, since int64) ([]XmasNewsItem, int64, error) {
	var last int64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM farm_xmas_news`).Scan(&last); err != nil {
		return nil, 0, err
	}
	out := []XmasNewsItem{}
	if since < 0 {
		return out, last, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, at, kind, text, from_id FROM farm_xmas_news
		WHERE id > ? AND at > ? AND ((user_id IS NULL AND skip NOT LIKE ?) OR user_id = ?)
		ORDER BY id DESC LIMIT ?`,
		since, s.now().Add(-xmasNewsAge).Unix(), fmt.Sprintf("%%,%d,%%", userID), userID, xmasInboxMax)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var it XmasNewsItem
		var at int64
		if err := rows.Scan(&it.ID, &at, &it.Kind, &it.Text, &it.From); err != nil {
			return nil, 0, err
		}
		it.At = rfc(at)
		out = append(out, it)
	}
	return out, last, rows.Err()
}
