package controller

import (
	"context"
	"time"

	"chat/game"
)

// SettlePendingCashoutsForTest: chạy bước trả chip bù lúc khởi động (chỉ dùng trong test).
func (a *API) SettlePendingCashoutsForTest(ctx context.Context) { a.settlePendingCashouts(ctx) }

// CancelGameWithCancelledContextForTest: chủ phòng huỷ ván trong một request mà context đã bị huỷ
// (người dùng đóng tab ngay sau khi bấm).
func (a *API) CancelGameWithCancelledContextForTest(messageID, hostID int64) error {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s, err := a.gameSession(context.Background(), messageID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.g.Act(game.Player{ID: hostID}, "cancel", nil, time.Now()); err != nil {
		return err
	}
	a.saveAndBroadcast(ctx, s)
	return nil
}
