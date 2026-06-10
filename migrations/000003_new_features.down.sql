ALTER TABLE self_bets DROP COLUMN IF EXISTS kills_threshold;

DROP TABLE IF EXISTS admin_settings;

ALTER TABLE user_match_snapshots
    DROP COLUMN IF EXISTS kills,
    DROP COLUMN IF EXISTS deaths,
    DROP COLUMN IF EXISTS assists,
    DROP COLUMN IF EXISTS party_size,
    DROP COLUMN IF EXISTS total_kills,
    DROP COLUMN IF EXISTS first_blood_time,
    DROP COLUMN IF EXISTS first_blood_slot,
    DROP COLUMN IF EXISTS avg_mmr;

ALTER TABLE users DROP COLUMN IF EXISTS hwid;
