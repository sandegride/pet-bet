package matches

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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
	tournamentName string,
	teamA string,
	teamB string,
	startsAt time.Time,
) (domain.Match, error) {
	return scanMatch(tx.QueryRow(
		ctx,
		`INSERT INTO matches (tournament_name, team_a, team_b, starts_at)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, game, COALESCE(tournament_name, ''), team_a, team_b, starts_at, status, COALESCE(winner_team, ''), created_at, updated_at`,
		tournamentName,
		teamA,
		teamB,
		startsAt,
	))
}

func (r *Repository) CreateOdds(ctx context.Context, tx pgx.Tx, matchID int64, teamAOdds string, teamBOdds string) error {
	_, err := tx.Exec(
		ctx,
		`INSERT INTO odds (match_id, team_a_odds, team_b_odds) VALUES ($1, $2, $3)`,
		matchID,
		teamAOdds,
		teamBOdds,
	)
	if err != nil {
		return fmt.Errorf("create odds: %w", err)
	}

	return nil
}

func (r *Repository) GetNextUpcoming(ctx context.Context) (domain.MatchWithOdds, error) {
	return scanMatchWithOdds(r.db.QueryRow(
		ctx,
		matchWithOddsSQL(`m.status = $1 AND m.starts_at > now() ORDER BY m.starts_at ASC LIMIT 1`, false),
		string(domain.MatchStatusUpcoming),
	))
}

func (r *Repository) GetByID(ctx context.Context, id int64) (domain.MatchWithOdds, error) {
	return scanMatchWithOdds(r.db.QueryRow(ctx, matchWithOddsSQL(`m.id = $1`, false), id))
}

func (r *Repository) GetByIDForUpdate(ctx context.Context, tx pgx.Tx, id int64) (domain.MatchWithOdds, error) {
	return scanMatchWithOdds(tx.QueryRow(ctx, matchWithOddsSQL(`m.id = $1`, true), id))
}

func (r *Repository) UpdateStatusWinner(
	ctx context.Context,
	tx pgx.Tx,
	matchID int64,
	status domain.MatchStatus,
	winnerTeam string,
) error {
	winner := sql.NullString{}
	if winnerTeam != "" {
		winner = sql.NullString{String: winnerTeam, Valid: true}
	}

	tag, err := tx.Exec(
		ctx,
		`UPDATE matches
		 SET status = $2, winner_team = $3, updated_at = now()
		 WHERE id = $1`,
		matchID,
		string(status),
		winner,
	)
	if err != nil {
		return fmt.Errorf("update match status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

func scanMatch(row pgx.Row) (domain.Match, error) {
	var match domain.Match
	var status string
	err := row.Scan(
		&match.ID,
		&match.Game,
		&match.TournamentName,
		&match.TeamA,
		&match.TeamB,
		&match.StartsAt,
		&status,
		&match.WinnerTeam,
		&match.CreatedAt,
		&match.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return domain.Match{}, ErrNotFound
		}
		return domain.Match{}, err
	}

	match.Status = domain.MatchStatus(status)
	return match, nil
}

func scanMatchWithOdds(row pgx.Row) (domain.MatchWithOdds, error) {
	var match domain.MatchWithOdds
	var status string
	err := row.Scan(
		&match.ID,
		&match.Game,
		&match.TournamentName,
		&match.TeamA,
		&match.TeamB,
		&match.StartsAt,
		&status,
		&match.WinnerTeam,
		&match.CreatedAt,
		&match.UpdatedAt,
		&match.TeamAOdds,
		&match.TeamBOdds,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return domain.MatchWithOdds{}, ErrNotFound
		}
		return domain.MatchWithOdds{}, err
	}

	match.Status = domain.MatchStatus(status)
	return match, nil
}

func matchWithOddsSQL(where string, forUpdate bool) string {
	lockClause := ""
	if forUpdate {
		lockClause = " FOR UPDATE OF m"
	}

	return fmt.Sprintf(
		`SELECT m.id, m.game, COALESCE(m.tournament_name, ''), m.team_a, m.team_b, m.starts_at,
		        m.status, COALESCE(m.winner_team, ''), m.created_at, m.updated_at,
		        o.team_a_odds::text, o.team_b_odds::text
		 FROM matches m
		 JOIN LATERAL (
		    SELECT team_a_odds, team_b_odds
		    FROM odds
		    WHERE match_id = m.id
		    ORDER BY created_at DESC, id DESC
		    LIMIT 1
		 ) o ON TRUE
		 WHERE %s%s`,
		where,
		lockClause,
	)
}
