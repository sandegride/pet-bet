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
	default:
		return nil, fmt.Errorf("unsupported dota provider %q", cfg.Provider)
	}
}
