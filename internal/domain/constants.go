package domain

import "time"

type MatchStatus string

const (
	MatchStatusUpcoming  MatchStatus = "upcoming"
	MatchStatusLive      MatchStatus = "live"
	MatchStatusFinished  MatchStatus = "finished"
	MatchStatusCancelled MatchStatus = "cancelled"
	MatchStatusSettled   MatchStatus = "settled"
)

type BetStatus string

const (
	BetStatusPending   BetStatus = "pending"
	BetStatusWon       BetStatus = "won"
	BetStatusLost      BetStatus = "lost"
	BetStatusVoid      BetStatus = "void"
	BetStatusCancelled BetStatus = "cancelled"
)

type TransactionType string

const (
	TransactionTypeInitialBonus    TransactionType = "initial_bonus"
	TransactionTypeBetDebit        TransactionType = "bet_debit"
	TransactionTypeBetWin          TransactionType = "bet_win"
	TransactionTypeBetRefund       TransactionType = "bet_refund"
	TransactionTypeAdminAdjustment TransactionType = "admin_adjustment"
)

const (
	ReferenceTypeUser  = "user"
	ReferenceTypeBet   = "bet"
	ReferenceTypeMatch = "match"
)

type User struct {
	ID         int64
	TelegramID int64
	Username   string
	FirstName  string
	Balance    int64
	IsAdmin    bool
	IsBlocked  bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Match struct {
	ID             int64
	Game           string
	TournamentName string
	TeamA          string
	TeamB          string
	StartsAt       time.Time
	Status         MatchStatus
	WinnerTeam     string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type MatchWithOdds struct {
	Match
	TeamAOdds string
	TeamBOdds string
}

type Bet struct {
	ID              int64
	UserID          int64
	MatchID         int64
	SelectedTeam    string
	Amount          int64
	Odds            string
	PotentialPayout int64
	Status          BetStatus
	CreatedAt       time.Time
	SettledAt       *time.Time
}

type BetHistoryItem struct {
	Bet
	TournamentName string
	TeamA          string
	TeamB          string
	StartsAt       time.Time
}
