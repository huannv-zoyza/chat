package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"chat/model"
)

// Ví xu ảo: dùng chung cả server, tạo lười (1.000 xu) lần đầu chạm tới.
// Ván dùng chip (bầu cua) chỉ chạm ví ở hai lúc: mua chip (BuyIn) và trả chip (PayOut),
// mỗi lần là một transaction ghi cả ví lẫn trạng thái ván.

const (
	WalletStart = 1000
	DailyAmount = 100
	LoanAmount  = 500
	LoanBelow   = 100 // chỉ được vay khi số dư dưới mức này
)

var (
	ErrAlreadyClaimed = errors.New("already claimed today")
	ErrLoanNotAllowed = errors.New("loan not allowed")
	ErrNothingToRepay = errors.New("nothing to repay")
)

type Wallet struct {
	UserID    int64  `json:"-"`
	Balance   int64  `json:"balance"`
	Debt      int64  `json:"debt"`
	LastClaim string `json:"-"`
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
	err := q.QueryRowContext(ctx, `SELECT balance, debt, last_claim FROM wallets WHERE user_id = ?`, userID).
		Scan(&w.Balance, &w.Debt, &last)
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
	if _, err := tx.ExecContext(ctx,
		`UPDATE wallets SET balance = balance + ?, debt = debt + ?, updated_at = CURRENT_TIMESTAMP WHERE user_id = ?`,
		amount, debt, userID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO wallet_tx (user_id, kind, amount, debt_delta) VALUES (?, ?, ?, ?)`, userID, kind, amount, debt)
	return err
}

// ClaimDaily: nhận DailyAmount xu, mỗi ngày (today = "YYYY-MM-DD" giờ VN) một lần.
func (s *Store) ClaimDaily(ctx context.Context, userID int64, today string) (Wallet, error) {
	return s.walletTx(ctx, userID, func(tx *sql.Tx, w Wallet) error {
		if w.LastClaim == today {
			return ErrAlreadyClaimed
		}
		if _, err := tx.ExecContext(ctx, `UPDATE wallets SET last_claim = ? WHERE user_id = ?`, today, userID); err != nil {
			return err
		}
		return addTx(ctx, tx, userID, "daily", DailyAmount, 0)
	})
}

// TakeLoan: vay LoanAmount xu (thành nợ), chỉ khi số dư dưới LoanBelow.
func (s *Store) TakeLoan(ctx context.Context, userID int64) (Wallet, error) {
	return s.walletTx(ctx, userID, func(tx *sql.Tx, w Wallet) error {
		if w.Balance >= LoanBelow {
			return ErrLoanNotAllowed
		}
		return addTx(ctx, tx, userID, "loan", LoanAmount, LoanAmount)
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
