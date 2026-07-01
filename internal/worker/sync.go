package worker

import (
	"context"
	"log/slog"
	"time"

	"stavki/internal/cs"
	"stavki/internal/csbets"
	"stavki/internal/dota"
	"stavki/internal/selfbets"
)

// Service синхронизирует историю матчей Dota 2 и CS2 для привязанных пользователей
// и рассчитывает их активные ставки.
type Service struct {
	provider   dota.Provider
	selfbets   *selfbets.Service
	csProvider cs.Provider
	csbets     *csbets.Service
	logger     *slog.Logger
}

func NewService(
	provider dota.Provider,
	selfBets *selfbets.Service,
	csProvider cs.Provider,
	csBets *csbets.Service,
	logger *slog.Logger,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		provider:   provider,
		selfbets:   selfBets,
		csProvider: csProvider,
		csbets:     csBets,
		logger:     logger,
	}
}

func (s *Service) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	s.SyncOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.SyncOnce(ctx)
		}
	}
}

func (s *Service) SyncOnce(ctx context.Context) {
	s.syncDota(ctx)
	s.syncCS(ctx)
}

func (s *Service) syncDota(ctx context.Context) {
	if s.provider == nil || s.selfbets == nil {
		return
	}

	users, err := s.selfbets.GetLinkedUsers(ctx, 500)
	if err != nil {
		s.logger.Error("load linked dota users", "error", err)
		return
	}

	for _, user := range users {
		if user.DotaAccountID == nil {
			continue
		}

		recentMatches, err := s.provider.GetRecentMatches(ctx, *user.DotaAccountID)
		if err != nil {
			s.logger.Error("get recent dota matches", "user_id", user.ID, "account_id", *user.DotaAccountID, "error", err)
			continue
		}

		newMatches := NewCompetitiveMatches(recentMatches, user.LastKnownMatchID)
		for _, match := range newMatches {
			if err := s.selfbets.SettleActiveBetForUser(ctx, user.ID, match); err != nil {
				s.logger.Error("settle self bet", "user_id", user.ID, "match_id", match.MatchID, "error", err)
				break
			}
		}
	}
}

func (s *Service) syncCS(ctx context.Context) {
	if s.csProvider == nil || s.csbets == nil {
		return
	}

	users, err := s.csbets.GetLinkedUsers(ctx, 500)
	if err != nil {
		s.logger.Error("load linked cs users", "error", err)
		return
	}

	for _, user := range users {
		if user.CSFaceitPlayerID == nil {
			continue
		}

		recentMatches, err := s.csProvider.GetRecentMatches(ctx, *user.CSFaceitPlayerID)
		if err != nil {
			s.logger.Error("get recent cs matches", "user_id", user.ID, "player_id", *user.CSFaceitPlayerID, "error", err)
			continue
		}

		newMatches := csbets.NewMatches(recentMatches, user.CSLastKnownMatchStartedAt)
		for _, match := range newMatches {
			if err := s.csbets.SettleActiveBetForUser(ctx, user.ID, match); err != nil {
				s.logger.Error("settle cs bet", "user_id", user.ID, "match_id", match.MatchID, "error", err)
				break
			}
		}
	}
}

func NewCompetitiveMatches(matches []dota.RecentMatch, lastKnownMatchID *int64) []dota.RecentMatch {
	return selfbets.NewCompetitiveMatches(matches, lastKnownMatchID)
}
