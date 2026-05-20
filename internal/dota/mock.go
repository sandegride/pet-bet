package dota

import (
	"context"
	"time"
)

type MockProvider struct{}

func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

func (p *MockProvider) GetRecentMatches(ctx context.Context, accountID int64) ([]RecentMatch, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	nowBucket := time.Now().Unix() / 60
	matches := make([]RecentMatch, 0, 3)
	for offset := int64(2); offset >= 0; offset-- {
		bucket := nowBucket - offset
		matchID := accountID*1000000 + bucket
		radiantWin := bucket%2 == 0
		playerSlot := 0
		if bucket%3 == 0 {
			playerSlot = 128
		}

		matches = append(matches, RecentMatch{
			MatchID:    matchID,
			StartedAt:  time.Unix(bucket*60, 0).UTC(),
			LobbyType:  LobbyTypeRanked,
			GameMode:   GameModeRankedAllPick,
			PlayerSlot: playerSlot,
			RadiantWin: radiantWin,
			HeroID:     1 + bucket%130,
			HasResult:  true,
		})
	}

	return matches, nil
}

func (p *MockProvider) GetMatchDetails(ctx context.Context, matchID int64) (*MatchDetails, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	bucket := matchID % 1000000
	radiantWin := bucket%2 == 0
	playerSlot := 0
	if bucket%3 == 0 {
		playerSlot = 128
	}

	return &MatchDetails{
		MatchID:    matchID,
		StartedAt:  time.Unix(bucket*60, 0).UTC(),
		LobbyType:  LobbyTypeRanked,
		GameMode:   GameModeRankedAllPick,
		RadiantWin: radiantWin,
		Players: []MatchPlayer{
			{AccountID: matchID / 1000000, PlayerSlot: playerSlot, HeroID: 1 + bucket%130},
		},
	}, nil
}
