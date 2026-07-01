package cs

import (
	"context"
	"time"
)

// Provider — источник данных о матчах CS2 для конкретного игрока.
type Provider interface {
	// ResolvePlayer ищет FACEIT-профиль по SteamID64 или FACEIT-нику.
	ResolvePlayer(ctx context.Context, input string) (Player, error)
	// GetRecentMatches возвращает последние завершённые матчи игрока.
	GetRecentMatches(ctx context.Context, playerID string) ([]RecentMatch, error)
	// GetMatchDetails возвращает детальную статистику матча (для тотала килов).
	GetMatchDetails(ctx context.Context, matchID string) (*MatchDetails, error)
}

// Player — профиль игрока CS2 на FACEIT.
type Player struct {
	PlayerID string
	Nickname string
	SteamID  string
}

// RecentMatch — сводная информация о завершённом матче конкретного игрока.
type RecentMatch struct {
	MatchID   string
	StartedAt time.Time
	HasResult bool
	Won       bool
	Map       string
	Kills     int
	Deaths    int
	Assists   int
}

// MatchDetails — детальная статистика матча.
type MatchDetails struct {
	MatchID    string
	Map        string
	TotalKills int // суммарные килы всех игроков обеих команд
}

// IsMatchAfter сообщает, произошёл ли candidate позже current (по времени начала,
// со строковым сравнением id как вторичным критерием при равном времени).
func IsMatchAfter(candidate RecentMatch, current RecentMatch) bool {
	if candidate.StartedAt.Equal(current.StartedAt) {
		return candidate.MatchID > current.MatchID
	}
	return candidate.StartedAt.After(current.StartedAt)
}
