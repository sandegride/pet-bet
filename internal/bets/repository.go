package bets

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"stavki/internal/domain"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(
	ctx context.Context,
	tx pgx.Tx,
	userID int64,
	matchID int64,
	selectedTeam string,
	amount int64,
	odds string,
	potentialPayout int64,
) (domain.Bet, error) {
	return scanBet(tx.QueryRow(
		ctx,
		`INSERT INTO bets (user_id, match_id, selected_team, amount, odds, potential_payout)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, user_id, match_id, selected_team, amount, odds::text, potential_payout, status, created_at, settled_at`,
		userID,
		matchID,
		selectedTeam,
		amount,
		odds,
		potentialPayout,
	))
}

func (r *Repository) GetUserHistory(ctx context.Context, telegramID int64, limit int) ([]domain.BetHistoryItem, error) {
	rows, err := r.db.Query(
		ctx,
		`SELECT b.id, b.user_id, b.match_id, b.selected_team, b.amount, b.odds::text,
		        b.potential_payout, b.status, b.created_at, b.settled_at,
		        COALESCE(m.tournament_name, ''), m.team_a, m.team_b, m.starts_at
		 FROM bets b
		 JOIN users u ON u.id = b.user_id
		 JOIN matches m ON m.id = b.match_id
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

	history := make([]domain.BetHistoryItem, 0, limit)
	for rows.Next() {
		item, err := scanBetHistory(rows)
		if err != nil {
			return nil, err
		}
		history = append(history, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return history, nil
}

func (r *Repository) GetPendingByMatchForUpdate(ctx context.Context, tx pgx.Tx, matchID int64) ([]domain.Bet, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT id, user_id, match_id, selected_team, amount, odds::text, potential_payout, status, created_at, settled_at
		 FROM bets
		 WHERE match_id = $1 AND status = $2
		 ORDER BY id
		 FOR UPDATE`,
		matchID,
		string(domain.BetStatusPending),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]domain.Bet, 0)
	for rows.Next() {
		bet, err := scanBet(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, bet)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

func (r *Repository) MarkWon(ctx context.Context, tx pgx.Tx, betID int64) error {
	return r.mark(ctx, tx, betID, domain.BetStatusWon)
}

func (r *Repository) MarkLost(ctx context.Context, tx pgx.Tx, betID int64) error {
	return r.mark(ctx, tx, betID, domain.BetStatusLost)
}

func (r *Repository) MarkVoid(ctx context.Context, tx pgx.Tx, betID int64) error {
	return r.mark(ctx, tx, betID, domain.BetStatusVoid)
}

func (r *Repository) mark(ctx context.Context, tx pgx.Tx, betID int64, status domain.BetStatus) error {
	tag, err := tx.Exec(
		ctx,
		`UPDATE bets
		 SET status = $2, settled_at = now()
		 WHERE id = $1 AND status = $3`,
		betID,
		string(status),
		string(domain.BetStatusPending),
	)
	if err != nil {
		return fmt.Errorf("mark bet %s: %w", status, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrBetNotPending
	}

	return nil
}

func scanBet(row pgx.Row) (domain.Bet, error) {
	var bet domain.Bet
	var status string
	var settledAt sql.NullTime
	err := row.Scan(
		&bet.ID,
		&bet.UserID,
		&bet.MatchID,
		&bet.SelectedTeam,
		&bet.Amount,
		&bet.Odds,
		&bet.PotentialPayout,
		&status,
		&bet.CreatedAt,
		&settledAt,
	)
	if err != nil {
		return domain.Bet{}, err
	}

	bet.Status = domain.BetStatus(status)
	if settledAt.Valid {
		bet.SettledAt = &settledAt.Time
	}

	return bet, nil
}

func scanBetHistory(row pgx.Row) (domain.BetHistoryItem, error) {
	var item domain.BetHistoryItem
	var status string
	var settledAt sql.NullTime
	err := row.Scan(
		&item.ID,
		&item.UserID,
		&item.MatchID,
		&item.SelectedTeam,
		&item.Amount,
		&item.Odds,
		&item.PotentialPayout,
		&status,
		&item.CreatedAt,
		&settledAt,
		&item.TournamentName,
		&item.TeamA,
		&item.TeamB,
		&item.StartsAt,
	)
	if err != nil {
		return domain.BetHistoryItem{}, err
	}

	item.Status = domain.BetStatus(status)
	if settledAt.Valid {
		item.SettledAt = &settledAt.Time
	}

	return item, nil
}
