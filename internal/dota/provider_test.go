package dota

import (
	"strconv"
	"testing"
)

func TestResolvePlayerResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		playerSlot int
		radiantWin bool
		want       string
	}{
		{name: "radiant wins", playerSlot: 0, radiantWin: true, want: "win"},
		{name: "radiant loses", playerSlot: 0, radiantWin: false, want: "loss"},
		{name: "dire loses", playerSlot: 128, radiantWin: true, want: "loss"},
		{name: "dire wins", playerSlot: 128, radiantWin: false, want: "win"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ResolvePlayerResult(tt.playerSlot, tt.radiantWin)
			if got != tt.want {
				t.Fatalf("ResolvePlayerResult() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeAccountID(t *testing.T) {
	t.Parallel()

	const accountID int64 = 1010282450
	steamID64 := accountID + steamID64Base

	if got := NormalizeAccountID(accountID); got != accountID {
		t.Fatalf("NormalizeAccountID(accountID) = %d, want %d", got, accountID)
	}
	if got := NormalizeAccountID(steamID64); got != accountID {
		t.Fatalf("NormalizeAccountID(steamID64) = %d, want %d", got, accountID)
	}
}

func TestParseAccountIDInput(t *testing.T) {
	t.Parallel()

	const accountID int64 = 1010282450
	steamID64 := accountID + steamID64Base

	tests := []struct {
		name  string
		input string
		want  int64
	}{
		{name: "account id", input: "1010282450", want: accountID},
		{name: "steam id64", input: strconv.FormatInt(steamID64, 10), want: accountID},
		{name: "profile url", input: "https://steamcommunity.com/profiles/" + strconv.FormatInt(steamID64, 10), want: accountID},
		{name: "profile url with slash", input: "https://steamcommunity.com/profiles/" + strconv.FormatInt(steamID64, 10) + "/", want: accountID},
		{name: "profile url without scheme", input: "steamcommunity.com/profiles/" + strconv.FormatInt(steamID64, 10), want: accountID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseAccountIDInput(tt.input)
			if err != nil {
				t.Fatalf("ParseAccountIDInput() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ParseAccountIDInput() = %d, want %d", got, tt.want)
			}
		})
	}
}
