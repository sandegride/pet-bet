package cs

import (
	"fmt"

	"stavki/internal/config"
)

func NewProvider(cfg config.CSConfig) (Provider, error) {
	switch cfg.Provider {
	case "mock":
		return NewMockProvider(), nil
	case "faceit":
		if cfg.FaceitAPIKey == "" {
			return nil, ErrFaceitAPIKeyRequired
		}
		return NewFaceitProvider(cfg.FaceitBaseURL, cfg.FaceitAPIKey), nil
	default:
		return nil, fmt.Errorf("unsupported cs provider %q", cfg.Provider)
	}
}
