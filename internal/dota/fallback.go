package dota

import (
	"context"
	"errors"
	"fmt"
)

type fallbackProvider struct {
	providers []Provider
}

func NewFallbackProvider(providers ...Provider) Provider {
	return &fallbackProvider{providers: providers}
}

func (p *fallbackProvider) GetRecentMatches(ctx context.Context, accountID int64) ([]RecentMatch, error) {
	var joined error
	for i, provider := range p.providers {
		matches, err := provider.GetRecentMatches(ctx, accountID)
		if err == nil {
			return matches, nil
		}
		joined = errors.Join(joined, fmt.Errorf("provider %d: %w", i+1, err))
	}

	return nil, joined
}

func (p *fallbackProvider) GetMatchDetails(ctx context.Context, matchID int64) (*MatchDetails, error) {
	var joined error
	for i, provider := range p.providers {
		details, err := provider.GetMatchDetails(ctx, matchID)
		if err == nil {
			return details, nil
		}
		joined = errors.Join(joined, fmt.Errorf("provider %d: %w", i+1, err))
	}

	return nil, joined
}
