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
	// Статистика игрока в матче
	Kills     int
	Deaths    int
	Assists   int
	PartySize int // 1 = соло, >1 = группа
}

type MatchDetails struct {
	MatchID    int64
	StartedAt  time.Time
	LobbyType  int
	GameMode   int
	RadiantWin bool
	Players    []MatchPlayer
	// Статистика матча
	RadiantScore   int // убийства команды Радиант
	DireScore      int // убийства команды Дайр
	FirstBloodTime int // секунды от начала матча, 0 = неизвестно
	FirstBloodSlot int // player_slot игрока давшего ФБ, -1 = неизвестно
	AvgMMR         int // средний MMR матча, 0 = неизвестно
}

// TotalKills возвращает суммарное количество убийств в матче.
func (d *MatchDetails) TotalKills() int {
	return d.RadiantScore + d.DireScore
}

// FirstBloodIsRadiant возвращает true если первую кровь дала команда Радиант.
func (d *MatchDetails) FirstBloodIsRadiant() bool {
	return d.FirstBloodSlot >= 0 && d.FirstBloodSlot < 128
}

type MatchPlayer struct {
	AccountID  int64
	PlayerSlot int
	HeroID     int64
	Kills      int
	Deaths     int
	Assists    int
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
