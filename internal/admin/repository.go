package admin

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"stavki/internal/domain"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// GetSettings читает все настройки из БД и возвращает AdminSettings.
// Если таблица ещё не создана (старая миграция), возвращает дефолты.
func (r *Repository) GetSettings(ctx context.Context) (domain.AdminSettings, error) {
	settings := domain.AdminSettings{
		DefaultOdds:     "2.00",
		KillsOverOdds:   "1.90",
		FirstBloodOdds:  "1.85",
		CSDefaultOdds:   "2.00",
		CSKillsOverOdds: "1.90",
	}

	rows, err := r.db.Query(ctx, `SELECT key, value FROM admin_settings`)
	if err != nil {
		// Таблица может ещё не существовать на старых инсталляциях
		return settings, nil
	}
	defer rows.Close()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return settings, fmt.Errorf("scan admin_settings row: %w", err)
		}
		switch key {
		case "default_odds":
			settings.DefaultOdds = value
		case "kills_over_odds":
			settings.KillsOverOdds = value
		case "first_blood_odds":
			settings.FirstBloodOdds = value
		case "solo_only_bets":
			settings.SoloOnlyBets = value == "true"
		case "min_avg_mmr":
			if v, err := strconv.Atoi(value); err == nil {
				settings.MinAvgMMR = v
			}
		case "hwid_required":
			settings.HWIDRequired = value == "true"
		case "cs_default_odds":
			settings.CSDefaultOdds = value
		case "cs_kills_over_odds":
			settings.CSKillsOverOdds = value
		}
	}

	return settings, rows.Err()
}

// SetSetting сохраняет одну пару key=value (upsert).
func (r *Repository) SetSetting(ctx context.Context, key, value string) error {
	_, err := r.db.Exec(
		ctx,
		`INSERT INTO admin_settings (key, value, updated_at)
		 VALUES ($1, $2, now())
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("set admin setting %s: %w", key, err)
	}
	return nil
}
