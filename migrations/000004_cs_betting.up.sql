-- CS2 (FACEIT) self-match betting, mirrors self_bets/dota linking for Dota 2.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS cs_faceit_player_id TEXT,
    ADD COLUMN IF NOT EXISTS cs_nickname TEXT,
    ADD COLUMN IF NOT EXISTS cs_last_known_match_id TEXT,
    ADD COLUMN IF NOT EXISTS cs_last_known_match_started_at TIMESTAMP,
    ADD COLUMN IF NOT EXISTS is_cs_linked BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS user_cs_match_snapshots (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    cs_match_id TEXT NOT NULL,
    started_at TIMESTAMP,
    result TEXT NOT NULL,
    map_name TEXT,
    kills INT,
    deaths INT,
    assists INT,
    raw JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    UNIQUE(user_id, cs_match_id)
);

CREATE TABLE IF NOT EXISTS cs_bets (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    amount BIGINT NOT NULL,
    frozen_amount BIGINT NOT NULL,
    odds NUMERIC(10,2) NOT NULL DEFAULT 2.00,
    potential_payout BIGINT NOT NULL,
    prediction TEXT NOT NULL DEFAULT 'win',
    status TEXT NOT NULL DEFAULT 'active',
    target_match_id TEXT,
    resolved_result TEXT,
    kills_threshold INT,
    created_at TIMESTAMP NOT NULL DEFAULT now(),
    settled_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_users_cs_faceit_player_id ON users(cs_faceit_player_id);
CREATE INDEX IF NOT EXISTS idx_cs_bets_user_id ON cs_bets(user_id);
CREATE INDEX IF NOT EXISTS idx_cs_bets_status ON cs_bets(status);
CREATE INDEX IF NOT EXISTS idx_cs_bets_target_match_id ON cs_bets(target_match_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_cs_bets_one_active_per_user ON cs_bets(user_id) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_user_cs_match_snapshots_user_id ON user_cs_match_snapshots(user_id);
CREATE INDEX IF NOT EXISTS idx_user_cs_match_snapshots_cs_match_id ON user_cs_match_snapshots(cs_match_id);

INSERT INTO admin_settings (key, value) VALUES
    ('cs_default_odds',    '2.00'),
    ('cs_kills_over_odds', '1.90')
ON CONFLICT (key) DO NOTHING;
