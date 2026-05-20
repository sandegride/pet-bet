package selfbets

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"stavki/internal/domain"
	"stavki/internal/dota"
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

func (r *Repository) LinkDotaAccount(
	ctx context.Context,
	tx pgx.Tx,
	userID int64,
	accountID int64,
	lastMatch *dota.RecentMatch,
) error {
	var lastMatchID sql.NullInt64
	var lastStartedAt sql.NullTime
	if lastMatch != nil {
		lastMatchID = sql.NullInt64{Int64: lastMatch.MatchID, Valid: true}
		lastStartedAt = sql.NullTime{Time: lastMatch.StartedAt, Valid: true}
	}

	tag, err := tx.Exec(
		ctx,
		`UPDATE users
		 SET dota_account_id = $2,
		     is_dota_linked = TRUE,
		     last_known_match_id = $3,
		     last_known_match_started_at = $4,
		     updated_at = now()
		 WHERE id = $1`,
		userID,
		accountID,
		lastMatchID,
		lastStartedAt,
	)
	if err != nil {
		return fmt.Errorf("link dota account: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

func (r *Repository) UpdateLastKnownMatch(ctx context.Context, tx pgx.Tx, userID int64, match dota.RecentMatch) error {
	tag, err := tx.Exec(
		ctx,
		`UPDATE users
		 SET last_known_match_id = $2,
		     last_known_match_started_at = $3,
		     updated_at = now()
		 WHERE id = $1`,
		userID,
		match.MatchID,
		match.StartedAt,
	)
	if err != nil {
		return fmt.Errorf("update last known match: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

func (r *Repository) GetActiveBetForUpdate(ctx context.Context, tx pgx.Tx, userID int64) (domain.SelfBet, error) {
	return scanSelfBet(tx.QueryRow(
		ctx,
		selfBetSelectSQL(`user_id = $1 AND status = $2 FOR UPDATE`),
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
) (domain.SelfBet, error) {
	return scanSelfBet(tx.QueryRow(
		ctx,
		`INSERT INTO self_bets (user_id, amount, frozen_amount, odds, potential_payout, prediction, status)
		 VALUES ($1, $2, $2, $3, $4, $5, $6)
		 RETURNING id, user_id, amount, frozen_amount, odds::text, potential_payout, prediction, status,
		           target_match_id, COALESCE(resolved_result, ''), created_at, settled_at`,
		userID,
		amount,
		odds,
		potentialPayout,
		string(domain.SelfBetPredictionWin),
		string(domain.SelfBetStatusActive),
	))
}

func (r *Repository) MarkSettled(
	ctx context.Context,
	tx pgx.Tx,
	betID int64,
	status domain.SelfBetStatus,
	matchID int64,
	result string,
) error {
	tag, err := tx.Exec(
		ctx,
		`UPDATE self_bets
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
		return fmt.Errorf("settle self bet: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoActiveBet
	}

	return nil
}

func (r *Repository) CancelActiveBet(ctx context.Context, tx pgx.Tx, betID int64) error {
	tag, err := tx.Exec(
		ctx,
		`UPDATE self_bets
		 SET status = $2, settled_at = now()
		 WHERE id = $1 AND status = $3 AND target_match_id IS NULL`,
		betID,
		string(domain.SelfBetStatusCancelled),
		string(domain.SelfBetStatusActive),
	)
	if err != nil {
		return fmt.Errorf("cancel self bet: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrBetAlreadyTargeted
	}

	return nil
}

func (r *Repository) SaveSnapshot(ctx context.Context, tx pgx.Tx, userID int64, match dota.RecentMatch) error {
	raw, err := json.Marshal(match)
	if err != nil {
		return fmt.Errorf("marshal match snapshot: %w", err)
	}

	result := dota.ResolvePlayerResult(match.PlayerSlot, match.RadiantWin)
	_, err = tx.Exec(
		ctx,
		`INSERT INTO user_match_snapshots
		    (user_id, dota_match_id, started_at, result, hero_id, player_slot, radiant_win, raw)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (user_id, dota_match_id) DO NOTHING`,
		userID,
		match.MatchID,
		match.StartedAt,
		result,
		match.HeroID,
		match.PlayerSlot,
		match.RadiantWin,
		string(raw),
	)
	if err != nil {
		return fmt.Errorf("save match snapshot: %w", err)
	}

	return nil
}

func (r *Repository) GetActiveBetByTelegramID(ctx context.Context, telegramID int64) (domain.SelfBet, error) {
	return scanSelfBet(r.db.QueryRow(
		ctx,
		`SELECT b.id, b.user_id, b.amount, b.frozen_amount, b.odds::text, b.potential_payout,
		        b.prediction, b.status, b.target_match_id, COALESCE(b.resolved_result, ''),
		        b.created_at, b.settled_at
		 FROM self_bets b
		 JOIN users u ON u.id = b.user_id
		 WHERE u.telegram_id = $1 AND b.status = $2`,
		telegramID,
		string(domain.SelfBetStatusActive),
	))
}

func (r *Repository) GetHistory(ctx context.Context, telegramID int64, limit int) ([]domain.SelfBetHistoryItem, error) {
	rows, err := r.db.Query(
		ctx,
		`SELECT b.id, b.user_id, b.amount, b.frozen_amount, b.odds::text, b.potential_payout,
		        b.prediction, b.status, b.target_match_id, COALESCE(b.resolved_result, ''),
		        b.created_at, b.settled_at, u.dota_account_id
		 FROM self_bets b
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

	items := make([]domain.SelfBetHistoryItem, 0, limit)
	for rows.Next() {
		item, err := scanSelfBetHistory(rows)
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
		userSelectSQL(`is_dota_linked = TRUE AND dota_account_id IS NOT NULL ORDER BY id LIMIT $1`),
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
		        created_at, updated_at
		 FROM users
		 WHERE %s`,
		where,
	)
}

func selfBetSelectSQL(where string) string {
	return fmt.Sprintf(
		`SELECT id, user_id, amount, frozen_amount, odds::text, potential_payout,
		        prediction, status, target_match_id, COALESCE(resolved_result, ''),
		        created_at, settled_at
		 FROM self_bets
		 WHERE %s`,
		where,
	)
}

func scanUser(row pgx.Row) (domain.User, error) {
	var user domain.User
	var dotaAccountID sql.NullInt64
	var lastKnownMatchID sql.NullInt64
	var lastKnownMatchStartedAt sql.NullTime
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

	return user, nil
}

func scanSelfBet(row pgx.Row) (domain.SelfBet, error) {
	var bet domain.SelfBet
	var prediction string
	var status string
	var targetMatchID sql.NullInt64
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
		&bet.CreatedAt,
		&settledAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return domain.SelfBet{}, ErrNoActiveBet
		}
		return domain.SelfBet{}, err
	}

	bet.Prediction = domain.SelfBetPrediction(prediction)
	bet.Status = domain.SelfBetStatus(status)
	if targetMatchID.Valid {
		bet.TargetMatchID = &targetMatchID.Int64
	}
	if settledAt.Valid {
		bet.SettledAt = &settledAt.Time
	}

	return bet, nil
}

func scanSelfBetHistory(row pgx.Row) (domain.SelfBetHistoryItem, error) {
	var item domain.SelfBetHistoryItem
	var prediction string
	var status string
	var targetMatchID sql.NullInt64
	var settledAt sql.NullTime
	var dotaAccountID sql.NullInt64
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
		&item.CreatedAt,
		&settledAt,
		&dotaAccountID,
	)
	if err != nil {
		return domain.SelfBetHistoryItem{}, err
	}

	item.Prediction = domain.SelfBetPrediction(prediction)
	item.Status = domain.SelfBetStatus(status)
	if targetMatchID.Valid {
		item.TargetMatchID = &targetMatchID.Int64
	}
	if settledAt.Valid {
		item.SettledAt = &settledAt.Time
	}
	if dotaAccountID.Valid {
		item.DotaAccountID = &dotaAccountID.Int64
	}

	return item, nil
}
