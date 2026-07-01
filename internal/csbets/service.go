package csbets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"stavki/internal/cs"
	"stavki/internal/domain"
	"stavki/internal/wallet"
)

type Notifier interface {
	Notify(ctx context.Context, telegramID int64, text string) error
}

// AdminSettingsProvider позволяет получать настройки администратора (коэффициенты CS2).
type AdminSettingsProvider interface {
	GetSettings(ctx context.Context) (domain.AdminSettings, error)
}

type Service struct {
	db            DB
	repo          *Repository
	wallet        *wallet.Service
	provider      cs.Provider
	notifier      Notifier
	adminSettings AdminSettingsProvider
	logger        *slog.Logger
}

type LinkResult struct {
	PlayerID  string
	Nickname  string
	LastMatch *cs.RecentMatch
}

func NewService(
	db DB,
	repo *Repository,
	walletService *wallet.Service,
	provider cs.Provider,
	notifier Notifier,
	adminSettings AdminSettingsProvider,
	logger *slog.Logger,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	return &Service{
		db:            db,
		repo:          repo,
		wallet:        walletService,
		provider:      provider,
		notifier:      notifier,
		adminSettings: adminSettings,
		logger:        logger,
	}
}

func (s *Service) GetLinkedUsers(ctx context.Context, limit int) ([]domain.User, error) {
	return s.repo.GetLinkedUsers(ctx, limit)
}

func (s *Service) getOddsForPrediction(ctx context.Context, prediction domain.SelfBetPrediction) string {
	if s.adminSettings == nil {
		return "2.00"
	}
	settings, err := s.adminSettings.GetSettings(ctx)
	if err != nil {
		return "2.00"
	}
	if prediction == domain.SelfBetPredictionTotalKillsOver {
		return settings.CSKillsOverOdds
	}
	return settings.CSDefaultOdds
}

// LinkCSAccount привязывает CS2/FACEIT аккаунт по SteamID64 или FACEIT-нику,
// присланному пользователем одним сообщением.
func (s *Service) LinkCSAccount(ctx context.Context, telegramID int64, accountInput string) (LinkResult, error) {
	player, err := s.provider.ResolvePlayer(ctx, accountInput)
	if err != nil {
		return LinkResult{}, fmt.Errorf("resolve faceit player: %w", err)
	}

	recentMatches, err := s.provider.GetRecentMatches(ctx, player.PlayerID)
	if err != nil {
		return LinkResult{}, fmt.Errorf("get recent cs matches: %w", err)
	}

	latest := LatestMatch(recentMatches)

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return LinkResult{}, fmt.Errorf("begin link cs transaction: %w", err)
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

	if err := s.repo.LinkCSAccount(ctx, tx, user.ID, player, latest); err != nil {
		return LinkResult{}, err
	}
	if latest != nil && latest.HasResult {
		if err := s.repo.SaveSnapshot(ctx, tx, user.ID, *latest); err != nil {
			return LinkResult{}, err
		}
	}
	if err := s.wallet.Record(
		ctx, tx, user.ID, 0,
		domain.TransactionTypeLinkCSAccount,
		domain.ReferenceTypeUser, user.ID,
	); err != nil {
		return LinkResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return LinkResult{}, fmt.Errorf("commit link cs transaction: %w", err)
	}

	return LinkResult{PlayerID: player.PlayerID, Nickname: player.Nickname, LastMatch: latest}, nil
}

// PlaceNextMatchWinBet — ставка на победу в следующем матче CS2.
func (s *Service) PlaceNextMatchWinBet(ctx context.Context, telegramID int64, amount int64) (domain.CSBet, error) {
	odds := s.getOddsForPrediction(ctx, domain.SelfBetPredictionWin)
	return s.placeNextMatchBet(ctx, telegramID, amount, domain.SelfBetPredictionWin, nil, odds)
}

// PlaceTotalKillsBet — ставка на то, что суммарные килы в матче будут больше threshold.
func (s *Service) PlaceTotalKillsBet(ctx context.Context, telegramID int64, amount int64, threshold int64) (domain.CSBet, error) {
	if threshold <= 0 {
		return domain.CSBet{}, ErrInvalidThreshold
	}
	odds := s.getOddsForPrediction(ctx, domain.SelfBetPredictionTotalKillsOver)
	return s.placeNextMatchBet(ctx, telegramID, amount, domain.SelfBetPredictionTotalKillsOver, &threshold, odds)
}

func (s *Service) placeNextMatchBet(
	ctx context.Context,
	telegramID int64,
	amount int64,
	prediction domain.SelfBetPrediction,
	killsThreshold *int64,
	oddsStr string,
) (domain.CSBet, error) {
	if amount <= 0 {
		return domain.CSBet{}, ErrInvalidAmount
	}

	potentialPayout, err := CalculatePotentialPayout(amount, oddsStr)
	if err != nil {
		return domain.CSBet{}, err
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.CSBet{}, fmt.Errorf("begin cs bet transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	user, err := s.repo.GetUserByTelegramIDForUpdate(ctx, tx, telegramID)
	if err != nil {
		return domain.CSBet{}, err
	}
	if !user.IsCSLinked || user.CSFaceitPlayerID == nil {
		return domain.CSBet{}, ErrCSNotLinked
	}

	hasActive, err := s.repo.HasActiveBet(ctx, tx, user.ID)
	if err != nil {
		return domain.CSBet{}, err
	}
	if hasActive {
		return domain.CSBet{}, ErrActiveBetExists
	}

	recentMatches, err := s.provider.GetRecentMatches(ctx, *user.CSFaceitPlayerID)
	if err != nil {
		return domain.CSBet{}, fmt.Errorf("get recent cs matches before bet: %w", err)
	}
	newMatches := NewMatches(recentMatches, user.CSLastKnownMatchStartedAt)
	if len(newMatches) > 0 {
		for _, match := range newMatches {
			if match.HasResult {
				if err := s.repo.SaveSnapshot(ctx, tx, user.ID, match); err != nil {
					return domain.CSBet{}, err
				}
			}
			if err := s.repo.UpdateLastKnownMatch(ctx, tx, user.ID, match); err != nil {
				return domain.CSBet{}, err
			}
			if err := s.wallet.Record(
				ctx, tx, user.ID, 0,
				domain.TransactionTypeSyncSnapshot,
				domain.ReferenceTypeCSMatch, 0,
			); err != nil {
				return domain.CSBet{}, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.CSBet{}, fmt.Errorf("commit pre-bet cs sync: %w", err)
		}
		return domain.CSBet{}, ErrHistoryAdvanced
	}

	bet, err := s.repo.CreateActiveBet(ctx, tx, user.ID, amount, oddsStr, potentialPayout, prediction, killsThreshold)
	if err != nil {
		return domain.CSBet{}, err
	}

	if err := s.wallet.Freeze(
		ctx, tx, user.ID, amount,
		domain.TransactionTypeBetFreeze,
		domain.ReferenceTypeCSBet, bet.ID,
	); err != nil {
		return domain.CSBet{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.CSBet{}, fmt.Errorf("commit cs bet transaction: %w", err)
	}

	return bet, nil
}

func (s *Service) GetActiveBet(ctx context.Context, telegramID int64) (domain.CSBet, error) {
	return s.repo.GetActiveBetByTelegramID(ctx, telegramID)
}

func (s *Service) GetHistory(ctx context.Context, telegramID int64, limit int) ([]domain.CSBetHistoryItem, error) {
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
		return fmt.Errorf("begin cancel cs bet transaction: %w", err)
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
		ctx, tx, user.ID, bet.FrozenAmount,
		domain.TransactionTypeBetUnfreeze,
		domain.ReferenceTypeCSBet, bet.ID,
	); err != nil {
		return err
	}

	if err := s.repo.CancelActiveBet(ctx, tx, bet.ID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit cancel cs bet transaction: %w", err)
	}

	return nil
}

func (s *Service) SettleActiveBetForUser(ctx context.Context, userID int64, match cs.RecentMatch) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin cs bet settlement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	user, err := s.repo.GetUserByIDForUpdate(ctx, tx, userID)
	if err != nil {
		return err
	}

	bet, err := s.repo.GetActiveBetForUpdate(ctx, tx, user.ID)
	if err != nil {
		if errors.Is(err, ErrNoActiveBet) {
			if match.HasResult {
				if err := s.repo.SaveSnapshot(ctx, tx, user.ID, match); err != nil {
					return err
				}
			}
			if err := s.repo.UpdateLastKnownMatch(ctx, tx, user.ID, match); err != nil {
				return err
			}
			if err := s.wallet.Record(
				ctx, tx, user.ID, 0,
				domain.TransactionTypeSyncSnapshot,
				domain.ReferenceTypeCSMatch, 0,
			); err != nil {
				return err
			}
			return tx.Commit(ctx)
		}
		return err
	}

	if !match.HasResult {
		return ErrMatchResultMissing
	}

	var details *cs.MatchDetails
	if bet.Prediction == domain.SelfBetPredictionTotalKillsOver {
		if details, err = s.provider.GetMatchDetails(ctx, match.MatchID); err != nil {
			return fmt.Errorf("get cs match details for settlement: %w", err)
		}
	}

	if err := s.repo.SaveSnapshot(ctx, tx, user.ID, match); err != nil {
		return err
	}

	status, result := determineBetOutcome(bet, match, details)

	if status == domain.SelfBetStatusWon {
		if err := s.wallet.SettleFrozenWin(
			ctx, tx, user.ID,
			bet.FrozenAmount, bet.PotentialPayout,
			domain.TransactionTypeBetWin, domain.ReferenceTypeCSBet, bet.ID,
		); err != nil {
			return err
		}
	} else if status == domain.SelfBetStatusLost {
		if err := s.wallet.SettleFrozenLoss(
			ctx, tx, user.ID,
			bet.FrozenAmount,
			domain.TransactionTypeBetLoss, domain.ReferenceTypeCSBet, bet.ID,
		); err != nil {
			return err
		}
	} else {
		if err := s.wallet.Unfreeze(
			ctx, tx, user.ID, bet.FrozenAmount,
			domain.TransactionTypeBetUnfreeze, domain.ReferenceTypeCSBet, bet.ID,
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
		return fmt.Errorf("commit cs bet settlement: %w", err)
	}

	s.notifySettlement(ctx, user.TelegramID, bet, match, status, details)
	return nil
}

func determineBetOutcome(
	bet domain.CSBet,
	match cs.RecentMatch,
	details *cs.MatchDetails,
) (domain.SelfBetStatus, string) {
	switch bet.Prediction {
	case domain.SelfBetPredictionWin:
		if match.Won {
			return domain.SelfBetStatusWon, "win"
		}
		return domain.SelfBetStatusLost, "loss"

	case domain.SelfBetPredictionTotalKillsOver:
		if details == nil || bet.KillsThreshold == nil {
			return domain.SelfBetStatusVoid, "no_data"
		}
		total := int64(details.TotalKills)
		if total > *bet.KillsThreshold {
			return domain.SelfBetStatusWon, fmt.Sprintf("total_%d_over_%d", total, *bet.KillsThreshold)
		}
		return domain.SelfBetStatusLost, fmt.Sprintf("total_%d_under_%d", total, *bet.KillsThreshold)

	default:
		return domain.SelfBetStatusVoid, "unknown_prediction"
	}
}

func (s *Service) notifySettlement(
	ctx context.Context,
	telegramID int64,
	bet domain.CSBet,
	match cs.RecentMatch,
	status domain.SelfBetStatus,
	details *cs.MatchDetails,
) {
	if s.notifier == nil {
		return
	}

	var emoji, statusText string
	switch status {
	case domain.SelfBetStatusWon:
		emoji, statusText = "🏆", "выигрыш начислен"
	case domain.SelfBetStatusLost:
		emoji, statusText = "💸", "сумма списана"
	default:
		emoji, statusText = "↩️", "ставка аннулирована, монеты возвращены"
	}

	extraInfo := ""
	if details != nil && bet.Prediction == domain.SelfBetPredictionTotalKillsOver {
		extraInfo = fmt.Sprintf("\nТотал килов: %d (порог: %d)", details.TotalKills, ptrVal(bet.KillsThreshold))
	}

	text := fmt.Sprintf(
		"%s Матч CS2 завершён: %s.\nСтавка: %d | Выплата: %d%s",
		emoji, statusText, bet.Amount, bet.PotentialPayout, extraInfo,
	)
	if err := s.notifier.Notify(ctx, telegramID, text); err != nil {
		s.logger.Error("send cs bet settlement notification",
			"telegram_id", telegramID, "error", err)
	}
}

func ptrVal(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// LatestMatch возвращает самый свежий матч по времени начала.
func LatestMatch(matches []cs.RecentMatch) *cs.RecentMatch {
	var latest *cs.RecentMatch
	for _, match := range matches {
		match := match
		if latest == nil || cs.IsMatchAfter(match, *latest) {
			latest = &match
		}
	}
	return latest
}

// NewMatches возвращает матчи, начавшиеся позже lastKnownStartedAt, отсортированные по времени начала.
func NewMatches(matches []cs.RecentMatch, lastKnownStartedAt *time.Time) []cs.RecentMatch {
	result := make([]cs.RecentMatch, 0, len(matches))
	for _, match := range matches {
		if lastKnownStartedAt != nil && !match.StartedAt.After(*lastKnownStartedAt) {
			continue
		}
		result = append(result, match)
	}

	sort.Slice(result, func(i, j int) bool {
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
