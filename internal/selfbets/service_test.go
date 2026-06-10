package selfbets

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"

	"stavki/internal/domain"
	"stavki/internal/dota"
	"stavki/internal/wallet"
)

func TestPlaceNextMatchWinBetCannotBetIfDotaNotLinked(t *testing.T) {
	t.Parallel()

	mock, service := newMockService(t)
	defer mock.Close()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, telegram_id").
		WithArgs(int64(100)).
		WillReturnRows(userRows().AddRow(
			int64(1), int64(100), "", "", int64(1000), int64(0), false, false, "",
			nil, nil, nil, false, time.Now(), time.Now(),
		))
	mock.ExpectRollback()

	_, err := service.PlaceNextMatchWinBet(context.Background(), 100, 100)
	if err != ErrDotaNotLinked {
		t.Fatalf("PlaceNextMatchWinBet() error = %v, want %v", err, ErrDotaNotLinked)
	}
	assertExpectations(t, mock)
}

func TestLinkDotaAccountSavesLastKnownCompetitiveMatch(t *testing.T) {
	t.Parallel()

	lastCompetitive := dota.RecentMatch{
		MatchID:    50,
		StartedAt:  time.Now(),
		LobbyType:  dota.LobbyTypeRanked,
		GameMode:   dota.GameModeRankedAllPick,
		PlayerSlot: 0,
		RadiantWin: true,
		HeroID:     1,
		HasResult:  true,
	}
	nonCompetitive := dota.RecentMatch{
		MatchID:    51,
		StartedAt:  lastCompetitive.StartedAt.Add(time.Minute),
		LobbyType:  0,
		GameMode:   1,
		PlayerSlot: 0,
		RadiantWin: false,
		HeroID:     2,
	}
	mock, service := newMockServiceWithProvider(t, fakeProvider{recentMatches: []dota.RecentMatch{lastCompetitive, nonCompetitive}})
	defer mock.Close()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, telegram_id").
		WithArgs(int64(100)).
		WillReturnRows(userRows().AddRow(
			int64(1), int64(100), "", "", int64(1000), int64(0), false, false, "",
			nil, nil, nil, false, time.Now(), time.Now(),
		))
	mock.ExpectQuery("SELECT id, user_id, amount").
		WithArgs(int64(1), string(domain.SelfBetStatusActive)).
		WillReturnError(pgx.ErrNoRows)
	mock.ExpectExec("UPDATE users").
		WithArgs(int64(1), int64(123), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectExec("INSERT INTO user_match_snapshots").
		WithArgs(int64(1), lastCompetitive.MatchID, lastCompetitive.StartedAt, "win", lastCompetitive.HeroID, lastCompetitive.PlayerSlot, lastCompetitive.RadiantWin, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("INSERT INTO transactions").
		WithArgs(int64(1), string(domain.TransactionTypeLinkDotaAccount), int64(0), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	result, err := service.LinkDotaAccount(context.Background(), 100, 123)
	if err != nil {
		t.Fatalf("LinkDotaAccount() error = %v", err)
	}
	if result.LastMatch == nil || result.LastMatch.MatchID != lastCompetitive.MatchID {
		t.Fatalf("LastMatch = %#v, want match id %d", result.LastMatch, lastCompetitive.MatchID)
	}
	assertExpectations(t, mock)
}

func TestLinkDotaAccountRejectsActiveBet(t *testing.T) {
	t.Parallel()

	mock, service := newMockService(t)
	defer mock.Close()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, telegram_id").
		WithArgs(int64(100)).
		WillReturnRows(linkedUserRows())
	mock.ExpectQuery("SELECT id, user_id, amount").
		WithArgs(int64(1), string(domain.SelfBetStatusActive)).
		WillReturnRows(activeBetRows())
	mock.ExpectRollback()

	_, err := service.LinkDotaAccount(context.Background(), 100, 123)
	if err != ErrActiveBetExists {
		t.Fatalf("LinkDotaAccount() error = %v, want %v", err, ErrActiveBetExists)
	}
	assertExpectations(t, mock)
}

func TestPlaceNextMatchWinBetCannotBetIfActiveBetExists(t *testing.T) {
	t.Parallel()

	mock, service := newMockService(t)
	defer mock.Close()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, telegram_id").
		WithArgs(int64(100)).
		WillReturnRows(linkedUserRows())
	mock.ExpectQuery("SELECT id, user_id, amount").
		WithArgs(int64(1), string(domain.SelfBetStatusActive)).
		WillReturnRows(selfBetRows().AddRow(
			int64(10), int64(1), int64(100), int64(100), "2.00", int64(200),
			string(domain.SelfBetPredictionWin), string(domain.SelfBetStatusActive), nil, "",
			time.Now(), nil,
		))
	mock.ExpectRollback()

	_, err := service.PlaceNextMatchWinBet(context.Background(), 100, 100)
	if err != ErrActiveBetExists {
		t.Fatalf("PlaceNextMatchWinBet() error = %v, want %v", err, ErrActiveBetExists)
	}
	assertExpectations(t, mock)
}

func TestPlaceNextMatchWinBetCannotBetMoreThanBalance(t *testing.T) {
	t.Parallel()

	mock, service := newMockService(t)
	defer mock.Close()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, telegram_id").
		WithArgs(int64(100)).
		WillReturnRows(linkedUserRows())
	mock.ExpectQuery("SELECT id, user_id, amount").
		WithArgs(int64(1), string(domain.SelfBetStatusActive)).
		WillReturnError(pgx.ErrNoRows)
	mock.ExpectQuery("INSERT INTO self_bets").
		WithArgs(int64(1), int64(1500), "2.00", int64(3000), string(domain.SelfBetPredictionWin), string(domain.SelfBetStatusActive)).
		WillReturnRows(selfBetRows().AddRow(
			int64(10), int64(1), int64(1500), int64(1500), "2.00", int64(3000),
			string(domain.SelfBetPredictionWin), string(domain.SelfBetStatusActive), nil, "",
			time.Now(), nil,
		))
	mock.ExpectQuery("SELECT balance, frozen_balance").
		WithArgs(int64(1)).
		WillReturnRows(pgxmock.NewRows([]string{"balance", "frozen_balance"}).AddRow(int64(1000), int64(0)))
	mock.ExpectRollback()

	_, err := service.PlaceNextMatchWinBet(context.Background(), 100, 1500)
	if err != wallet.ErrInsufficientFunds {
		t.Fatalf("PlaceNextMatchWinBet() error = %v, want %v", err, wallet.ErrInsufficientFunds)
	}
	assertExpectations(t, mock)
}

func TestPlaceNextMatchWinBetSyncsAdvancedHistoryBeforeAcceptingBet(t *testing.T) {
	t.Parallel()

	match := dota.RecentMatch{
		MatchID:    50,
		StartedAt:  time.Now(),
		LobbyType:  dota.LobbyTypeRanked,
		GameMode:   dota.GameModeRankedAllPick,
		PlayerSlot: 0,
		RadiantWin: true,
		HeroID:     1,
		HasResult:  true,
	}
	mock, service := newMockServiceWithProvider(t, fakeProvider{recentMatches: []dota.RecentMatch{match}})
	defer mock.Close()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, telegram_id").
		WithArgs(int64(100)).
		WillReturnRows(linkedUserRows())
	mock.ExpectQuery("SELECT id, user_id, amount").
		WithArgs(int64(1), string(domain.SelfBetStatusActive)).
		WillReturnError(pgx.ErrNoRows)
	mock.ExpectExec("INSERT INTO user_match_snapshots").
		WithArgs(int64(1), match.MatchID, match.StartedAt, "win", match.HeroID, match.PlayerSlot, match.RadiantWin, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("UPDATE users").
		WithArgs(int64(1), match.MatchID, match.StartedAt).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectExec("INSERT INTO transactions").
		WithArgs(int64(1), string(domain.TransactionTypeSyncSnapshot), int64(0), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	_, err := service.PlaceNextMatchWinBet(context.Background(), 100, 100)
	if err != ErrHistoryAdvanced {
		t.Fatalf("PlaceNextMatchWinBet() error = %v, want %v", err, ErrHistoryAdvanced)
	}
	assertExpectations(t, mock)
}

func TestPlaceNextMatchWinBetFreezesBalance(t *testing.T) {
	t.Parallel()

	mock, service := newMockService(t)
	defer mock.Close()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, telegram_id").
		WithArgs(int64(100)).
		WillReturnRows(linkedUserRows())
	mock.ExpectQuery("SELECT id, user_id, amount").
		WithArgs(int64(1), string(domain.SelfBetStatusActive)).
		WillReturnError(pgx.ErrNoRows)
	mock.ExpectQuery("INSERT INTO self_bets").
		WithArgs(int64(1), int64(100), "2.00", int64(200), string(domain.SelfBetPredictionWin), string(domain.SelfBetStatusActive)).
		WillReturnRows(selfBetRows().AddRow(
			int64(10), int64(1), int64(100), int64(100), "2.00", int64(200),
			string(domain.SelfBetPredictionWin), string(domain.SelfBetStatusActive), nil, "",
			time.Now(), nil,
		))
	mock.ExpectQuery("SELECT balance, frozen_balance").
		WithArgs(int64(1)).
		WillReturnRows(pgxmock.NewRows([]string{"balance", "frozen_balance"}).AddRow(int64(1000), int64(0)))
	mock.ExpectExec("UPDATE users").
		WithArgs(int64(100), int64(1)).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectExec("INSERT INTO transactions").
		WithArgs(int64(1), string(domain.TransactionTypeBetFreeze), int64(-100), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	bet, err := service.PlaceNextMatchWinBet(context.Background(), 100, 100)
	if err != nil {
		t.Fatalf("PlaceNextMatchWinBet() error = %v", err)
	}
	if bet.PotentialPayout != 200 {
		t.Fatalf("PotentialPayout = %d, want 200", bet.PotentialPayout)
	}
	assertExpectations(t, mock)
}

func TestSettleActiveBetForUserWinCreditsPayout(t *testing.T) {
	t.Parallel()

	mock, service := newMockService(t)
	defer mock.Close()

	match := dota.RecentMatch{MatchID: 50, StartedAt: time.Now(), LobbyType: dota.LobbyTypeRanked, GameMode: dota.GameModeRankedAllPick, PlayerSlot: 0, RadiantWin: true, HeroID: 1, HasResult: true}
	expectSettlementPrefix(mock, match)
	mock.ExpectQuery("SELECT id, user_id, amount").
		WithArgs(int64(1), string(domain.SelfBetStatusActive)).
		WillReturnRows(activeBetRows())
	expectSnapshot(mock, match)
	mock.ExpectQuery("SELECT balance, frozen_balance").
		WithArgs(int64(1)).
		WillReturnRows(pgxmock.NewRows([]string{"balance", "frozen_balance"}).AddRow(int64(900), int64(100)))
	mock.ExpectExec("UPDATE users").
		WithArgs(int64(200), int64(100), int64(1)).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectExec("INSERT INTO transactions").
		WithArgs(int64(1), string(domain.TransactionTypeBetWin), int64(200), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("UPDATE self_bets").
		WithArgs(int64(10), string(domain.SelfBetStatusWon), int64(50), "win", string(domain.SelfBetStatusActive)).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectExec("UPDATE users").
		WithArgs(int64(1), int64(50), match.StartedAt).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectCommit()

	if err := service.SettleActiveBetForUser(context.Background(), 1, match); err != nil {
		t.Fatalf("SettleActiveBetForUser() error = %v", err)
	}
	assertExpectations(t, mock)
}

func TestSettleActiveBetForUserLossSpendsFrozen(t *testing.T) {
	t.Parallel()

	mock, service := newMockService(t)
	defer mock.Close()

	match := dota.RecentMatch{MatchID: 51, StartedAt: time.Now(), LobbyType: dota.LobbyTypeRanked, GameMode: dota.GameModeRankedAllPick, PlayerSlot: 0, RadiantWin: false, HeroID: 1, HasResult: true}
	expectSettlementPrefix(mock, match)
	mock.ExpectQuery("SELECT id, user_id, amount").
		WithArgs(int64(1), string(domain.SelfBetStatusActive)).
		WillReturnRows(activeBetRows())
	expectSnapshot(mock, match)
	mock.ExpectQuery("SELECT balance, frozen_balance").
		WithArgs(int64(1)).
		WillReturnRows(pgxmock.NewRows([]string{"balance", "frozen_balance"}).AddRow(int64(900), int64(100)))
	mock.ExpectExec("UPDATE users").
		WithArgs(int64(100), int64(1)).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectExec("INSERT INTO transactions").
		WithArgs(int64(1), string(domain.TransactionTypeBetLoss), int64(-100), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("UPDATE self_bets").
		WithArgs(int64(10), string(domain.SelfBetStatusLost), int64(51), "loss", string(domain.SelfBetStatusActive)).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectExec("UPDATE users").
		WithArgs(int64(1), int64(51), match.StartedAt).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectCommit()

	if err := service.SettleActiveBetForUser(context.Background(), 1, match); err != nil {
		t.Fatalf("SettleActiveBetForUser() error = %v", err)
	}
	assertExpectations(t, mock)
}

func TestSettleActiveBetForUserActiveBetWaitsForMissingResult(t *testing.T) {
	t.Parallel()

	mock, service := newMockService(t)
	defer mock.Close()

	match := dota.RecentMatch{MatchID: 52, StartedAt: time.Now(), LobbyType: dota.LobbyTypeRanked, PlayerSlot: 0, HeroID: 1}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, telegram_id").
		WithArgs(int64(1)).
		WillReturnRows(linkedUserRows())
	mock.ExpectQuery("SELECT id, user_id, amount").
		WithArgs(int64(1), string(domain.SelfBetStatusActive)).
		WillReturnRows(activeBetRows())
	mock.ExpectRollback()

	err := service.SettleActiveBetForUser(context.Background(), 1, match)
	if err != ErrMatchResultMissing {
		t.Fatalf("SettleActiveBetForUser() error = %v, want %v", err, ErrMatchResultMissing)
	}
	assertExpectations(t, mock)
}

func TestSettleActiveBetForUserNoActiveBetAdvancesWithoutResult(t *testing.T) {
	t.Parallel()

	mock, service := newMockService(t)
	defer mock.Close()

	match := dota.RecentMatch{MatchID: 52, StartedAt: time.Now(), LobbyType: dota.LobbyTypeRanked, PlayerSlot: 0, HeroID: 1}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, telegram_id").
		WithArgs(int64(1)).
		WillReturnRows(linkedUserRows())
	mock.ExpectQuery("SELECT id, user_id, amount").
		WithArgs(int64(1), string(domain.SelfBetStatusActive)).
		WillReturnError(pgx.ErrNoRows)
	mock.ExpectExec("UPDATE users").
		WithArgs(int64(1), int64(52), match.StartedAt).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectExec("INSERT INTO transactions").
		WithArgs(int64(1), string(domain.TransactionTypeSyncSnapshot), int64(0), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	if err := service.SettleActiveBetForUser(context.Background(), 1, match); err != nil {
		t.Fatalf("SettleActiveBetForUser() error = %v", err)
	}
	assertExpectations(t, mock)
}

func TestSettleActiveBetForUserRepeatedSettlementDoesNotDoubleCredit(t *testing.T) {
	t.Parallel()

	mock, service := newMockService(t)
	defer mock.Close()

	match := dota.RecentMatch{MatchID: 50, StartedAt: time.Now(), LobbyType: dota.LobbyTypeRanked, GameMode: dota.GameModeRankedAllPick, PlayerSlot: 0, RadiantWin: true, HeroID: 1, HasResult: true}
	expectSettlementPrefix(mock, match)
	mock.ExpectQuery("SELECT id, user_id, amount").
		WithArgs(int64(1), string(domain.SelfBetStatusActive)).
		WillReturnError(pgx.ErrNoRows)
	expectSnapshot(mock, match)
	mock.ExpectExec("UPDATE users").
		WithArgs(int64(1), int64(50), match.StartedAt).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectExec("INSERT INTO transactions").
		WithArgs(int64(1), string(domain.TransactionTypeSyncSnapshot), int64(0), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	if err := service.SettleActiveBetForUser(context.Background(), 1, match); err != nil {
		t.Fatalf("SettleActiveBetForUser() error = %v", err)
	}
	assertExpectations(t, mock)
}

func TestSettleActiveBetForUserSnapshotDuplicateDoesNotBreakSettlement(t *testing.T) {
	t.Parallel()

	mock, service := newMockService(t)
	defer mock.Close()

	match := dota.RecentMatch{MatchID: 50, StartedAt: time.Now(), LobbyType: dota.LobbyTypeRanked, GameMode: dota.GameModeRankedAllPick, PlayerSlot: 0, RadiantWin: true, HeroID: 1, HasResult: true}
	expectSettlementPrefix(mock, match)
	mock.ExpectQuery("SELECT id, user_id, amount").
		WithArgs(int64(1), string(domain.SelfBetStatusActive)).
		WillReturnError(pgx.ErrNoRows)
	expectSnapshot(mock, match)
	mock.ExpectExec("UPDATE users").
		WithArgs(int64(1), int64(50), match.StartedAt).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectExec("INSERT INTO transactions").
		WithArgs(int64(1), string(domain.TransactionTypeSyncSnapshot), int64(0), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	if err := service.SettleActiveBetForUser(context.Background(), 1, match); err != nil {
		t.Fatalf("SettleActiveBetForUser() error = %v", err)
	}
	assertExpectations(t, mock)
}

func newMockService(t *testing.T) (pgxmock.PgxPoolIface, *Service) {
	t.Helper()

	return newMockServiceWithProvider(t, fakeProvider{})
}

func newMockServiceWithProvider(t *testing.T, provider dota.Provider) (pgxmock.PgxPoolIface, *Service) {
	t.Helper()

	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock.NewPool() error = %v", err)
	}

	repo := NewRepository(mock)
	walletService := wallet.NewService(nil)
	service := NewService(mock, repo, walletService, provider, nil, nil, nil)
	return mock, service
}

type fakeProvider struct {
	recentMatches []dota.RecentMatch
	err           error
}

func (p fakeProvider) GetRecentMatches(ctx context.Context, accountID int64) ([]dota.RecentMatch, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.recentMatches, nil
}

func (p fakeProvider) GetMatchDetails(ctx context.Context, matchID int64) (*dota.MatchDetails, error) {
	return nil, nil
}

func userRows() *pgxmock.Rows {
	return pgxmock.NewRows([]string{
		"id", "telegram_id", "username", "first_name", "balance", "frozen_balance",
		"is_admin", "is_blocked", "steam_id", "dota_account_id", "last_known_match_id",
		"last_known_match_started_at", "is_dota_linked", "created_at", "updated_at",
	})
}

func linkedUserRows() *pgxmock.Rows {
	return userRows().AddRow(
		int64(1), int64(100), "", "", int64(1000), int64(0), false, false, "",
		int64(123), int64(49), time.Now().Add(-time.Hour), true, time.Now(), time.Now(),
	)
}

func selfBetRows() *pgxmock.Rows {
	return pgxmock.NewRows([]string{
		"id", "user_id", "amount", "frozen_amount", "odds", "potential_payout",
		"prediction", "status", "target_match_id", "resolved_result", "created_at", "settled_at",
	})
}

func activeBetRows() *pgxmock.Rows {
	return selfBetRows().AddRow(
		int64(10), int64(1), int64(100), int64(100), "2.00", int64(200),
		string(domain.SelfBetPredictionWin), string(domain.SelfBetStatusActive), nil, "",
		time.Now(), nil,
	)
}

func expectSettlementPrefix(mock pgxmock.PgxPoolIface, match dota.RecentMatch) {
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, telegram_id").
		WithArgs(int64(1)).
		WillReturnRows(linkedUserRows())
}

func expectSnapshot(mock pgxmock.PgxPoolIface, match dota.RecentMatch) {
	mock.ExpectExec("INSERT INTO user_match_snapshots").
		WithArgs(int64(1), match.MatchID, match.StartedAt, dota.ResolvePlayerResult(match.PlayerSlot, match.RadiantWin), match.HeroID, match.PlayerSlot, match.RadiantWin, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
}

func assertExpectations(t *testing.T, mock pgxmock.PgxPoolIface) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
