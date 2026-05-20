package users

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"stavki/internal/domain"
	"stavki/internal/wallet"
)

var ErrNotFound = errors.New("user not found")

type Service struct {
	db               *pgxpool.Pool
	repo             *Repository
	wallet           *wallet.Service
	initialBalance   int64
	adminTelegramIDs map[int64]struct{}
}

func NewService(
	db *pgxpool.Pool,
	repo *Repository,
	walletService *wallet.Service,
	initialBalance int64,
	adminTelegramIDs []int64,
) *Service {
	admins := make(map[int64]struct{}, len(adminTelegramIDs))
	for _, id := range adminTelegramIDs {
		admins[id] = struct{}{}
	}

	return &Service{
		db:               db,
		repo:             repo,
		wallet:           walletService,
		initialBalance:   initialBalance,
		adminTelegramIDs: admins,
	}
}

func (s *Service) GetOrCreateByTelegram(
	ctx context.Context,
	telegramID int64,
	username string,
	firstName string,
) (domain.User, error) {
	return s.getOrCreateByTelegram(ctx, telegramID, username, firstName, true)
}

func (s *Service) getOrCreateByTelegram(
	ctx context.Context,
	telegramID int64,
	username string,
	firstName string,
	retryOnConflict bool,
) (domain.User, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.User{}, fmt.Errorf("begin user transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	isAdmin := s.isConfiguredAdmin(telegramID)

	user, err := s.repo.GetByTelegramIDForUpdate(ctx, tx, telegramID)
	if err == nil {
		user, err = s.repo.UpdateProfile(ctx, tx, user.ID, username, firstName, isAdmin)
		if err != nil {
			return domain.User{}, fmt.Errorf("update user profile: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.User{}, fmt.Errorf("commit existing user: %w", err)
		}

		return user, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return domain.User{}, fmt.Errorf("get user for update: %w", err)
	}

	user, err = s.repo.Create(ctx, tx, telegramID, username, firstName, isAdmin)
	if err != nil {
		if retryOnConflict && isUniqueViolation(err) {
			_ = tx.Rollback(ctx)
			return s.getOrCreateByTelegram(ctx, telegramID, username, firstName, false)
		}
		return domain.User{}, fmt.Errorf("create user: %w", err)
	}

	if err := s.wallet.Credit(
		ctx,
		tx,
		user.ID,
		s.initialBalance,
		domain.TransactionTypeInitialBonus,
		domain.ReferenceTypeUser,
		user.ID,
	); err != nil {
		return domain.User{}, fmt.Errorf("credit initial balance: %w", err)
	}
	user.Balance = s.initialBalance

	if err := tx.Commit(ctx); err != nil {
		return domain.User{}, fmt.Errorf("commit new user: %w", err)
	}

	return user, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (s *Service) GetByTelegramID(ctx context.Context, telegramID int64) (domain.User, error) {
	return s.repo.GetByTelegramID(ctx, telegramID)
}

func (s *Service) SetAdminByTelegramID(ctx context.Context, telegramID int64) error {
	return s.repo.SetAdminByTelegramID(ctx, telegramID)
}

func (s *Service) IsAdmin(ctx context.Context, telegramID int64) (bool, error) {
	return s.isConfiguredAdmin(telegramID), nil
}

func (s *Service) isConfiguredAdmin(telegramID int64) bool {
	_, ok := s.adminTelegramIDs[telegramID]
	return ok
}
