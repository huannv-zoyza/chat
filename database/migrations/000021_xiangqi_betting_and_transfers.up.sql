-- Bổ sung trường giao dịch P2P cho wallet_tx và bảng escrow game_bets cho Cờ tướng.
ALTER TABLE wallet_tx ADD COLUMN peer_user_id INTEGER REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE wallet_tx ADD COLUMN note TEXT NOT NULL DEFAULT '';

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
