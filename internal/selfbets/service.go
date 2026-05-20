package selfbets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"stavki/internal/domain"
	"stavki/internal/dota"
	"stavki/internal/wallet"
)

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrDotaNotLinked      = errors.New("dota account is not linked")
	ErrActiveBetExists    = errors.New("active self bet already exists")
	ErrNoActiveBet        = errors.New("active self bet not found")
	ErrInvalidAmount      = errors.New("bet amount must be greater than 0")
	ErrInvalidAccountID   = errors.New("dota account id must be greater than 0")
	ErrPayoutOverflow     = errors.New("potential payout is too large")
	ErrBetAlreadyTargeted = errors.New("bet is already attached to a match")
	ErrHistoryAdvanced    = errors.New("new competitive matches were found before bet")
)

type Notifier interface {
	Notify(ctx context.Context, telegramID int64, text string) error
}

type Service struct {
	db       DB
	repo     *Repository
	wallet   *wallet.Service
	provider dota.Provider
	notifier Notifier
	logger   *slog.Logger
	odds     string
}

type LinkResult struct {
	AccountID int64
	LastMatch *dota.RecentMatch
}

func NewService(
	db DB,
	repo *Repository,
	walletService *wallet.Service,
	provider dota.Provider,
	notifier Notifier,
	logger *slog.Logger,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	return &Service{
		db:       db,
		repo:     repo,
		wallet:   walletService,
		provider: provider,
		notifier: notifier,
		logger:   logger,
		odds:     "2.00",
	}
}

func (s *Service) LinkDotaAccount(ctx context.Context, telegramID int64, accountID int64) (LinkResult, error) {
	if accountID <= 0 {
		return LinkResult{}, ErrInvalidAccountID
	}

	recentMatches, err := s.provider.GetRecentMatches(ctx, accountID)
	if err != nil {
		return LinkResult{}, fmt.Errorf("get recent dota matches: %w", err)
	}

	var latest *dota.RecentMatch
	for _, match := range recentMatches {
		if !dota.IsCompetitiveMatch(match) {
			continue
		}
		match := match
		if latest == nil || isMatchAfter(match, *latest) {
			latest = &match
		}
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return LinkResult{}, fmt.Errorf("begin link dota transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	user, err := s.repo.GetUserByTelegramIDForUpdate(ctx, tx, telegramID)
	if err != nil {
		return LinkResult{}, err
	}

	hasActive, err := s.repo.HasActiveBet(ctx, tx, user.ID)
	if err != nil {
		return LinkResult{}, err
	}
	if hasActive {
		return LinkResult{}, ErrActiveBetExists
	}

	if err := s.repo.LinkDotaAccount(ctx, tx, user.ID, accountID, latest); err != nil {
		return LinkResult{}, err
	}
	if latest != nil {
		if err := s.repo.SaveSnapshot(ctx, tx, user.ID, *latest); err != nil {
			return LinkResult{}, err
		}
	}
	if err := s.wallet.Record(
		ctx,
		tx,
		user.ID,
		0,
		domain.TransactionTypeLinkDotaAccount,
		domain.ReferenceTypeUser,
		user.ID,
	); err != nil {
		return LinkResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return LinkResult{}, fmt.Errorf("commit link dota transaction: %w", err)
	}

	return LinkResult{AccountID: accountID, LastMatch: latest}, nil
}

func (s *Service) PlaceNextMatchWinBet(ctx context.Context, telegramID int64, amount int64) (domain.SelfBet, error) {
	if amount <= 0 {
		return domain.SelfBet{}, ErrInvalidAmount
	}

	potentialPayout, err := CalculatePotentialPayout(amount, s.odds)
	if err != nil {
		return domain.SelfBet{}, err
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.SelfBet{}, fmt.Errorf("begin self bet transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	user, err := s.repo.GetUserByTelegramIDForUpdate(ctx, tx, telegramID)
	if err != nil {
		return domain.SelfBet{}, err
	}
	if !user.IsDotaLinked || user.DotaAccountID == nil {
		return domain.SelfBet{}, ErrDotaNotLinked
	}

	hasActive, err := s.repo.HasActiveBet(ctx, tx, user.ID)
	if err != nil {
		return domain.SelfBet{}, err
	}
	if hasActive {
		return domain.SelfBet{}, ErrActiveBetExists
	}

	recentMatches, err := s.provider.GetRecentMatches(ctx, *user.DotaAccountID)
	if err != nil {
		return domain.SelfBet{}, fmt.Errorf("get recent dota matches before bet: %w", err)
	}
	newMatches := NewCompetitiveMatches(recentMatches, user.LastKnownMatchID)
	if len(newMatches) > 0 {
		for _, match := range newMatches {
			if err := s.repo.SaveSnapshot(ctx, tx, user.ID, match); err != nil {
				return domain.SelfBet{}, err
			}
			if err := s.repo.UpdateLastKnownMatch(ctx, tx, user.ID, match); err != nil {
				return domain.SelfBet{}, err
			}
			if err := s.wallet.Record(
				ctx,
				tx,
				user.ID,
				0,
				domain.TransactionTypeSyncSnapshot,
				domain.ReferenceTypeDotaMatch,
				match.MatchID,
			); err != nil {
				return domain.SelfBet{}, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.SelfBet{}, fmt.Errorf("commit pre-bet sync: %w", err)
		}
		return domain.SelfBet{}, ErrHistoryAdvanced
	}

	bet, err := s.repo.CreateActiveBet(ctx, tx, user.ID, amount, s.odds, potentialPayout)
	if err != nil {
		return domain.SelfBet{}, err
	}

	if err := s.wallet.Freeze(
		ctx,
		tx,
		user.ID,
		amount,
		domain.TransactionTypeBetFreeze,
		domain.ReferenceTypeSelfBet,
		bet.ID,
	); err != nil {
		return domain.SelfBet{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.SelfBet{}, fmt.Errorf("commit self bet transaction: %w", err)
	}

	return bet, nil
}

func (s *Service) GetActiveBet(ctx context.Context, telegramID int64) (domain.SelfBet, error) {
	return s.repo.GetActiveBetByTelegramID(ctx, telegramID)
}

func (s *Service) GetHistory(ctx context.Context, telegramID int64, limit int) ([]domain.SelfBetHistoryItem, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	return s.repo.GetHistory(ctx, telegramID, limit)
}

func (s *Service) CancelActiveBet(ctx context.Context, telegramID int64) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin cancel self bet transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	user, err := s.repo.GetUserByTelegramIDForUpdate(ctx, tx, telegramID)
	if err != nil {
		return err
	}

	bet, err := s.repo.GetActiveBetForUpdate(ctx, tx, user.ID)
	if err != nil {
		return err
	}
	if bet.TargetMatchID != nil {
		return ErrBetAlreadyTargeted
	}

	if err := s.wallet.Unfreeze(
		ctx,
		tx,
		user.ID,
		bet.FrozenAmount,
		domain.TransactionTypeBetUnfreeze,
		domain.ReferenceTypeSelfBet,
		bet.ID,
	); err != nil {
		return err
	}

	if err := s.repo.CancelActiveBet(ctx, tx, bet.ID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit cancel self bet transaction: %w", err)
	}

	return nil
}

func (s *Service) SettleActiveBetForUser(ctx context.Context, userID int64, match dota.RecentMatch) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin self bet settlement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	user, err := s.repo.GetUserByIDForUpdate(ctx, tx, userID)
	if err != nil {
		return err
	}

	if err := s.repo.SaveSnapshot(ctx, tx, user.ID, match); err != nil {
		return err
	}

	bet, err := s.repo.GetActiveBetForUpdate(ctx, tx, user.ID)
	if err != nil {
		if errors.Is(err, ErrNoActiveBet) {
			if err := s.repo.UpdateLastKnownMatch(ctx, tx, user.ID, match); err != nil {
				return err
			}
			if err := s.wallet.Record(
				ctx,
				tx,
				user.ID,
				0,
				domain.TransactionTypeSyncSnapshot,
				domain.ReferenceTypeDotaMatch,
				match.MatchID,
			); err != nil {
				return err
			}
			return tx.Commit(ctx)
		}
		return err
	}

	result := dota.ResolvePlayerResult(match.PlayerSlot, match.RadiantWin)
	status := domain.SelfBetStatusLost
	if result == string(domain.MatchResultWin) {
		status = domain.SelfBetStatusWon
	}

	if status == domain.SelfBetStatusWon {
		if err := s.wallet.SettleFrozenWin(
			ctx,
			tx,
			user.ID,
			bet.FrozenAmount,
			bet.PotentialPayout,
			domain.TransactionTypeBetWin,
			domain.ReferenceTypeSelfBet,
			bet.ID,
		); err != nil {
			return err
		}
	} else {
		if err := s.wallet.SettleFrozenLoss(
			ctx,
			tx,
			user.ID,
			bet.FrozenAmount,
			domain.TransactionTypeBetLoss,
			domain.ReferenceTypeSelfBet,
			bet.ID,
		); err != nil {
			return err
		}
	}

	if err := s.repo.MarkSettled(ctx, tx, bet.ID, status, match.MatchID, result); err != nil {
		return err
	}
	if err := s.repo.UpdateLastKnownMatch(ctx, tx, user.ID, match); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit self bet settlement: %w", err)
	}

	s.notifySettlement(ctx, user.TelegramID, bet, match, result)
	return nil
}

func (s *Service) GetLinkedUsers(ctx context.Context, limit int) ([]domain.User, error) {
	if limit <= 0 {
		limit = 100
	}

	return s.repo.GetLinkedUsers(ctx, limit)
}

func (s *Service) notifySettlement(ctx context.Context, telegramID int64, bet domain.SelfBet, match dota.RecentMatch, result string) {
	if s.notifier == nil {
		return
	}

	text := fmt.Sprintf(
		"Матч %d завершён: %s.\nСтавка: %d\nПотенциальная выплата: %d",
		match.MatchID,
		resultText(result),
		bet.Amount,
		bet.PotentialPayout,
	)
	if err := s.notifier.Notify(ctx, telegramID, text); err != nil {
		s.logger.Error("send self bet settlement notification", "telegram_id", telegramID, "error", err)
	}
}

func resultText(result string) string {
	if result == string(domain.MatchResultWin) {
		return "победа, выигрыш начислен"
	}

	return "поражение, замороженная сумма списана"
}

func LatestCompetitiveMatch(matches []dota.RecentMatch) *dota.RecentMatch {
	var latest *dota.RecentMatch
	for _, match := range matches {
		if !dota.IsCompetitiveMatch(match) {
			continue
		}
		match := match
		if latest == nil || isMatchAfter(match, *latest) {
			latest = &match
		}
	}

	return latest
}

func NewCompetitiveMatches(matches []dota.RecentMatch, lastKnownMatchID *int64) []dota.RecentMatch {
	result := make([]dota.RecentMatch, 0, len(matches))
	for _, match := range matches {
		if !dota.IsCompetitiveMatch(match) {
			continue
		}
		if lastKnownMatchID != nil && match.MatchID <= *lastKnownMatchID {
			continue
		}
		result = append(result, match)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].StartedAt.Equal(result[j].StartedAt) {
			return result[i].MatchID < result[j].MatchID
		}
		return result[i].StartedAt.Before(result[j].StartedAt)
	})

	return result
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
		return 0, fmt.Errorf("invalid odds: %s", oddsValue)
	}

	payout := decimal.NewFromInt(amount).Mul(odds).Floor()
	if payout.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return 0, ErrPayoutOverflow
	}

	return payout.IntPart(), nil
}

func isMatchAfter(candidate dota.RecentMatch, current dota.RecentMatch) bool {
	if candidate.StartedAt.Equal(current.StartedAt) {
		return candidate.MatchID > current.MatchID
	}
	return candidate.StartedAt.After(current.StartedAt)
}
