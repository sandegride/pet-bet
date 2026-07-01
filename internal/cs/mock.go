package cs

import (
	"context"
	"fmt"
	"time"
)

// MockProvider — провайдер для локальной разработки без обращения к FACEIT API.
// Генерирует новый завершённый матч примерно раз в минуту.
type MockProvider struct{}

func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

func (p *MockProvider) ResolvePlayer(ctx context.Context, input string) (Player, error) {
	select {
	case <-ctx.Done():
		return Player{}, ctx.Err()
	default:
	}

	normalized, err := ParseAccountInput(input)
	if err != nil {
		return Player{}, err
	}

	return Player{
		PlayerID: "mock-" + normalized,
		Nickname: normalized,
		SteamID:  normalized,
	}, nil
}

func (p *MockProvider) GetRecentMatches(ctx context.Context, playerID string) ([]RecentMatch, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	nowBucket := time.Now().Unix() / 60
	matches := make([]RecentMatch, 0, 3)
	for offset := int64(2); offset >= 0; offset-- {
		bucket := nowBucket - offset
		matchID := fmt.Sprintf("mock-%s-%d", playerID, bucket)

		matches = append(matches, RecentMatch{
			MatchID:   matchID,
			StartedAt: time.Unix(bucket*60, 0).UTC(),
			HasResult: true,
			Won:       bucket%2 == 0,
			Map:       "de_mirage",
			Kills:     int(10 + bucket%15),
			Deaths:    int(8 + bucket%10),
			Assists:   int(3 + bucket%6),
		})
	}

	return matches, nil
}

func (p *MockProvider) GetMatchDetails(ctx context.Context, matchID string) (*MatchDetails, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	return &MatchDetails{
		MatchID:    matchID,
		Map:        "de_mirage",
		TotalKills: 140,
	}, nil
}
