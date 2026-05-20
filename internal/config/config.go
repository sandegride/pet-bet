package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	AppEnv           string
	TelegramBotToken string
	DatabaseURL      string
	Postgres         PostgresConfig
	RedisAddr        string
	InitialBalance   int64
	BetLockMinutes   int
	AdminTelegramIDs []int64
}

type PostgresConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	DB       string
}

func Load() (Config, error) {
	var cfg Config

	cfg.AppEnv = getEnv("APP_ENV", "local")
	cfg.TelegramBotToken = strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	cfg.DatabaseURL = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	cfg.RedisAddr = strings.TrimSpace(os.Getenv("REDIS_ADDR"))

	cfg.Postgres = PostgresConfig{
		Host:     strings.TrimSpace(os.Getenv("POSTGRES_HOST")),
		Port:     strings.TrimSpace(os.Getenv("POSTGRES_PORT")),
		User:     strings.TrimSpace(os.Getenv("POSTGRES_USER")),
		Password: strings.TrimSpace(os.Getenv("POSTGRES_PASSWORD")),
		DB:       strings.TrimSpace(os.Getenv("POSTGRES_DB")),
	}

	initialBalance, err := parseInt64Env("INITIAL_BALANCE")
	if err != nil {
		return Config{}, err
	}
	cfg.InitialBalance = initialBalance

	betLockMinutes, err := parseIntEnv("BET_LOCK_MINUTES")
	if err != nil {
		return Config{}, err
	}
	cfg.BetLockMinutes = betLockMinutes

	adminIDs, err := parseInt64ListEnv("ADMIN_TELEGRAM_IDS")
	if err != nil {
		return Config{}, err
	}
	cfg.AdminTelegramIDs = adminIDs

	if cfg.DatabaseURL == "" {
		cfg.DatabaseURL = buildPostgresURL(cfg.Postgres)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (cfg Config) Validate() error {
	var missing []string

	required := map[string]string{
		"TELEGRAM_BOT_TOKEN": cfg.TelegramBotToken,
		"DATABASE_URL":       cfg.DatabaseURL,
		"POSTGRES_HOST":      cfg.Postgres.Host,
		"POSTGRES_PORT":      cfg.Postgres.Port,
		"POSTGRES_USER":      cfg.Postgres.User,
		"POSTGRES_PASSWORD":  cfg.Postgres.Password,
		"POSTGRES_DB":        cfg.Postgres.DB,
	}

	for name, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	if cfg.InitialBalance < 0 {
		return errors.New("INITIAL_BALANCE must be greater than or equal to 0")
	}

	if cfg.BetLockMinutes <= 0 {
		return errors.New("BET_LOCK_MINUTES must be greater than 0")
	}

	return nil
}

func getEnv(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func parseInt64Env(name string) (int64, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return 0, fmt.Errorf("%s is required", name)
	}

	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be int64: %w", name, err)
	}

	return parsed, nil
}

func parseIntEnv(name string) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return 0, fmt.Errorf("%s is required", name)
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be int: %w", name, err)
	}

	return parsed, nil
}

func parseInt64ListEnv(name string) ([]int64, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return nil, nil
	}

	parts := strings.Split(value, ",")
	result := make([]int64, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		parsed, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s contains invalid telegram id %q: %w", name, part, err)
		}

		result = append(result, parsed)
	}

	return result, nil
}

func buildPostgresURL(pg PostgresConfig) string {
	dsn := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(pg.User, pg.Password),
		Host:     net.JoinHostPort(pg.Host, pg.Port),
		Path:     pg.DB,
		RawQuery: "sslmode=disable",
	}

	return dsn.String()
}
