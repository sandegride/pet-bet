package wallet

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetBalance(ctx context.Context, userID int64) (int64, error) {
	balances, err := r.GetBalances(ctx, userID)
	if err != nil {
		return 0, err
	}

	return balances.Balance, nil
}

func (r *Repository) GetBalances(ctx context.Context, userID int64) (Balances, error) {
	var balances Balances
	err := r.db.QueryRow(ctx, `SELECT balance, frozen_balance FROM users WHERE id = $1`, userID).Scan(
		&balances.Balance,
		&balances.FrozenBalance,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Balances{}, ErrUserNotFound
		}
		return Balances{}, err
	}

	return balances, nil
}

// AdminAdjustByTelegramID adjusts balance for a user identified by telegram_id.
// delta can be negative (deduction) or positive (top-up). Uses an internal transaction.
func (r *Repository) AdminAdjustByTelegramID(ctx context.Context, telegramID int64, delta int64) error {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE telegram_id = $1 FOR UPDATE`, telegramID).Scan(&userID); err != nil {
		if err == pgx.ErrNoRows {
			return ErrUserNotFound
		}
		return fmt.Errorf("find user: %w", err)
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE users SET balance = balance + $1, updated_at = now() WHERE id = $2`,
		delta,
		userID,
	); err != nil {
		return fmt.Errorf("adjust balance: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *Repository) GetBalanceLegacy(ctx context.Context, userID int64) (int64, error) {
	var balance int64
	err := r.db.QueryRow(ctx, `SELECT balance FROM users WHERE id = $1`, userID).Scan(&balance)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, ErrUserNotFound
		}
		return 0, err
	}

	return balance, nil
}
