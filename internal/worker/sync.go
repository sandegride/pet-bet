package worker

import (
	"context"
	"log/slog"
	"time"

	"stavki/internal/dota"
	"stavki/internal/selfbets"
)

type Service struct {
	provider dota.Provider
	selfbets *selfbets.Service
	logger   *slog.Logger
}

func NewService(provider dota.Provider, selfBets *selfbets.Service, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{provider: provider, selfbets: selfBets, logger: logger}
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

func NewCompetitiveMatches(matches []dota.RecentMatch, lastKnownMatchID *int64) []dota.RecentMatch {
	return selfbets.NewCompetitiveMatches(matches, lastKnownMatchID)
}
