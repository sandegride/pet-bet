DELETE FROM admin_settings WHERE key IN ('cs_default_odds', 'cs_kills_over_odds');

DROP INDEX IF EXISTS idx_user_cs_match_snapshots_cs_match_id;
DROP INDEX IF EXISTS idx_user_cs_match_snapshots_user_id;
DROP INDEX IF EXISTS idx_cs_bets_one_active_per_user;
DROP INDEX IF EXISTS idx_cs_bets_target_match_id;
DROP INDEX IF EXISTS idx_cs_bets_status;
DROP INDEX IF EXISTS idx_cs_bets_user_id;
DROP INDEX IF EXISTS idx_users_cs_faceit_player_id;

DROP TABLE IF EXISTS cs_bets;
DROP TABLE IF EXISTS user_cs_match_snapshots;

ALTER TABLE users
    DROP COLUMN IF EXISTS is_cs_linked,
    DROP COLUMN IF EXISTS cs_last_known_match_started_at,
    DROP COLUMN IF EXISTS cs_last_known_match_id,
    DROP COLUMN IF EXISTS cs_nickname,
    DROP COLUMN IF EXISTS cs_faceit_player_id;
