package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"stavki/internal/domain"
	"stavki/internal/selfbets"
	"stavki/internal/users"
	"stavki/internal/wallet"
)

type Handler struct {
	users    *users.Service
	wallet   *wallet.Service
	selfbets *selfbets.Service
	logger   *slog.Logger
}

func NewHandler(
	usersService *users.Service,
	walletService *wallet.Service,
	selfBetsService *selfbets.Service,
	logger *slog.Logger,
) *Handler {
	if logger == nil {
		logger = slog.Default()
	}

	return &Handler{
		users:    usersService,
		wallet:   walletService,
		selfbets: selfBetsService,
		logger:   logger,
	}
}

func (h *Handler) HandleUpdate(ctx context.Context, api *tgbotapi.BotAPI, update tgbotapi.Update) {
	if update.CallbackQuery != nil {
		if _, err := api.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "")); err != nil {
			h.logger.Error("answer callback", "error", err)
		}
		return
	}

	if update.Message == nil {
		return
	}

	if !update.Message.IsCommand() {
		h.sendText(api, update.Message.Chat.ID, "Я понимаю команды. Используй /help.")
		return
	}

	h.handleCommand(ctx, api, update.Message)
}

func (h *Handler) handleCommand(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	switch msg.Command() {
	case "start":
		h.handleStart(ctx, api, msg)
	case "link_dota":
		h.handleLinkDota(ctx, api, msg)
	case "balance":
		h.handleBalance(ctx, api, msg)
	case "bet":
		h.handleBet(ctx, api, msg)
	case "active_bet":
		h.handleActiveBet(ctx, api, msg)
	case "cancel_bet":
		h.handleCancelBet(ctx, api, msg)
	case "history":
		h.handleHistory(ctx, api, msg)
	case "help":
		h.sendText(api, msg.Chat.ID, helpText())
	default:
		h.sendText(api, msg.Chat.ID, "Неизвестная команда. Используй /help.")
	}
}

func (h *Handler) handleStart(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	user, err := h.users.GetOrCreateByTelegram(ctx, msg.From.ID, msg.From.UserName, msg.From.FirstName)
	if err != nil {
		h.replyError(api, msg.Chat.ID, "Не удалось создать профиль.", err)
		return
	}

	h.sendText(api, msg.Chat.ID, fmt.Sprintf(
		"Привет, %s!\nДоступно: %d виртуальных монет.\nЗаморожено: %d.\n\nСначала привяжи Dota аккаунт:\n/link_dota <account_id>",
		displayName(user),
		user.Balance,
		user.FrozenBalance,
	))
}

func (h *Handler) handleLinkDota(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	accountID, err := parsePositiveInt64(msg.CommandArguments())
	if err != nil {
		h.sendText(api, msg.Chat.ID, "Неверный формат.\nПример: /link_dota 123456789")
		return
	}

	if _, err := h.users.GetOrCreateByTelegram(ctx, msg.From.ID, msg.From.UserName, msg.From.FirstName); err != nil {
		h.replyError(api, msg.Chat.ID, "Не удалось создать профиль.", err)
		return
	}

	result, err := h.selfbets.LinkDotaAccount(ctx, msg.From.ID, accountID)
	if err != nil {
		h.replyError(api, msg.Chat.ID, "Не удалось привязать Dota аккаунт.", err)
		return
	}

	if result.LastMatch == nil {
		h.sendText(api, msg.Chat.ID, "Аккаунт привязан. Соревновательных матчей пока не найдено: первая ставка будет рассчитана после первого найденного ranked/competitive матча.")
		return
	}

	h.sendText(api, msg.Chat.ID, fmt.Sprintf(
		"Аккаунт привязан. Последний известный матч сохранён: %d.\nТеперь можно поставить на следующий соревновательный матч: /bet 100",
		result.LastMatch.MatchID,
	))
}

func (h *Handler) handleBalance(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	user, err := h.users.GetByTelegramID(ctx, msg.From.ID)
	if err != nil {
		h.replyError(api, msg.Chat.ID, "Сначала создай профиль через /start.", err)
		return
	}

	balances, err := h.wallet.GetBalances(ctx, user.ID)
	if err != nil {
		h.replyError(api, msg.Chat.ID, "Не удалось получить баланс.", err)
		return
	}

	h.sendText(api, msg.Chat.ID, fmt.Sprintf(
		"Баланс:\nДоступно: %d\nЗаморожено: %d\nВсего: %d",
		balances.Balance,
		balances.FrozenBalance,
		balances.Balance+balances.FrozenBalance,
	))
}

func (h *Handler) handleBet(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	amount, err := parsePositiveInt64(msg.CommandArguments())
	if err != nil {
		h.sendText(api, msg.Chat.ID, "Неверный формат.\nПример: /bet 100")
		return
	}

	bet, err := h.selfbets.PlaceNextMatchWinBet(ctx, msg.From.ID, amount)
	if err != nil {
		h.sendText(api, msg.Chat.ID, friendlyError(err))
		return
	}

	h.sendText(api, msg.Chat.ID, fmt.Sprintf(
		"Ставка принята на победу в следующем соревновательном матче.\nСумма: %d\nКэф: %s\nПотенциальная выплата: %d\nБаланс заморожен до результата матча.",
		bet.Amount,
		bet.Odds,
		bet.PotentialPayout,
	))
}

func (h *Handler) handleActiveBet(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	bet, err := h.selfbets.GetActiveBet(ctx, msg.From.ID)
	if err != nil {
		if errors.Is(err, selfbets.ErrNoActiveBet) {
			h.sendText(api, msg.Chat.ID, "Активной ставки нет.")
			return
		}
		h.replyError(api, msg.Chat.ID, "Не удалось получить активную ставку.", err)
		return
	}

	h.sendText(api, msg.Chat.ID, formatSelfBet("Активная ставка", bet))
}

func (h *Handler) handleCancelBet(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	if err := h.selfbets.CancelActiveBet(ctx, msg.From.ID); err != nil {
		h.sendText(api, msg.Chat.ID, friendlyError(err))
		return
	}

	h.sendText(api, msg.Chat.ID, "Ставка отменена, замороженные монеты вернулись в доступный баланс.")
}

func (h *Handler) handleHistory(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	history, err := h.selfbets.GetHistory(ctx, msg.From.ID, 10)
	if err != nil {
		h.replyError(api, msg.Chat.ID, "Не удалось получить историю ставок.", err)
		return
	}
	if len(history) == 0 {
		h.sendText(api, msg.Chat.ID, "История ставок пока пустая.")
		return
	}

	var b strings.Builder
	b.WriteString("Последние ставки:\n")
	for _, item := range history {
		fmt.Fprintf(
			&b,
			"\n#%d | %s | сумма %d | кэф %s | выплата %d | матч %s",
			item.ID,
			item.Status,
			item.Amount,
			item.Odds,
			item.PotentialPayout,
			formatOptionalMatchID(item.TargetMatchID),
		)
	}

	h.sendText(api, msg.Chat.ID, b.String())
}

func (h *Handler) sendText(api *tgbotapi.BotAPI, chatID int64, text string) {
	if _, err := api.Send(tgbotapi.NewMessage(chatID, text)); err != nil {
		h.logger.Error("send message", "error", err)
	}
}

func (h *Handler) replyError(api *tgbotapi.BotAPI, chatID int64, message string, err error) {
	h.logger.Error(message, "error", err)
	h.sendText(api, chatID, fmt.Sprintf("%s\n%s", message, friendlyError(err)))
}

func parsePositiveInt64(value string) (int64, error) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed <= 0 {
		return 0, errors.New("invalid positive int64")
	}

	return parsed, nil
}

func friendlyError(err error) string {
	switch {
	case errors.Is(err, users.ErrNotFound), errors.Is(err, selfbets.ErrUserNotFound):
		return "Сначала создай профиль через /start."
	case errors.Is(err, selfbets.ErrDotaNotLinked):
		return "Сначала привяжи Dota аккаунт: /link_dota <account_id>."
	case errors.Is(err, selfbets.ErrActiveBetExists):
		return "У тебя уже есть активная ставка. Посмотри её через /active_bet."
	case errors.Is(err, selfbets.ErrNoActiveBet):
		return "Активной ставки нет."
	case errors.Is(err, selfbets.ErrInvalidAmount), errors.Is(err, wallet.ErrInvalidAmount):
		return "Сумма должна быть положительным целым числом."
	case errors.Is(err, wallet.ErrInsufficientFunds):
		return "Недостаточно доступных виртуальных монет."
	case errors.Is(err, wallet.ErrInsufficientFrozen):
		return "Недостаточно замороженных виртуальных монет."
	case errors.Is(err, selfbets.ErrBetAlreadyTargeted):
		return "Ставку уже нельзя отменить: она привязана к найденному матчу."
	case errors.Is(err, selfbets.ErrInvalidAccountID):
		return "Dota account id должен быть положительным числом."
	case errors.Is(err, selfbets.ErrHistoryAdvanced):
		return "В истории уже появился новый соревновательный матч. Я обновил сохранённый match id; повтори /bet, чтобы поставить на следующий матч."
	default:
		return "Попробуй ещё раз позже."
	}
}

func formatSelfBet(title string, bet domain.SelfBet) string {
	return fmt.Sprintf(
		"%s:\nСумма: %d\nКэф: %s\nПотенциальная выплата: %d\nСтатус: %s\nСоздана: %s",
		title,
		bet.Amount,
		bet.Odds,
		bet.PotentialPayout,
		bet.Status,
		bet.CreatedAt.Local().Format("2006-01-02 15:04"),
	)
}

func formatOptionalMatchID(matchID *int64) string {
	if matchID == nil {
		return "ожидается"
	}
	return strconv.FormatInt(*matchID, 10)
}

func displayName(user domain.User) string {
	if user.FirstName != "" {
		return user.FirstName
	}
	if user.Username != "" {
		return user.Username
	}
	return "игрок"
}

func helpText() string {
	return strings.Join([]string{
		"Команды:",
		"/start — создать профиль",
		"/link_dota <account_id> — привязать Dota аккаунт",
		"/balance — доступный и замороженный баланс",
		"/bet <amount> — поставить на победу в следующем ranked/competitive матче",
		"/active_bet — активная ставка",
		"/cancel_bet — отменить ставку до найденного матча",
		"/history — история ставок",
		"/help — помощь",
	}, "\n")
}
