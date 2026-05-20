package bets

import "testing"

func TestCalculatePotentialPayout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		amount int64
		odds   string
		want   int64
	}{
		{name: "integer result", amount: 100, odds: "2.05", want: 205},
		{name: "rounds down", amount: 3, odds: "1.33", want: 3},
		{name: "keeps stake at 1 odds", amount: 50, odds: "1.00", want: 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := CalculatePotentialPayout(tt.amount, tt.odds)
			if err != nil {
				t.Fatalf("CalculatePotentialPayout() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("CalculatePotentialPayout() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCalculatePotentialPayoutRejectsInvalidAmount(t *testing.T) {
	t.Parallel()

	if _, err := CalculatePotentialPayout(0, "1.50"); err == nil {
		t.Fatal("expected error for zero amount")
	}
}
