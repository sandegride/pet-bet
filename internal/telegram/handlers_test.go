package telegram

import "testing"

func TestParseAdminAddMatchArgs(t *testing.T) {
	t.Parallel()

	got, err := ParseAdminAddMatchArgs("Team A | Team B | The International | 2026-05-25 18:00 | 1.75 | 2.05")
	if err != nil {
		t.Fatalf("ParseAdminAddMatchArgs() error = %v", err)
	}

	if got.TeamA != "Team A" {
		t.Fatalf("TeamA = %q", got.TeamA)
	}
	if got.TeamB != "Team B" {
		t.Fatalf("TeamB = %q", got.TeamB)
	}
	if got.TournamentName != "The International" {
		t.Fatalf("TournamentName = %q", got.TournamentName)
	}
	if got.StartsAt.Format(adminTimeLayout) != "2026-05-25 18:00" {
		t.Fatalf("StartsAt = %s", got.StartsAt.Format(adminTimeLayout))
	}
	if got.TeamAOdds != "1.75" || got.TeamBOdds != "2.05" {
		t.Fatalf("odds = %s/%s", got.TeamAOdds, got.TeamBOdds)
	}
}

func TestParseAdminAddMatchArgsRejectsBadFormat(t *testing.T) {
	t.Parallel()

	if _, err := ParseAdminAddMatchArgs("Team A | Team B"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseAdminFinishMatchArgs(t *testing.T) {
	t.Parallel()

	got, err := ParseAdminFinishMatchArgs("42 | Team A")
	if err != nil {
		t.Fatalf("ParseAdminFinishMatchArgs() error = %v", err)
	}
	if got.MatchID != 42 {
		t.Fatalf("MatchID = %d", got.MatchID)
	}
	if got.WinnerTeam != "Team A" {
		t.Fatalf("WinnerTeam = %q", got.WinnerTeam)
	}
}
