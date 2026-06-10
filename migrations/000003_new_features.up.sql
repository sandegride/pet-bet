-- Add kills threshold for total_kills_over bets
ALTER TABLE self_bets ADD COLUMN IF NOT EXISTS kills_threshold INT;

-- Admin settings key-value store
CREATE TABLE IF NOT EXISTS admin_settings (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  TIMESTAMP NOT NULL DEFAULT now()
);

INSERT INTO admin_settings (key, value) VALUES
    ('default_odds',    '2.00'),
    ('kills_over_odds', '1.90'),
    ('first_blood_odds','1.85'),
    ('solo_only_bets',  'false'),
    ('min_avg_mmr',     '0'),
    ('hwid_required',   'false')
ON CONFLICT (key) DO NOTHING;

-- Extended match snapshot fields
ALTER TABLE user_match_snapshots
    ADD COLUMN IF NOT EXISTS kills             INT,
    ADD COLUMN IF NOT EXISTS deaths            INT,
    ADD COLUMN IF NOT EXISTS assists           INT,
    ADD COLUMN IF NOT EXISTS party_size        INT,
    ADD COLUMN IF NOT EXISTS total_kills       INT,
    ADD COLUMN IF NOT EXISTS first_blood_time  INT,
    ADD COLUMN IF NOT EXISTS first_blood_slot  INT,
    ADD COLUMN IF NOT EXISTS avg_mmr           INT;

-- Hardware ID binding
ALTER TABLE users ADD COLUMN IF NOT EXISTS hwid TEXT;
