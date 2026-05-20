package telegram

import (
	"strings"
	"testing"
)

func TestParsePositiveInt64(t *testing.T) {
	t.Parallel()

	got, err := parsePositiveInt64("100")
	if err != nil {
		t.Fatalf("parsePositiveInt64() error = %v", err)
	}
	if got != 100 {
		t.Fatalf("parsePositiveInt64() = %d, want 100", got)
	}
}

func TestHelpTextDoesNotExposeProMatchCommands(t *testing.T) {
	t.Parallel()

	text := helpText()
	for _, command := range []string{"/next", "/admin_add_match", "/admin_finish_match", "/admin_cancel_match"} {
		if strings.Contains(text, command) {
			t.Fatalf("help text contains old pro-match command %s", command)
		}
	}
	for _, command := range []string{"/link_dota", "/bet", "/active_bet"} {
		if !strings.Contains(text, command) {
			t.Fatalf("help text does not contain self-match command %s", command)
		}
	}
}
