package dota

import "errors"

var (
	ErrProviderUnavailable = errors.New("dota provider unavailable")
	ErrMatchHistoryPrivate = errors.New("dota match history is private")
	ErrMatchDetailsMissing = errors.New("dota match details are not available")
	ErrSteamAPIKeyRequired = errors.New("steam web api key is required")
)
