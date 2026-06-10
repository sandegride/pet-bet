package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"stavki/internal/admin"
	"stavki/internal/config"
	"stavki/internal/dota"
	"stavki/internal/selfbets"
	"stavki/internal/telegram"
	"stavki/internal/wallet"
	workersvc "stavki/internal/worker"
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

	provider, err := dota.NewProvider(cfg.Dota)
	if err != nil {
		logger.Error("failed to create dota provider", "error", err)
		os.Exit(1)
	}

	notifier, err := telegram.NewNotifier(cfg.TelegramBotToken, logger)
	if err != nil {
		logger.Error("failed to create telegram notifier", "error", err)
		os.Exit(1)
	}

	adminRepo := admin.NewRepository(pool)
	adminService := admin.NewService(adminRepo)

	walletRepo := wallet.NewRepository(pool)
	walletService := wallet.NewService(walletRepo)
	selfBetsRepo := selfbets.NewRepository(pool)
	selfBetsService := selfbets.NewService(pool, selfBetsRepo, walletService, provider, notifier, adminService, logger)

	worker := workersvc.NewService(provider, selfBetsService, logger)
	logger.Info(
		"worker started",
		"dota_provider", cfg.Dota.Provider,
		"sync_interval_seconds", cfg.Dota.SyncIntervalSeconds,
	)
	worker.Run(ctx, time.Duration(cfg.Dota.SyncIntervalSeconds)*time.Second)
	logger.Info("worker stopped")
}
