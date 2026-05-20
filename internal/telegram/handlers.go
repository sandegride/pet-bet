package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/shopspring/decimal"

	"stavki/internal/bets"
	"stavki/internal/domain"
	"stavki/internal/matches"
	"stavki/internal/settlement"
	"stavki/internal/users"
	"stavki/internal/wallet"
)

const (
	adminAddMatchExample    = "/admin_add_match Team A | Team B | Tournament | 2026-05-25 18:00 | 1.75 | 2.05"
	adminFinishMatchExample = "/admin_finish_match 1 | Team A"
	adminCancelMatchExample = "/admin_cancel_match 1"
	adminTimeLayout         = "2006-01-02 15:04"
)

type AdminAddMatchCommand struct {
	TeamA          string
	TeamB          string
	TournamentName string
	StartsAt       time.Time
	TeamAOdds      string
	TeamBOdds      string
}

type AdminFinishMatchCommand struct {
	MatchID    int64
	WinnerTeam string
}

type Handler struct {
	users   *users.Service
	wallet  *wallet.Service
	matches *matches.Service
	bets    *bets.Service
	states  *StateStore
	logger  *slog.Logger
}

func NewHandler(
	usersService *users.Service,
	walletService *wallet.Service,
	matchesService *matches.Service,
	betsService *bets.Service,
	states *StateStore,
	logger *slog.Logger,
) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	if states == nil {
		states = NewStateStore()
	}

	return &Handler{
		users:   usersService,
		wallet:  walletService,
		matches: matchesService,
		bets:    betsService,
		states:  states,
		logger:  logger,
	}
}

func (h *Handler) HandleUpdate(ctx context.Context, api *tgbotapi.BotAPI, update tgbotapi.Update) {
	if update.CallbackQuery != nil {
		h.handleCallback(ctx, api, update.CallbackQuery)
		return
	}

	if update.Message == nil {
		return
	}

	if update.Message.IsCommand() {
		h.handleCommand(ctx, api, update.Message)
		return
	}

	h.handleText(ctx, api, update.Message)
}

func (h *Handler) handleCommand(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	switch msg.Command() {
	case "start":
		h.handleStart(ctx, api, msg)
	case "balance":
		h.handleBalance(ctx, api, msg)
	case "next":
		h.handleNext(ctx, api, msg)
	case "history":
		h.handleHistory(ctx, api, msg)
	case "help":
		h.sendText(api, msg.Chat.ID, helpText())
	case "admin_add_match":
		h.handleAdminAddMatch(ctx, api, msg)
	case "admin_finish_match":
		h.handleAdminFinishMatch(ctx, api, msg)
	case "admin_cancel_match":
		h.handleAdminCancelMatch(ctx, api, msg)
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
		"Привет, %s!\nПрофиль готов. Баланс: %d виртуальных монет.\n\nОткрой /next, чтобы посмотреть ближайший матч.",
		displayName(user),
		user.Balance,
	))
}

func (h *Handler) handleBalance(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	user, err := h.users.GetByTelegramID(ctx, msg.From.ID)
	if err != nil {
		h.replyError(api, msg.Chat.ID, "Сначала создай профиль через /start.", err)
		return
	}

	balance, err := h.wallet.GetBalance(ctx, user.ID)
	if err != nil {
		h.replyError(api, msg.Chat.ID, "Не удалось получить баланс.", err)
		return
	}

	h.sendText(api, msg.Chat.ID, fmt.Sprintf("Баланс: %d виртуальных монет.", balance))
}

func (h *Handler) handleNext(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	match, err := h.matches.GetNextUpcoming(ctx)
	if err != nil {
		if errors.Is(err, matches.ErrNotFound) {
			h.sendText(api, msg.Chat.ID, "Ближайших матчей пока нет.")
			return
		}
		h.replyError(api, msg.Chat.ID, "Не удалось получить матч.", err)
		return
	}

	message := tgbotapi.NewMessage(msg.Chat.ID, formatMatch(match))
	keyboard := nextMatchKeyboard(match)
	message.ReplyMarkup = keyboard
	if _, err := api.Send(message); err != nil {
		h.logger.Error("send next match", "error", err)
	}
}

func (h *Handler) handleHistory(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	history, err := h.bets.GetUserHistory(ctx, msg.From.ID, 10)
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
			"\n#%d: %s vs %s\nВыбор: %s | Сумма: %d | Кэф: %s | Потенциально: %d | Статус: %s",
			item.ID,
			item.TeamA,
			item.TeamB,
			item.SelectedTeam,
			item.Amount,
			item.Odds,
			item.PotentialPayout,
			item.Status,
		)
	}

	h.sendText(api, msg.Chat.ID, b.String())
}

func (h *Handler) handleCallback(ctx context.Context, api *tgbotapi.BotAPI, callback *tgbotapi.CallbackQuery) {
	if _, err := api.Request(tgbotapi.NewCallback(callback.ID, "")); err != nil {
		h.logger.Error("answer callback", "error", err)
	}

	if callback.Message == nil {
		return
	}

	parts := strings.Split(callback.Data, ":")
	if len(parts) != 3 || parts[0] != "bet" {
		h.sendText(api, callback.Message.Chat.ID, "Неизвестное действие.")
		return
	}

	matchID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || matchID <= 0 {
		h.sendText(api, callback.Message.Chat.ID, "Некорректный матч.")
		return
	}

	if _, err := h.users.GetByTelegramID(ctx, callback.From.ID); err != nil {
		h.sendText(api, callback.Message.Chat.ID, "Сначала создай профиль через /start.")
		return
	}

	match, err := h.matches.GetByID(ctx, matchID)
	if err != nil {
		h.replyError(api, callback.Message.Chat.ID, "Матч не найден.", err)
		return
	}

	var selectedTeam string
	switch parts[2] {
	case "a":
		selectedTeam = match.TeamA
	case "b":
		selectedTeam = match.TeamB
	default:
		h.sendText(api, callback.Message.Chat.ID, "Некорректная команда.")
		return
	}

	h.states.SetBetState(callback.From.ID, BetState{MatchID: match.ID, SelectedTeam: selectedTeam})
	h.sendText(api, callback.Message.Chat.ID, fmt.Sprintf("Введите сумму ставки на %s целым числом.", selectedTeam))
}

func (h *Handler) handleText(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	state, ok := h.states.GetBetState(msg.From.ID)
	if !ok {
		h.sendText(api, msg.Chat.ID, "Я не понял сообщение. Используй /help.")
		return
	}

	amount, err := strconv.ParseInt(strings.TrimSpace(msg.Text), 10, 64)
	if err != nil || amount <= 0 {
		h.sendText(api, msg.Chat.ID, "Введите положительное целое число.")
		return
	}

	bet, err := h.bets.PlaceBet(ctx, msg.From.ID, state.MatchID, state.SelectedTeam, amount)
	if err != nil {
		h.states.Clear(msg.From.ID)
		h.sendText(api, msg.Chat.ID, friendlyError(err))
		return
	}
	h.states.Clear(msg.From.ID)

	h.sendText(api, msg.Chat.ID, fmt.Sprintf(
		"Ставка принята.\nКоманда: %s\nСумма: %d\nКэф: %s\nПотенциальный выигрыш: %d",
		bet.SelectedTeam,
		bet.Amount,
		bet.Odds,
		bet.PotentialPayout,
	))
}

func (h *Handler) handleAdminAddMatch(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	if !h.ensureAdmin(ctx, api, msg) {
		return
	}

	command, err := ParseAdminAddMatchArgs(msg.CommandArguments())
	if err != nil {
		h.sendText(api, msg.Chat.ID, fmt.Sprintf("Неверный формат.\nПример:\n%s", adminAddMatchExample))
		return
	}

	match, err := h.matches.CreateMatch(
		ctx,
		command.TournamentName,
		command.TeamA,
		command.TeamB,
		command.StartsAt,
		command.TeamAOdds,
		command.TeamBOdds,
	)
	if err != nil {
		h.replyError(api, msg.Chat.ID, "Не удалось создать матч.", err)
		return
	}

	h.sendText(api, msg.Chat.ID, fmt.Sprintf(
		"Матч создан: #%d\n%s vs %s\nСтарт: %s\nКэфы: %s / %s",
		match.ID,
		match.TeamA,
		match.TeamB,
		match.StartsAt.Format(adminTimeLayout),
		match.TeamAOdds,
		match.TeamBOdds,
	))
}

func (h *Handler) handleAdminFinishMatch(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	if !h.ensureAdmin(ctx, api, msg) {
		return
	}

	command, err := ParseAdminFinishMatchArgs(msg.CommandArguments())
	if err != nil {
		h.sendText(api, msg.Chat.ID, fmt.Sprintf("Неверный формат.\nПример:\n%s", adminFinishMatchExample))
		return
	}

	if err := h.matches.FinishMatch(ctx, command.MatchID, command.WinnerTeam); err != nil {
		h.replyError(api, msg.Chat.ID, "Не удалось завершить матч.", err)
		return
	}

	h.sendText(api, msg.Chat.ID, "Матч завершён, ставки рассчитаны.")
}

func (h *Handler) handleAdminCancelMatch(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	if !h.ensureAdmin(ctx, api, msg) {
		return
	}

	matchID, err := ParseAdminCancelMatchArgs(msg.CommandArguments())
	if err != nil {
		h.sendText(api, msg.Chat.ID, fmt.Sprintf("Неверный формат.\nПример:\n%s", adminCancelMatchExample))
		return
	}

	if err := h.matches.CancelMatch(ctx, matchID); err != nil {
		h.replyError(api, msg.Chat.ID, "Не удалось отменить матч.", err)
		return
	}

	h.sendText(api, msg.Chat.ID, "Матч отменён, pending-ставки возвращены.")
}

func (h *Handler) ensureAdmin(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) bool {
	isAdmin, err := h.users.IsAdmin(ctx, msg.From.ID)
	if err != nil {
		h.replyError(api, msg.Chat.ID, "Не удалось проверить права администратора.", err)
		return false
	}
	if !isAdmin {
		h.sendText(api, msg.Chat.ID, "Нет доступа.")
		return false
	}

	return true
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

func ParseAdminAddMatchArgs(args string) (AdminAddMatchCommand, error) {
	parts := splitPipeArgs(args)
	if len(parts) != 6 {
		return AdminAddMatchCommand{}, errors.New("invalid parts count")
	}

	startsAt, err := time.ParseInLocation(adminTimeLayout, parts[3], time.Local)
	if err != nil {
		return AdminAddMatchCommand{}, err
	}

	if parts[0] == "" || parts[1] == "" || strings.EqualFold(parts[0], parts[1]) {
		return AdminAddMatchCommand{}, errors.New("invalid teams")
	}

	teamAOdds, err := normalizeCommandOdds(parts[4])
	if err != nil {
		return AdminAddMatchCommand{}, err
	}
	teamBOdds, err := normalizeCommandOdds(parts[5])
	if err != nil {
		return AdminAddMatchCommand{}, err
	}

	return AdminAddMatchCommand{
		TeamA:          parts[0],
		TeamB:          parts[1],
		TournamentName: parts[2],
		StartsAt:       startsAt,
		TeamAOdds:      teamAOdds,
		TeamBOdds:      teamBOdds,
	}, nil
}

func ParseAdminFinishMatchArgs(args string) (AdminFinishMatchCommand, error) {
	parts := splitPipeArgs(args)
	if len(parts) != 2 {
		return AdminFinishMatchCommand{}, errors.New("invalid parts count")
	}

	matchID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || matchID <= 0 {
		return AdminFinishMatchCommand{}, errors.New("invalid match id")
	}
	if parts[1] == "" {
		return AdminFinishMatchCommand{}, errors.New("empty winner")
	}

	return AdminFinishMatchCommand{MatchID: matchID, WinnerTeam: parts[1]}, nil
}

func ParseAdminCancelMatchArgs(args string) (int64, error) {
	parts := splitPipeArgs(args)
	if len(parts) != 1 {
		return 0, errors.New("invalid parts count")
	}

	matchID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || matchID <= 0 {
		return 0, errors.New("invalid match id")
	}

	return matchID, nil
}

func splitPipeArgs(args string) []string {
	rawParts := strings.Split(args, "|")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		parts = append(parts, strings.TrimSpace(part))
	}

	return parts
}

func normalizeCommandOdds(value string) (string, error) {
	odds, err := decimal.NewFromString(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	if odds.LessThan(decimal.NewFromInt(1)) {
		return "", errors.New("odds must be at least 1.00")
	}

	return odds.StringFixed(2), nil
}

func friendlyError(err error) string {
	switch {
	case errors.Is(err, users.ErrNotFound):
		return "Сначала создай профиль через /start."
	case errors.Is(err, wallet.ErrInsufficientFunds):
		return "Недостаточно виртуальных монет на балансе."
	case errors.Is(err, bets.ErrInvalidAmount):
		return "Сумма ставки должна быть положительным целым числом."
	case errors.Is(err, bets.ErrInvalidOdds), errors.Is(err, matches.ErrInvalidOdds):
		return "Коэффициент должен быть не меньше 1.00."
	case errors.Is(err, bets.ErrUserBlocked):
		return "Профиль заблокирован."
	case errors.Is(err, bets.ErrMatchNotUpcoming):
		return "На этот матч уже нельзя поставить."
	case errors.Is(err, bets.ErrBettingClosed):
		return "Приём ставок на этот матч закрыт."
	case errors.Is(err, bets.ErrInvalidSelectedTeam), errors.Is(err, settlement.ErrInvalidWinner), errors.Is(err, matches.ErrInvalidTeam):
		return "Команда указана некорректно."
	case errors.Is(err, matches.ErrNotFound):
		return "Матч не найден."
	case errors.Is(err, settlement.ErrMatchCanceled):
		return "Матч отменён."
	case errors.Is(err, settlement.ErrMatchSettled):
		return "Матч уже рассчитан."
	default:
		return "Попробуй ещё раз позже."
	}
}

func formatMatch(match domain.MatchWithOdds) string {
	tournament := match.TournamentName
	if tournament == "" {
		tournament = "Без турнира"
	}

	return fmt.Sprintf(
		"Ближайший матч #%d\nТурнир: %s\n%s vs %s\nСтарт: %s\nКэфы: %s — %s, %s — %s",
		match.ID,
		tournament,
		match.TeamA,
		match.TeamB,
		match.StartsAt.Local().Format(adminTimeLayout),
		match.TeamA,
		match.TeamAOdds,
		match.TeamB,
		match.TeamBOdds,
	)
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
		"/balance — баланс",
		"/next — ближайший матч",
		"/history — история ставок",
		"/help — помощь",
		"",
		"Админ:",
		"/admin_add_match Team A | Team B | Tournament | 2026-05-25 18:00 | 1.75 | 2.05",
		"/admin_finish_match 1 | Team A",
		"/admin_cancel_match 1",
	}, "\n")
}
