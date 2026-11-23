package controller

import "context"

// SettlePendingCashoutsForTest: chạy bước trả chip bù lúc khởi động (chỉ dùng trong test).
func (a *API) SettlePendingCashoutsForTest(ctx context.Context) { a.settlePendingCashouts(ctx) }
