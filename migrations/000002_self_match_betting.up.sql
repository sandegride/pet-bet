ALTER TABLE users
    ADD COLUMN IF NOT EXISTS frozen_balance BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS steam_id TEXT,
    ADD COLUMN IF NOT EXISTS dota_account_id BIGINT,
    ADD COLUMN IF NOT EXISTS last_known_match_id BIGINT,
    ADD COLUMN IF NOT EXISTS last_known_match_started_at TIMESTAMP,
    ADD COLUMN IF NOT EXISTS is_dota_linked BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS user_match_snapshots (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    dota_match_id BIGINT NOT NULL,
    started_at TIMESTAMP,
    result TEXT NOT NULL,
    hero_id BIGINT,
    player_slot INT,
    radiant_win BOOLEAN,
    raw JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    UNIQUE(user_id, dota_match_id)
);

CREATE TABLE IF NOT EXISTS self_bets (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    amount BIGINT NOT NULL,
    frozen_amount BIGINT NOT NULL,
    odds NUMERIC(10,2) NOT NULL DEFAULT 2.00,
    potential_payout BIGINT NOT NULL,
    prediction TEXT NOT NULL DEFAULT 'win',
    status TEXT NOT NULL DEFAULT 'active',
    target_match_id BIGINT,
    resolved_result TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    settled_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_users_dota_account_id ON users(dota_account_id);
CREATE INDEX IF NOT EXISTS idx_users_last_known_match_id ON users(last_known_match_id);
CREATE INDEX IF NOT EXISTS idx_self_bets_user_id ON self_bets(user_id);
CREATE INDEX IF NOT EXISTS idx_self_bets_status ON self_bets(status);
CREATE INDEX IF NOT EXISTS idx_self_bets_target_match_id ON self_bets(target_match_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_self_bets_one_active_per_user ON self_bets(user_id) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_user_match_snapshots_user_id ON user_match_snapshots(user_id);
CREATE INDEX IF NOT EXISTS idx_user_match_snapshots_dota_match_id ON user_match_snapshots(dota_match_id);
