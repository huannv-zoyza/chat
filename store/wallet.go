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

const (
	InitialWalletBalance int64 = 10000
	DailyCheckinReward   int64 = 2000
	BailoutReward        int64 = 1000
	BailoutMaxTotal      int64 = 500
	BailoutDailyLimit          = 3
)

var (
	ErrInsufficientBalance   = errors.New("insufficient balance")
	ErrCheckinAlreadyClaimed = errors.New("daily checkin already claimed")
	ErrBailoutNotEligible    = errors.New("bailout not eligible")
	ErrBailoutLimitReached   = errors.New("bailout daily limit reached")
	ErrInvalidAmount         = errors.New("invalid amount")
	ErrSelfTransfer          = errors.New("cannot transfer to self")
	ErrRecipientNotFound     = errors.New("recipient not found")
	ErrBetAlreadyLocked      = errors.New("bet already locked")
)

var ictZone = time.FixedZone("ICT", 7*3600)

func ictDate(t time.Time) string {
	return t.In(ictZone).Format("2006-01-02")
}

// WalletUpdate đại diện cho trạng thái ví và bút toán mới nhất của một người dùng sau một thao tác.
type WalletUpdate struct {
	UserID      int64
	Wallet      model.Wallet
	Transaction model.WalletTransaction
}

type walletRow struct {
	UserID        int64
	Balance       int64
	LockedBalance int64
	LastCheckinAt sql.NullTime
	BailoutDate   string
	BailoutCount  int
	UpdatedAt     time.Time
}

func toModelWallet(w walletRow, now time.Time) model.Wallet {
	today := ictDate(now)
	canCheckin := true
	var lastCheckin *time.Time
	if w.LastCheckinAt.Valid {
		t := w.LastCheckinAt.Time
		lastCheckin = &t
		if ictDate(t) == today {
			canCheckin = false
		}
	}
	bailoutCount := w.BailoutCount
	if w.BailoutDate != today {
		bailoutCount = 0
	}
	canBailout := (w.Balance+w.LockedBalance) < BailoutMaxTotal && bailoutCount < BailoutDailyLimit
	return model.Wallet{
		UserID:        w.UserID,
		Balance:       w.Balance,
		LockedBalance: w.LockedBalance,
		CanCheckin:    canCheckin,
		CanBailout:    canBailout,
		LastCheckinAt: lastCheckin,
		UpdatedAt:     w.UpdatedAt,
	}
}

func readWalletTx(ctx context.Context, tx *sql.Tx, userID int64) (walletRow, error) {
	var w walletRow
	err := tx.QueryRowContext(ctx, `
		SELECT user_id, balance, locked_balance, last_checkin_at, bailout_date, bailout_count, updated_at
		FROM wallets WHERE user_id = ?`, userID).
		Scan(&w.UserID, &w.Balance, &w.LockedBalance, &w.LastCheckinAt, &w.BailoutDate, &w.BailoutCount, &w.UpdatedAt)
	return w, err
}

func ensureWalletTx(ctx context.Context, tx *sql.Tx, userID int64) (walletRow, error) {
	w, err := readWalletTx(ctx, tx, userID)
	if err == nil {
		return w, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return walletRow{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO wallets (user_id, balance, locked_balance) VALUES (?, ?, 0)`,
		userID, InitialWalletBalance); err != nil {
		return walletRow{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO wallet_transactions (user_id, type, amount, balance_after, ref_type, note, idempotency_key)
		VALUES (?, 'signup_bonus', ?, ?, 'system', 'Vốn khởi tạo tài khoản', ?)`,
		userID, InitialWalletBalance, InitialWalletBalance, fmt.Sprintf("signup_bonus:%d", userID)); err != nil {
		return walletRow{}, err
	}
	return readWalletTx(ctx, tx, userID)
}

func insertWalletTx(ctx context.Context, tx *sql.Tx, userID int64, txType string, amount, balanceAfter int64, refType string, refID *int64, peerUserID *int64, note, idempotencyKey string) (model.WalletTransaction, error) {
	var idem any
	if idempotencyKey != "" {
		idem = idempotencyKey
	}
	var rID any
	if refID != nil {
		rID = *refID
	}
	var pID any
	if peerUserID != nil {
		pID = *peerUserID
	}
	var wt model.WalletTransaction
	err := tx.QueryRowContext(ctx, `
		INSERT INTO wallet_transactions (user_id, type, amount, balance_after, ref_type, ref_id, peer_user_id, note, idempotency_key)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id, user_id, type, amount, balance_after, ref_type, ref_id, note, created_at`,
		userID, txType, amount, balanceAfter, refType, rID, pID, note, idem).
		Scan(&wt.ID, &wt.UserID, &wt.Type, &wt.Amount, &wt.BalanceAfter, &wt.RefType, &wt.RefID, &wt.Note, &wt.CreatedAt)
	if err != nil {
		return model.WalletTransaction{}, err
	}
	if peerUserID != nil {
		var u model.PublicUser
		err := tx.QueryRowContext(ctx, `SELECT id, username, display_name, avatar, status_text FROM users WHERE id = ?`, *peerUserID).
			Scan(&u.ID, &u.Username, &u.DisplayName, &u.Avatar, &u.StatusText)
		if err == nil {
			wt.PeerUser = &u
		}
	}
	return wt, nil
}

// GetWallet trả về thông tin ví Xu hiện tại của người dùng.
func (s *Store) GetWallet(ctx context.Context, userID int64) (model.Wallet, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Wallet{}, err
	}
	defer tx.Rollback()
	w, err := ensureWalletTx(ctx, tx, userID)
	if err != nil {
		return model.Wallet{}, notFound(err)
	}
	if err := tx.Commit(); err != nil {
		return model.Wallet{}, err
	}
	return toModelWallet(w, time.Now()), nil
}

// ListWalletTransactions trả về lịch sử giao dịch ví Xu phân trang theo cursor beforeID.
func (s *Store) ListWalletTransactions(ctx context.Context, userID int64, txType string, beforeID int64, limit int) ([]model.WalletTransaction, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	query := `
		SELECT t.id, t.user_id, t.type, t.amount, t.balance_after, t.ref_type, t.ref_id, t.note, t.created_at,
		       u.id, u.username, u.display_name, u.avatar, u.status_text
		FROM wallet_transactions t
		LEFT JOIN users u ON u.id = t.peer_user_id
		WHERE t.user_id = ?`
	args := []any{userID}
	if txType != "" && txType != "all" {
		query += ` AND t.type = ?`
		args = append(args, txType)
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
			&wt.ID, &wt.UserID, &wt.Type, &wt.Amount, &wt.BalanceAfter, &wt.RefType, &wt.RefID, &wt.Note, &wt.CreatedAt,
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

// ClaimDailyCheckin điểm danh nhận +2,000 Xu mỗi ngày (theo ngày UTC+7).
func (s *Store) ClaimDailyCheckin(ctx context.Context, userID int64, now time.Time) (model.Wallet, model.WalletTransaction, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Wallet{}, model.WalletTransaction{}, err
	}
	defer tx.Rollback()

	w, err := ensureWalletTx(ctx, tx, userID)
	if err != nil {
		return model.Wallet{}, model.WalletTransaction{}, err
	}
	today := ictDate(now)
	if w.LastCheckinAt.Valid && ictDate(w.LastCheckinAt.Time) == today {
		return model.Wallet{}, model.WalletTransaction{}, ErrCheckinAlreadyClaimed
	}
	newBal := w.Balance + DailyCheckinReward
	if _, err := tx.ExecContext(ctx, `
		UPDATE wallets SET balance = ?, last_checkin_at = ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`,
		newBal, now.UTC(), userID); err != nil {
		return model.Wallet{}, model.WalletTransaction{}, err
	}
	wt, err := insertWalletTx(ctx, tx, userID, "daily_checkin", DailyCheckinReward, newBal, "system", nil, nil,
		fmt.Sprintf("Điểm danh ngày %s", today), fmt.Sprintf("checkin:%d:%s", userID, today))
	if err != nil {
		if isUniqueViolation(err) {
			return model.Wallet{}, model.WalletTransaction{}, ErrCheckinAlreadyClaimed
		}
		return model.Wallet{}, model.WalletTransaction{}, err
	}
	w, err = readWalletTx(ctx, tx, userID)
	if err != nil {
		return model.Wallet{}, model.WalletTransaction{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Wallet{}, model.WalletTransaction{}, err
	}
	return toModelWallet(w, now), wt, nil
}

// ClaimBailout nhận hỗ trợ +1,000 Xu khi tổng tài sản (balance + locked_balance) < 500 Xu (tối đa 3 lần/ngày).
func (s *Store) ClaimBailout(ctx context.Context, userID int64, now time.Time) (model.Wallet, model.WalletTransaction, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Wallet{}, model.WalletTransaction{}, err
	}
	defer tx.Rollback()

	w, err := ensureWalletTx(ctx, tx, userID)
	if err != nil {
		return model.Wallet{}, model.WalletTransaction{}, err
	}
	if w.Balance+w.LockedBalance >= BailoutMaxTotal {
		return model.Wallet{}, model.WalletTransaction{}, ErrBailoutNotEligible
	}
	today := ictDate(now)
	count := w.BailoutCount
	if w.BailoutDate != today {
		count = 0
	}
	if count >= BailoutDailyLimit {
		return model.Wallet{}, model.WalletTransaction{}, ErrBailoutLimitReached
	}
	count++
	newBal := w.Balance + BailoutReward
	if _, err := tx.ExecContext(ctx, `
		UPDATE wallets SET balance = ?, bailout_date = ?, bailout_count = ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`,
		newBal, today, count, userID); err != nil {
		return model.Wallet{}, model.WalletTransaction{}, err
	}
	wt, err := insertWalletTx(ctx, tx, userID, "bailout", BailoutReward, newBal, "system", nil, nil,
		fmt.Sprintf("Nhận cứu trợ vốn lần %d/%d ngày %s", count, BailoutDailyLimit, today),
		fmt.Sprintf("bailout:%d:%s:%d", userID, today, count))
	if err != nil {
		return model.Wallet{}, model.WalletTransaction{}, err
	}
	w, err = readWalletTx(ctx, tx, userID)
	if err != nil {
		return model.Wallet{}, model.WalletTransaction{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Wallet{}, model.WalletTransaction{}, err
	}
	return toModelWallet(w, now), wt, nil
}

// TransferCoins chuyển Xu P2P nguyên tử từ fromUserID sang toUserID.
func (s *Store) TransferCoins(ctx context.Context, fromUserID, toUserID, amount int64, note string) (WalletUpdate, WalletUpdate, error) {
	if amount <= 0 {
		return WalletUpdate{}, WalletUpdate{}, ErrInvalidAmount
	}
	if fromUserID == toUserID {
		return WalletUpdate{}, WalletUpdate{}, ErrSelfTransfer
	}
	note = strings.TrimSpace(note)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WalletUpdate{}, WalletUpdate{}, err
	}
	defer tx.Rollback()

	var toStatus string
	var toName, toUsername string
	if err := tx.QueryRowContext(ctx, `SELECT status, display_name, username FROM users WHERE id = ?`, toUserID).
		Scan(&toStatus, &toName, &toUsername); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WalletUpdate{}, WalletUpdate{}, ErrRecipientNotFound
		}
		return WalletUpdate{}, WalletUpdate{}, err
	}
	if toStatus != string(model.UserStatusActive) {
		return WalletUpdate{}, WalletUpdate{}, ErrRecipientNotFound
	}
	var fromName, fromUsername string
	if err := tx.QueryRowContext(ctx, `SELECT display_name, username FROM users WHERE id = ?`, fromUserID).
		Scan(&fromName, &fromUsername); err != nil {
		return WalletUpdate{}, WalletUpdate{}, notFound(err)
	}

	senderW, err := ensureWalletTx(ctx, tx, fromUserID)
	if err != nil {
		return WalletUpdate{}, WalletUpdate{}, err
	}
	if senderW.Balance < amount {
		return WalletUpdate{}, WalletUpdate{}, ErrInsufficientBalance
	}
	recvW, err := ensureWalletTx(ctx, tx, toUserID)
	if err != nil {
		return WalletUpdate{}, WalletUpdate{}, err
	}

	newSenderBal := senderW.Balance - amount
	newRecvBal := recvW.Balance + amount
	if _, err := tx.ExecContext(ctx, `UPDATE wallets SET balance = ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`, newSenderBal, fromUserID); err != nil {
		return WalletUpdate{}, WalletUpdate{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE wallets SET balance = ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`, newRecvBal, toUserID); err != nil {
		return WalletUpdate{}, WalletUpdate{}, err
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

	sTx, err := insertWalletTx(ctx, tx, fromUserID, "transfer_out", -amount, newSenderBal, "transfer", nil, &toUserID, senderNote, "")
	if err != nil {
		return WalletUpdate{}, WalletUpdate{}, err
	}
	rTx, err := insertWalletTx(ctx, tx, toUserID, "transfer_in", amount, newRecvBal, "transfer", &sTx.ID, &fromUserID, recvNote, "")
	if err != nil {
		return WalletUpdate{}, WalletUpdate{}, err
	}

	senderW, err = readWalletTx(ctx, tx, fromUserID)
	if err != nil {
		return WalletUpdate{}, WalletUpdate{}, err
	}
	recvW, err = readWalletTx(ctx, tx, toUserID)
	if err != nil {
		return WalletUpdate{}, WalletUpdate{}, err
	}
	if err := tx.Commit(); err != nil {
		return WalletUpdate{}, WalletUpdate{}, err
	}
	now := time.Now()
	return WalletUpdate{UserID: fromUserID, Wallet: toModelWallet(senderW, now), Transaction: sTx},
		WalletUpdate{UserID: toUserID, Wallet: toModelWallet(recvW, now), Transaction: rTx}, nil
}

func lockGameBetTx(ctx context.Context, tx *sql.Tx, userID, messageID, amount int64) (WalletUpdate, error) {
	if amount <= 0 {
		return WalletUpdate{}, ErrInvalidAmount
	}
	w, err := ensureWalletTx(ctx, tx, userID)
	if err != nil {
		return WalletUpdate{}, err
	}
	if w.Balance < amount {
		return WalletUpdate{}, ErrInsufficientBalance
	}
	res, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO game_bets (message_id, user_id, amount, status) VALUES (?, ?, ?, 'locked')`,
		messageID, userID, amount)
	if err != nil {
		return WalletUpdate{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return WalletUpdate{}, ErrBetAlreadyLocked
	}
	newBal := w.Balance - amount
	newLocked := w.LockedBalance + amount
	if _, err := tx.ExecContext(ctx, `
		UPDATE wallets SET balance = ?, locked_balance = ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`,
		newBal, newLocked, userID); err != nil {
		return WalletUpdate{}, err
	}
	wt, err := insertWalletTx(ctx, tx, userID, "bet_lock", -amount, newBal, "game", &messageID, nil,
		fmt.Sprintf("Đặt cược Cờ tướng #%d", messageID), fmt.Sprintf("bet_lock:%d:%d", messageID, userID))
	if err != nil {
		return WalletUpdate{}, err
	}
	w, err = readWalletTx(ctx, tx, userID)
	if err != nil {
		return WalletUpdate{}, err
	}
	return WalletUpdate{UserID: userID, Wallet: toModelWallet(w, time.Now()), Transaction: wt}, nil
}

// CreateGameWithBet tạo tin nhắn game và tạm giữ tiền cược của chủ phòng (nếu betAmount > 0) trong một transaction nguyên tử.
func (s *Store) CreateGameWithBet(ctx context.Context, channelID, userID int64, embed model.Embed, status string, state []byte, betAmount int64) (model.Message, *WalletUpdate, error) {
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
	var wu *WalletUpdate
	if betAmount > 0 {
		u, err := lockGameBetTx(ctx, tx, userID, id, betAmount)
		if err != nil {
			return model.Message{}, nil, err
		}
		wu = &u
	}
	if err := tx.Commit(); err != nil {
		return model.Message{}, nil, err
	}
	m, err := s.GetMessage(ctx, id)
	return m, wu, err
}

// LockGameBet tạm giữ tiền cược của người chơi khi tham gia ván cờ.
func (s *Store) LockGameBet(ctx context.Context, userID, messageID, amount int64) (WalletUpdate, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WalletUpdate{}, err
	}
	defer tx.Rollback()

	wu, err := lockGameBetTx(ctx, tx, userID, messageID, amount)
	if err != nil {
		return WalletUpdate{}, err
	}
	if err := tx.Commit(); err != nil {
		return WalletUpdate{}, err
	}
	return wu, nil
}

// RefundUserBet hoàn lại tiền cược đã giữ cho một người chơi cụ thể (khi rời phòng hoặc rollback join lỗi).
func (s *Store) RefundUserBet(ctx context.Context, userID, messageID int64, note string) (*WalletUpdate, error) {
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
	wu, err := refundLockedBetRowTx(ctx, tx, messageID, userID, amount, note)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &wu, nil
}

func refundLockedBetRowTx(ctx context.Context, tx *sql.Tx, messageID, userID, amount int64, note string) (WalletUpdate, error) {
	w, err := ensureWalletTx(ctx, tx, userID)
	if err != nil {
		return WalletUpdate{}, err
	}
	newBal := w.Balance + amount
	newLocked := w.LockedBalance - amount
	if newLocked < 0 {
		newLocked = 0
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE wallets SET balance = ?, locked_balance = ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`,
		newBal, newLocked, userID); err != nil {
		return WalletUpdate{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE game_bets SET status = 'refunded', payout = ?, settled_at = CURRENT_TIMESTAMP
		WHERE message_id = ? AND user_id = ? AND status = 'locked'`,
		amount, messageID, userID); err != nil {
		return WalletUpdate{}, err
	}
	if note == "" {
		note = fmt.Sprintf("Hoàn tiền cược ván #%d", messageID)
	}
	wt, err := insertWalletTx(ctx, tx, userID, "bet_refund", amount, newBal, "game", &messageID, nil,
		note, fmt.Sprintf("bet_settle:%d:%d", messageID, userID))
	if err != nil {
		return WalletUpdate{}, err
	}
	w, err = readWalletTx(ctx, tx, userID)
	if err != nil {
		return WalletUpdate{}, err
	}
	return WalletUpdate{UserID: userID, Wallet: toModelWallet(w, time.Now()), Transaction: wt}, nil
}

func refundGameBetsTx(ctx context.Context, tx *sql.Tx, messageID int64, note string) ([]WalletUpdate, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT user_id, amount FROM game_bets WHERE message_id = ? AND status = 'locked'`, messageID)
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
	updates := make([]WalletUpdate, 0, len(list))
	for _, r := range list {
		wu, err := refundLockedBetRowTx(ctx, tx, messageID, r.userID, r.amount, note)
		if err != nil {
			return nil, err
		}
		updates = append(updates, wu)
	}
	return updates, nil
}

// RefundGameBets hoàn tiền cho tất cả người chơi đang khoá cược trên ván messageID (idempotent).
func (s *Store) RefundGameBets(ctx context.Context, messageID int64, note string) ([]WalletUpdate, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	updates, err := refundGameBetsTx(ctx, tx, messageID, note)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return updates, nil
}

// SettleGameBets thanh toán kết quả ván cờ một lần duy nhất (idempotent theo game_bets.status='locked').
func (s *Store) SettleGameBets(ctx context.Context, messageID int64, winners []int64) ([]WalletUpdate, error) {
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

	// Nếu chỉ có <= 1 người lock (ví dụ huỷ ở lobby / hết hạn lobby) hoặc hoà (len(winners) == 0): hoàn tiền 100%.
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
		note := fmt.Sprintf("Hoàn tiền cược ván Cờ tướng #%d (hoà/huỷ)", messageID)
		updates, err := refundGameBetsTx(ctx, tx, messageID, note)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return updates, nil
	}

	perWinner := totalPot / int64(len(validWinners))
	remainder := totalPot % int64(len(validWinners))
	updates := make([]WalletUpdate, 0, len(list))

	for _, r := range list {
		w, err := ensureWalletTx(ctx, tx, r.userID)
		if err != nil {
			return nil, err
		}
		newLocked := w.LockedBalance - r.amount
		if newLocked < 0 {
			newLocked = 0
		}
		var wt model.WalletTransaction
		if winSet[r.userID] {
			payout := perWinner
			if r.userID == validWinners[0] {
				payout += remainder
			}
			newBal := w.Balance + payout
			if _, err := tx.ExecContext(ctx, `
				UPDATE wallets SET balance = ?, locked_balance = ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`,
				newBal, newLocked, r.userID); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE game_bets SET status = 'won', payout = ?, settled_at = CURRENT_TIMESTAMP
				WHERE message_id = ? AND user_id = ? AND status = 'locked'`,
				payout, messageID, r.userID); err != nil {
				return nil, err
			}
			wt, err = insertWalletTx(ctx, tx, r.userID, "bet_win", payout, newBal, "game", &messageID, nil,
				fmt.Sprintf("Thắng cược ván Cờ tướng #%d (+%d Xu)", messageID, payout),
				fmt.Sprintf("bet_settle:%d:%d", messageID, r.userID))
			if err != nil {
				return nil, err
			}
		} else {
			if _, err := tx.ExecContext(ctx, `
				UPDATE wallets SET locked_balance = ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`,
				newLocked, r.userID); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE game_bets SET status = 'lost', payout = 0, settled_at = CURRENT_TIMESTAMP
				WHERE message_id = ? AND user_id = ? AND status = 'locked'`,
				messageID, r.userID); err != nil {
				return nil, err
			}
			wt, err = insertWalletTx(ctx, tx, r.userID, "bet_loss", 0, w.Balance, "game", &messageID, nil,
				fmt.Sprintf("Thua cược ván Cờ tướng #%d (-%d Xu)", messageID, r.amount),
				fmt.Sprintf("bet_settle:%d:%d", messageID, r.userID))
			if err != nil {
				return nil, err
			}
		}
		w, err = readWalletTx(ctx, tx, r.userID)
		if err != nil {
			return nil, err
		}
		updates = append(updates, WalletUpdate{UserID: r.userID, Wallet: toModelWallet(w, time.Now()), Transaction: wt})
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return updates, nil
}
