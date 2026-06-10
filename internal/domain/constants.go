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

const (
	// Классическая ставка — победа в матче.
	SelfBetPredictionWin SelfBetPrediction = "win"
	// Тотал килов в матче больше порога (KillsThreshold).
	SelfBetPredictionTotalKillsOver SelfBetPrediction = "total_kills_over"
	// Первая кровь — команда Радиант.
	SelfBetPredictionFirstBloodRadiant SelfBetPrediction = "first_blood_radiant"
	// Первая кровь — команда Дайр.
	SelfBetPredictionFirstBloodDire SelfBetPrediction = "first_blood_dire"
)

// AdminSettings — настройки сервиса, задаются администратором через бота.
type AdminSettings struct {
	DefaultOdds    string // коэф для ставки "победа"
	KillsOverOdds  string // коэф для ставки "тотал килов"
	FirstBloodOdds string // коэф для ставки "первая кровь"
	SoloOnlyBets   bool   // учитывать только соло-игры
	MinAvgMMR      int    // минимальный средний рейтинг матча (0 = отключено)
	HWIDRequired   bool   // требовать привязку железа
}

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
	HWID                    string
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
	KillsThreshold  *int64 // только для SelfBetPredictionTotalKillsOver
	CreatedAt       time.Time
	SettledAt       *time.Time
}

type SelfBetHistoryItem struct {
	SelfBet
	DotaAccountID *int64
}
