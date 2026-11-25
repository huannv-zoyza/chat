package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"chat/model"
)

// Ví xu ảo: dùng chung cả server, tạo lười (1.000 xu) lần đầu chạm tới.
// Ván dùng chip (bầu cua) chỉ chạm ví ở hai lúc: mua chip (BuyIn) và trả chip (PayOut),
// mỗi lần là một transaction ghi cả ví lẫn trạng thái ván.

const (
	WalletStart  = 1000
	LoanAmount   = 300            // mỗi lần vay nhận 300 xu
	LoanDebt     = 360            // ... và nợ 360 (lãi 20%)
	LoanBelow    = 100            // chỉ được vay khi số dư dưới mức này
	LoanCooldown = 72 * time.Hour // mỗi 3 ngày vay một lần
)

// StreakRewards: thưởng điểm danh theo ngày thứ mấy trong chuỗi liên tục (vòng 7 ngày).
var StreakRewards = []int64{100, 120, 140, 160, 180, 200, 300}

var (
	ErrAlreadyClaimed      = errors.New("already claimed today")
	ErrLoanNotAllowed      = errors.New("loan not allowed")
	ErrLoanInDebt          = errors.New("loan not allowed while in debt")
	ErrLoanCooldown        = errors.New("loan cooldown")
	ErrNothingToRepay      = errors.New("nothing to repay")
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrInvalidAmount       = errors.New("invalid amount")
	ErrSelfTransfer        = errors.New("cannot transfer to self")
	ErrRecipientNotFound   = errors.New("recipient not found")
	ErrTransferInDebt      = errors.New("cannot transfer while in debt")
	ErrBetAlreadyLocked    = errors.New("bet already locked")
)

// LoanCooldownError: vừa vay chưa đủ 3 ngày; Until là lúc vay lại được. errors.Is(err, ErrLoanCooldown) đúng.
type LoanCooldownError struct{ Until time.Time }

func (e *LoanCooldownError) Error() string {
	return "loan cooldown until " + e.Until.Format(time.RFC3339)
}
func (e *LoanCooldownError) Is(target error) bool { return target == ErrLoanCooldown }

type Wallet struct {
	UserID    int64  `json:"-"`
	Balance   int64  `json:"balance"`
	Debt      int64  `json:"debt"`
	LastClaim string `json:"-"`
	Streak    int    `json:"-"` // chuỗi điểm danh tính tới LastClaim
}

// StreakOn: số ngày điểm danh liên tục tính tới hôm nay (today "YYYY-MM-DD" giờ VN), 0 nếu đã đứt.
func (w Wallet) StreakOn(today string) int {
	if w.LastClaim != "" && (w.LastClaim == today || w.LastClaim == prevDay(today)) {
		return w.Streak
	}
	return 0
}

// prevDay: ngày liền trước của "YYYY-MM-DD" (chuỗi rỗng nếu sai định dạng).
func prevDay(day string) string {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 0, -1).Format("2006-01-02")
}

// Payout: số chip trả về ví và nợ phát sinh trong ván của một người.
type Payout struct {
	UserID, Chips, Loans int64
}

type BoardEntry struct {
	User    model.PublicUser `json:"user"`
	Balance int64            `json:"balance"`
	Debt    int64            `json:"debt"`
}

type execQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func loadWallet(ctx context.Context, q execQuerier, userID int64) (Wallet, error) {
	if _, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO wallets (user_id) VALUES (?)`, userID); err != nil {
		return Wallet{}, err
	}
	w := Wallet{UserID: userID}
	var last sql.NullString
	err := q.QueryRowContext(ctx, `SELECT balance, debt, last_claim, streak FROM wallets WHERE user_id = ?`, userID).
		Scan(&w.Balance, &w.Debt, &last, &w.Streak)
	w.LastClaim = last.String
	return w, err
}

func (s *Store) GetWallet(ctx context.Context, userID int64) (Wallet, error) {
	return loadWallet(ctx, s.db, userID)
}

// walletTx chạy fn trong một transaction trên ví userID rồi trả ví sau khi commit.
func (s *Store) walletTx(ctx context.Context, userID int64, fn func(tx *sql.Tx, w Wallet) error) (Wallet, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Wallet{}, err
	}
	defer tx.Rollback()
	w, err := loadWallet(ctx, tx, userID)
	if err != nil {
		return Wallet{}, err
	}
	if err := fn(tx, w); err != nil {
		return Wallet{}, err
	}
	w, err = loadWallet(ctx, tx, userID)
	if err != nil {
		return Wallet{}, err
	}
	return w, tx.Commit()
}

func addTx(ctx context.Context, tx *sql.Tx, userID int64, kind string, amount, debt int64) error {
	return addTxNote(ctx, tx, userID, kind, amount, debt, "")
}

// addTxNote đổi ví userID (amount vào số dư, debt vào nợ) và ghi một dòng sổ giao dịch kèm ghi chú.
func addTxNote(ctx context.Context, tx *sql.Tx, userID int64, kind string, amount, debt int64, note string) error {
	if _, err := tx.ExecContext(ctx,
		`UPDATE wallets SET balance = balance + ?, debt = debt + ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`,
		amount, debt, userID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO wallet_tx (user_id, kind, amount, debt_delta, note) VALUES (?, ?, ?, ?, ?)`,
		userID, kind, amount, debt, note)
	return err
}

// earn cộng amount xu kiếm được (nông trại, điểm danh) vào ví userID. Đang nợ thì một nửa số đó
// (không quá số nợ) chuyển ngay sang trả nợ, ghi thêm dòng "auto_repay". Trả về số đã trừ nợ.
func earn(ctx context.Context, tx *sql.Tx, userID int64, kind string, amount int64, note string) (int64, error) {
	if amount <= 0 {
		return 0, nil
	}
	w, err := loadWallet(ctx, tx, userID)
	if err != nil {
		return 0, err
	}
	if err := addTxNote(ctx, tx, userID, kind, amount, 0, note); err != nil {
		return 0, err
	}
	repay := min(w.Debt, amount/2)
	if repay <= 0 {
		return 0, nil
	}
	return repay, addTxNote(ctx, tx, userID, "auto_repay", -repay, -repay, "Tự trừ nợ 50% tiền kiếm được")
}

// spend trừ amount xu khỏi ví userID; không đủ thì ErrInsufficientBalance (không đụng ràng buộc CHECK).
func spend(ctx context.Context, tx *sql.Tx, userID int64, kind string, amount int64, note string) error {
	w, err := loadWallet(ctx, tx, userID)
	if err != nil {
		return err
	}
	if w.Balance < amount {
		return ErrInsufficientBalance
	}
	return addTxNote(ctx, tx, userID, kind, -amount, 0, note)
}

// ClaimDaily: điểm danh mỗi ngày (today = "YYYY-MM-DD" giờ VN) một lần. Hôm qua cũng điểm danh thì
// chuỗi +1, không thì bắt đầu lại từ 1; thưởng theo StreakRewards (qua earn: đang nợ thì tự trừ nợ).
func (s *Store) ClaimDaily(ctx context.Context, userID int64, today string) (Wallet, error) {
	return s.walletTx(ctx, userID, func(tx *sql.Tx, w Wallet) error {
		if w.LastClaim == today {
			return ErrAlreadyClaimed
		}
		streak := 1
		if w.LastClaim != "" && w.LastClaim == prevDay(today) {
			streak = w.Streak + 1
		}
		if _, err := tx.ExecContext(ctx, `UPDATE wallets SET last_claim = ?, streak = ? WHERE user_id = ?`,
			today, streak, userID); err != nil {
			return err
		}
		reward := StreakRewards[(streak-1)%len(StreakRewards)]
		_, err := earn(ctx, tx, userID, "daily", reward, fmt.Sprintf("Điểm danh ngày %d liên tục", streak))
		return err
	})
}

// lastLoanAt: lúc vay ngân hàng gần nhất (ok=false nếu chưa vay bao giờ).
func lastLoanAt(ctx context.Context, q execQuerier, userID int64) (time.Time, bool, error) {
	var t time.Time
	err := q.QueryRowContext(ctx,
		`SELECT created_at FROM wallet_tx WHERE user_id = ? AND kind = 'loan' ORDER BY id DESC LIMIT 1`, userID).Scan(&t)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	return t, err == nil, err
}

// LoanAvailableAt: lúc vay lại được nếu còn đang trong thời gian chờ, nil nếu không phải chờ.
func (s *Store) LoanAvailableAt(ctx context.Context, userID int64) (*time.Time, error) {
	last, ok, err := lastLoanAt(ctx, s.db, userID)
	if err != nil || !ok {
		return nil, err
	}
	until := last.Add(LoanCooldown)
	if !s.now().Before(until) {
		return nil, nil
	}
	return &until, nil
}

// TakeLoan: vay LoanAmount xu (nợ LoanDebt), chỉ khi hết nợ, số dư dưới LoanBelow và đã qua
// LoanCooldown kể từ lần vay trước.
func (s *Store) TakeLoan(ctx context.Context, userID int64) (Wallet, error) {
	now := s.now()
	return s.walletTx(ctx, userID, func(tx *sql.Tx, w Wallet) error {
		if w.Debt > 0 {
			return ErrLoanInDebt
		}
		if w.Balance >= LoanBelow {
			return ErrLoanNotAllowed
		}
		last, ok, err := lastLoanAt(ctx, tx, userID)
		if err != nil {
			return err
		}
		if until := last.Add(LoanCooldown); ok && now.Before(until) {
			return &LoanCooldownError{Until: until}
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE wallets SET balance = balance + ?, debt = debt + ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`,
			LoanAmount, LoanDebt, userID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO wallet_tx (user_id, kind, amount, debt_delta, note, created_at) VALUES (?, 'loan', ?, ?, ?, ?)`,
			userID, LoanAmount, LoanDebt, "Vay ngân hàng", DBTime(now))
		return err
	})
}

// RepayDebt: trả nợ bằng số dư, tối đa bằng số nợ.
func (s *Store) RepayDebt(ctx context.Context, userID int64) (Wallet, error) {
	return s.walletTx(ctx, userID, func(tx *sql.Tx, w Wallet) error {
		pay := min(w.Balance, w.Debt)
		if pay <= 0 {
			return ErrNothingToRepay
		}
		return addTx(ctx, tx, userID, "repay", -pay, -pay)
	})
}

// BuyIn: mang toàn bộ số dư của userID vào ván gameID. apply nhận số xu mang vào, cập nhật ván
// trong bộ nhớ và trả trạng thái mới để lưu cùng transaction. apply lỗi thì không gì thay đổi.
func (s *Store) BuyIn(ctx context.Context, gameID, userID, seq int64,
	apply func(amount int64) (status string, state []byte, err error)) (Wallet, error) {
	return s.walletTx(ctx, userID, func(tx *sql.Tx, w Wallet) error {
		status, state, err := apply(w.Balance)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE wallets SET balance = 0, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`, userID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO wallet_tx (user_id, kind, amount, game_id, seq) VALUES (?, 'buyin', ?, ?, ?)`,
			userID, -w.Balance, gameID, seq); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			`UPDATE games SET status = ?, state = ?, updated_at = CURRENT_TIMESTAMP WHERE message_id = ?`,
			status, string(state), gameID)
		return err
	})
}

// PayOut trả chip về ví (kind "refund" khi rời phòng chờ, "cashout" khi hết ván) và lưu trạng thái ván,
// trong một transaction. Mỗi (ván, người, kind, seq) chỉ được cộng một lần: gọi lại không đổi gì.
// Trả về các ví đã thay đổi.
func (s *Store) PayOut(ctx context.Context, gameID int64, kind string, seq int64, payouts []Payout,
	status string, state []byte) ([]Wallet, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var changed []int64
	for _, p := range payouts {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO wallets (user_id) VALUES (?)`, p.UserID); err != nil {
			return nil, err
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO wallet_tx (user_id, kind, amount, debt_delta, game_id, seq) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT DO NOTHING`, p.UserID, kind, p.Chips, p.Loans, gameID, seq)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE wallets SET balance = balance + ?, debt = debt + ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`,
			p.Chips, p.Loans, p.UserID); err != nil {
			return nil, err
		}
		changed = append(changed, p.UserID)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE games SET status = ?, state = ?, updated_at = CURRENT_TIMESTAMP WHERE message_id = ?`,
		status, string(state), gameID); err != nil {
		return nil, err
	}
	out := make([]Wallet, 0, len(changed))
	for _, id := range changed {
		w, err := loadWallet(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, tx.Commit()
}

// GamesNeedingCashout: ván kind đã kết thúc (hoặc tin đã bị xoá) mà chưa trả chip.
func (s *Store) GamesNeedingCashout(ctx context.Context, kind string) ([]GameRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.message_id, m.channel_id, g.kind, g.state FROM games g JOIN messages m ON m.id = g.message_id
		WHERE g.kind = ? AND (g.status = 'ended' OR m.deleted_at IS NOT NULL)
		  AND NOT EXISTS (SELECT 1 FROM wallet_tx t WHERE t.game_id = g.message_id AND t.kind = 'cashout')`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GameRow
	for rows.Next() {
		var g GameRow
		var state string
		if err := rows.Scan(&g.MessageID, &g.ChannelID, &g.Kind, &state); err != nil {
			return nil, err
		}
		g.State = []byte(state)
		out = append(out, g)
	}
	return out, rows.Err()
}

// WalletBoard: thành viên kênh xếp theo tài sản ròng (số dư - nợ) và danh sách con nợ, mỗi bảng tối đa 10 người.
func (s *Store) WalletBoard(ctx context.Context, channelID int64) (rich, debtors []BoardEntry, err error) {
	members, err := s.ListMembers(ctx, channelID)
	if err != nil {
		return nil, nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT w.user_id, w.balance, w.debt FROM wallets w
		JOIN channel_members m ON m.user_id = w.user_id WHERE m.channel_id = ?`, channelID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	type bal struct{ b, d int64 }
	wallets := map[int64]bal{}
	for rows.Next() {
		var id int64
		var v bal
		if err := rows.Scan(&id, &v.b, &v.d); err != nil {
			return nil, nil, err
		}
		wallets[id] = v
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	all := make([]BoardEntry, 0, len(members))
	for _, u := range members {
		v, ok := wallets[u.ID]
		if !ok {
			v = bal{WalletStart, 0}
		}
		all = append(all, BoardEntry{User: u, Balance: v.b, Debt: v.d})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Balance-all[i].Debt > all[j].Balance-all[j].Debt })
	rich = all[:min(10, len(all))]
	debtors = []BoardEntry{}
	for _, e := range all {
		if e.Debt > 0 {
			debtors = append(debtors, e)
		}
	}
	sort.SliceStable(debtors, func(i, j int) bool { return debtors[i].Debt > debtors[j].Debt })
	debtors = debtors[:min(10, len(debtors))]
	return rich, debtors, nil
}

/* ---------- Lịch sử giao dịch, Chuyển Xu P2P & Đặt cược Cờ tướng ---------- */

func insertWalletLedgerTx(ctx context.Context, tx *sql.Tx, userID int64, kind string, amount, debtDelta int64, gameID *int64, seq int64, peerUserID *int64, note string) (model.WalletTransaction, error) {
	var gID any
	if gameID != nil {
		gID = *gameID
	}
	var pID any
	if peerUserID != nil {
		pID = *peerUserID
	}
	var wt model.WalletTransaction
	err := tx.QueryRowContext(ctx, `
		INSERT INTO wallet_tx (user_id, kind, amount, debt_delta, game_id, seq, peer_user_id, note)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id, user_id, kind, amount, debt_delta, game_id, seq, note, created_at`,
		userID, kind, amount, debtDelta, gID, seq, pID, note).
		Scan(&wt.ID, &wt.UserID, &wt.Kind, &wt.Amount, &wt.DebtDelta, &wt.GameID, &wt.Seq, &wt.Note, &wt.CreatedAt)
	if err != nil {
		return model.WalletTransaction{}, err
	}
	if peerUserID != nil {
		var u model.PublicUser
		if err := tx.QueryRowContext(ctx, `SELECT id, username, display_name, avatar, status_text FROM users WHERE id = ?`, *peerUserID).
			Scan(&u.ID, &u.Username, &u.DisplayName, &u.Avatar, &u.StatusText); err == nil {
			wt.PeerUser = &u
		}
	}
	return wt, nil
}

// ListWalletTransactions trả về lịch sử giao dịch ví xu phân trang theo cursor beforeID.
func (s *Store) ListWalletTransactions(ctx context.Context, userID int64, kind string, beforeID int64, limit int) ([]model.WalletTransaction, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	query := `
		SELECT t.id, t.user_id, t.kind, t.amount, t.debt_delta, t.game_id, t.seq, t.note, t.created_at,
		       u.id, u.username, u.display_name, u.avatar, u.status_text
		FROM wallet_tx t
		LEFT JOIN users u ON u.id = t.peer_user_id
		WHERE t.user_id = ?`
	args := []any{userID}
	if kind != "" && kind != "all" {
		query += ` AND t.kind = ?`
		args = append(args, kind)
	}
	if beforeID > 0 {
		query += ` AND t.id < ?`
		args = append(args, beforeID)
	}
	query += ` ORDER BY t.id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	out := make([]model.WalletTransaction, 0, limit)
	for rows.Next() {
		var wt model.WalletTransaction
		var pID sql.NullInt64
		var pUser, pDisp, pStatus sql.NullString
		var pAvatar *string
		if err := rows.Scan(
			&wt.ID, &wt.UserID, &wt.Kind, &wt.Amount, &wt.DebtDelta, &wt.GameID, &wt.Seq, &wt.Note, &wt.CreatedAt,
			&pID, &pUser, &pDisp, &pAvatar, &pStatus,
		); err != nil {
			return nil, false, err
		}
		if pID.Valid {
			wt.PeerUser = &model.PublicUser{
				ID:          pID.Int64,
				Username:    pUser.String,
				DisplayName: pDisp.String,
				Avatar:      pAvatar,
				StatusText:  pStatus.String,
			}
		}
		out = append(out, wt)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := false
	if len(out) > limit {
		hasMore = true
		out = out[:limit]
	}
	return out, hasMore, nil
}

// TransferCoins chuyển xu P2P nguyên tử từ fromUserID sang toUserID.
func (s *Store) TransferCoins(ctx context.Context, fromUserID, toUserID, amount int64, note string) (Wallet, Wallet, model.WalletTransaction, error) {
	if amount <= 0 {
		return Wallet{}, Wallet{}, model.WalletTransaction{}, ErrInvalidAmount
	}
	if fromUserID == toUserID {
		return Wallet{}, Wallet{}, model.WalletTransaction{}, ErrSelfTransfer
	}
	note = strings.TrimSpace(note)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Wallet{}, Wallet{}, model.WalletTransaction{}, err
	}
	defer tx.Rollback()

	var toStatus, toName, toUsername string
	if err := tx.QueryRowContext(ctx, `SELECT status, display_name, username FROM users WHERE id = ?`, toUserID).
		Scan(&toStatus, &toName, &toUsername); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Wallet{}, Wallet{}, model.WalletTransaction{}, ErrRecipientNotFound
		}
		return Wallet{}, Wallet{}, model.WalletTransaction{}, err
	}
	if toStatus != string(model.UserStatusActive) {
		return Wallet{}, Wallet{}, model.WalletTransaction{}, ErrRecipientNotFound
	}
	var fromName, fromUsername string
	if err := tx.QueryRowContext(ctx, `SELECT display_name, username FROM users WHERE id = ?`, fromUserID).
		Scan(&fromName, &fromUsername); err != nil {
		return Wallet{}, Wallet{}, model.WalletTransaction{}, notFound(err)
	}

	senderW, err := loadWallet(ctx, tx, fromUserID)
	if err != nil {
		return Wallet{}, Wallet{}, model.WalletTransaction{}, err
	}
	// Đang nợ ngân hàng thì không chuyển được: chặn vay rồi chuyển cho bạn để "in tiền".
	if senderW.Debt > 0 {
		return Wallet{}, Wallet{}, model.WalletTransaction{}, ErrTransferInDebt
	}
	if senderW.Balance < amount {
		return Wallet{}, Wallet{}, model.WalletTransaction{}, ErrInsufficientBalance
	}
	recvW, err := loadWallet(ctx, tx, toUserID)
	if err != nil {
		return Wallet{}, Wallet{}, model.WalletTransaction{}, err
	}
	_ = recvW

	if _, err := tx.ExecContext(ctx, `UPDATE wallets SET balance = balance - ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`, amount, fromUserID); err != nil {
		return Wallet{}, Wallet{}, model.WalletTransaction{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE wallets SET balance = balance + ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`, amount, toUserID); err != nil {
		return Wallet{}, Wallet{}, model.WalletTransaction{}, err
	}

	toLabel := toName
	if toLabel == "" {
		toLabel = toUsername
	}
	fromLabel := fromName
	if fromLabel == "" {
		fromLabel = fromUsername
	}
	senderNote := fmt.Sprintf("Chuyển cho %s", toLabel)
	recvNote := fmt.Sprintf("Nhận từ %s", fromLabel)
	if note != "" {
		senderNote += ": " + note
		recvNote += ": " + note
	}

	sTx, err := insertWalletLedgerTx(ctx, tx, fromUserID, "transfer_out", -amount, 0, nil, 0, &toUserID, senderNote)
	if err != nil {
		return Wallet{}, Wallet{}, model.WalletTransaction{}, err
	}
	if _, err := insertWalletLedgerTx(ctx, tx, toUserID, "transfer_in", amount, 0, nil, 0, &fromUserID, recvNote); err != nil {
		return Wallet{}, Wallet{}, model.WalletTransaction{}, err
	}

	senderW, err = loadWallet(ctx, tx, fromUserID)
	if err != nil {
		return Wallet{}, Wallet{}, model.WalletTransaction{}, err
	}
	recvW, err = loadWallet(ctx, tx, toUserID)
	if err != nil {
		return Wallet{}, Wallet{}, model.WalletTransaction{}, err
	}
	if err := tx.Commit(); err != nil {
		return Wallet{}, Wallet{}, model.WalletTransaction{}, err
	}
	return senderW, recvW, sTx, nil
}

// LockedXiangqiBets trả về tổng số xu của userID đang bị tạm giữ trong các ván Cờ tướng chưa kết thúc.
func (s *Store) LockedXiangqiBets(ctx context.Context, userID int64) (int64, error) {
	var total int64
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(gb.amount), 0)
		FROM game_bets gb
		JOIN games g ON g.message_id = gb.message_id
		JOIN messages m ON m.id = g.message_id
		WHERE gb.user_id = ? AND gb.status = 'locked' AND m.deleted_at IS NULL`, userID).Scan(&total)
	return total, err
}

func lockGameBetTx(ctx context.Context, tx *sql.Tx, userID, messageID, amount int64) (Wallet, error) {
	if amount <= 0 {
		return Wallet{}, ErrInvalidAmount
	}
	w, err := loadWallet(ctx, tx, userID)
	if err != nil {
		return Wallet{}, err
	}
	if w.Balance < amount {
		return Wallet{}, ErrInsufficientBalance
	}
	res, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO game_bets (message_id, user_id, amount, status) VALUES (?, ?, ?, 'locked')`,
		messageID, userID, amount)
	if err != nil {
		return Wallet{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Wallet{}, ErrBetAlreadyLocked
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE wallets SET balance = balance - ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`,
		amount, userID); err != nil {
		return Wallet{}, err
	}
	if _, err := insertWalletLedgerTx(ctx, tx, userID, "bet_lock", -amount, 0, &messageID, 0, nil,
		fmt.Sprintf("Đặt cược Cờ tướng #%d", messageID)); err != nil {
		return Wallet{}, err
	}
	return loadWallet(ctx, tx, userID)
}

// CreateGameWithBet tạo tin nhắn game và tạm giữ tiền cược của chủ phòng (nếu betAmount > 0) trong một transaction.
func (s *Store) CreateGameWithBet(ctx context.Context, channelID, userID int64, embed model.Embed, status string, state []byte, betAmount int64) (model.Message, *Wallet, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Message{}, nil, err
	}
	defer tx.Rollback()

	e, err := json.Marshal(embed)
	if err != nil {
		return model.Message{}, nil, err
	}
	var id int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO messages (channel_id, user_id, content, embed) VALUES (?, ?, '', ?) RETURNING id`,
		channelID, userID, string(e)).Scan(&id); err != nil {
		return model.Message{}, nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO games (message_id, kind, status, state) VALUES (?, ?, ?, ?)`,
		id, embed.ID, status, string(state)); err != nil {
		return model.Message{}, nil, err
	}
	var wPtr *Wallet
	if betAmount > 0 {
		w, err := lockGameBetTx(ctx, tx, userID, id, betAmount)
		if err != nil {
			return model.Message{}, nil, err
		}
		wPtr = &w
	}
	if err := tx.Commit(); err != nil {
		return model.Message{}, nil, err
	}
	m, err := s.GetMessage(ctx, id)
	return m, wPtr, err
}

// LockGameBet tạm giữ tiền cược của người chơi khi tham gia bàn Cờ tướng.
func (s *Store) LockGameBet(ctx context.Context, userID, messageID, amount int64) (Wallet, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Wallet{}, err
	}
	defer tx.Rollback()

	w, err := lockGameBetTx(ctx, tx, userID, messageID, amount)
	if err != nil {
		return Wallet{}, err
	}
	if err := tx.Commit(); err != nil {
		return Wallet{}, err
	}
	return w, nil
}

func refundLockedBetRowTx(ctx context.Context, tx *sql.Tx, messageID, userID, amount int64, note string) (Wallet, error) {
	if _, err := loadWallet(ctx, tx, userID); err != nil {
		return Wallet{}, err
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE game_bets SET status = 'refunded', payout = ?, settled_at = CURRENT_TIMESTAMP
		WHERE message_id = ? AND user_id = ? AND status = 'locked'`,
		amount, messageID, userID)
	if err != nil {
		return Wallet{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return loadWallet(ctx, tx, userID)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE wallets SET balance = balance + ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`,
		amount, userID); err != nil {
		return Wallet{}, err
	}
	if note == "" {
		note = fmt.Sprintf("Hoàn tiền cược ván #%d", messageID)
	}
	if _, err := insertWalletLedgerTx(ctx, tx, userID, "bet_refund", amount, 0, &messageID, 0, nil, note); err != nil {
		return Wallet{}, err
	}
	return loadWallet(ctx, tx, userID)
}

// RefundUserBet hoàn tiền cược đã giữ cho một người chơi (khi rời phòng hoặc rollback join lỗi).
func (s *Store) RefundUserBet(ctx context.Context, userID, messageID int64, note string) (*Wallet, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var amount int64
	err = tx.QueryRowContext(ctx, `
		SELECT amount FROM game_bets WHERE message_id = ? AND user_id = ? AND status = 'locked'`,
		messageID, userID).Scan(&amount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	w, err := refundLockedBetRowTx(ctx, tx, messageID, userID, amount, note)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &w, nil
}

func refundGameBetsTx(ctx context.Context, tx *sql.Tx, messageID int64, note string) ([]Wallet, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT user_id, amount FROM game_bets WHERE message_id = ? AND status = 'locked' ORDER BY user_id`, messageID)
	if err != nil {
		return nil, err
	}
	type lockedRow struct {
		userID int64
		amount int64
	}
	var list []lockedRow
	for rows.Next() {
		var r lockedRow
		if err := rows.Scan(&r.userID, &r.amount); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	out := make([]Wallet, 0, len(list))
	for _, r := range list {
		w, err := refundLockedBetRowTx(ctx, tx, messageID, r.userID, r.amount, note)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, nil
}

// RefundGameBets hoàn tiền cho tất cả người chơi đang khoá cược trên ván messageID (idempotent).
func (s *Store) RefundGameBets(ctx context.Context, messageID int64, note string) ([]Wallet, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	out, err := refundGameBetsTx(ctx, tx, messageID, note)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// SettleGameBets thanh toán kết quả ván Cờ tướng một lần duy nhất (idempotent theo game_bets.status='locked').
func (s *Store) SettleGameBets(ctx context.Context, messageID int64, winners []int64) ([]Wallet, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT user_id, amount FROM game_bets WHERE message_id = ? AND status = 'locked' ORDER BY user_id`, messageID)
	if err != nil {
		return nil, err
	}
	type lockedRow struct {
		userID int64
		amount int64
	}
	var list []lockedRow
	var totalPot int64
	for rows.Next() {
		var r lockedRow
		if err := rows.Scan(&r.userID, &r.amount); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, r)
		totalPot += r.amount
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}

	winSet := make(map[int64]bool, len(winners))
	var validWinners []int64
	for _, wid := range winners {
		for _, r := range list {
			if r.userID == wid && !winSet[wid] {
				winSet[wid] = true
				validWinners = append(validWinners, wid)
				break
			}
		}
	}
	if len(validWinners) == 0 || len(list) < 2 {
		out, err := refundGameBetsTx(ctx, tx, messageID, fmt.Sprintf("Hoàn tiền cược ván Cờ tướng #%d (hoà/huỷ)", messageID))
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return out, nil
	}

	perWinner := totalPot / int64(len(validWinners))
	remainder := totalPot % int64(len(validWinners))
	out := make([]Wallet, 0, len(list))

	for _, r := range list {
		if _, err := loadWallet(ctx, tx, r.userID); err != nil {
			return nil, err
		}
		if winSet[r.userID] {
			payout := perWinner
			if r.userID == validWinners[0] {
				payout += remainder
			}
			res, err := tx.ExecContext(ctx, `
				UPDATE game_bets SET status = 'won', payout = ?, settled_at = CURRENT_TIMESTAMP
				WHERE message_id = ? AND user_id = ? AND status = 'locked'`,
				payout, messageID, r.userID)
			if err != nil {
				return nil, err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE wallets SET balance = balance + ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`,
				payout, r.userID); err != nil {
				return nil, err
			}
			if _, err := insertWalletLedgerTx(ctx, tx, r.userID, "bet_win", payout, 0, &messageID, 0, nil,
				fmt.Sprintf("Thắng cược ván Cờ tướng #%d (+%d xu)", messageID, payout)); err != nil {
				return nil, err
			}
		} else {
			res, err := tx.ExecContext(ctx, `
				UPDATE game_bets SET status = 'lost', payout = 0, settled_at = CURRENT_TIMESTAMP
				WHERE message_id = ? AND user_id = ? AND status = 'locked'`,
				messageID, r.userID)
			if err != nil {
				return nil, err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				continue
			}
			if _, err := insertWalletLedgerTx(ctx, tx, r.userID, "bet_loss", 0, 0, &messageID, 0, nil,
				fmt.Sprintf("Thua cược ván Cờ tướng #%d (-%d xu)", messageID, r.amount)); err != nil {
				return nil, err
			}
		}
		w, err := loadWallet(ctx, tx, r.userID)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}
