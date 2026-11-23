-- Ví xu ảo của mỗi người (dùng chung cả server) và sổ giao dịch.
-- wallet_tx có khoá duy nhất theo (ván, người, loại, seq) để việc trả chip của một ván chỉ cộng ví đúng một lần.
CREATE TABLE IF NOT EXISTS wallets (
    user_id    INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    balance    INTEGER  NOT NULL DEFAULT 1000 CHECK (balance >= 0),
    debt       INTEGER  NOT NULL DEFAULT 0 CHECK (debt >= 0),
    last_claim TEXT,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS wallet_tx (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER  NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       TEXT     NOT NULL,
    amount     INTEGER  NOT NULL,
    debt_delta INTEGER  NOT NULL DEFAULT 0,
    game_id    INTEGER,
    seq        INTEGER  NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_wallet_tx_once ON wallet_tx (game_id, user_id, kind, seq) WHERE game_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_wallet_tx_user ON wallet_tx (user_id, id);
