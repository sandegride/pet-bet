DROP INDEX IF EXISTS idx_user_match_snapshots_dota_match_id;
DROP INDEX IF EXISTS idx_user_match_snapshots_user_id;
DROP INDEX IF EXISTS idx_self_bets_one_active_per_user;
DROP INDEX IF EXISTS idx_self_bets_target_match_id;
DROP INDEX IF EXISTS idx_self_bets_status;
DROP INDEX IF EXISTS idx_self_bets_user_id;
DROP INDEX IF EXISTS idx_users_last_known_match_id;
DROP INDEX IF EXISTS idx_users_dota_account_id;

DROP TABLE IF EXISTS self_bets;
DROP TABLE IF EXISTS user_match_snapshots;

ALTER TABLE users
    DROP COLUMN IF EXISTS is_dota_linked,
    DROP COLUMN IF EXISTS last_known_match_started_at,
    DROP COLUMN IF EXISTS last_known_match_id,
    DROP COLUMN IF EXISTS dota_account_id,
    DROP COLUMN IF EXISTS steam_id,
    DROP COLUMN IF EXISTS frozen_balance;
