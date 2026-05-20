package telegram

import (
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"stavki/internal/domain"
)

func nextMatchKeyboard(match domain.MatchWithOdds) tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("Поставить на %s", match.TeamA),
				fmt.Sprintf("bet:%d:a", match.ID),
			),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("Поставить на %s", match.TeamB),
				fmt.Sprintf("bet:%d:b", match.ID),
			),
		),
	)
}
