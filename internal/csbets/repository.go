package csbets

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"stavki/internal/cs"
	"stavki/internal/domain"
)

type DB interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Repository struct {
	db DB
}

func NewRepository(db DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetUserByTelegramIDForUpdate(ctx context.Context, tx pgx.Tx, telegramID int64) (domain.User, error) {
	return scanUser(tx.QueryRow(ctx, userSelectSQL(`telegram_id = $1 FOR UPDATE`), telegramID))
}

func (r *Repository) GetUserByIDForUpdate(ctx context.Context, tx pgx.Tx, userID int64) (domain.User, error) {
	return scanUser(tx.QueryRow(ctx, userSelectSQL(`id = $1 FOR UPDATE`), userID))
}

func (r *Repository) LinkCSAccount(
	ctx context.Context,
	tx pgx.Tx,
	userID int64,
	player cs.Player,
	lastMatch *cs.RecentMatch,
) error {
	var lastMatchID sql.NullString
	var lastStartedAt sql.NullTime
	if lastMatch != nil {
		lastMatchID = sql.NullString{String: lastMatch.MatchID, Valid: true}
		lastStartedAt = sql.NullTime{Time: lastMatch.StartedAt, Valid: true}
	}

	tag, err := tx.Exec(
		ctx,
		`UPDATE users
		 SET cs_faceit_player_id = $2,
		     cs_nickname = $3,
		     is_cs_linked = TRUE,
		     cs_last_known_match_id = $4,
		     cs_last_known_match_started_at = $5,
		     updated_at = now()
		 WHERE id = $1`,
		userID,
		player.PlayerID,
		player.Nickname,
		lastMatchID,
		lastStartedAt,
	)
	if err != nil {
		return fmt.Errorf("link cs account: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

func (r *Repository) UpdateLastKnownMatch(ctx context.Context, tx pgx.Tx, userID int64, match cs.RecentMatch) error {
	tag, err := tx.Exec(
		ctx,
		`UPDATE users
		 SET cs_last_known_match_id = $2,
		     cs_last_known_match_started_at = $3,
		     updated_at = now()
		 WHERE id = $1`,
		userID,
		match.MatchID,
		match.StartedAt,
	)
	if err != nil {
		return fmt.Errorf("update last known cs match: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

func (r *Repository) GetActiveBetForUpdate(ctx context.Context, tx pgx.Tx, userID int64) (domain.CSBet, error) {
	return scanCSBet(tx.QueryRow(
		ctx,
		csBetSelectSQL(`user_id = $1 AND status = $2 FOR UPDATE`),
		userID,
		string(domain.SelfBetStatusActive),
	))
}

func (r *Repository) HasActiveBet(ctx context.Context, tx pgx.Tx, userID int64) (bool, error) {
	_, err := r.GetActiveBetForUpdate(ctx, tx, userID)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrNoActiveBet) {
		return false, nil
	}

	return false, err
}

func (r *Repository) CreateActiveBet(
	ctx context.Context,
	tx pgx.Tx,
	userID int64,
	amount int64,
	odds string,
	potentialPayout int64,
	prediction domain.SelfBetPrediction,
	killsThreshold *int64,
) (domain.CSBet, error) {
	return scanCSBet(tx.QueryRow(
		ctx,
		`INSERT INTO cs_bets
		    (user_id, amount, frozen_amount, odds, potential_payout, prediction, status, kills_threshold)
		 VALUES ($1, $2, $2, $3, $4, $5, $6, $7)
		 RETURNING id, user_id, amount, frozen_amount, odds::text, potential_payout, prediction, status,
		           target_match_id, COALESCE(resolved_result, ''), kills_threshold, created_at, settled_at`,
		userID,
		amount,
		odds,
		potentialPayout,
		string(prediction),
		string(domain.SelfBetStatusActive),
		killsThreshold,
	))
}

func (r *Repository) MarkSettled(
	ctx context.Context,
	tx pgx.Tx,
	betID int64,
	status domain.SelfBetStatus,
	matchID string,
	result string,
) error {
	tag, err := tx.Exec(
		ctx,
		`UPDATE cs_bets
		 SET status = $2,
		     target_match_id = $3,
		     resolved_result = $4,
		     settled_at = now()
		 WHERE id = $1 AND status = $5`,
		betID,
		string(status),
		matchID,
		result,
		string(domain.SelfBetStatusActive),
	)
	if err != nil {
		return fmt.Errorf("settle cs bet: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoActiveBet
	}

	return nil
}

func (r *Repository) CancelActiveBet(ctx context.Context, tx pgx.Tx, betID int64) error {
	tag, err := tx.Exec(
		ctx,
		`UPDATE cs_bets
		 SET status = $2, settled_at = now()
		 WHERE id = $1 AND status = $3 AND target_match_id IS NULL`,
		betID,
		string(domain.SelfBetStatusCancelled),
		string(domain.SelfBetStatusActive),
	)
	if err != nil {
		return fmt.Errorf("cancel cs bet: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrBetAlreadyTargeted
	}

	return nil
}

func (r *Repository) SaveSnapshot(ctx context.Context, tx pgx.Tx, userID int64, match cs.RecentMatch) error {
	raw, err := json.Marshal(match)
	if err != nil {
		return fmt.Errorf("marshal cs match snapshot: %w", err)
	}

	result := "loss"
	if match.Won {
		result = "win"
	}

	_, err = tx.Exec(
		ctx,
		`INSERT INTO user_cs_match_snapshots
		    (user_id, cs_match_id, started_at, result, map_name, kills, deaths, assists, raw)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (user_id, cs_match_id) DO NOTHING`,
		userID,
		match.MatchID,
		match.StartedAt,
		result,
		match.Map,
		match.Kills,
		match.Deaths,
		match.Assists,
		string(raw),
	)
	if err != nil {
		return fmt.Errorf("save cs match snapshot: %w", err)
	}

	return nil
}

func (r *Repository) GetActiveBetByTelegramID(ctx context.Context, telegramID int64) (domain.CSBet, error) {
	return scanCSBet(r.db.QueryRow(
		ctx,
		`SELECT b.id, b.user_id, b.amount, b.frozen_amount, b.odds::text, b.potential_payout,
		        b.prediction, b.status, b.target_match_id, COALESCE(b.resolved_result, ''),
		        b.kills_threshold, b.created_at, b.settled_at
		 FROM cs_bets b
		 JOIN users u ON u.id = b.user_id
		 WHERE u.telegram_id = $1 AND b.status = $2`,
		telegramID,
		string(domain.SelfBetStatusActive),
	))
}

func (r *Repository) GetHistory(ctx context.Context, telegramID int64, limit int) ([]domain.CSBetHistoryItem, error) {
	rows, err := r.db.Query(
		ctx,
		`SELECT b.id, b.user_id, b.amount, b.frozen_amount, b.odds::text, b.potential_payout,
		        b.prediction, b.status, b.target_match_id, COALESCE(b.resolved_result, ''),
		        b.kills_threshold, b.created_at, b.settled_at, u.cs_faceit_player_id
		 FROM cs_bets b
		 JOIN users u ON u.id = b.user_id
		 WHERE u.telegram_id = $1
		 ORDER BY b.created_at DESC, b.id DESC
		 LIMIT $2`,
		telegramID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domain.CSBetHistoryItem, 0, limit)
	for rows.Next() {
		item, err := scanCSBetHistory(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (r *Repository) GetLinkedUsers(ctx context.Context, limit int) ([]domain.User, error) {
	rows, err := r.db.Query(
		ctx,
		userSelectSQL(`is_cs_linked = TRUE AND cs_faceit_player_id IS NOT NULL ORDER BY id LIMIT $1`),
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]domain.User, 0, limit)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

func userSelectSQL(where string) string {
	return fmt.Sprintf(
		`SELECT id, telegram_id, COALESCE(username, ''), COALESCE(first_name, ''),
		        balance, frozen_balance, is_admin, is_blocked, COALESCE(steam_id, ''),
		        dota_account_id, last_known_match_id, last_known_match_started_at, is_dota_linked,
		        COALESCE(hwid, ''), cs_faceit_player_id, COALESCE(cs_nickname, ''),
		        cs_last_known_match_id, cs_last_known_match_started_at, is_cs_linked,
		        created_at, updated_at
		 FROM users
		 WHERE %s`,
		where,
	)
}

func csBetSelectSQL(where string) string {
	return fmt.Sprintf(
		`SELECT id, user_id, amount, frozen_amount, odds::text, potential_payout,
		        prediction, status, target_match_id, COALESCE(resolved_result, ''),
		        kills_threshold, created_at, settled_at
		 FROM cs_bets
		 WHERE %s`,
		where,
	)
}

func scanUser(row pgx.Row) (domain.User, error) {
	var user domain.User
	var dotaAccountID sql.NullInt64
	var lastKnownMatchID sql.NullInt64
	var lastKnownMatchStartedAt sql.NullTime
	var csFaceitPlayerID sql.NullString
	var csLastKnownMatchID sql.NullString
	var csLastKnownMatchStartedAt sql.NullTime
	err := row.Scan(
		&user.ID,
		&user.TelegramID,
		&user.Username,
		&user.FirstName,
		&user.Balance,
		&user.FrozenBalance,
		&user.IsAdmin,
		&user.IsBlocked,
		&user.SteamID,
		&dotaAccountID,
		&lastKnownMatchID,
		&lastKnownMatchStartedAt,
		&user.IsDotaLinked,
		&user.HWID,
		&csFaceitPlayerID,
		&user.CSNickname,
		&csLastKnownMatchID,
		&csLastKnownMatchStartedAt,
		&user.IsCSLinked,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return domain.User{}, ErrUserNotFound
		}
		return domain.User{}, err
	}

	if dotaAccountID.Valid {
		user.DotaAccountID = &dotaAccountID.Int64
	}
	if lastKnownMatchID.Valid {
		user.LastKnownMatchID = &lastKnownMatchID.Int64
	}
	if lastKnownMatchStartedAt.Valid {
		user.LastKnownMatchStartedAt = &lastKnownMatchStartedAt.Time
	}
	if csFaceitPlayerID.Valid {
		user.CSFaceitPlayerID = &csFaceitPlayerID.String
	}
	if csLastKnownMatchID.Valid {
		user.CSLastKnownMatchID = &csLastKnownMatchID.String
	}
	if csLastKnownMatchStartedAt.Valid {
		user.CSLastKnownMatchStartedAt = &csLastKnownMatchStartedAt.Time
	}

	return user, nil
}

func scanCSBet(row pgx.Row) (domain.CSBet, error) {
	var bet domain.CSBet
	var prediction string
	var status string
	var targetMatchID sql.NullString
	var killsThreshold sql.NullInt64
	var settledAt sql.NullTime
	err := row.Scan(
		&bet.ID,
		&bet.UserID,
		&bet.Amount,
		&bet.FrozenAmount,
		&bet.Odds,
		&bet.PotentialPayout,
		&prediction,
		&status,
		&targetMatchID,
		&bet.ResolvedResult,
		&killsThreshold,
		&bet.CreatedAt,
		&settledAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return domain.CSBet{}, ErrNoActiveBet
		}
		return domain.CSBet{}, err
	}

	bet.Prediction = domain.SelfBetPrediction(prediction)
	bet.Status = domain.SelfBetStatus(status)
	if targetMatchID.Valid {
		bet.TargetMatchID = &targetMatchID.String
	}
	if killsThreshold.Valid {
		bet.KillsThreshold = &killsThreshold.Int64
	}
	if settledAt.Valid {
		bet.SettledAt = &settledAt.Time
	}

	return bet, nil
}

func scanCSBetHistory(row pgx.Row) (domain.CSBetHistoryItem, error) {
	var item domain.CSBetHistoryItem
	var prediction string
	var status string
	var targetMatchID sql.NullString
	var killsThreshold sql.NullInt64
	var settledAt sql.NullTime
	var csFaceitPlayerID sql.NullString
	err := row.Scan(
		&item.ID,
		&item.UserID,
		&item.Amount,
		&item.FrozenAmount,
		&item.Odds,
		&item.PotentialPayout,
		&prediction,
		&status,
		&targetMatchID,
		&item.ResolvedResult,
		&killsThreshold,
		&item.CreatedAt,
		&settledAt,
		&csFaceitPlayerID,
	)
	if err != nil {
		return domain.CSBetHistoryItem{}, err
	}

	item.Prediction = domain.SelfBetPrediction(prediction)
	item.Status = domain.SelfBetStatus(status)
	if targetMatchID.Valid {
		item.TargetMatchID = &targetMatchID.String
	}
	if killsThreshold.Valid {
		item.KillsThreshold = &killsThreshold.Int64
	}
	if settledAt.Valid {
		item.SettledAt = &settledAt.Time
	}
	if csFaceitPlayerID.Valid {
		item.CSFaceitPlayerID = &csFaceitPlayerID.String
	}

	return item, nil
}
