package cs

import (
	"regexp"
	"strings"
)

var steamID64Re = regexp.MustCompile(`^7656119\d{10}$`)

// IsSteamID64 сообщает, похожа ли строка на SteamID64.
func IsSteamID64(value string) bool {
	return steamID64Re.MatchString(value)
}

// ParseAccountInput принимает то, что прислал пользователь одним сообщением:
// SteamID64, ссылку на профиль Steam (steamcommunity.com/profiles/<id>) или FACEIT-ник,
// и возвращает нормализованное значение для поиска через провайдера.
func ParseAccountInput(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", ErrInvalidAccountInput
	}

	lower := strings.ToLower(value)
	if strings.Contains(lower, "steamcommunity.com/profiles/") {
		trimmed := strings.Trim(value, "/")
		parts := strings.Split(trimmed, "/")
		for i, part := range parts {
			if strings.ToLower(part) == "profiles" && i+1 < len(parts) {
				candidate := strings.TrimSpace(parts[i+1])
				if IsSteamID64(candidate) {
					return candidate, nil
				}
			}
		}
		return "", ErrInvalidAccountInput
	}

	if IsSteamID64(value) {
		return value, nil
	}

	// Любая другая короткая строка без пробелов считается FACEIT-ником.
	if strings.ContainsAny(value, " \t\n") || len(value) > 64 || len(value) < 2 {
		return "", ErrInvalidAccountInput
	}

	return value, nil
}
