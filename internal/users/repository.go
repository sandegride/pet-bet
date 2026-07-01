package users

import (
	"context"
	"database/sql"
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
		 RETURNING id, telegram_id, COALESCE(username, ''), COALESCE(first_name, ''),
		           balance, frozen_balance, is_admin, is_blocked, COALESCE(steam_id, ''),
		           dota_account_id, last_known_match_id, last_known_match_started_at, is_dota_linked,
		           COALESCE(hwid, ''), cs_faceit_player_id, COALESCE(cs_nickname, ''),
		           cs_last_known_match_id, cs_last_known_match_started_at, is_cs_linked,
		           created_at, updated_at`,
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
		 RETURNING id, telegram_id, COALESCE(username, ''), COALESCE(first_name, ''),
		           balance, frozen_balance, is_admin, is_blocked, COALESCE(steam_id, ''),
		           dota_account_id, last_known_match_id, last_known_match_started_at, is_dota_linked,
		           COALESCE(hwid, ''), cs_faceit_player_id, COALESCE(cs_nickname, ''),
		           cs_last_known_match_id, cs_last_known_match_started_at, is_cs_linked,
		           created_at, updated_at`,
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

func (r *Repository) SetBlocked(ctx context.Context, telegramID int64, blocked bool) error {
	tag, err := r.db.Exec(
		ctx,
		`UPDATE users SET is_blocked = $2, updated_at = now() WHERE telegram_id = $1`,
		telegramID,
		blocked,
	)
	if err != nil {
		return fmt.Errorf("set blocked: %w", err)
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

// ListRecent возвращает страницу пользователей, отсортированных по id (новые сначала).
// Используется административной панелью бота для выбора игрока.
func (r *Repository) ListRecent(ctx context.Context, limit, offset int) ([]domain.User, error) {
	rows, err := r.db.Query(ctx, selectUserSQL(`TRUE ORDER BY id DESC LIMIT $1 OFFSET $2`), limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]domain.User, 0, limit)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

func selectUserSQL(where string) string {
	return fmt.Sprintf(
		`SELECT id, telegram_id, COALESCE(username, ''), COALESCE(first_name, ''),
		        balance, frozen_balance, is_admin, is_blocked, COALESCE(steam_id, ''),
		        dota_account_id, last_known_match_id, last_known_match_started_at, is_dota_linked,
		        COALESCE(hwid, ''), cs_faceit_player_id, COALESCE(cs_nickname, ''),
		        cs_last_known_match_id, cs_last_known_match_started_at, is_cs_linked,
		        created_at, updated_at
		 FROM users
		 WHERE %s`,
		where,
	)
}

func scanUser(row pgx.Row) (domain.User, error) {
	var user domain.User
	var dotaAccountID sql.NullInt64
	var lastKnownMatchID sql.NullInt64
	var lastKnownMatchStartedAt sql.NullTime
	var csFaceitPlayerID sql.NullString
	var csLastKnownMatchID sql.NullString
	var csLastKnownMatchStartedAt sql.NullTime
	err := row.Scan(
		&user.ID,
		&user.TelegramID,
		&user.Username,
		&user.FirstName,
		&user.Balance,
		&user.FrozenBalance,
		&user.IsAdmin,
		&user.IsBlocked,
		&user.SteamID,
		&dotaAccountID,
		&lastKnownMatchID,
		&lastKnownMatchStartedAt,
		&user.IsDotaLinked,
		&user.HWID,
		&csFaceitPlayerID,
		&user.CSNickname,
		&csLastKnownMatchID,
		&csLastKnownMatchStartedAt,
		&user.IsCSLinked,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return domain.User{}, ErrNotFound
		}
		return domain.User{}, err
	}

	if dotaAccountID.Valid {
		user.DotaAccountID = &dotaAccountID.Int64
	}
	if lastKnownMatchID.Valid {
		user.LastKnownMatchID = &lastKnownMatchID.Int64
	}
	if lastKnownMatchStartedAt.Valid {
		user.LastKnownMatchStartedAt = &lastKnownMatchStartedAt.Time
	}
	if csFaceitPlayerID.Valid {
		user.CSFaceitPlayerID = &csFaceitPlayerID.String
	}
	if csLastKnownMatchID.Valid {
		user.CSLastKnownMatchID = &csLastKnownMatchID.String
	}
	if csLastKnownMatchStartedAt.Valid {
		user.CSLastKnownMatchStartedAt = &csLastKnownMatchStartedAt.Time
	}

	return user, nil
}
