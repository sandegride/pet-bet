package worker

import (
	"testing"
	"time"

	"stavki/internal/dota"
)

func TestNewCompetitiveMatches(t *testing.T) {
	t.Parallel()

	lastKnown := int64(10)
	base := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	matches := []dota.RecentMatch{
		{MatchID: 10, StartedAt: base, LobbyType: dota.LobbyTypeRanked, GameMode: dota.GameModeRankedAllPick},
		{MatchID: 12, StartedAt: base.Add(2 * time.Minute), LobbyType: 0, GameMode: 1},
		{MatchID: 11, StartedAt: base.Add(time.Minute), LobbyType: dota.LobbyTypeRanked, GameMode: dota.GameModeRankedAllPick},
		{MatchID: 13, StartedAt: base.Add(3 * time.Minute), LobbyType: dota.LobbyTypeRanked, GameMode: dota.GameModeRankedAllPick},
	}

	got := NewCompetitiveMatches(matches, &lastKnown)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].MatchID != 11 {
		t.Fatalf("first match = %d, want 11", got[0].MatchID)
	}
	if got[1].MatchID != 13 {
		t.Fatalf("second match = %d, want 13", got[1].MatchID)
	}
}
