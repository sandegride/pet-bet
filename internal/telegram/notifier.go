package telegram

import (
	"context"
	"fmt"
	"log/slog"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Notifier struct {
	api    *tgbotapi.BotAPI
	logger *slog.Logger
}

func NewNotifier(token string, logger *slog.Logger) (*Notifier, error) {
	if logger == nil {
		logger = slog.Default()
	}

	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("create telegram notifier: %w", err)
	}

	return &Notifier{api: api, logger: logger}, nil
}

func (n *Notifier) Notify(ctx context.Context, telegramID int64, text string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	_, err := n.api.Send(tgbotapi.NewMessage(telegramID, text))
	return err
}
