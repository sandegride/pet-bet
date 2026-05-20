package dota

import "testing"

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
