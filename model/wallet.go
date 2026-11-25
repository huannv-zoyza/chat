package model

import "time"

// WalletTransaction là một bút toán trong sổ cái giao dịch ví xu.
type WalletTransaction struct {
	ID        int64       `json:"id"`
	UserID    int64       `json:"user_id"`
	Kind      string      `json:"kind"`
	Amount    int64       `json:"amount"`
	DebtDelta int64       `json:"debt_delta"`
	GameID    *int64      `json:"game_id,omitempty"`
	Seq       int64       `json:"seq"`
	PeerUser  *PublicUser `json:"peer_user,omitempty"`
	Note      string      `json:"note"`
	CreatedAt time.Time   `json:"created_at"`
}
