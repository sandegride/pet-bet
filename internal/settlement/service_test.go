package settlement

import (
	"testing"

	"stavki/internal/domain"
)

func TestIsIdempotentSettledStatus(t *testing.T) {
	t.Parallel()

	if !IsIdempotentSettledStatus(domain.MatchStatusSettled) {
		t.Fatal("settled status must be idempotent")
	}
	if IsIdempotentSettledStatus(domain.MatchStatusUpcoming) {
		t.Fatal("upcoming status must not be treated as idempotently settled")
	}
}
