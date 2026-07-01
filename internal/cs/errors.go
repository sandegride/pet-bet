package cs

import "errors"

var (
	ErrProviderUnavailable  = errors.New("cs provider unavailable")
	ErrPlayerNotFound       = errors.New("faceit player not found")
	ErrMatchDetailsMissing  = errors.New("cs match details are not available")
	ErrFaceitAPIKeyRequired = errors.New("faceit api key is required")
	ErrInvalidAccountInput  = errors.New("invalid cs account input")
)
