-- Chợ đen (specs/FARM_SPEC.md §15): hàng người chơi treo bán, dùng chung toàn server.
-- slot 0..9 duy nhất: cả server chỉ có 10 chỗ, đầy thì không ai treo thêm được.
-- Hàng đã lấy khỏi kho người bán lúc treo; qty đơn vị item, price là giá cả lô (xu).
-- expires_at: giây unix hết hạn, quá hạn thì hàng tự về kho người bán.
CREATE TABLE IF NOT EXISTS farm_market (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    slot       INTEGER NOT NULL UNIQUE CHECK (slot BETWEEN 0 AND 9),
    seller_id  INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    item       TEXT    NOT NULL,
    qty        INTEGER NOT NULL CHECK (qty > 0),
    price      INTEGER NOT NULL CHECK (price > 0),
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_farm_market_seller ON farm_market (seller_id);
