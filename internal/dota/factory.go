package dota

import (
	"fmt"

	"stavki/internal/config"
)

func NewProvider(cfg config.DotaConfig) (Provider, error) {
	switch cfg.Provider {
	case "mock":
		return NewMockProvider(), nil
	case "opendota":
		return NewOpenDotaProvider(cfg.OpenDotaBaseURL), nil
	case "steam":
		if cfg.SteamWebAPIKey == "" {
			return nil, ErrSteamAPIKeyRequired
		}
		return NewSteamProvider(cfg.SteamBaseURL, cfg.SteamWebAPIKey, cfg.MatchesRequested), nil
	case "auto":
		providers := make([]Provider, 0, 2)
		if cfg.SteamWebAPIKey != "" {
			providers = append(providers, NewSteamProvider(cfg.SteamBaseURL, cfg.SteamWebAPIKey, cfg.MatchesRequested))
		}
		providers = append(providers, NewOpenDotaProvider(cfg.OpenDotaBaseURL))
		return NewFallbackProvider(providers...), nil
	default:
		return nil, fmt.Errorf("unsupported dota provider %q", cfg.Provider)
	}
}
