package model

import "time"

// Wallet là thông tin ví Xu của một thành viên.
type Wallet struct {
	UserID        int64      `json:"user_id"`
	Balance       int64      `json:"balance"`
	LockedBalance int64      `json:"locked_balance"`
	CanCheckin    bool       `json:"can_checkin"`
	CanBailout    bool       `json:"can_bailout"`
	LastCheckinAt *time.Time `json:"last_checkin_at,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// WalletTransaction là một bút toán trong sổ cái giao dịch ví Xu.
type WalletTransaction struct {
	ID           int64       `json:"id"`
	UserID       int64       `json:"user_id"`
	Type         string      `json:"type"`
	Amount       int64       `json:"amount"`
	BalanceAfter int64       `json:"balance_after"`
	RefType      string      `json:"ref_type,omitempty"`
	RefID        *int64      `json:"ref_id,omitempty"`
	PeerUser     *PublicUser `json:"peer_user,omitempty"`
	Note         string      `json:"note"`
	CreatedAt    time.Time   `json:"created_at"`
}
