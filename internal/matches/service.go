package matches

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"stavki/internal/domain"
)

var (
	ErrNotFound              = errors.New("match not found")
	ErrInvalidTeam           = errors.New("team is invalid")
	ErrInvalidOdds           = errors.New("odds must be at least 1.00")
	ErrSettlementUnavailable = errors.New("settlement service is not configured")
)

type SettlementService interface {
	SettleMatch(ctx context.Context, matchID int64, winnerTeam string) error
	CancelMatch(ctx context.Context, matchID int64) error
}

type Service struct {
	db       *pgxpool.Pool
	repo     *Repository
	settler  SettlementService
	location *time.Location
}

func NewService(db *pgxpool.Pool, repo *Repository, location *time.Location) *Service {
	if location == nil {
		location = time.Local
	}

	return &Service{db: db, repo: repo, location: location}
}

func (s *Service) SetSettlementService(settler SettlementService) {
	s.settler = settler
}

func (s *Service) CreateMatch(
	ctx context.Context,
	tournamentName string,
	teamA string,
	teamB string,
	startsAt time.Time,
	teamAOdds string,
	teamBOdds string,
) (domain.MatchWithOdds, error) {
	tournamentName = strings.TrimSpace(tournamentName)
	teamA = strings.TrimSpace(teamA)
	teamB = strings.TrimSpace(teamB)
	if teamA == "" || teamB == "" || strings.EqualFold(teamA, teamB) {
		return domain.MatchWithOdds{}, ErrInvalidTeam
	}

	normalizedTeamAOdds, err := normalizeOdds(teamAOdds)
	if err != nil {
		return domain.MatchWithOdds{}, err
	}
	normalizedTeamBOdds, err := normalizeOdds(teamBOdds)
	if err != nil {
		return domain.MatchWithOdds{}, err
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.MatchWithOdds{}, fmt.Errorf("begin create match: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	match, err := s.repo.Create(ctx, tx, tournamentName, teamA, teamB, startsAt.In(s.location))
	if err != nil {
		return domain.MatchWithOdds{}, err
	}

	if err := s.repo.CreateOdds(ctx, tx, match.ID, normalizedTeamAOdds, normalizedTeamBOdds); err != nil {
		return domain.MatchWithOdds{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.MatchWithOdds{}, fmt.Errorf("commit create match: %w", err)
	}

	return domain.MatchWithOdds{
		Match:     match,
		TeamAOdds: normalizedTeamAOdds,
		TeamBOdds: normalizedTeamBOdds,
	}, nil
}

func (s *Service) GetNextUpcoming(ctx context.Context) (domain.MatchWithOdds, error) {
	return s.repo.GetNextUpcoming(ctx)
}

func (s *Service) GetByID(ctx context.Context, id int64) (domain.MatchWithOdds, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *Service) FinishMatch(ctx context.Context, matchID int64, winnerTeam string) error {
	if s.settler == nil {
		return ErrSettlementUnavailable
	}

	return s.settler.SettleMatch(ctx, matchID, winnerTeam)
}

func (s *Service) CancelMatch(ctx context.Context, matchID int64) error {
	if s.settler == nil {
		return ErrSettlementUnavailable
	}

	return s.settler.CancelMatch(ctx, matchID)
}

func normalizeOdds(value string) (string, error) {
	odds, err := decimal.NewFromString(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalidOdds, value)
	}
	if odds.LessThan(decimal.NewFromInt(1)) {
		return "", ErrInvalidOdds
	}

	return odds.StringFixed(2), nil
}
