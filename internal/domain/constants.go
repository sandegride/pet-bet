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
	TransactionTypeBetFreeze       TransactionType = "bet_freeze"
	TransactionTypeBetUnfreeze     TransactionType = "bet_unfreeze"
	TransactionTypeBetWin          TransactionType = "bet_win"
	TransactionTypeBetLoss         TransactionType = "bet_loss"
	TransactionTypeBetRefund       TransactionType = "bet_refund"
	TransactionTypeAdminAdjustment TransactionType = "admin_adjustment"
	TransactionTypeLinkDotaAccount TransactionType = "link_dota_account"
	TransactionTypeSyncSnapshot    TransactionType = "sync_match_snapshot"
)

const (
	ReferenceTypeUser      = "user"
	ReferenceTypeBet       = "bet"
	ReferenceTypeMatch     = "match"
	ReferenceTypeSelfBet   = "self_bet"
	ReferenceTypeDotaMatch = "dota_match"
)

type SelfBetStatus string

const (
	SelfBetStatusActive    SelfBetStatus = "active"
	SelfBetStatusWon       SelfBetStatus = "won"
	SelfBetStatusLost      SelfBetStatus = "lost"
	SelfBetStatusCancelled SelfBetStatus = "cancelled"
	SelfBetStatusVoid      SelfBetStatus = "void"
)

type SelfBetPrediction string

const SelfBetPredictionWin SelfBetPrediction = "win"

type MatchResult string

const (
	MatchResultWin  MatchResult = "win"
	MatchResultLoss MatchResult = "loss"
)

type User struct {
	ID                      int64
	TelegramID              int64
	Username                string
	FirstName               string
	Balance                 int64
	FrozenBalance           int64
	IsAdmin                 bool
	IsBlocked               bool
	SteamID                 string
	DotaAccountID           *int64
	LastKnownMatchID        *int64
	LastKnownMatchStartedAt *time.Time
	IsDotaLinked            bool
	CreatedAt               time.Time
	UpdatedAt               time.Time
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

type SelfBet struct {
	ID              int64
	UserID          int64
	Amount          int64
	FrozenAmount    int64
	Odds            string
	PotentialPayout int64
	Prediction      SelfBetPrediction
	Status          SelfBetStatus
	TargetMatchID   *int64
	ResolvedResult  string
	CreatedAt       time.Time
	SettledAt       *time.Time
}

type SelfBetHistoryItem struct {
	SelfBet
	DotaAccountID *int64
}
