package users

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"stavki/internal/domain"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetByTelegramID(ctx context.Context, telegramID int64) (domain.User, error) {
	return scanUser(r.db.QueryRow(ctx, selectUserSQL(`telegram_id = $1`), telegramID))
}

func (r *Repository) GetByTelegramIDForUpdate(ctx context.Context, tx pgx.Tx, telegramID int64) (domain.User, error) {
	return scanUser(tx.QueryRow(ctx, selectUserSQL(`telegram_id = $1 FOR UPDATE`), telegramID))
}

func (r *Repository) Create(
	ctx context.Context,
	tx pgx.Tx,
	telegramID int64,
	username string,
	firstName string,
	isAdmin bool,
) (domain.User, error) {
	return scanUser(tx.QueryRow(
		ctx,
		`INSERT INTO users (telegram_id, username, first_name, balance, is_admin)
		 VALUES ($1, $2, $3, 0, $4)
		 RETURNING id, telegram_id, COALESCE(username, ''), COALESCE(first_name, ''), balance, is_admin, is_blocked, created_at, updated_at`,
		telegramID,
		username,
		firstName,
		isAdmin,
	))
}

func (r *Repository) UpdateProfile(
	ctx context.Context,
	tx pgx.Tx,
	userID int64,
	username string,
	firstName string,
	isAdmin bool,
) (domain.User, error) {
	return scanUser(tx.QueryRow(
		ctx,
		`UPDATE users
		 SET username = $2, first_name = $3, is_admin = is_admin OR $4, updated_at = now()
		 WHERE id = $1
		 RETURNING id, telegram_id, COALESCE(username, ''), COALESCE(first_name, ''), balance, is_admin, is_blocked, created_at, updated_at`,
		userID,
		username,
		firstName,
		isAdmin,
	))
}

func (r *Repository) SetAdminByTelegramID(ctx context.Context, telegramID int64) error {
	tag, err := r.db.Exec(
		ctx,
		`UPDATE users SET is_admin = TRUE, updated_at = now() WHERE telegram_id = $1`,
		telegramID,
	)
	if err != nil {
		return fmt.Errorf("set admin: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

func (r *Repository) IsAdmin(ctx context.Context, telegramID int64) (bool, error) {
	var isAdmin bool
	err := r.db.QueryRow(ctx, `SELECT is_admin FROM users WHERE telegram_id = $1`, telegramID).Scan(&isAdmin)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, err
	}

	return isAdmin, nil
}

func selectUserSQL(where string) string {
	return fmt.Sprintf(
		`SELECT id, telegram_id, COALESCE(username, ''), COALESCE(first_name, ''), balance, is_admin, is_blocked, created_at, updated_at
		 FROM users
		 WHERE %s`,
		where,
	)
}

func scanUser(row pgx.Row) (domain.User, error) {
	var user domain.User
	err := row.Scan(
		&user.ID,
		&user.TelegramID,
		&user.Username,
		&user.FirstName,
		&user.Balance,
		&user.IsAdmin,
		&user.IsBlocked,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return domain.User{}, ErrNotFound
		}
		return domain.User{}, err
	}

	return user, nil
}
