package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"stavki/internal/admin"
	"stavki/internal/cs"
	"stavki/internal/csbets"
	"stavki/internal/domain"
	"stavki/internal/dota"
	"stavki/internal/selfbets"
	"stavki/internal/users"
	"stavki/internal/wallet"
)

// gameDota / gameCS — идентификаторы игр, используются в callback_data ("game:dota", "game:cs", ...).
const (
	gameDota = "dota"
	gameCS   = "cs"
)

// Подписи кнопок постоянной клавиатуры внизу экрана — как обычное меню/таб-бар
// в приложениях: всегда на виду, не нужно листать чат или жать /start.
const (
	btnDota    = "🎮 Dota 2"
	btnCS      = "🔫 CS2"
	btnBalance = "💰 Баланс"
	btnTopUp   = "💳 Пополнить"
	btnHelp    = "❓ Помощь"
	btnMenu    = "🏠 Меню"
	btnAdmin   = "🔧 Админ"
)

// mainReplyKeyboard — постоянная клавиатура внизу чата (Reply Keyboard).
// В отличие от инлайн-кнопок под сообщением, она не пропадает при прокрутке
// и не заменяется другими сообщениями — остаётся на экране всегда, как нижнее
// меню в обычных приложениях. Кнопка «🔧 Админ» показывается только администраторам.
func mainReplyKeyboard(isAdmin bool) tgbotapi.ReplyKeyboardMarkup {
	rows := [][]tgbotapi.KeyboardButton{
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(btnDota),
			tgbotapi.NewKeyboardButton(btnCS),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(btnBalance),
			tgbotapi.NewKeyboardButton(btnTopUp),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(btnHelp),
			tgbotapi.NewKeyboardButton(btnMenu),
		),
	}
	if isAdmin {
		rows = append(rows, tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(btnAdmin),
		))
	}

	kb := tgbotapi.NewReplyKeyboard(rows...)
	kb.ResizeKeyboard = true
	return kb
}

// pendingInput хранит ожидаемый свободный текстовый ввод от пользователя:
// привязка аккаунта, своя сумма ставки или ввод администратора.
// target — необязательный telegram_id игрока, к которому относится ввод
// (например, сумма начисления/списания конкретному игроку).
type pendingInput struct {
	action string
	target int64
}

const (
	pendingLinkDota          = "link_dota"
	pendingLinkCS            = "link_cs"
	pendingAmountDota        = "amount_dota"
	pendingAmountCS          = "amount_cs"
	pendingAdminSetWinOdds   = "set_win_odds"
	pendingAdminSetKillsOdds = "set_kills_odds"
	pendingAdminSetFBOdds    = "set_fb_odds"
	pendingAdminFindUser     = "admin_find_user"
	pendingAdminCredit       = "admin_credit"
	pendingAdminDebit        = "admin_debit"
	pendingAdminSetMinMMR    = "set_min_mmr"
	pendingAdminSetCSWin     = "set_cs_win_odds"
	pendingAdminSetCSKills   = "set_cs_kills_odds"
)

type Handler struct {
	users        *users.Service
	wallet       *wallet.Service
	selfbets     *selfbets.Service // Dota 2
	csbets       *csbets.Service   // CS2 (FACEIT)
	adminService *admin.Service
	adminIDs     map[int64]bool
	logger       *slog.Logger

	mu            sync.Mutex
	pendingInputs map[int64]*pendingInput
}

func NewHandler(
	usersService *users.Service,
	walletService *wallet.Service,
	selfBetsService *selfbets.Service,
	csBetsService *csbets.Service,
	adminService *admin.Service,
	adminIDs []int64,
	logger *slog.Logger,
) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	ids := make(map[int64]bool, len(adminIDs))
	for _, id := range adminIDs {
		ids[id] = true
	}
	return &Handler{
		users:         usersService,
		wallet:        walletService,
		selfbets:      selfBetsService,
		csbets:        csBetsService,
		adminService:  adminService,
		adminIDs:      ids,
		logger:        logger,
		pendingInputs: make(map[int64]*pendingInput),
	}
}

func (h *Handler) isAdmin(telegramID int64) bool {
	return h.adminIDs[telegramID]
}

func (h *Handler) HandleUpdate(ctx context.Context, api *tgbotapi.BotAPI, update tgbotapi.Update) {
	if update.CallbackQuery != nil {
		h.handleCallback(ctx, api, update.CallbackQuery)
		if _, err := api.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "")); err != nil {
			h.logger.Error("answer callback", "error", err)
		}
		return
	}
	if update.Message == nil {
		return
	}

	if update.Message.IsCommand() {
		h.handleCommand(ctx, api, update.Message)
		return
	}

	h.handlePlainText(ctx, api, update.Message)
}

// handlePlainText обрабатывает обычный текст: привязку аккаунта, свою сумму ставки
// или ожидаемый ввод администратора. Это основной способ привязки аккаунта —
// без команд, просто отправь свой ID одним сообщением.
func (h *Handler) handlePlainText(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	telegramID := msg.From.ID
	text := strings.TrimSpace(msg.Text)

	// Кнопки постоянной клавиатуры внизу экрана имеют приоритет над любым
	// ожидаемым вводом — нажатие "🏠 Меню" и т.п. должно отменять текущий шаг
	// (например, ожидание ID для привязки), а не пытаться распарсить его как ID.
	if h.handleMenuButtonText(ctx, api, msg.Chat.ID, telegramID, text) {
		h.clearPendingInput(telegramID)
		return
	}

	h.mu.Lock()
	pending, hasPending := h.pendingInputs[telegramID]
	if hasPending {
		delete(h.pendingInputs, telegramID)
	}
	h.mu.Unlock()

	if !hasPending {
		m := tgbotapi.NewMessage(msg.Chat.ID, "Не понимаю это сообщение 🤔\n\nВоспользуйся кнопками меню внизу экрана 👇")
		m.ReplyMarkup = backToMenuKeyboard()
		h.send(api, m)
		return
	}

	switch pending.action {
	case pendingLinkDota:
		h.linkDotaFromInput(ctx, api, msg.Chat.ID, telegramID, msg.From.UserName, msg.From.FirstName, msg.Text)
	case pendingLinkCS:
		h.linkCSFromInput(ctx, api, msg.Chat.ID, telegramID, msg.From.UserName, msg.From.FirstName, msg.Text)
	case pendingAmountDota:
		h.handleCustomAmount(ctx, api, msg.Chat.ID, gameDota, msg.Text)
	case pendingAmountCS:
		h.handleCustomAmount(ctx, api, msg.Chat.ID, gameCS, msg.Text)
	default:
		if h.isAdmin(telegramID) {
			h.handleAdminInput(ctx, api, msg, pending)
			return
		}
		m := tgbotapi.NewMessage(msg.Chat.ID, "Не понимаю это сообщение 🤔")
		m.ReplyMarkup = backToMenuKeyboard()
		h.send(api, m)
	}
}

func (h *Handler) handleCommand(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	switch msg.Command() {
	case "start", "menu":
		h.handleStart(ctx, api, msg)
	case "link_dota":
		h.handleLinkDota(ctx, api, msg)
	case "link_cs":
		h.handleLinkCSCommand(ctx, api, msg)
	case "balance":
		h.handleBalance(ctx, api, msg)
	case "bet":
		h.handleBet(ctx, api, msg)
	case "bet_win":
		h.handleBetWin(ctx, api, msg)
	case "bet_kills":
		h.handleBetKills(ctx, api, msg)
	case "bet_fb":
		h.handleBetFB(ctx, api, msg)
	case "active_bet":
		h.handleActiveBet(ctx, api, msg)
	case "cancel_bet":
		h.handleCancelBet(ctx, api, msg)
	case "history":
		h.handleHistory(ctx, api, msg)
	case "tutorial":
		h.handleTutorial(api, msg)
	case "help":
		h.handleHelp(api, msg)
	case "admin":
		h.handleAdmin(ctx, api, msg)
	case "admin_set_odds":
		h.handleAdminSetOdds(ctx, api, msg, "win")
	case "admin_set_kills_odds":
		h.handleAdminSetOdds(ctx, api, msg, "kills")
	case "admin_set_fb_odds":
		h.handleAdminSetOdds(ctx, api, msg, "fb")
	case "admin_set_min_mmr":
		h.handleAdminSetMinMMR(ctx, api, msg)
	case "admin_adjust":
		h.handleAdminAdjust(ctx, api, msg)
	case "admin_block":
		h.handleAdminBlock(ctx, api, msg, true)
	case "admin_unblock":
		h.handleAdminBlock(ctx, api, msg, false)
	default:
		m := tgbotapi.NewMessage(msg.Chat.ID, "Неизвестная команда.")
		m.ReplyMarkup = backToMenuKeyboard()
		h.send(api, m)
	}
}

// ─── Главное меню ─────────────────────────────────────────────────────────────

func (h *Handler) handleStart(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	if _, err := h.users.GetOrCreateByTelegram(ctx, msg.From.ID, msg.From.UserName, msg.From.FirstName); err != nil {
		h.replyError(api, msg.Chat.ID, "Не удалось создать профиль.", err)
		return
	}
	h.sendMainMenu(ctx, api, msg.Chat.ID, msg.From.ID)
}

func (h *Handler) sendMainMenu(ctx context.Context, api *tgbotapi.BotAPI, chatID, telegramID int64) {
	user, err := h.users.GetByTelegramID(ctx, telegramID)
	if err != nil {
		h.replyError(api, chatID, "Не удалось загрузить профиль.", err)
		return
	}

	text := fmt.Sprintf(
		"👋 %s\n\n💰 Баланс: %d монет\n\nВыбирай в меню внизу экрана 👇",
		displayName(user), user.Balance,
	)

	m := tgbotapi.NewMessage(chatID, text)
	m.ReplyMarkup = mainReplyKeyboard(h.isAdmin(telegramID))
	h.send(api, m)
}

// handleMenuButtonText обрабатывает нажатие кнопки постоянной клавиатуры внизу
// экрана (это обычное текстовое сообщение с подписью кнопки). Возвращает true,
// если сообщение было распознано как нажатие кнопки меню.
func (h *Handler) handleMenuButtonText(ctx context.Context, api *tgbotapi.BotAPI, chatID, telegramID int64, text string) bool {
	switch text {
	case btnDota:
		h.sendGameMenu(ctx, api, chatID, telegramID, gameDota)
	case btnCS:
		h.sendGameMenu(ctx, api, chatID, telegramID, gameCS)
	case btnBalance:
		h.sendBalance(ctx, api, chatID, telegramID)
	case btnTopUp:
		h.sendTopUpPlaceholder(api, chatID)
	case btnHelp:
		m := tgbotapi.NewMessage(chatID, helpText())
		m.ReplyMarkup = backToMenuKeyboard()
		h.send(api, m)
	case btnMenu:
		h.sendMainMenu(ctx, api, chatID, telegramID)
	case btnAdmin:
		if !h.isAdmin(telegramID) {
			return false
		}
		h.sendAdminMenu(ctx, api, chatID)
	default:
		return false
	}
	return true
}

func backToMenuKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(backToMenuRow()))
}

func backToMenuRow() tgbotapi.InlineKeyboardButton {
	return tgbotapi.NewInlineKeyboardButtonData("🏠 В меню", "menu")
}

func withBackRow(rows ...[]tgbotapi.InlineKeyboardButton) tgbotapi.InlineKeyboardMarkup {
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(backToMenuRow()))
	return tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func gameTitle(game string) string {
	if game == gameCS {
		return "🔫 CS2"
	}
	return "🎮 Dota 2"
}

// ─── Игровое меню (Dota 2 / CS2) ──────────────────────────────────────────────

func (h *Handler) sendGameMenu(ctx context.Context, api *tgbotapi.BotAPI, chatID, telegramID int64, game string) {
	user, err := h.users.GetByTelegramID(ctx, telegramID)
	if err != nil {
		h.replyError(api, chatID, "Не удалось загрузить профиль.", err)
		return
	}

	linked := false
	info := ""
	if game == gameCS {
		linked = user.IsCSLinked && user.CSFaceitPlayerID != nil
		if linked {
			info = fmt.Sprintf("Аккаунт: %s ✅", displayOrDash(user.CSNickname))
		}
	} else {
		linked = user.IsDotaLinked && user.DotaAccountID != nil
		if linked {
			info = fmt.Sprintf("Dota account_id: %d ✅", *user.DotaAccountID)
		}
	}

	if !linked {
		h.promptLinkAccount(api, chatID, telegramID, game)
		return
	}

	text := fmt.Sprintf("%s\n\n%s", gameTitle(game), info)
	m := tgbotapi.NewMessage(chatID, text)
	m.ReplyMarkup = withBackRow(
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("🎲 Сделать ставку", "amount_menu:"+game),
			tgbotapi.NewInlineKeyboardButtonData("👁 Активная ставка", "active_bet:"+game),
		},
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("📋 История", "history:"+game),
			tgbotapi.NewInlineKeyboardButtonData("🔁 Перепривязать", "link:"+game),
		},
	)
	h.send(api, m)
}

// promptLinkAccount просит прислать ID одним сообщением — без команд.
func (h *Handler) promptLinkAccount(api *tgbotapi.BotAPI, chatID, telegramID int64, game string) {
	var text string
	var action string
	if game == gameCS {
		text = "🔫 CS2\n\nАккаунт не привязан.\n\nПросто пришли мне одним сообщением:\n• свой SteamID64\n• ссылку на профиль Steam\n• или свой ник на FACEIT"
		action = pendingLinkCS
	} else {
		text = "🎮 Dota 2\n\nАккаунт не привязан.\n\nПросто пришли мне одним сообщением:\n• свой Dota account_id\n• свой SteamID64\n• или ссылку на профиль Steam\n\nВ Dota 2 включи: Settings → Social → Expose Public Match Data."
		action = pendingLinkDota
	}
	h.setPendingInput(telegramID, &pendingInput{action: action})

	m := tgbotapi.NewMessage(chatID, text)
	m.ReplyMarkup = backToMenuKeyboard()
	h.send(api, m)
}

func displayOrDash(value string) string {
	if value == "" {
		return "—"
	}
	return value
}

// ─── Привязка аккаунта (свободный текст) ──────────────────────────────────────

func (h *Handler) linkDotaFromInput(ctx context.Context, api *tgbotapi.BotAPI, chatID, telegramID int64, username, firstName, input string) {
	accountID, err := dota.ParseAccountIDInput(input)
	if err != nil {
		m := tgbotapi.NewMessage(chatID, "❌ Не понял этот ID.\n\nПришли Dota account_id, SteamID64 или ссылку вида\nhttps://steamcommunity.com/profiles/<id>")
		m.ReplyMarkup = backToMenuKeyboard()
		h.send(api, m)
		h.setPendingInput(telegramID, &pendingInput{action: pendingLinkDota})
		return
	}

	if _, err := h.users.GetOrCreateByTelegram(ctx, telegramID, username, firstName); err != nil {
		h.replyError(api, chatID, "Не удалось создать профиль.", err)
		return
	}

	result, err := h.selfbets.LinkDotaAccount(ctx, telegramID, accountID)
	if err != nil {
		m := tgbotapi.NewMessage(chatID, friendlyError(err))
		m.ReplyMarkup = backToMenuKeyboard()
		h.send(api, m)
		return
	}

	text := "✅ Dota аккаунт привязан!"
	if result.LastMatch != nil {
		text += fmt.Sprintf("\nПоследний матч: %d", result.LastMatch.MatchID)
	} else {
		text += "\nСоревновательных матчей не найдено — ставка будет рассчитана после первого ranked/competitive матча."
	}

	m := tgbotapi.NewMessage(chatID, text)
	m.ReplyMarkup = withBackRow([]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("🎲 Сделать ставку", "amount_menu:"+gameDota),
	})
	h.send(api, m)
}

func (h *Handler) linkCSFromInput(ctx context.Context, api *tgbotapi.BotAPI, chatID, telegramID int64, username, firstName, input string) {
	if _, err := h.users.GetOrCreateByTelegram(ctx, telegramID, username, firstName); err != nil {
		h.replyError(api, chatID, "Не удалось создать профиль.", err)
		return
	}

	result, err := h.csbets.LinkCSAccount(ctx, telegramID, strings.TrimSpace(input))
	if err != nil {
		m := tgbotapi.NewMessage(chatID, friendlyError(err))
		m.ReplyMarkup = backToMenuKeyboard()
		h.send(api, m)
		return
	}

	text := fmt.Sprintf("✅ CS2 аккаунт привязан! FACEIT: %s", displayOrDash(result.Nickname))
	if result.LastMatch == nil {
		text += "\nЗавершённых матчей не найдено — ставка будет рассчитана после первого матча."
	}

	m := tgbotapi.NewMessage(chatID, text)
	m.ReplyMarkup = withBackRow([]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("🎲 Сделать ставку", "amount_menu:"+gameCS),
	})
	h.send(api, m)
}

// handleLinkDota и handleLinkCSCommand оставлены для обратной совместимости с командами.
func (h *Handler) handleLinkDota(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	h.linkDotaFromInput(ctx, api, msg.Chat.ID, msg.From.ID, msg.From.UserName, msg.From.FirstName, msg.CommandArguments())
}

func (h *Handler) handleLinkCSCommand(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	h.linkCSFromInput(ctx, api, msg.Chat.ID, msg.From.ID, msg.From.UserName, msg.From.FirstName, msg.CommandArguments())
}

// ─── Баланс / пополнение ──────────────────────────────────────────────────────

func (h *Handler) handleBalance(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	h.sendBalance(ctx, api, msg.Chat.ID, msg.From.ID)
}

func (h *Handler) sendBalance(ctx context.Context, api *tgbotapi.BotAPI, chatID, telegramID int64) {
	user, err := h.users.GetByTelegramID(ctx, telegramID)
	if err != nil {
		m := tgbotapi.NewMessage(chatID, "Сначала открой меню: /start")
		m.ReplyMarkup = backToMenuKeyboard()
		h.send(api, m)
		return
	}
	balances, err := h.wallet.GetBalances(ctx, user.ID)
	if err != nil {
		h.replyError(api, chatID, "Не удалось получить баланс.", err)
		return
	}

	m := tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"💰 Баланс\n\nДоступно: %d 🟢\nЗаморожено: %d 🔒\nВсего: %d",
		balances.Balance, balances.FrozenBalance, balances.Balance+balances.FrozenBalance,
	))
	m.ReplyMarkup = withBackRow([]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("💳 Пополнить", "topup"),
	})
	h.send(api, m)
}

func (h *Handler) sendTopUpPlaceholder(api *tgbotapi.BotAPI, chatID int64) {
	m := tgbotapi.NewMessage(chatID, "💳 Пополнение баланса\n\nСкоро будет доступно.\n\nНапоминание: монеты в боте виртуальные — без реальных денег.")
	m.ReplyMarkup = backToMenuKeyboard()
	h.send(api, m)
}

// ─── Выбор суммы ставки ───────────────────────────────────────────────────────

var presetAmounts = []int64{50, 100, 250, 500, 1000}

func (h *Handler) sendAmountMenu(api *tgbotapi.BotAPI, chatID int64, game string) {
	var rows [][]tgbotapi.InlineKeyboardButton
	var row []tgbotapi.InlineKeyboardButton
	for i, amount := range presetAmounts {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData(
			strconv.FormatInt(amount, 10), fmt.Sprintf("amount:%s:%d", game, amount),
		))
		if (i+1)%3 == 0 {
			rows = append(rows, row)
			row = nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("✏️ Своя сумма", "amount_custom:"+game),
	))

	m := tgbotapi.NewMessage(chatID, fmt.Sprintf("%s\n\n🎲 Выбери сумму ставки:", gameTitle(game)))
	m.ReplyMarkup = withBackRow(rows...)
	h.send(api, m)
}

func (h *Handler) promptCustomAmount(telegramID int64, api *tgbotapi.BotAPI, chatID int64, game string) {
	action := pendingAmountDota
	if game == gameCS {
		action = pendingAmountCS
	}
	h.setPendingInput(telegramID, &pendingInput{action: action})

	m := tgbotapi.NewMessage(chatID, "Пришли сумму ставки числом, например: 150")
	m.ReplyMarkup = backToMenuKeyboard()
	h.send(api, m)
}

func (h *Handler) handleCustomAmount(ctx context.Context, api *tgbotapi.BotAPI, chatID int64, game string, input string) {
	amount, err := parsePositiveInt64(input)
	if err != nil {
		m := tgbotapi.NewMessage(chatID, "❌ Нужно положительное число. Пример: 150")
		m.ReplyMarkup = backToMenuKeyboard()
		h.send(api, m)
		return
	}
	h.sendBetTypeKeyboard(ctx, api, chatID, game, amount)
}

// ─── Команда /bet (Dota, обратная совместимость) ──────────────────────────────

func (h *Handler) handleBet(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	arg := strings.TrimSpace(msg.CommandArguments())
	if arg == "" {
		h.sendAmountMenu(api, msg.Chat.ID, gameDota)
		return
	}
	amount, err := parsePositiveInt64(arg)
	if err != nil {
		m := tgbotapi.NewMessage(msg.Chat.ID, "❌ Неверная сумма. Пример: /bet 100")
		m.ReplyMarkup = backToMenuKeyboard()
		h.send(api, m)
		return
	}
	h.sendBetTypeKeyboard(ctx, api, msg.Chat.ID, gameDota, amount)
}

// ─── Тип ставки ───────────────────────────────────────────────────────────────

func (h *Handler) sendBetTypeKeyboard(ctx context.Context, api *tgbotapi.BotAPI, chatID int64, game string, amount int64) {
	if game == gameCS {
		h.sendCSBetTypeKeyboard(ctx, api, chatID, amount)
		return
	}

	winOdds, killsOdds, fbOdds := "2.00", "1.90", "1.85"
	if h.adminService != nil {
		if s, err := h.adminService.GetSettings(ctx); err == nil {
			winOdds, killsOdds, fbOdds = s.DefaultOdds, s.KillsOverOdds, s.FirstBloodOdds
		}
	}

	text := fmt.Sprintf(
		"🎲 Ставка: %d монет\n\nВыбери тип:\n\n🏆 Победа (×%s) — выиграй следующий матч\n💀 Тотал (×%s) — сумма килов > порога\n🩸 ФБ (×%s) — угадай первую кровь",
		amount, winOdds, killsOdds, fbOdds,
	)
	m := tgbotapi.NewMessage(chatID, text)
	m.ReplyMarkup = withBackRow(
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("🏆 Победа ×%s", winOdds),
				fmt.Sprintf("place_win:%s:%d", gameDota, amount),
			),
		},
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("💀 Тотал ×%s", killsOdds),
				fmt.Sprintf("kills_menu:%s:%d", gameDota, amount),
			),
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("🩸 ФБ ×%s", fbOdds),
				fmt.Sprintf("fb_menu:%d", amount),
			),
		},
	)
	h.send(api, m)
}

func (h *Handler) sendCSBetTypeKeyboard(ctx context.Context, api *tgbotapi.BotAPI, chatID int64, amount int64) {
	winOdds, killsOdds := "2.00", "1.90"
	if h.adminService != nil {
		if s, err := h.adminService.GetSettings(ctx); err == nil {
			winOdds, killsOdds = s.CSDefaultOdds, s.CSKillsOverOdds
		}
	}

	text := fmt.Sprintf(
		"🎲 Ставка: %d монет\n\nВыбери тип:\n\n🏆 Победа (×%s) — выиграй следующий матч\n💀 Тотал (×%s) — сумма килов в матче > порога",
		amount, winOdds, killsOdds,
	)
	m := tgbotapi.NewMessage(chatID, text)
	m.ReplyMarkup = withBackRow(
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("🏆 Победа ×%s", winOdds),
				fmt.Sprintf("place_win:%s:%d", gameCS, amount),
			),
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("💀 Тотал ×%s", killsOdds),
				fmt.Sprintf("kills_menu:%s:%d", gameCS, amount),
			),
		},
	)
	h.send(api, m)
}

func (h *Handler) sendKillsThresholdKeyboard(api *tgbotapi.BotAPI, chatID int64, game string, amount int64) {
	thresholds := []int64{25, 30, 35, 40, 45, 50, 55, 60}
	var rows [][]tgbotapi.InlineKeyboardButton
	var row []tgbotapi.InlineKeyboardButton
	for i, t := range thresholds {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf(">%d", t),
			fmt.Sprintf("place_kills:%s:%d:%d", game, amount, t),
		))
		if (i+1)%4 == 0 || i == len(thresholds)-1 {
			rows = append(rows, row)
			row = nil
		}
	}

	m := tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"%s\n\n💀 Тотал килов, ставка %d монет\nВыбери порог (выиграешь если тотал > N):", gameTitle(game), amount))
	m.ReplyMarkup = withBackRow(rows...)
	h.send(api, m)
}

func (h *Handler) sendFirstBloodKeyboard(api *tgbotapi.BotAPI, chatID int64, amount int64) {
	m := tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"🎮 Dota 2\n\n🩸 Первая кровь, ставка %d монет\nВыбери команду:", amount))
	m.ReplyMarkup = withBackRow([]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("🌿 Радиант", fmt.Sprintf("place_fb:%d:radiant", amount)),
		tgbotapi.NewInlineKeyboardButtonData("😈 Дайр", fmt.Sprintf("place_fb:%d:dire", amount)),
	})
	h.send(api, m)
}

// ─── Команды /bet_win, /bet_kills, /bet_fb (Dota, обратная совместимость) ─────

func (h *Handler) handleBetWin(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	amount, err := parsePositiveInt64(msg.CommandArguments())
	if err != nil {
		h.sendText(api, msg.Chat.ID, "❌ Пример: /bet_win 100")
		return
	}
	h.placeDotaBetWin(ctx, api, msg.Chat.ID, msg.From.ID, amount)
}

func (h *Handler) handleBetKills(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	parts := strings.Fields(msg.CommandArguments())
	if len(parts) != 2 {
		h.sendText(api, msg.Chat.ID, "❌ Пример: /bet_kills 100 50\n(ставка 100, тотал > 50)")
		return
	}
	amount, err1 := parsePositiveInt64(parts[0])
	threshold, err2 := parsePositiveInt64(parts[1])
	if err1 != nil || err2 != nil {
		h.sendText(api, msg.Chat.ID, "❌ Оба числа должны быть положительными.")
		return
	}
	h.placeDotaBetKills(ctx, api, msg.Chat.ID, msg.From.ID, amount, threshold)
}

func (h *Handler) handleBetFB(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	parts := strings.Fields(msg.CommandArguments())
	if len(parts) != 2 {
		h.sendText(api, msg.Chat.ID, "❌ Пример:\n/bet_fb 100 radiant\n/bet_fb 100 dire")
		return
	}
	amount, err := parsePositiveInt64(parts[0])
	if err != nil {
		h.sendText(api, msg.Chat.ID, "❌ Сумма должна быть положительным числом.")
		return
	}
	var prediction domain.SelfBetPrediction
	switch strings.ToLower(parts[1]) {
	case "radiant", "р", "радиант":
		prediction = domain.SelfBetPredictionFirstBloodRadiant
	case "dire", "д", "дайр":
		prediction = domain.SelfBetPredictionFirstBloodDire
	default:
		h.sendText(api, msg.Chat.ID, "❌ Команда: radiant или dire")
		return
	}
	h.placeDotaBetFB(ctx, api, msg.Chat.ID, msg.From.ID, amount, prediction)
}

// ─── Размещение ставок: Dota 2 ─────────────────────────────────────────────────

func (h *Handler) placeDotaBetWin(ctx context.Context, api *tgbotapi.BotAPI, chatID, telegramID, amount int64) {
	bet, err := h.selfbets.PlaceNextMatchWinBet(ctx, telegramID, amount)
	if err != nil {
		m := tgbotapi.NewMessage(chatID, friendlyError(err))
		m.ReplyMarkup = backToMenuKeyboard()
		h.send(api, m)
		return
	}
	m := tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"✅ Ставка принята!\n\n🏆 Тип: победа\n💰 Сумма: %d\n📈 Коэф: %s\n🎯 Выплата: %d\n\nМонеты заморожены.",
		bet.Amount, bet.Odds, bet.PotentialPayout,
	))
	m.ReplyMarkup = activeBetKeyboard(gameDota)
	h.send(api, m)
}

func (h *Handler) placeDotaBetKills(ctx context.Context, api *tgbotapi.BotAPI, chatID, telegramID, amount, threshold int64) {
	bet, err := h.selfbets.PlaceTotalKillsBet(ctx, telegramID, amount, threshold)
	if err != nil {
		m := tgbotapi.NewMessage(chatID, friendlyError(err))
		m.ReplyMarkup = backToMenuKeyboard()
		h.send(api, m)
		return
	}
	m := tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"✅ Ставка принята!\n\n💀 Тип: тотал > %d\n💰 Сумма: %d\n📈 Коэф: %s\n🎯 Выплата: %d\n\nМонеты заморожены.",
		threshold, bet.Amount, bet.Odds, bet.PotentialPayout,
	))
	m.ReplyMarkup = activeBetKeyboard(gameDota)
	h.send(api, m)
}

func (h *Handler) placeDotaBetFB(ctx context.Context, api *tgbotapi.BotAPI, chatID, telegramID int64, amount int64, prediction domain.SelfBetPrediction) {
	bet, err := h.selfbets.PlaceFirstBloodBet(ctx, telegramID, amount, prediction)
	if err != nil {
		m := tgbotapi.NewMessage(chatID, friendlyError(err))
		m.ReplyMarkup = backToMenuKeyboard()
		h.send(api, m)
		return
	}
	teamName := "Дайр 😈"
	if prediction == domain.SelfBetPredictionFirstBloodRadiant {
		teamName = "Радиант 🌿"
	}
	m := tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"✅ Ставка принята!\n\n🩸 Тип: первая кровь (%s)\n💰 Сумма: %d\n📈 Коэф: %s\n🎯 Выплата: %d\n\nМонеты заморожены.",
		teamName, bet.Amount, bet.Odds, bet.PotentialPayout,
	))
	m.ReplyMarkup = activeBetKeyboard(gameDota)
	h.send(api, m)
}

// ─── Размещение ставок: CS2 ─────────────────────────────────────────────────────

func (h *Handler) placeCSBetWin(ctx context.Context, api *tgbotapi.BotAPI, chatID, telegramID, amount int64) {
	bet, err := h.csbets.PlaceNextMatchWinBet(ctx, telegramID, amount)
	if err != nil {
		m := tgbotapi.NewMessage(chatID, friendlyError(err))
		m.ReplyMarkup = backToMenuKeyboard()
		h.send(api, m)
		return
	}
	m := tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"✅ Ставка принята!\n\n🏆 Тип: победа\n💰 Сумма: %d\n📈 Коэф: %s\n🎯 Выплата: %d\n\nМонеты заморожены.",
		bet.Amount, bet.Odds, bet.PotentialPayout,
	))
	m.ReplyMarkup = activeBetKeyboard(gameCS)
	h.send(api, m)
}

func (h *Handler) placeCSBetKills(ctx context.Context, api *tgbotapi.BotAPI, chatID, telegramID, amount, threshold int64) {
	bet, err := h.csbets.PlaceTotalKillsBet(ctx, telegramID, amount, threshold)
	if err != nil {
		m := tgbotapi.NewMessage(chatID, friendlyError(err))
		m.ReplyMarkup = backToMenuKeyboard()
		h.send(api, m)
		return
	}
	m := tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"✅ Ставка принята!\n\n💀 Тип: тотал > %d\n💰 Сумма: %d\n📈 Коэф: %s\n🎯 Выплата: %d\n\nМонеты заморожены.",
		threshold, bet.Amount, bet.Odds, bet.PotentialPayout,
	))
	m.ReplyMarkup = activeBetKeyboard(gameCS)
	h.send(api, m)
}

func activeBetKeyboard(game string) tgbotapi.InlineKeyboardMarkup {
	return withBackRow([]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("👁 Активная ставка", "active_bet:"+game),
		tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel_bet:"+game),
	})
}

// ─── Активная ставка / отмена / история ────────────────────────────────────────

func (h *Handler) handleActiveBet(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	h.sendActiveBet(ctx, api, msg.Chat.ID, msg.From.ID, gameDota)
}

func (h *Handler) sendActiveBet(ctx context.Context, api *tgbotapi.BotAPI, chatID, telegramID int64, game string) {
	if game == gameCS {
		bet, err := h.csbets.GetActiveBet(ctx, telegramID)
		if err != nil {
			if errors.Is(err, csbets.ErrNoActiveBet) {
				m := tgbotapi.NewMessage(chatID, "Активной ставки нет.")
				m.ReplyMarkup = withBackRow([]tgbotapi.InlineKeyboardButton{
					tgbotapi.NewInlineKeyboardButtonData("🎲 Сделать ставку", "amount_menu:"+gameCS),
				})
				h.send(api, m)
				return
			}
			h.replyError(api, chatID, "Не удалось получить активную ставку.", err)
			return
		}
		m := tgbotapi.NewMessage(chatID, formatCSBet("👁 Активная ставка", bet))
		m.ReplyMarkup = withBackRow([]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("❌ Отменить ставку", "cancel_bet:"+gameCS),
		})
		h.send(api, m)
		return
	}

	bet, err := h.selfbets.GetActiveBet(ctx, telegramID)
	if err != nil {
		if errors.Is(err, selfbets.ErrNoActiveBet) {
			m := tgbotapi.NewMessage(chatID, "Активной ставки нет.")
			m.ReplyMarkup = withBackRow([]tgbotapi.InlineKeyboardButton{
				tgbotapi.NewInlineKeyboardButtonData("🎲 Сделать ставку", "amount_menu:"+gameDota),
			})
			h.send(api, m)
			return
		}
		h.replyError(api, chatID, "Не удалось получить активную ставку.", err)
		return
	}
	m := tgbotapi.NewMessage(chatID, formatSelfBet("👁 Активная ставка", bet))
	m.ReplyMarkup = withBackRow([]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("❌ Отменить ставку", "cancel_bet:"+gameDota),
	})
	h.send(api, m)
}

func (h *Handler) handleCancelBet(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	h.doCancelBet(ctx, api, msg.Chat.ID, msg.From.ID, gameDota)
}

func (h *Handler) doCancelBet(ctx context.Context, api *tgbotapi.BotAPI, chatID, telegramID int64, game string) {
	var err error
	if game == gameCS {
		err = h.csbets.CancelActiveBet(ctx, telegramID)
	} else {
		err = h.selfbets.CancelActiveBet(ctx, telegramID)
	}
	if err != nil {
		m := tgbotapi.NewMessage(chatID, friendlyError(err))
		m.ReplyMarkup = backToMenuKeyboard()
		h.send(api, m)
		return
	}
	m := tgbotapi.NewMessage(chatID, "✅ Ставка отменена. Монеты возвращены на баланс.")
	m.ReplyMarkup = backToMenuKeyboard()
	h.send(api, m)
}

func (h *Handler) handleHistory(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	h.sendHistory(ctx, api, msg.Chat.ID, msg.From.ID, gameDota)
}

func (h *Handler) sendHistory(ctx context.Context, api *tgbotapi.BotAPI, chatID, telegramID int64, game string) {
	var b strings.Builder
	b.WriteString("📋 Последние ставки:\n")
	empty := true

	if game == gameCS {
		history, err := h.csbets.GetHistory(ctx, telegramID, 10)
		if err != nil {
			h.replyError(api, chatID, "Не удалось получить историю.", err)
			return
		}
		for _, item := range history {
			empty = false
			fmt.Fprintf(&b, "\n%s #%d | %s | %d монет | ×%s | выплата %d | матч: %s",
				betStatusEmoji(item.Status), item.ID,
				predictionLabel(item.Prediction, item.KillsThreshold),
				item.Amount, item.Odds, item.PotentialPayout,
				formatOptionalCSMatchID(item.TargetMatchID),
			)
		}
	} else {
		history, err := h.selfbets.GetHistory(ctx, telegramID, 10)
		if err != nil {
			h.replyError(api, chatID, "Не удалось получить историю.", err)
			return
		}
		for _, item := range history {
			empty = false
			fmt.Fprintf(&b, "\n%s #%d | %s | %d монет | ×%s | выплата %d | матч: %s",
				betStatusEmoji(item.Status), item.ID,
				predictionLabel(item.Prediction, item.KillsThreshold),
				item.Amount, item.Odds, item.PotentialPayout,
				formatOptionalMatchID(item.TargetMatchID),
			)
		}
	}

	if empty {
		m := tgbotapi.NewMessage(chatID, "История пустая.")
		m.ReplyMarkup = withBackRow([]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("🎲 Сделать ставку", "amount_menu:"+game),
		})
		h.send(api, m)
		return
	}

	m := tgbotapi.NewMessage(chatID, b.String())
	m.ReplyMarkup = backToMenuKeyboard()
	h.send(api, m)
}

// ─── Туториал / помощь ─────────────────────────────────────────────────────────

func (h *Handler) handleTutorial(api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	m := tgbotapi.NewMessage(msg.Chat.ID, tutorialText())
	m.ReplyMarkup = backToMenuKeyboard()
	h.send(api, m)
}

func (h *Handler) handleHelp(api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	m := tgbotapi.NewMessage(msg.Chat.ID, helpText())
	m.ReplyMarkup = backToMenuKeyboard()
	h.send(api, m)
}

// ─── Callback обработчик ─────────────────────────────────────────────────────

func (h *Handler) handleCallback(ctx context.Context, api *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery) {
	chatID := cb.Message.Chat.ID
	telegramID := cb.From.ID
	parts := strings.Split(cb.Data, ":")
	action := parts[0]

	switch action {
	case "menu":
		h.sendMainMenu(ctx, api, chatID, telegramID)

	case "game":
		if len(parts) == 2 {
			h.sendGameMenu(ctx, api, chatID, telegramID, parts[1])
		}

	case "link":
		if len(parts) == 2 {
			h.promptLinkAccount(api, chatID, telegramID, parts[1])
		}

	case "balance":
		h.sendBalance(ctx, api, chatID, telegramID)

	case "topup":
		h.sendTopUpPlaceholder(api, chatID)

	case "help":
		m := tgbotapi.NewMessage(chatID, helpText())
		m.ReplyMarkup = backToMenuKeyboard()
		h.send(api, m)

	case "tutorial":
		m := tgbotapi.NewMessage(chatID, tutorialText())
		m.ReplyMarkup = backToMenuKeyboard()
		h.send(api, m)

	case "amount_menu":
		if len(parts) == 2 {
			h.sendAmountMenu(api, chatID, parts[1])
		}

	case "amount_custom":
		if len(parts) == 2 {
			h.promptCustomAmount(telegramID, api, chatID, parts[1])
		}

	case "amount":
		if len(parts) == 3 {
			amount, err := strconv.ParseInt(parts[2], 10, 64)
			if err == nil && amount > 0 {
				h.sendBetTypeKeyboard(ctx, api, chatID, parts[1], amount)
			}
		}

	case "active_bet":
		game := gameDota
		if len(parts) == 2 {
			game = parts[1]
		}
		h.sendActiveBet(ctx, api, chatID, telegramID, game)

	case "cancel_bet":
		game := gameDota
		if len(parts) == 2 {
			game = parts[1]
		}
		h.doCancelBet(ctx, api, chatID, telegramID, game)

	case "history":
		game := gameDota
		if len(parts) == 2 {
			game = parts[1]
		}
		h.sendHistory(ctx, api, chatID, telegramID, game)

	case "place_win":
		if len(parts) == 3 {
			amount, err := strconv.ParseInt(parts[2], 10, 64)
			if err == nil && amount > 0 {
				if parts[1] == gameCS {
					h.placeCSBetWin(ctx, api, chatID, telegramID, amount)
				} else {
					h.placeDotaBetWin(ctx, api, chatID, telegramID, amount)
				}
			}
		}

	case "kills_menu":
		if len(parts) == 3 {
			amount, err := strconv.ParseInt(parts[2], 10, 64)
			if err == nil && amount > 0 {
				h.sendKillsThresholdKeyboard(api, chatID, parts[1], amount)
			}
		}

	case "place_kills":
		if len(parts) == 4 {
			amount, err1 := strconv.ParseInt(parts[2], 10, 64)
			threshold, err2 := strconv.ParseInt(parts[3], 10, 64)
			if err1 == nil && err2 == nil && amount > 0 && threshold > 0 {
				if parts[1] == gameCS {
					h.placeCSBetKills(ctx, api, chatID, telegramID, amount, threshold)
				} else {
					h.placeDotaBetKills(ctx, api, chatID, telegramID, amount, threshold)
				}
			}
		}

	case "fb_menu":
		if len(parts) == 2 {
			amount, err := strconv.ParseInt(parts[1], 10, 64)
			if err == nil && amount > 0 {
				h.sendFirstBloodKeyboard(api, chatID, amount)
			}
		}

	case "place_fb":
		if len(parts) == 3 {
			amount, err := strconv.ParseInt(parts[1], 10, 64)
			if err == nil && amount > 0 {
				var prediction domain.SelfBetPrediction
				switch parts[2] {
				case "radiant":
					prediction = domain.SelfBetPredictionFirstBloodRadiant
				case "dire":
					prediction = domain.SelfBetPredictionFirstBloodDire
				default:
					return
				}
				h.placeDotaBetFB(ctx, api, chatID, telegramID, amount, prediction)
			}
		}

	// Admin callbacks
	case "admin_menu":
		if h.isAdmin(telegramID) {
			h.sendAdminMenu(ctx, api, chatID)
		}

	case "admin_toggle_solo":
		if !h.isAdmin(telegramID) || h.adminService == nil {
			return
		}
		newVal, err := h.adminService.ToggleSoloOnly(ctx)
		if err != nil {
			h.sendText(api, chatID, "❌ Ошибка: "+err.Error())
			return
		}
		h.sendText(api, chatID, fmt.Sprintf("🎯 Только соло игры: %s", boolIcon(newVal)))
		h.sendAdminMenu(ctx, api, chatID)

	case "admin_toggle_hwid":
		if !h.isAdmin(telegramID) || h.adminService == nil {
			return
		}
		newVal, err := h.adminService.ToggleHWIDRequired(ctx)
		if err != nil {
			h.sendText(api, chatID, "❌ Ошибка: "+err.Error())
			return
		}
		h.sendText(api, chatID, fmt.Sprintf("🔒 Привязка железа: %s", boolIcon(newVal)))
		h.sendAdminMenu(ctx, api, chatID)

	case "admin_prompt_win_odds":
		if h.isAdmin(telegramID) {
			h.setPendingInput(telegramID, &pendingInput{action: pendingAdminSetWinOdds})
			h.sendText(api, chatID, "Введи коэффициент для ставки «Победа» Dota (пример: 2.50):")
		}

	case "admin_prompt_kills_odds":
		if h.isAdmin(telegramID) {
			h.setPendingInput(telegramID, &pendingInput{action: pendingAdminSetKillsOdds})
			h.sendText(api, chatID, "Введи коэффициент для ставки «Тотал килов» Dota (пример: 1.90):")
		}

	case "admin_prompt_fb_odds":
		if h.isAdmin(telegramID) {
			h.setPendingInput(telegramID, &pendingInput{action: pendingAdminSetFBOdds})
			h.sendText(api, chatID, "Введи коэффициент для ставки «Первая кровь» (пример: 1.85):")
		}

	case "admin_prompt_min_mmr":
		if h.isAdmin(telegramID) {
			h.setPendingInput(telegramID, &pendingInput{action: pendingAdminSetMinMMR})
			h.sendText(api, chatID, "Введи минимальный средний MMR матча (0 = отключить):")
		}

	case "admin_prompt_cs_win_odds":
		if h.isAdmin(telegramID) {
			h.setPendingInput(telegramID, &pendingInput{action: pendingAdminSetCSWin})
			h.sendText(api, chatID, "Введи коэффициент для ставки «Победа» CS2 (пример: 2.50):")
		}

	case "admin_prompt_cs_kills_odds":
		if h.isAdmin(telegramID) {
			h.setPendingInput(telegramID, &pendingInput{action: pendingAdminSetCSKills})
			h.sendText(api, chatID, "Введи коэффициент для ставки «Тотал килов» CS2 (пример: 1.90):")
		}

	case "admin_users":
		if !h.isAdmin(telegramID) {
			return
		}
		page := 0
		if len(parts) == 2 {
			if p, err := strconv.Atoi(parts[1]); err == nil {
				page = p
			}
		}
		h.sendAdminUsersList(ctx, api, chatID, page)

	case "admin_user":
		if !h.isAdmin(telegramID) || len(parts) != 2 {
			return
		}
		if targetID, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
			h.sendAdminUserPanel(ctx, api, chatID, targetID)
		}

	case "admin_find_user":
		if h.isAdmin(telegramID) {
			h.setPendingInput(telegramID, &pendingInput{action: pendingAdminFindUser})
			m := tgbotapi.NewMessage(chatID, "Пришли Telegram ID игрока числом.")
			m.ReplyMarkup = backToMenuKeyboard()
			h.send(api, m)
		}

	case "admin_credit":
		if !h.isAdmin(telegramID) || len(parts) != 2 {
			return
		}
		if targetID, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
			h.setPendingInput(telegramID, &pendingInput{action: pendingAdminCredit, target: targetID})
			m := tgbotapi.NewMessage(chatID, fmt.Sprintf("Сколько монет начислить игроку %d? Пришли число.", targetID))
			m.ReplyMarkup = backToMenuKeyboard()
			h.send(api, m)
		}

	case "admin_debit":
		if !h.isAdmin(telegramID) || len(parts) != 2 {
			return
		}
		if targetID, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
			h.setPendingInput(telegramID, &pendingInput{action: pendingAdminDebit, target: targetID})
			m := tgbotapi.NewMessage(chatID, fmt.Sprintf("Сколько монет списать у игрока %d? Пришли число.", targetID))
			m.ReplyMarkup = backToMenuKeyboard()
			h.send(api, m)
		}

	case "admin_block_user":
		if !h.isAdmin(telegramID) || len(parts) != 2 {
			return
		}
		if targetID, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
			if err := h.users.SetBlocked(ctx, targetID, true); err != nil {
				h.sendText(api, chatID, "❌ Ошибка: "+err.Error())
				return
			}
			h.sendAdminUserPanel(ctx, api, chatID, targetID)
		}

	case "admin_unblock_user":
		if !h.isAdmin(telegramID) || len(parts) != 2 {
			return
		}
		if targetID, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
			if err := h.users.SetBlocked(ctx, targetID, false); err != nil {
				h.sendText(api, chatID, "❌ Ошибка: "+err.Error())
				return
			}
			h.sendAdminUserPanel(ctx, api, chatID, targetID)
		}
	}
}

// ─── Администратор ────────────────────────────────────────────────────────────

func (h *Handler) handleAdmin(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	if !h.isAdmin(msg.From.ID) {
		h.sendText(api, msg.Chat.ID, "❌ Нет прав администратора.")
		return
	}
	h.sendAdminMenu(ctx, api, msg.Chat.ID)
}

func (h *Handler) sendAdminMenu(ctx context.Context, api *tgbotapi.BotAPI, chatID int64) {
	var settings domain.AdminSettings
	if h.adminService != nil {
		settings, _ = h.adminService.GetSettings(ctx)
	}

	minMMR := "—"
	if settings.MinAvgMMR > 0 {
		minMMR = strconv.Itoa(settings.MinAvgMMR)
	}

	text := fmt.Sprintf(
		"🔧 Панель администратора\n\n"+
			"🎮 Dota — победа: %s | тотал: %s | ФБ: %s\n"+
			"🔫 CS2 — победа: %s | тотал: %s\n"+
			"🎯 Только соло (Dota): %s\n"+
			"📈 Мин. ММР (Dota): %s\n"+
			"🔒 Привязка железа: %s",
		settings.DefaultOdds, settings.KillsOverOdds, settings.FirstBloodOdds,
		settings.CSDefaultOdds, settings.CSKillsOverOdds,
		boolIcon(settings.SoloOnlyBets), minMMR, boolIcon(settings.HWIDRequired),
	)

	m := tgbotapi.NewMessage(chatID, text)
	m.ReplyMarkup = withBackRow(
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("📊 Dota: победа", "admin_prompt_win_odds"),
			tgbotapi.NewInlineKeyboardButtonData("💀 Dota: тотал", "admin_prompt_kills_odds"),
		},
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("🩸 Dota: ФБ", "admin_prompt_fb_odds"),
			tgbotapi.NewInlineKeyboardButtonData("📈 Мин. ММР", "admin_prompt_min_mmr"),
		},
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("📊 CS2: победа", "admin_prompt_cs_win_odds"),
			tgbotapi.NewInlineKeyboardButtonData("💀 CS2: тотал", "admin_prompt_cs_kills_odds"),
		},
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("🎯 Соло: %s", boolIcon(settings.SoloOnlyBets)),
				"admin_toggle_solo",
			),
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("🔒 HWID: %s", boolIcon(settings.HWIDRequired)),
				"admin_toggle_hwid",
			),
		},
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("👥 Игроки", "admin_users:0"),
			tgbotapi.NewInlineKeyboardButtonData("🔍 Найти по ID", "admin_find_user"),
		},
	)
	h.send(api, m)
}

// ─── Администратор: управление игроками ───────────────────────────────────────

const adminUsersPageSize = 8

// sendAdminUsersList показывает постраничный список игроков — тап по игроку
// открывает его карточку с кнопками начисления/списания и блокировки.
func (h *Handler) sendAdminUsersList(ctx context.Context, api *tgbotapi.BotAPI, chatID int64, page int) {
	if page < 0 {
		page = 0
	}
	// Запрашиваем на одну запись больше, чтобы понять, есть ли следующая страница.
	list, err := h.users.ListUsers(ctx, adminUsersPageSize+1, page*adminUsersPageSize)
	if err != nil {
		h.replyError(api, chatID, "Не удалось получить список игроков.", err)
		return
	}

	hasNext := len(list) > adminUsersPageSize
	if hasNext {
		list = list[:adminUsersPageSize]
	}

	if len(list) == 0 && page == 0 {
		m := tgbotapi.NewMessage(chatID, "👥 Игроков пока нет.")
		m.ReplyMarkup = withBackRow([]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("🔧 Панель администратора", "admin_menu"),
		})
		h.send(api, m)
		return
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, u := range list {
		label := fmt.Sprintf("%s%s | %d 💰 | id:%d", blockedIcon(u.IsBlocked), displayName(u), u.Balance, u.TelegramID)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("admin_user:%d", u.TelegramID)),
		))
	}

	var navRow []tgbotapi.InlineKeyboardButton
	if page > 0 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", fmt.Sprintf("admin_users:%d", page-1)))
	}
	if hasNext {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("Вперёд ➡️", fmt.Sprintf("admin_users:%d", page+1)))
	}
	if len(navRow) > 0 {
		rows = append(rows, navRow)
	}
	rows = append(rows, []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("🔍 Найти по ID", "admin_find_user"),
	})

	m := tgbotapi.NewMessage(chatID, fmt.Sprintf("👥 Игроки (страница %d)\n🚫 — заблокирован", page+1))
	m.ReplyMarkup = withBackRow(rows...)
	h.send(api, m)
}

// sendAdminUserPanel показывает карточку игрока с кнопками управления.
func (h *Handler) sendAdminUserPanel(ctx context.Context, api *tgbotapi.BotAPI, chatID, targetTelegramID int64) {
	user, err := h.users.GetByTelegramID(ctx, targetTelegramID)
	if err != nil {
		if errors.Is(err, users.ErrNotFound) {
			m := tgbotapi.NewMessage(chatID, "Игрок с таким Telegram ID не найден.")
			m.ReplyMarkup = withBackRow([]tgbotapi.InlineKeyboardButton{
				tgbotapi.NewInlineKeyboardButtonData("👥 К списку", "admin_users:0"),
			})
			h.send(api, m)
			return
		}
		h.replyError(api, chatID, "Не удалось загрузить игрока.", err)
		return
	}

	balances, err := h.wallet.GetBalances(ctx, user.ID)
	if err != nil {
		h.replyError(api, chatID, "Не удалось получить баланс игрока.", err)
		return
	}

	text := fmt.Sprintf(
		"👤 %s\nTelegram ID: %d\n\n💰 Доступно: %d 🟢\nЗаморожено: %d 🔒\nСтатус: %s",
		displayName(user), user.TelegramID, balances.Balance, balances.FrozenBalance, blockStatusLabel(user.IsBlocked),
	)

	blockButton := tgbotapi.NewInlineKeyboardButtonData("🚫 Заблокировать", fmt.Sprintf("admin_block_user:%d", user.TelegramID))
	if user.IsBlocked {
		blockButton = tgbotapi.NewInlineKeyboardButtonData("✅ Разблокировать", fmt.Sprintf("admin_unblock_user:%d", user.TelegramID))
	}

	m := tgbotapi.NewMessage(chatID, text)
	m.ReplyMarkup = withBackRow(
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("➕ Начислить", fmt.Sprintf("admin_credit:%d", user.TelegramID)),
			tgbotapi.NewInlineKeyboardButtonData("➖ Списать", fmt.Sprintf("admin_debit:%d", user.TelegramID)),
		},
		[]tgbotapi.InlineKeyboardButton{blockButton},
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("👥 К списку", "admin_users:0"),
		},
	)
	h.send(api, m)
}

func blockedIcon(isBlocked bool) string {
	if isBlocked {
		return "🚫 "
	}
	return ""
}

func blockStatusLabel(isBlocked bool) string {
	if isBlocked {
		return "🚫 заблокирован"
	}
	return "✅ активен"
}

func (h *Handler) handleAdminSetOdds(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message, kind string) {
	if !h.isAdmin(msg.From.ID) || h.adminService == nil {
		return
	}
	value := strings.TrimSpace(msg.CommandArguments())
	if value == "" {
		h.sendText(api, msg.Chat.ID, "Укажи коэффициент. Пример: /admin_set_odds 2.50")
		return
	}
	var err error
	switch kind {
	case "win":
		err = h.adminService.SetDefaultOdds(ctx, value)
	case "kills":
		err = h.adminService.SetKillsOverOdds(ctx, value)
	case "fb":
		err = h.adminService.SetFirstBloodOdds(ctx, value)
	}
	if err != nil {
		h.sendText(api, msg.Chat.ID, "❌ Ошибка: "+err.Error())
		return
	}
	h.sendText(api, msg.Chat.ID, fmt.Sprintf("✅ Коэффициент обновлён: %s", value))
}

func (h *Handler) handleAdminSetMinMMR(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	if !h.isAdmin(msg.From.ID) || h.adminService == nil {
		return
	}
	mmr, err := strconv.Atoi(strings.TrimSpace(msg.CommandArguments()))
	if err != nil || mmr < 0 {
		h.sendText(api, msg.Chat.ID, "Укажи число ≥ 0. Пример: /admin_set_min_mmr 3000")
		return
	}
	if err := h.adminService.SetMinAvgMMR(ctx, mmr); err != nil {
		h.sendText(api, msg.Chat.ID, "❌ Ошибка: "+err.Error())
		return
	}
	if mmr == 0 {
		h.sendText(api, msg.Chat.ID, "✅ Мин. ММР отключён.")
	} else {
		h.sendText(api, msg.Chat.ID, fmt.Sprintf("✅ Мин. ММР: %d", mmr))
	}
}

func (h *Handler) handleAdminAdjust(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	if !h.isAdmin(msg.From.ID) {
		return
	}
	parts := strings.Fields(msg.CommandArguments())
	if len(parts) != 2 {
		h.sendText(api, msg.Chat.ID, "Формат: /admin_adjust <telegram_id> <delta>\nПример: /admin_adjust 123456789 500")
		return
	}
	targetID, err1 := strconv.ParseInt(parts[0], 10, 64)
	delta, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil {
		h.sendText(api, msg.Chat.ID, "❌ Оба аргумента должны быть числами.")
		return
	}
	if err := h.wallet.AdminAdjust(ctx, targetID, delta); err != nil {
		h.sendText(api, msg.Chat.ID, "❌ Ошибка: "+err.Error())
		return
	}
	sign := "+"
	if delta < 0 {
		sign = ""
	}
	h.sendText(api, msg.Chat.ID, fmt.Sprintf("✅ Баланс изменён: %s%d", sign, delta))
}

func (h *Handler) handleAdminBlock(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message, block bool) {
	if !h.isAdmin(msg.From.ID) {
		return
	}
	targetID, err := parsePositiveInt64(msg.CommandArguments())
	if err != nil {
		cmd := "admin_block"
		if !block {
			cmd = "admin_unblock"
		}
		h.sendText(api, msg.Chat.ID, fmt.Sprintf("Формат: /%s <telegram_id>", cmd))
		return
	}
	if err := h.users.SetBlocked(ctx, targetID, block); err != nil {
		h.sendText(api, msg.Chat.ID, "❌ Ошибка: "+err.Error())
		return
	}
	action := "заблокирован"
	if !block {
		action = "разблокирован"
	}
	h.sendText(api, msg.Chat.ID, fmt.Sprintf("✅ Пользователь %d %s.", targetID, action))
}

func (h *Handler) handleAdminInput(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message, pending *pendingInput) {
	value := strings.TrimSpace(msg.Text)
	switch pending.action {
	case pendingAdminFindUser:
		targetID, err := strconv.ParseInt(value, 10, 64)
		if err != nil || targetID <= 0 {
			h.sendText(api, msg.Chat.ID, "❌ Нужно число — Telegram ID игрока.")
			h.setPendingInput(msg.From.ID, &pendingInput{action: pendingAdminFindUser})
			return
		}
		h.sendAdminUserPanel(ctx, api, msg.Chat.ID, targetID)
		return

	case pendingAdminCredit, pendingAdminDebit:
		amount, err := parsePositiveInt64(value)
		if err != nil {
			h.sendText(api, msg.Chat.ID, "❌ Нужно положительное число. Пример: 500")
			h.setPendingInput(msg.From.ID, pending)
			return
		}
		delta := amount
		if pending.action == pendingAdminDebit {
			delta = -amount
		}
		if err := h.wallet.AdminAdjust(ctx, pending.target, delta); err != nil {
			h.sendText(api, msg.Chat.ID, "❌ Ошибка: "+err.Error())
			return
		}
		sign := "+"
		if delta < 0 {
			sign = ""
		}
		h.sendText(api, msg.Chat.ID, fmt.Sprintf("✅ Баланс игрока %d изменён: %s%d", pending.target, sign, delta))
		h.sendAdminUserPanel(ctx, api, msg.Chat.ID, pending.target)
		return
	}

	if h.adminService == nil {
		return
	}

	switch pending.action {
	case pendingAdminSetWinOdds:
		if err := h.adminService.SetDefaultOdds(ctx, value); err != nil {
			h.sendText(api, msg.Chat.ID, "❌ Ошибка: "+err.Error())
			return
		}
		h.sendText(api, msg.Chat.ID, fmt.Sprintf("✅ Коэф победа (Dota): %s", value))
		h.sendAdminMenu(ctx, api, msg.Chat.ID)
	case pendingAdminSetKillsOdds:
		if err := h.adminService.SetKillsOverOdds(ctx, value); err != nil {
			h.sendText(api, msg.Chat.ID, "❌ Ошибка: "+err.Error())
			return
		}
		h.sendText(api, msg.Chat.ID, fmt.Sprintf("✅ Коэф тотал (Dota): %s", value))
		h.sendAdminMenu(ctx, api, msg.Chat.ID)
	case pendingAdminSetFBOdds:
		if err := h.adminService.SetFirstBloodOdds(ctx, value); err != nil {
			h.sendText(api, msg.Chat.ID, "❌ Ошибка: "+err.Error())
			return
		}
		h.sendText(api, msg.Chat.ID, fmt.Sprintf("✅ Коэф ФБ: %s", value))
		h.sendAdminMenu(ctx, api, msg.Chat.ID)
	case pendingAdminSetMinMMR:
		mmr, err := strconv.Atoi(value)
		if err != nil || mmr < 0 {
			h.sendText(api, msg.Chat.ID, "❌ Введи число ≥ 0")
			return
		}
		if err := h.adminService.SetMinAvgMMR(ctx, mmr); err != nil {
			h.sendText(api, msg.Chat.ID, "❌ Ошибка: "+err.Error())
			return
		}
		if mmr == 0 {
			h.sendText(api, msg.Chat.ID, "✅ Мин. ММР отключён.")
		} else {
			h.sendText(api, msg.Chat.ID, fmt.Sprintf("✅ Мин. ММР: %d", mmr))
		}
		h.sendAdminMenu(ctx, api, msg.Chat.ID)
	case pendingAdminSetCSWin:
		if err := h.adminService.SetCSDefaultOdds(ctx, value); err != nil {
			h.sendText(api, msg.Chat.ID, "❌ Ошибка: "+err.Error())
			return
		}
		h.sendText(api, msg.Chat.ID, fmt.Sprintf("✅ Коэф победа (CS2): %s", value))
		h.sendAdminMenu(ctx, api, msg.Chat.ID)
	case pendingAdminSetCSKills:
		if err := h.adminService.SetCSKillsOverOdds(ctx, value); err != nil {
			h.sendText(api, msg.Chat.ID, "❌ Ошибка: "+err.Error())
			return
		}
		h.sendText(api, msg.Chat.ID, fmt.Sprintf("✅ Коэф тотал (CS2): %s", value))
		h.sendAdminMenu(ctx, api, msg.Chat.ID)
	}
}

func (h *Handler) setPendingInput(telegramID int64, pending *pendingInput) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pendingInputs[telegramID] = pending
}

func (h *Handler) clearPendingInput(telegramID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.pendingInputs, telegramID)
}

// ─── Вспомогательные методы ──────────────────────────────────────────────────

func (h *Handler) sendText(api *tgbotapi.BotAPI, chatID int64, text string) {
	if _, err := api.Send(tgbotapi.NewMessage(chatID, text)); err != nil {
		h.logger.Error("send message", "error", err)
	}
}

func (h *Handler) send(api *tgbotapi.BotAPI, msg tgbotapi.MessageConfig) {
	if _, err := api.Send(msg); err != nil {
		h.logger.Error("send message", "error", err)
	}
}

func (h *Handler) replyError(api *tgbotapi.BotAPI, chatID int64, message string, err error) {
	h.logger.Error(message, "error", err)
	m := tgbotapi.NewMessage(chatID, message+"\n"+friendlyError(err))
	m.ReplyMarkup = backToMenuKeyboard()
	h.send(api, m)
}

// ─── Форматирование ──────────────────────────────────────────────────────────

func parsePositiveInt64(value string) (int64, error) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed <= 0 {
		return 0, errors.New("invalid positive int64")
	}
	return parsed, nil
}

func friendlyError(err error) string {
	switch {
	case errors.Is(err, users.ErrNotFound), errors.Is(err, selfbets.ErrUserNotFound), errors.Is(err, csbets.ErrUserNotFound):
		return "Сначала открой меню: /start"
	case errors.Is(err, selfbets.ErrDotaNotLinked):
		return "Сначала привяжи Dota аккаунт — пришли свой ID в меню «🎮 Dota 2»."
	case errors.Is(err, csbets.ErrCSNotLinked):
		return "Сначала привяжи CS2 аккаунт — пришли свой ID в меню «🔫 CS2»."
	case errors.Is(err, selfbets.ErrActiveBetExists), errors.Is(err, csbets.ErrActiveBetExists):
		return "У тебя уже есть активная ставка."
	case errors.Is(err, selfbets.ErrNoActiveBet), errors.Is(err, csbets.ErrNoActiveBet):
		return "Активной ставки нет."
	case errors.Is(err, selfbets.ErrInvalidAmount), errors.Is(err, csbets.ErrInvalidAmount), errors.Is(err, wallet.ErrInvalidAmount):
		return "Сумма должна быть положительным числом."
	case errors.Is(err, selfbets.ErrInvalidThreshold), errors.Is(err, csbets.ErrInvalidThreshold):
		return "Порог килов должен быть больше 0."
	case errors.Is(err, wallet.ErrInsufficientFunds):
		return "Недостаточно монет. Проверь баланс в меню."
	case errors.Is(err, wallet.ErrInsufficientFrozen):
		return "Недостаточно замороженных монет."
	case errors.Is(err, selfbets.ErrBetAlreadyTargeted), errors.Is(err, csbets.ErrBetAlreadyTargeted):
		return "Ставку нельзя отменить — она привязана к матчу."
	case errors.Is(err, selfbets.ErrInvalidAccountID):
		return "Dota account_id должен быть положительным числом."
	case errors.Is(err, selfbets.ErrHWIDRequired):
		return "Требуется привязка железа. Обратись к администратору."
	case errors.Is(err, dota.ErrMatchHistoryPrivate):
		return "История матчей закрыта. В Dota 2: Settings → Social → Expose Public Match Data, сыграй матч, попробуй снова."
	case errors.Is(err, dota.ErrSteamAPIKeyRequired):
		return "На сервере не настроен Steam API Key. Напиши администратору."
	case errors.Is(err, dota.ErrProviderUnavailable):
		return "Dota API временно недоступен. Попробуй позже."
	case errors.Is(err, selfbets.ErrMatchResultMissing), errors.Is(err, csbets.ErrMatchResultMissing):
		return "Результат матча ещё не готов. Попробуй позже."
	case errors.Is(err, selfbets.ErrHistoryAdvanced), errors.Is(err, csbets.ErrHistoryAdvanced):
		return "В истории появился новый матч — данные обновлены. Повтори ставку."
	case errors.Is(err, cs.ErrPlayerNotFound):
		return "Не нашёл такой профиль на FACEIT. Проверь ник или SteamID64."
	case errors.Is(err, cs.ErrFaceitAPIKeyRequired):
		return "На сервере не настроен FACEIT API Key. Напиши администратору."
	case errors.Is(err, cs.ErrProviderUnavailable):
		return "CS2/FACEIT API временно недоступен. Попробуй позже."
	case errors.Is(err, cs.ErrInvalidAccountInput):
		return "Не понял этот ID. Пришли SteamID64, ссылку на профиль Steam или FACEIT-ник."
	default:
		return "Что-то пошло не так. Попробуй позже."
	}
}

func formatSelfBet(title string, bet domain.SelfBet) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n\n", title)
	fmt.Fprintf(&sb, "Тип: %s\n", predictionLabel(bet.Prediction, bet.KillsThreshold))
	fmt.Fprintf(&sb, "Сумма: %d\n", bet.Amount)
	fmt.Fprintf(&sb, "Коэф: %s\n", bet.Odds)
	fmt.Fprintf(&sb, "Выплата: %d\n", bet.PotentialPayout)
	fmt.Fprintf(&sb, "Статус: %s %s\n", betStatusEmoji(bet.Status), bet.Status)
	fmt.Fprintf(&sb, "Создана: %s", bet.CreatedAt.Local().Format("02.01.2006 15:04"))
	return sb.String()
}

func formatCSBet(title string, bet domain.CSBet) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n\n", title)
	fmt.Fprintf(&sb, "Тип: %s\n", predictionLabel(bet.Prediction, bet.KillsThreshold))
	fmt.Fprintf(&sb, "Сумма: %d\n", bet.Amount)
	fmt.Fprintf(&sb, "Коэф: %s\n", bet.Odds)
	fmt.Fprintf(&sb, "Выплата: %d\n", bet.PotentialPayout)
	fmt.Fprintf(&sb, "Статус: %s %s\n", betStatusEmoji(bet.Status), bet.Status)
	fmt.Fprintf(&sb, "Создана: %s", bet.CreatedAt.Local().Format("02.01.2006 15:04"))
	return sb.String()
}

func predictionLabel(p domain.SelfBetPrediction, threshold *int64) string {
	switch p {
	case domain.SelfBetPredictionWin:
		return "🏆 Победа"
	case domain.SelfBetPredictionTotalKillsOver:
		if threshold != nil {
			return fmt.Sprintf("💀 Тотал > %d", *threshold)
		}
		return "💀 Тотал килов"
	case domain.SelfBetPredictionFirstBloodRadiant:
		return "🩸 Первая кровь (Радиант)"
	case domain.SelfBetPredictionFirstBloodDire:
		return "🩸 Первая кровь (Дайр)"
	default:
		return string(p)
	}
}

func betStatusEmoji(s domain.SelfBetStatus) string {
	switch s {
	case domain.SelfBetStatusWon:
		return "🏆"
	case domain.SelfBetStatusLost:
		return "💸"
	case domain.SelfBetStatusActive:
		return "⏳"
	case domain.SelfBetStatusCancelled:
		return "🚫"
	case domain.SelfBetStatusVoid:
		return "↩️"
	default:
		return "•"
	}
}

func boolIcon(v bool) string {
	if v {
		return "✅ Вкл"
	}
	return "❌ Выкл"
}

func formatOptionalMatchID(matchID *int64) string {
	if matchID == nil {
		return "ожидается"
	}
	return strconv.FormatInt(*matchID, 10)
}

func formatOptionalCSMatchID(matchID *string) string {
	if matchID == nil || *matchID == "" {
		return "ожидается"
	}
	return *matchID
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

func tutorialText() string {
	return strings.Join([]string{
		"📚 Туториал",
		"",
		"1️⃣ Открой меню: /start",
		"2️⃣ Выбери игру — Dota 2 или CS2",
		"3️⃣ Пришли свой ID одним сообщением (без команд):",
		"   • Dota: account_id, SteamID64 или ссылка на профиль Steam",
		"   • CS2: SteamID64, ссылка на профиль Steam или FACEIT-ник",
		"4️⃣ Нажми «🎲 Сделать ставку», выбери сумму и тип ставки",
		"",
		"📦 Типы ставок:",
		"🏆 Победа — угадай исход следующего матча",
		"💀 Тотал килов — сумма убийств в матче > N",
		"🩸 Первая кровь (только Dota) — какая команда даст первый килл",
		"",
		"5️⃣ Монеты замораживаются до конца матча",
		"6️⃣ Результат определяется автоматически, бот пришлёт уведомление",
		"",
		"Всё управление — кнопками. Команда /start всегда открывает меню заново.",
	}, "\n")
}

func helpText() string {
	return strings.Join([]string{
		"❓ Как это работает",
		"",
		"1. Открой меню (кнопка «🏠 В меню» есть на любом экране).",
		"2. Выбери игру: Dota 2 или CS2.",
		"3. Привяжи аккаунт — просто пришли свой ID одним сообщением.",
		"4. Нажми «Сделать ставку», выбери сумму и тип ставки.",
		"5. Дождись результата следующего матча — бот пришлёт уведомление.",
		"",
		"Всё управляется кнопками, команды не нужны.",
		"Команды (необязательно): /link_dota, /bet, /active_bet, /history",
	}, "\n")
}
