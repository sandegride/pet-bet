package wallet

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"

	"stavki/internal/domain"
)

var (
	ErrInvalidAmount      = errors.New("amount must be greater than 0")
	ErrInsufficientFunds  = errors.New("insufficient funds")
	ErrInsufficientFrozen = errors.New("insufficient frozen balance")
	ErrUserNotFound       = errors.New("user not found")
	ErrBalanceOverflow    = errors.New("balance overflow")
)

type Balances struct {
	Balance       int64
	FrozenBalance int64
}

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Credit(
	ctx context.Context,
	tx pgx.Tx,
	userID int64,
	amount int64,
	txType domain.TransactionType,
	referenceType string,
	referenceID int64,
) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}

	balances, err := lockBalances(ctx, tx, userID)
	if err != nil {
		return err
	}

	if amount > math.MaxInt64-balances.Balance {
		return ErrBalanceOverflow
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE users SET balance = balance + $1, updated_at = now() WHERE id = $2`,
		amount,
		userID,
	); err != nil {
		return fmt.Errorf("credit user balance: %w", err)
	}

	return insertTransaction(ctx, tx, userID, txType, amount, referenceType, referenceID)
}

func (s *Service) Debit(
	ctx context.Context,
	tx pgx.Tx,
	userID int64,
	amount int64,
	txType domain.TransactionType,
	referenceType string,
	referenceID int64,
) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}

	balances, err := lockBalances(ctx, tx, userID)
	if err != nil {
		return err
	}

	if balances.Balance < amount {
		return ErrInsufficientFunds
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE users SET balance = balance - $1, updated_at = now() WHERE id = $2`,
		amount,
		userID,
	); err != nil {
		return fmt.Errorf("debit user balance: %w", err)
	}

	return insertTransaction(ctx, tx, userID, txType, -amount, referenceType, referenceID)
}

func (s *Service) GetBalance(ctx context.Context, userID int64) (int64, error) {
	return s.repo.GetBalance(ctx, userID)
}

func (s *Service) GetBalances(ctx context.Context, userID int64) (Balances, error) {
	return s.repo.GetBalances(ctx, userID)
}

func (s *Service) Freeze(
	ctx context.Context,
	tx pgx.Tx,
	userID int64,
	amount int64,
	txType domain.TransactionType,
	referenceType string,
	referenceID int64,
) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}

	balances, err := lockBalances(ctx, tx, userID)
	if err != nil {
		return err
	}
	if balances.Balance < amount {
		return ErrInsufficientFunds
	}
	if amount > math.MaxInt64-balances.FrozenBalance {
		return ErrBalanceOverflow
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE users
		 SET balance = balance - $1, frozen_balance = frozen_balance + $1, updated_at = now()
		 WHERE id = $2`,
		amount,
		userID,
	); err != nil {
		return fmt.Errorf("freeze balance: %w", err)
	}

	return insertTransaction(ctx, tx, userID, txType, -amount, referenceType, referenceID)
}

func (s *Service) Unfreeze(
	ctx context.Context,
	tx pgx.Tx,
	userID int64,
	amount int64,
	txType domain.TransactionType,
	referenceType string,
	referenceID int64,
) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}

	balances, err := lockBalances(ctx, tx, userID)
	if err != nil {
		return err
	}
	if balances.FrozenBalance < amount {
		return ErrInsufficientFrozen
	}
	if amount > math.MaxInt64-balances.Balance {
		return ErrBalanceOverflow
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE users
		 SET balance = balance + $1, frozen_balance = frozen_balance - $1, updated_at = now()
		 WHERE id = $2`,
		amount,
		userID,
	); err != nil {
		return fmt.Errorf("unfreeze balance: %w", err)
	}

	return insertTransaction(ctx, tx, userID, txType, amount, referenceType, referenceID)
}

func (s *Service) SettleFrozenWin(
	ctx context.Context,
	tx pgx.Tx,
	userID int64,
	frozenAmount int64,
	payout int64,
	txType domain.TransactionType,
	referenceType string,
	referenceID int64,
) error {
	if frozenAmount <= 0 || payout <= 0 {
		return ErrInvalidAmount
	}

	balances, err := lockBalances(ctx, tx, userID)
	if err != nil {
		return err
	}
	if balances.FrozenBalance < frozenAmount {
		return ErrInsufficientFrozen
	}
	if payout > math.MaxInt64-balances.Balance {
		return ErrBalanceOverflow
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE users
		 SET balance = balance + $1, frozen_balance = frozen_balance - $2, updated_at = now()
		 WHERE id = $3`,
		payout,
		frozenAmount,
		userID,
	); err != nil {
		return fmt.Errorf("settle frozen win: %w", err)
	}

	return insertTransaction(ctx, tx, userID, txType, payout, referenceType, referenceID)
}

func (s *Service) SettleFrozenLoss(
	ctx context.Context,
	tx pgx.Tx,
	userID int64,
	frozenAmount int64,
	txType domain.TransactionType,
	referenceType string,
	referenceID int64,
) error {
	if frozenAmount <= 0 {
		return ErrInvalidAmount
	}

	balances, err := lockBalances(ctx, tx, userID)
	if err != nil {
		return err
	}
	if balances.FrozenBalance < frozenAmount {
		return ErrInsufficientFrozen
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE users
		 SET frozen_balance = frozen_balance - $1, updated_at = now()
		 WHERE id = $2`,
		frozenAmount,
		userID,
	); err != nil {
		return fmt.Errorf("settle frozen loss: %w", err)
	}

	return insertTransaction(ctx, tx, userID, txType, -frozenAmount, referenceType, referenceID)
}

func (s *Service) Record(
	ctx context.Context,
	tx pgx.Tx,
	userID int64,
	amount int64,
	txType domain.TransactionType,
	referenceType string,
	referenceID int64,
) error {
	return insertTransaction(ctx, tx, userID, txType, amount, referenceType, referenceID)
}

func lockBalances(ctx context.Context, tx pgx.Tx, userID int64) (Balances, error) {
	var balances Balances
	err := tx.QueryRow(ctx, `SELECT balance, frozen_balance FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(
		&balances.Balance,
		&balances.FrozenBalance,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Balances{}, ErrUserNotFound
		}
		return Balances{}, fmt.Errorf("lock user balance: %w", err)
	}

	return balances, nil
}

func insertTransaction(
	ctx context.Context,
	tx pgx.Tx,
	userID int64,
	txType domain.TransactionType,
	amount int64,
	referenceType string,
	referenceID int64,
) error {
	var refType sql.NullString
	var refID sql.NullInt64
	if referenceType != "" {
		refType = sql.NullString{String: referenceType, Valid: true}
		refID = sql.NullInt64{Int64: referenceID, Valid: true}
	}

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO transactions (user_id, type, amount, reference_type, reference_id)
		 VALUES ($1, $2, $3, $4, $5)`,
		userID,
		string(txType),
		amount,
		refType,
		refID,
	); err != nil {
		return fmt.Errorf("insert transaction: %w", err)
	}

	return nil
}
