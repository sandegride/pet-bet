package wallet

import (
	"context"

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
