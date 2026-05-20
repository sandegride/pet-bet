package domain

import "strings"

func CanonicalMatchTeam(match Match, team string) (string, bool) {
	team = strings.TrimSpace(team)
	switch {
	case strings.EqualFold(team, match.TeamA):
		return match.TeamA, true
	case strings.EqualFold(team, match.TeamB):
		return match.TeamB, true
	default:
		return "", false
	}
}
