-- Ví Xu của từng thành viên
CREATE TABLE IF NOT EXISTS wallets (
    user_id         INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    balance         INTEGER NOT NULL DEFAULT 10000 CHECK (balance >= 0),
    locked_balance  INTEGER NOT NULL DEFAULT 0 CHECK (locked_balance >= 0),
    last_checkin_at DATETIME,
    bailout_date    TEXT    NOT NULL DEFAULT '',
    bailout_count   INTEGER NOT NULL DEFAULT 0,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Sổ cái giao dịch (Append-Only Ledger)
-- Bất biến: wallets.balance = SUM(amount) của user_id trong bảng này
CREATE TABLE IF NOT EXISTS wallet_transactions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id         INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    type            TEXT    NOT NULL,
    amount          INTEGER NOT NULL,
    balance_after   INTEGER NOT NULL,
    ref_type        TEXT    NOT NULL DEFAULT '',
    ref_id          INTEGER,
    peer_user_id    INTEGER REFERENCES users (id) ON DELETE SET NULL,
    note            TEXT    NOT NULL DEFAULT '',
    idempotency_key TEXT    UNIQUE,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_wallet_tx_user_created ON wallet_transactions (user_id, id DESC);

-- Trạng thái tiền cược tạm giữ (Escrow) cho từng ván chơi
CREATE TABLE IF NOT EXISTS game_bets (
    message_id  INTEGER NOT NULL REFERENCES games (message_id) ON DELETE CASCADE,
    user_id     INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    amount      INTEGER NOT NULL CHECK (amount > 0),
    status      TEXT    NOT NULL DEFAULT 'locked', -- locked | won | lost | refunded
    payout      INTEGER NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    settled_at  DATETIME,
    PRIMARY KEY (message_id, user_id)
);

-- Backfill ví 10,000 Xu và bút toán khởi tạo cho các user đã tồn tại trước migration
INSERT OR IGNORE INTO wallets (user_id, balance, locked_balance)
SELECT id, 10000, 0 FROM users;

INSERT OR IGNORE INTO wallet_transactions (user_id, type, amount, balance_after, ref_type, note, idempotency_key)
SELECT id, 'signup_bonus', 10000, 10000, 'system', 'Vốn khởi tạo tài khoản', 'signup_bonus:' || id
FROM users;
