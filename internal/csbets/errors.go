package csbets

import "errors"

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrCSNotLinked        = errors.New("cs account is not linked")
	ErrActiveBetExists    = errors.New("active cs bet already exists")
	ErrNoActiveBet        = errors.New("active cs bet not found")
	ErrInvalidAmount      = errors.New("bet amount must be greater than 0")
	ErrInvalidAccountID   = errors.New("invalid cs account identifier")
	ErrInvalidThreshold   = errors.New("kills threshold must be greater than 0")
	ErrPayoutOverflow     = errors.New("potential payout is too large")
	ErrBetAlreadyTargeted = errors.New("bet is already attached to a match")
	ErrHistoryAdvanced    = errors.New("new matches were found before bet")
	ErrMatchResultMissing = errors.New("cs match result is not available yet")
)
