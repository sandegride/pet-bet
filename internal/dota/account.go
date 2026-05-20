package dota

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

const steamID64Base int64 = 76561197960265728

var ErrInvalidAccountID = errors.New("invalid dota account id")

func NormalizeAccountID(accountID int64) int64 {
	if accountID > steamID64Base {
		return accountID - steamID64Base
	}

	return accountID
}

func ParseAccountIDInput(input string) (int64, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return 0, ErrInvalidAccountID
	}

	if parsed, err := strconv.ParseInt(value, 10, 64); err == nil && parsed > 0 {
		return NormalizeAccountID(parsed), nil
	}

	urlValue := value
	if strings.HasPrefix(strings.ToLower(urlValue), "steamcommunity.com/") {
		urlValue = "https://" + urlValue
	}

	parsedURL, err := url.Parse(urlValue)
	if err != nil || parsedURL.Host == "" {
		return 0, ErrInvalidAccountID
	}

	parts := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")
	for i, part := range parts {
		if part != "profiles" || i+1 >= len(parts) {
			continue
		}

		steamID64, err := strconv.ParseInt(parts[i+1], 10, 64)
		if err != nil || steamID64 <= 0 {
			return 0, ErrInvalidAccountID
		}

		return NormalizeAccountID(steamID64), nil
	}

	return 0, ErrInvalidAccountID
}
