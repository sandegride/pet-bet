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
	ErrInvalidAmount     = errors.New("amount must be greater than 0")
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrUserNotFound      = errors.New("user not found")
	ErrBalanceOverflow   = errors.New("balance overflow")
)

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

	balance, err := lockBalance(ctx, tx, userID)
	if err != nil {
		return err
	}

	if amount > math.MaxInt64-balance {
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

	balance, err := lockBalance(ctx, tx, userID)
	if err != nil {
		return err
	}

	if balance < amount {
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

func lockBalance(ctx context.Context, tx pgx.Tx, userID int64) (int64, error) {
	var balance int64
	err := tx.QueryRow(ctx, `SELECT balance FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&balance)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, ErrUserNotFound
		}
		return 0, fmt.Errorf("lock user balance: %w", err)
	}

	return balance, nil
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
