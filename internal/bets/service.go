package bets

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"stavki/internal/domain"
	"stavki/internal/matches"
	"stavki/internal/users"
	"stavki/internal/wallet"
)

var (
	ErrInvalidAmount       = errors.New("bet amount must be greater than 0")
	ErrInvalidOdds         = errors.New("odds must be at least 1.00")
	ErrPayoutOverflow      = errors.New("potential payout is too large")
	ErrUserBlocked         = errors.New("user is blocked")
	ErrMatchNotUpcoming    = errors.New("match is not upcoming")
	ErrBettingClosed       = errors.New("betting is closed for this match")
	ErrInvalidSelectedTeam = errors.New("selected team is invalid")
	ErrBetNotPending       = errors.New("bet is not pending")
)

type Service struct {
	db             *pgxpool.Pool
	repo           *Repository
	userRepo       *users.Repository
	matchRepo      *matches.Repository
	wallet         *wallet.Service
	betLockMinutes int
}

func NewService(
	db *pgxpool.Pool,
	repo *Repository,
	userRepo *users.Repository,
	matchRepo *matches.Repository,
	walletService *wallet.Service,
	betLockMinutes int,
) *Service {
	return &Service{
		db:             db,
		repo:           repo,
		userRepo:       userRepo,
		matchRepo:      matchRepo,
		wallet:         walletService,
		betLockMinutes: betLockMinutes,
	}
}

func (s *Service) PlaceBet(
	ctx context.Context,
	telegramID int64,
	matchID int64,
	selectedTeam string,
	amount int64,
) (domain.Bet, error) {
	if amount <= 0 {
		return domain.Bet{}, ErrInvalidAmount
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Bet{}, fmt.Errorf("begin place bet: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	match, err := s.matchRepo.GetByIDForUpdate(ctx, tx, matchID)
	if err != nil {
		return domain.Bet{}, err
	}
	if match.Status != domain.MatchStatusUpcoming {
		return domain.Bet{}, ErrMatchNotUpcoming
	}
	if !time.Now().Before(match.StartsAt.Add(-time.Duration(s.betLockMinutes) * time.Minute)) {
		return domain.Bet{}, ErrBettingClosed
	}

	canonicalTeam, ok := domain.CanonicalMatchTeam(match.Match, selectedTeam)
	if !ok {
		return domain.Bet{}, ErrInvalidSelectedTeam
	}

	user, err := s.userRepo.GetByTelegramIDForUpdate(ctx, tx, telegramID)
	if err != nil {
		if errors.Is(err, users.ErrNotFound) {
			return domain.Bet{}, users.ErrNotFound
		}
		return domain.Bet{}, fmt.Errorf("lock user: %w", err)
	}
	if user.IsBlocked {
		return domain.Bet{}, ErrUserBlocked
	}

	odds := match.TeamBOdds
	if canonicalTeam == match.TeamA {
		odds = match.TeamAOdds
	}

	potentialPayout, err := CalculatePotentialPayout(amount, odds)
	if err != nil {
		return domain.Bet{}, err
	}

	if err := s.wallet.Debit(
		ctx,
		tx,
		user.ID,
		amount,
		domain.TransactionTypeBetDebit,
		domain.ReferenceTypeMatch,
		matchID,
	); err != nil {
		return domain.Bet{}, err
	}

	bet, err := s.repo.Create(ctx, tx, user.ID, matchID, canonicalTeam, amount, odds, potentialPayout)
	if err != nil {
		return domain.Bet{}, fmt.Errorf("create bet: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Bet{}, fmt.Errorf("commit place bet: %w", err)
	}

	return bet, nil
}

func (s *Service) GetUserHistory(ctx context.Context, telegramID int64, limit int) ([]domain.BetHistoryItem, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	return s.repo.GetUserHistory(ctx, telegramID, limit)
}

func (s *Service) GetPendingByMatchForUpdate(ctx context.Context, tx pgx.Tx, matchID int64) ([]domain.Bet, error) {
	return s.repo.GetPendingByMatchForUpdate(ctx, tx, matchID)
}

func (s *Service) MarkWon(ctx context.Context, tx pgx.Tx, betID int64) error {
	return s.repo.MarkWon(ctx, tx, betID)
}

func (s *Service) MarkLost(ctx context.Context, tx pgx.Tx, betID int64) error {
	return s.repo.MarkLost(ctx, tx, betID)
}

func (s *Service) MarkVoid(ctx context.Context, tx pgx.Tx, betID int64) error {
	return s.repo.MarkVoid(ctx, tx, betID)
}

func CalculatePotentialPayout(amount int64, oddsValue string) (int64, error) {
	if amount <= 0 {
		return 0, ErrInvalidAmount
	}

	odds, err := decimal.NewFromString(strings.TrimSpace(oddsValue))
	if err != nil {
		return 0, fmt.Errorf("invalid odds: %w", err)
	}
	if odds.LessThan(decimal.NewFromInt(1)) {
		return 0, ErrInvalidOdds
	}

	payout := decimal.NewFromInt(amount).Mul(odds).Floor()
	if payout.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return 0, ErrPayoutOverflow
	}

	return payout.IntPart(), nil
}
