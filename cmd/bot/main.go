package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"stavki/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to create postgres pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		logger.Error("failed to ping postgres", "error", err)
		os.Exit(1)
	}

	logger.Info(
		"bot started",
		"app_env", cfg.AppEnv,
		"postgres_host", cfg.Postgres.Host,
		"postgres_db", cfg.Postgres.DB,
		"initial_balance", cfg.InitialBalance,
		"bet_lock_minutes", cfg.BetLockMinutes,
		"admin_count", len(cfg.AdminTelegramIDs),
	)

	<-ctx.Done()
	logger.Info("bot stopped")
}
