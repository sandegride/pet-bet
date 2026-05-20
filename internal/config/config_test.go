package config

import (
	"reflect"
	"testing"
)

func TestLoadParsesAdminTelegramIDs(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("POSTGRES_HOST", "postgres")
	t.Setenv("POSTGRES_PORT", "5432")
	t.Setenv("POSTGRES_USER", "bot")
	t.Setenv("POSTGRES_PASSWORD", "bot")
	t.Setenv("POSTGRES_DB", "dota_bet_bot")
	t.Setenv("REDIS_ADDR", "redis:6379")
	t.Setenv("INITIAL_BALANCE", "1000")
	t.Setenv("BET_LOCK_MINUTES", "5")
	t.Setenv("ADMIN_TELEGRAM_IDS", "123, 456,789")
	t.Setenv("DOTA_PROVIDER", "mock")
	t.Setenv("OPENDOTA_BASE_URL", "https://api.opendota.com/api")
	t.Setenv("DOTA_SYNC_INTERVAL_SECONDS", "30")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := []int64{123, 456, 789}
	if !reflect.DeepEqual(cfg.AdminTelegramIDs, want) {
		t.Fatalf("AdminTelegramIDs = %#v, want %#v", cfg.AdminTelegramIDs, want)
	}
	if cfg.Dota.Provider != "mock" {
		t.Fatalf("Dota.Provider = %q", cfg.Dota.Provider)
	}
	if cfg.Dota.SyncIntervalSeconds != 30 {
		t.Fatalf("Dota.SyncIntervalSeconds = %d", cfg.Dota.SyncIntervalSeconds)
	}
}
