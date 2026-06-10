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
	ErrInvalidThreshold   = errors.New("kills threshold must be greater than 0")
	ErrPayoutOverflow     = errors.New("potential payout is too large")
	ErrBetAlreadyTargeted = errors.New("bet is already attached to a match")
	ErrHistoryAdvanced    = errors.New("new competitive matches were found before bet")
	ErrMatchResultMissing = errors.New("dota match result is not available yet")
	ErrHWIDRequired       = errors.New("hardware ID registration required to place bets")
)

type Notifier interface {
	Notify(ctx context.Context, telegramID int64, text string) error
}

// AdminSettingsProvider позволяет получать настройки администратора.
type AdminSettingsProvider interface {
	GetSettings(ctx context.Context) (domain.AdminSettings, error)
}

type Service struct {
	db            DB
	repo          *Repository
	wallet        *wallet.Service
	provider      dota.Provider
	notifier      Notifier
	adminSettings AdminSettingsProvider
	logger        *slog.Logger
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

func (s *Service) getOddsForPrediction(ctx context.Context, prediction domain.SelfBetPrediction) string {
	if s.adminSettings == nil {
		return "2.00"
	}
	settings, err := s.adminSettings.GetSettings(ctx)
	if err != nil {
		return "2.00"
	}
	switch prediction {
	case domain.SelfBetPredictionTotalKillsOver:
		return settings.KillsOverOdds
	case domain.SelfBetPredictionFirstBloodRadiant, domain.SelfBetPredictionFirstBloodDire:
		return settings.FirstBloodOdds
	default:
		return settings.DefaultOdds
	}
}

func (s *Service) checkHWIDIfRequired(ctx context.Context, user domain.User) error {
	if s.adminSettings == nil {
		return nil
	}
	settings, err := s.adminSettings.GetSettings(ctx)
	if err != nil {
		return nil
	}
	if settings.HWIDRequired && user.HWID == "" {
		return ErrHWIDRequired
	}
	return nil
}

func (s *Service) LinkDotaAccount(ctx context.Context, telegramID int64, accountID int64) (LinkResult, error) {
	if accountID <= 0 {
		return LinkResult{}, ErrInvalidAccountID
	}
	accountID = dota.NormalizeAccountID(accountID)

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
	if latest != nil && latest.HasResult {
		if err := s.repo.SaveSnapshot(ctx, tx, user.ID, *latest); err != nil {
			return LinkResult{}, err
		}
	}
	if err := s.wallet.Record(
		ctx, tx, user.ID, 0,
		domain.TransactionTypeLinkDotaAccount,
		domain.ReferenceTypeUser, user.ID,
	); err != nil {
		return LinkResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return LinkResult{}, fmt.Errorf("commit link dota transaction: %w", err)
	}

	return LinkResult{AccountID: accountID, LastMatch: latest}, nil
}

// PlaceNextMatchWinBet — ставка на победу в следующем матче.
func (s *Service) PlaceNextMatchWinBet(ctx context.Context, telegramID int64, amount int64) (domain.SelfBet, error) {
	odds := s.getOddsForPrediction(ctx, domain.SelfBetPredictionWin)
	return s.placeNextMatchBet(ctx, telegramID, amount, domain.SelfBetPredictionWin, nil, odds)
}

// PlaceTotalKillsBet — ставка на то, что тотал килов в матче будет больше threshold.
func (s *Service) PlaceTotalKillsBet(ctx context.Context, telegramID int64, amount int64, threshold int64) (domain.SelfBet, error) {
	if threshold <= 0 {
		return domain.SelfBet{}, ErrInvalidThreshold
	}
	odds := s.getOddsForPrediction(ctx, domain.SelfBetPredictionTotalKillsOver)
	return s.placeNextMatchBet(ctx, telegramID, amount, domain.SelfBetPredictionTotalKillsOver, &threshold, odds)
}

// PlaceFirstBloodBet — ставка на то, что первую кровь даст указанная команда.
func (s *Service) PlaceFirstBloodBet(ctx context.Context, telegramID int64, amount int64, prediction domain.SelfBetPrediction) (domain.SelfBet, error) {
	if prediction != domain.SelfBetPredictionFirstBloodRadiant && prediction != domain.SelfBetPredictionFirstBloodDire {
		return domain.SelfBet{}, fmt.Errorf("invalid first blood prediction: %s", prediction)
	}
	odds := s.getOddsForPrediction(ctx, prediction)
	return s.placeNextMatchBet(ctx, telegramID, amount, prediction, nil, odds)
}

func (s *Service) placeNextMatchBet(
	ctx context.Context,
	telegramID int64,
	amount int64,
	prediction domain.SelfBetPrediction,
	killsThreshold *int64,
	oddsStr string,
) (domain.SelfBet, error) {
	if amount <= 0 {
		return domain.SelfBet{}, ErrInvalidAmount
	}

	potentialPayout, err := CalculatePotentialPayout(amount, oddsStr)
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
	if err := s.checkHWIDIfRequired(ctx, user); err != nil {
		return domain.SelfBet{}, err
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
			if match.HasResult {
				if err := s.repo.SaveSnapshot(ctx, tx, user.ID, match); err != nil {
					return domain.SelfBet{}, err
				}
			}
			if err := s.repo.UpdateLastKnownMatch(ctx, tx, user.ID, match); err != nil {
				return domain.SelfBet{}, err
			}
			if err := s.wallet.Record(
				ctx, tx, user.ID, 0,
				domain.TransactionTypeSyncSnapshot,
				domain.ReferenceTypeDotaMatch, match.MatchID,
			); err != nil {
				return domain.SelfBet{}, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.SelfBet{}, fmt.Errorf("commit pre-bet sync: %w", err)
		}
		return domain.SelfBet{}, ErrHistoryAdvanced
	}

	bet, err := s.repo.CreateActiveBet(ctx, tx, user.ID, amount, oddsStr, potentialPayout, prediction, killsThreshold)
	if err != nil {
		return domain.SelfBet{}, err
	}

	if err := s.wallet.Freeze(
		ctx, tx, user.ID, amount,
		domain.TransactionTypeBetFreeze,
		domain.ReferenceTypeSelfBet, bet.ID,
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
		ctx, tx, user.ID, bet.FrozenAmount,
		domain.TransactionTypeBetUnfreeze,
		domain.ReferenceTypeSelfBet, bet.ID,
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
				domain.ReferenceTypeDotaMatch, match.MatchID,
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

	// Загружаем admin settings для фильтров
	var adminSettings domain.AdminSettings
	if s.adminSettings != nil {
		adminSettings, _ = s.adminSettings.GetSettings(ctx)
	}

	// Фильтр: только соло игры
	if adminSettings.SoloOnlyBets && match.PartySize > 1 {
		s.logger.Info("voiding bet: match is not solo",
			"bet_id", bet.ID, "party_size", match.PartySize)
		return s.voidBet(ctx, tx, user, bet, match, "party_match")
	}

	// Загружаем детали матча если нужно
	var details *dota.MatchDetails
	needsDetails := bet.Prediction == domain.SelfBetPredictionTotalKillsOver ||
		bet.Prediction == domain.SelfBetPredictionFirstBloodRadiant ||
		bet.Prediction == domain.SelfBetPredictionFirstBloodDire ||
		adminSettings.MinAvgMMR > 0

	if needsDetails {
		if details, err = s.provider.GetMatchDetails(ctx, match.MatchID); err != nil {
			return fmt.Errorf("get match details for settlement: %w", err)
		}
	}

	// Фильтр: минимальный средний MMR
	if adminSettings.MinAvgMMR > 0 && details != nil && details.AvgMMR > 0 && details.AvgMMR < adminSettings.MinAvgMMR {
		s.logger.Info("voiding bet: avg_mmr below minimum",
			"bet_id", bet.ID, "avg_mmr", details.AvgMMR, "min_avg_mmr", adminSettings.MinAvgMMR)
		return s.voidBet(ctx, tx, user, bet, match, "low_mmr")
	}

	if err := s.repo.SaveSnapshot(ctx, tx, user.ID, match); err != nil {
		return err
	}

	status, result := s.determineBetOutcome(bet, match, details)

	if status == domain.SelfBetStatusWon {
		if err := s.wallet.SettleFrozenWin(
			ctx, tx, user.ID,
			bet.FrozenAmount, bet.PotentialPayout,
			domain.TransactionTypeBetWin, domain.ReferenceTypeSelfBet, bet.ID,
		); err != nil {
			return err
		}
	} else if status == domain.SelfBetStatusLost {
		if err := s.wallet.SettleFrozenLoss(
			ctx, tx, user.ID,
			bet.FrozenAmount,
			domain.TransactionTypeBetLoss, domain.ReferenceTypeSelfBet, bet.ID,
		); err != nil {
			return err
		}
	} else {
		// Void
		if err := s.wallet.Unfreeze(
			ctx, tx, user.ID, bet.FrozenAmount,
			domain.TransactionTypeBetUnfreeze, domain.ReferenceTypeSelfBet, bet.ID,
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

	s.notifySettlement(ctx, user.TelegramID, bet, match, status, details)
	return nil
}

func (s *Service) determineBetOutcome(
	bet domain.SelfBet,
	match dota.RecentMatch,
	details *dota.MatchDetails,
) (domain.SelfBetStatus, string) {
	switch bet.Prediction {
	case domain.SelfBetPredictionWin:
		result := dota.ResolvePlayerResult(match.PlayerSlot, match.RadiantWin)
		if result == string(domain.MatchResultWin) {
			return domain.SelfBetStatusWon, result
		}
		return domain.SelfBetStatusLost, result

	case domain.SelfBetPredictionTotalKillsOver:
		if details == nil || bet.KillsThreshold == nil {
			return domain.SelfBetStatusVoid, "no_data"
		}
		total := int64(details.TotalKills())
		if total > *bet.KillsThreshold {
			return domain.SelfBetStatusWon, fmt.Sprintf("total_%d_over_%d", total, *bet.KillsThreshold)
		}
		return domain.SelfBetStatusLost, fmt.Sprintf("total_%d_under_%d", total, *bet.KillsThreshold)

	case domain.SelfBetPredictionFirstBloodRadiant:
		if details == nil || details.FirstBloodSlot < 0 {
			return domain.SelfBetStatusVoid, "no_data"
		}
		if details.FirstBloodIsRadiant() {
			return domain.SelfBetStatusWon, "first_blood_radiant"
		}
		return domain.SelfBetStatusLost, "first_blood_dire"

	case domain.SelfBetPredictionFirstBloodDire:
		if details == nil || details.FirstBloodSlot < 0 {
			return domain.SelfBetStatusVoid, "no_data"
		}
		if !details.FirstBloodIsRadiant() {
			return domain.SelfBetStatusWon, "first_blood_dire"
		}
		return domain.SelfBetStatusLost, "first_blood_radiant"

	default:
		return domain.SelfBetStatusVoid, "unknown_prediction"
	}
}

func (s *Service) voidBet(
	ctx context.Context,
	tx pgx.Tx,
	user domain.User,
	bet domain.SelfBet,
	match dota.RecentMatch,
	reason string,
) error {
	if err := s.wallet.Unfreeze(
		ctx, tx, user.ID, bet.FrozenAmount,
		domain.TransactionTypeBetUnfreeze, domain.ReferenceTypeSelfBet, bet.ID,
	); err != nil {
		return err
	}
	if err := s.repo.MarkSettled(ctx, tx, bet.ID, domain.SelfBetStatusVoid, match.MatchID, reason); err != nil {
		return err
	}
	if err := s.repo.UpdateLastKnownMatch(ctx, tx, user.ID, match); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit void bet: %w", err)
	}
	if s.notifier != nil {
		_ = s.notifier.Notify(ctx, user.TelegramID,
			fmt.Sprintf("↩️ Ставка #%d аннулирована (%s). Монеты возвращены.", bet.ID, reason))
	}
	return nil
}

func (s *Service) notifySettlement(
	ctx context.Context,
	telegramID int64,
	bet domain.SelfBet,
	match dota.RecentMatch,
	status domain.SelfBetStatus,
	details *dota.MatchDetails,
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
	if details != nil {
		switch bet.Prediction {
		case domain.SelfBetPredictionTotalKillsOver:
			extraInfo = fmt.Sprintf("\nТотал килов: %d (порог: %d)", details.TotalKills(), ptrVal(bet.KillsThreshold))
		case domain.SelfBetPredictionFirstBloodRadiant, domain.SelfBetPredictionFirstBloodDire:
			fb := "Дайр"
			if details.FirstBloodIsRadiant() {
				fb = "Радиант"
			}
			extraInfo = fmt.Sprintf("\nПервая кровь: %s", fb)
		}
	}

	text := fmt.Sprintf(
		"%s Матч %d завершён: %s.\nСтавка: %d | Выплата: %d%s",
		emoji, match.MatchID, statusText, bet.Amount, bet.PotentialPayout, extraInfo,
	)
	if err := s.notifier.Notify(ctx, telegramID, text); err != nil {
		s.logger.Error("send self bet settlement notification",
			"telegram_id", telegramID, "error", err)
	}
}

func ptrVal(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
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
