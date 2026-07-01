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
	"stavki/internal/cs"
	"stavki/internal/csbets"
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

	csProvider, err := cs.NewProvider(cfg.CS)
	if err != nil {
		logger.Error("failed to create cs provider", "error", err)
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

	csBetsRepo := csbets.NewRepository(pool)
	csBetsService := csbets.NewService(pool, csBetsRepo, walletService, csProvider, notifier, adminService, logger)

	syncInterval := cfg.Dota.SyncIntervalSeconds
	if cfg.CS.SyncIntervalSeconds < syncInterval {
		syncInterval = cfg.CS.SyncIntervalSeconds
	}

	worker := workersvc.NewService(provider, selfBetsService, csProvider, csBetsService, logger)
	logger.Info(
		"worker started",
		"dota_provider", cfg.Dota.Provider,
		"dota_sync_interval_seconds", cfg.Dota.SyncIntervalSeconds,
		"cs_provider", cfg.CS.Provider,
		"cs_sync_interval_seconds", cfg.CS.SyncIntervalSeconds,
	)
	worker.Run(ctx, time.Duration(syncInterval)*time.Second)
	logger.Info("worker stopped")
}
