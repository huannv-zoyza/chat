package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"chat/model"
)

// Chợ đen (specs/FARM_SPEC.md §15): người chơi treo hàng trong kho với giá tự đặt, cả server dùng chung
// MarketSlots chỗ. Hàng rời kho người bán ngay lúc treo; có người mua thì xu chuyển thẳng cho người bán
// (qua earn: đang nợ thì tự trừ 50%), gỡ xuống hoặc hết hạn thì hàng về kho.

const (
	MarketSlots    = 10
	MarketPerUser  = 3
	MarketTTL      = 24 * time.Hour
	MarketMaxQty   = 50
	MarketMaxPrice = 1_000_000
)

var (
	ErrMarketFull     = &FarmError{"Chợ đen đã kín 10 chỗ, chờ có người mua hoặc gỡ hàng nhé."}
	ErrMarketUserMax  = &FarmError{fmt.Sprintf("Bạn đã treo đủ %d món ở chợ đen.", MarketPerUser)}
	ErrMarketBadPrice = &FarmError{fmt.Sprintf("Giá phải từ 1 tới %d xu.", MarketMaxPrice)}
	ErrMarketGone     = &FarmError{"Món này đã bán hoặc bị gỡ rồi."}
	ErrMarketOwnBuy   = &FarmError{"Không mua được hàng của chính mình."}
	ErrMarketNotOwner = &FarmError{"Đây không phải hàng của bạn."}
)

// MarketListing: một lô hàng đang treo. Price là giá cả lô.
type MarketListing struct {
	ID        int64            `json:"id"`
	Slot      int              `json:"slot"`
	Seller    model.PublicUser `json:"seller"`
	Item      string           `json:"item"`
	Qty       int64            `json:"qty"`
	Price     int64            `json:"price"`
	ExpiresAt string           `json:"expires_at"` // RFC3339
	Mine      bool             `json:"mine"`
}

// MarketView: chợ đen như người xem thấy (FarmView.market, chỉ khi me).
type MarketView struct {
	Slots    int             `json:"slots"`
	Used     int             `json:"used"` // số chỗ đang bị chiếm (kể cả lô của người bị khoá, không hiện)
	PerUser  int             `json:"per_user"`
	MaxQty   int64           `json:"max_qty"`
	MaxPrice int64           `json:"max_price"`
	Listings []MarketListing `json:"listings"` // theo slot tăng dần
}

type marketRow struct {
	ID, Seller, Qty, Price, Expires int64
	Slot                            int
	Item                            string
}

func loadMarket(ctx context.Context, q queryer) ([]marketRow, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, slot, seller_id, item, qty, price, expires_at FROM farm_market ORDER BY slot`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []marketRow
	for rows.Next() {
		var m marketRow
		if err := rows.Scan(&m.ID, &m.Slot, &m.Seller, &m.Item, &m.Qty, &m.Price, &m.Expires); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// expireMarket trả mọi lô quá hạn về kho người bán (được vượt sức chứa: hàng vốn của họ) và ghi nhật ký.
func (op *farmOp) expireMarket() error {
	rows, err := loadMarket(op.ctx, op.tx)
	if err != nil {
		return err
	}
	for _, m := range rows {
		if m.Expires > op.unix() {
			continue
		}
		if _, err := op.tx.ExecContext(op.ctx, `DELETE FROM farm_market WHERE id = ?`, m.ID); err != nil {
			return err
		}
		if err := op.addStock(m.Seller, m.Item, m.Qty); err != nil {
			return err
		}
		seller, err := farmUser(op.ctx, op.tx, m.Seller)
		if errors.Is(err, ErrNotFound) {
			continue // người bán bị khoá: hàng vẫn về kho, bỏ qua nhật ký
		} else if err != nil {
			return err
		}
		if _, err := op.log(m.Seller, seller, "market_expired", m.Item, m.Qty); err != nil {
			return err
		}
	}
	return nil
}

// FarmMarketExpire: dọn lô quá hạn (chỉ mở transaction khi có lô quá hạn).
func (s *Store) FarmMarketExpire(ctx context.Context) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM farm_market WHERE expires_at <= ?`, s.now().Unix()).
		Scan(&n); err != nil || n == 0 {
		return err
	}
	_, err := s.farmTx(ctx, func(op *farmOp) error { return op.expireMarket() })
	return err
}

func (op *farmOp) loadListing(id int64) (marketRow, error) {
	var m marketRow
	err := op.tx.QueryRowContext(op.ctx,
		`SELECT id, slot, seller_id, item, qty, price, expires_at FROM farm_market WHERE id = ?`, id).
		Scan(&m.ID, &m.Slot, &m.Seller, &m.Item, &m.Qty, &m.Price, &m.Expires)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrMarketGone
	}
	return m, err
}

// FarmMarketList: treo qty đơn vị itemID trong kho của sellerID lên chợ đen với giá price (cả lô).
func (s *Store) FarmMarketList(ctx context.Context, sellerID int64, itemID string, qty, price int64) (FarmResult, error) {
	res := FarmResult{OwnerID: sellerID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		it, ok := itemByID(itemID)
		if !ok {
			return ErrFarmUnknownItem
		}
		if qty <= 0 || qty > MarketMaxQty {
			return ErrFarmBadQty
		}
		if price <= 0 || price > MarketMaxPrice {
			return ErrMarketBadPrice
		}
		if _, err := ensureFarm(ctx, op.tx, sellerID); err != nil {
			return err
		}
		if err := op.expireMarket(); err != nil {
			return err
		}
		rows, err := loadMarket(ctx, op.tx)
		if err != nil {
			return err
		}
		used := map[int]bool{}
		mine := 0
		for _, m := range rows {
			used[m.Slot] = true
			if m.Seller == sellerID {
				mine++
			}
		}
		if len(rows) >= MarketSlots {
			return ErrMarketFull
		}
		if mine >= MarketPerUser {
			return ErrMarketUserMax
		}
		slot := 0
		for used[slot] {
			slot++
		}
		if ok, err := op.takeStock(sellerID, it.ID, qty); err != nil {
			return err
		} else if !ok {
			return farmErr("Trong kho không đủ %d %s để treo bán.", qty, it.Name)
		}
		_, err = op.tx.ExecContext(ctx, `
			INSERT INTO farm_market (slot, seller_id, item, qty, price, created_at, expires_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			slot, sellerID, it.ID, qty, price, op.unix(), op.now.Add(MarketTTL).Unix())
		return err
	})
	if err == nil {
		res.Market = true
	}
	return res, err
}

// FarmMarketBuy: buyerID mua cả lô listingID; xu chuyển cho người bán, hàng vào kho người mua.
func (s *Store) FarmMarketBuy(ctx context.Context, buyerID, listingID int64) (FarmResult, error) {
	res := FarmResult{OwnerID: buyerID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		if err := op.expireMarket(); err != nil {
			return err
		}
		m, err := op.loadListing(listingID)
		if err != nil {
			return err
		}
		if m.Seller == buyerID {
			return ErrMarketOwnBuy
		}
		buyer, err := farmUser(ctx, op.tx, buyerID)
		if err != nil {
			return err
		}
		if room, err := op.storageRoom(buyerID); err != nil {
			return err
		} else if room < m.Qty {
			return ErrFarmStorageFull
		}
		name := itemName(m.Item)
		if err := op.spend(buyerID, m.Price, fmt.Sprintf("Mua %d %s ở chợ đen", m.Qty, name)); err != nil {
			return err
		}
		if _, err := op.tx.ExecContext(ctx, `DELETE FROM farm_market WHERE id = ?`, m.ID); err != nil {
			return err
		}
		if err := op.addStock(buyerID, m.Item, m.Qty); err != nil {
			return err
		}
		if err := op.earn(m.Seller, m.Price,
			fmt.Sprintf("Bán %d %s ở chợ đen cho %s", m.Qty, name, userLabel(buyer))); err != nil {
			return err
		}
		res.Gained = &Gained{Xu: -m.Price, Items: map[string]int64{m.Item: m.Qty}}
		res.Notify = m.Seller
		res.Log, err = op.log(m.Seller, buyer, "market_sold", m.Item, m.Price)
		return err
	})
	if err == nil {
		res.Market = true
	}
	return res, err
}

// FarmMarketCancel: người bán gỡ lô của mình, hàng về kho (cần đủ chỗ).
func (s *Store) FarmMarketCancel(ctx context.Context, sellerID, listingID int64) (FarmResult, error) {
	res := FarmResult{OwnerID: sellerID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		if err := op.expireMarket(); err != nil {
			return err
		}
		m, err := op.loadListing(listingID)
		if err != nil {
			return err
		}
		if m.Seller != sellerID {
			return ErrMarketNotOwner
		}
		if room, err := op.storageRoom(sellerID); err != nil {
			return err
		} else if room < m.Qty {
			return ErrFarmStorageFull
		}
		if _, err := op.tx.ExecContext(ctx, `DELETE FROM farm_market WHERE id = ?`, m.ID); err != nil {
			return err
		}
		res.Gained = &Gained{Items: map[string]int64{m.Item: m.Qty}}
		return op.addStock(sellerID, m.Item, m.Qty)
	})
	if err == nil {
		res.Market = true
	}
	return res, err
}

// marketView: chợ đen như viewerID thấy (lô quá hạn mà chưa dọn thì ẩn).
func (s *Store) marketView(ctx context.Context, viewerID int64) (*MarketView, error) {
	rows, err := loadMarket(ctx, s.db)
	if err != nil {
		return nil, err
	}
	v := &MarketView{Slots: MarketSlots, PerUser: MarketPerUser, MaxQty: MarketMaxQty, MaxPrice: MarketMaxPrice,
		Listings: []MarketListing{}}
	now := s.now().Unix()
	for _, m := range rows {
		if m.Expires <= now {
			continue
		}
		v.Used++
		seller, err := farmUser(ctx, s.db, m.Seller)
		if errors.Is(err, ErrNotFound) {
			continue
		} else if err != nil {
			return nil, err
		}
		v.Listings = append(v.Listings, MarketListing{ID: m.ID, Slot: m.Slot, Seller: seller, Item: m.Item,
			Qty: m.Qty, Price: m.Price, ExpiresAt: rfc(m.Expires), Mine: m.Seller == viewerID})
	}
	return v, nil
}
