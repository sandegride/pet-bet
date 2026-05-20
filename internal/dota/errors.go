package dota

import "errors"

var (
	ErrProviderUnavailable = errors.New("dota provider unavailable")
	ErrMatchHistoryPrivate = errors.New("dota match history is private")
	ErrSteamAPIKeyRequired = errors.New("steam web api key is required")
)
