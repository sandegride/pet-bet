package settlement

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"stavki/internal/bets"
	"stavki/internal/domain"
	"stavki/internal/matches"
	"stavki/internal/wallet"
)

var (
	ErrInvalidWinner = errors.New("winner must be one of match teams")
	ErrMatchSettled  = errors.New("match is already settled")
	ErrMatchCanceled = errors.New("match is cancelled")
)

type Service struct {
	db        *pgxpool.Pool
	matchRepo *matches.Repository
	bets      *bets.Service
	wallet    *wallet.Service
}

func NewService(
	db *pgxpool.Pool,
	matchRepo *matches.Repository,
	betsService *bets.Service,
	walletService *wallet.Service,
) *Service {
	return &Service{db: db, matchRepo: matchRepo, bets: betsService, wallet: walletService}
}

func (s *Service) SettleMatch(ctx context.Context, matchID int64, winnerTeam string) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin settlement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	match, err := s.matchRepo.GetByIDForUpdate(ctx, tx, matchID)
	if err != nil {
		return err
	}

	if match.Status == domain.MatchStatusSettled {
		return tx.Commit(ctx)
	}
	if match.Status == domain.MatchStatusCancelled {
		return ErrMatchCanceled
	}

	canonicalWinner, ok := domain.CanonicalMatchTeam(match.Match, winnerTeam)
	if !ok {
		return ErrInvalidWinner
	}

	pendingBets, err := s.bets.GetPendingByMatchForUpdate(ctx, tx, matchID)
	if err != nil {
		return fmt.Errorf("get pending bets: %w", err)
	}

	for _, bet := range pendingBets {
		if bet.SelectedTeam == canonicalWinner {
			if err := s.bets.MarkWon(ctx, tx, bet.ID); err != nil {
				return err
			}
			if err := s.wallet.Credit(
				ctx,
				tx,
				bet.UserID,
				bet.PotentialPayout,
				domain.TransactionTypeBetWin,
				domain.ReferenceTypeBet,
				bet.ID,
			); err != nil {
				return err
			}
			continue
		}

		if err := s.bets.MarkLost(ctx, tx, bet.ID); err != nil {
			return err
		}
	}

	if err := s.matchRepo.UpdateStatusWinner(ctx, tx, matchID, domain.MatchStatusSettled, canonicalWinner); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit settlement: %w", err)
	}

	return nil
}

func (s *Service) CancelMatch(ctx context.Context, matchID int64) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin cancellation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	match, err := s.matchRepo.GetByIDForUpdate(ctx, tx, matchID)
	if err != nil {
		return err
	}

	if match.Status == domain.MatchStatusCancelled {
		return tx.Commit(ctx)
	}
	if match.Status == domain.MatchStatusSettled {
		return ErrMatchSettled
	}

	pendingBets, err := s.bets.GetPendingByMatchForUpdate(ctx, tx, matchID)
	if err != nil {
		return fmt.Errorf("get pending bets: %w", err)
	}

	for _, bet := range pendingBets {
		if err := s.bets.MarkVoid(ctx, tx, bet.ID); err != nil {
			return err
		}
		if err := s.wallet.Credit(
			ctx,
			tx,
			bet.UserID,
			bet.Amount,
			domain.TransactionTypeBetRefund,
			domain.ReferenceTypeBet,
			bet.ID,
		); err != nil {
			return err
		}
	}

	if err := s.matchRepo.UpdateStatusWinner(ctx, tx, matchID, domain.MatchStatusCancelled, ""); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit cancellation: %w", err)
	}

	return nil
}

func IsIdempotentSettledStatus(status domain.MatchStatus) bool {
	return status == domain.MatchStatusSettled
}
