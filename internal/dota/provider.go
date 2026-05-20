package dota

import (
	"context"
	"time"
)

const (
	LobbyTypeRanked       = 7
	GameModeRankedAllPick = 22
)

type Provider interface {
	GetRecentMatches(ctx context.Context, accountID int64) ([]RecentMatch, error)
	GetMatchDetails(ctx context.Context, matchID int64) (*MatchDetails, error)
}

type RecentMatch struct {
	MatchID    int64
	StartedAt  time.Time
	LobbyType  int
	GameMode   int
	PlayerSlot int
	RadiantWin bool
	HeroID     int64
	HasResult  bool
}

type MatchDetails struct {
	MatchID    int64
	StartedAt  time.Time
	LobbyType  int
	GameMode   int
	RadiantWin bool
	Players    []MatchPlayer
}

type MatchPlayer struct {
	AccountID  int64
	PlayerSlot int
	HeroID     int64
}

func IsCompetitiveMatch(recent RecentMatch) bool {
	return recent.LobbyType == LobbyTypeRanked || recent.GameMode == GameModeRankedAllPick
}

func ResolvePlayerResult(playerSlot int, radiantWin bool) string {
	isRadiant := playerSlot < 128
	if (isRadiant && radiantWin) || (!isRadiant && !radiantWin) {
		return "win"
	}

	return "loss"
}
