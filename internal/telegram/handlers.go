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
	"stavki/internal/domain"
	"stavki/internal/dota"
	"stavki/internal/selfbets"
	"stavki/internal/users"
	"stavki/internal/wallet"
)

// pendingAdminInput хранит ожидаемый ввод от администратора.
type pendingAdminInput struct {
	action string
}

type Handler struct {
	users        *users.Service
	wallet       *wallet.Service
	selfbets     *selfbets.Service
	adminService *admin.Service
	adminIDs     map[int64]bool
	logger       *slog.Logger

	mu            sync.Mutex
	pendingInputs map[int64]*pendingAdminInput
}

func NewHandler(
	usersService *users.Service,
	walletService *wallet.Service,
	selfBetsService *selfbets.Service,
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
		adminService:  adminService,
		adminIDs:      ids,
		logger:        logger,
		pendingInputs: make(map[int64]*pendingAdminInput),
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

	if !update.Message.IsCommand() {
		// Проверяем ожидаемый ввод от администратора
		h.mu.Lock()
		pending, hasPending := h.pendingInputs[update.Message.From.ID]
		if hasPending {
			delete(h.pendingInputs, update.Message.From.ID)
		}
		h.mu.Unlock()

		if hasPending && h.isAdmin(update.Message.From.ID) {
			h.handleAdminInput(ctx, api, update.Message, pending)
			return
		}
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
		h.handleHelp(ctx, api, msg)
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
		h.sendText(api, msg.Chat.ID, "Неизвестная команда. Используй /help.")
	}
}

// ─── Пользовательские команды ────────────────────────────────────────────────

func (h *Handler) handleStart(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	user, err := h.users.GetOrCreateByTelegram(ctx, msg.From.ID, msg.From.UserName, msg.From.FirstName)
	if err != nil {
		h.replyError(api, msg.Chat.ID, "Не удалось создать профиль.", err)
		return
	}

	text := fmt.Sprintf(
		"👋 Привет, %s!\n\n💰 Баланс: %d монет\n\nДля начала привяжи Dota 2 аккаунт:\n/link_dota <account_id>\n\nНе знаешь account_id? /tutorial",
		displayName(user), user.Balance,
	)
	m := tgbotapi.NewMessage(msg.Chat.ID, text)
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📚 Туториал", "tutorial"),
			tgbotapi.NewInlineKeyboardButtonData("❓ Помощь", "help"),
		),
	)
	h.send(api, m)
}

func (h *Handler) handleLinkDota(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	accountID, err := dota.ParseAccountIDInput(msg.CommandArguments())
	if err != nil {
		h.sendText(api, msg.Chat.ID,
			"❌ Неверный формат.\n\nПримеры:\n/link_dota 123456789\n/link_dota 76561198083956128\n/link_dota https://steamcommunity.com/profiles/76561198...\n\nКак найти → /tutorial")
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
		h.sendText(api, msg.Chat.ID,
			"✅ Аккаунт привязан!\n\nСоревновательных матчей не найдено — ставка будет рассчитана после первого ranked/competitive матча.\n\nГотов? /bet 100")
		return
	}

	m := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf(
		"✅ Аккаунт привязан! Последний матч: %d\n\nМожешь ставить:", result.LastMatch.MatchID))
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🎲 Поставить 100", "bet:100"),
		),
	)
	h.send(api, m)
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

	m := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf(
		"💰 Баланс\n\nДоступно: %d 🟢\nЗаморожено: %d 🔒\nВсего: %d",
		balances.Balance, balances.FrozenBalance, balances.Balance+balances.FrozenBalance,
	))
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🎲 Ставка", "bet:0"),
			tgbotapi.NewInlineKeyboardButtonData("📋 История", "history"),
		),
	)
	h.send(api, m)
}

func (h *Handler) handleBet(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	arg := strings.TrimSpace(msg.CommandArguments())
	if arg == "" {
		h.sendText(api, msg.Chat.ID, "Укажи сумму: /bet <сумма>\nПример: /bet 100")
		return
	}
	amount, err := parsePositiveInt64(arg)
	if err != nil {
		h.sendText(api, msg.Chat.ID, "❌ Неверная сумма. Пример: /bet 100")
		return
	}
	h.sendBetTypeKeyboard(ctx, api, msg.Chat.ID, amount)
}

func (h *Handler) sendBetTypeKeyboard(ctx context.Context, api *tgbotapi.BotAPI, chatID int64, amount int64) {
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
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("🏆 Победа ×%s", winOdds),
				fmt.Sprintf("place:win:%d", amount),
			),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("💀 Тотал ×%s", killsOdds),
				fmt.Sprintf("kills_menu:%d", amount),
			),
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("🩸 ФБ ×%s", fbOdds),
				fmt.Sprintf("fb_menu:%d", amount),
			),
		),
	)
	h.send(api, m)
}

func (h *Handler) handleBetWin(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	amount, err := parsePositiveInt64(msg.CommandArguments())
	if err != nil {
		h.sendText(api, msg.Chat.ID, "❌ Пример: /bet_win 100")
		return
	}
	h.placeBetWin(ctx, api, msg.Chat.ID, msg.From.ID, amount)
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
	h.placeBetKills(ctx, api, msg.Chat.ID, msg.From.ID, amount, threshold)
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
	h.placeBetFB(ctx, api, msg.Chat.ID, msg.From.ID, amount, prediction)
}

// ─── Размещение ставок ────────────────────────────────────────────────────────

func (h *Handler) placeBetWin(ctx context.Context, api *tgbotapi.BotAPI, chatID, telegramID, amount int64) {
	bet, err := h.selfbets.PlaceNextMatchWinBet(ctx, telegramID, amount)
	if err != nil {
		h.sendText(api, chatID, friendlyError(err))
		return
	}
	m := tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"✅ Ставка принята!\n\n🏆 Тип: победа\n💰 Сумма: %d\n📈 Коэф: %s\n🎯 Выплата: %d\n\nМонеты заморожены.",
		bet.Amount, bet.Odds, bet.PotentialPayout,
	))
	m.ReplyMarkup = activeBetKeyboard()
	h.send(api, m)
}

func (h *Handler) placeBetKills(ctx context.Context, api *tgbotapi.BotAPI, chatID, telegramID, amount, threshold int64) {
	bet, err := h.selfbets.PlaceTotalKillsBet(ctx, telegramID, amount, threshold)
	if err != nil {
		h.sendText(api, chatID, friendlyError(err))
		return
	}
	m := tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"✅ Ставка принята!\n\n💀 Тип: тотал > %d\n💰 Сумма: %d\n📈 Коэф: %s\n🎯 Выплата: %d\n\nМонеты заморожены.",
		threshold, bet.Amount, bet.Odds, bet.PotentialPayout,
	))
	m.ReplyMarkup = activeBetKeyboard()
	h.send(api, m)
}

func (h *Handler) placeBetFB(ctx context.Context, api *tgbotapi.BotAPI, chatID, telegramID int64, amount int64, prediction domain.SelfBetPrediction) {
	bet, err := h.selfbets.PlaceFirstBloodBet(ctx, telegramID, amount, prediction)
	if err != nil {
		h.sendText(api, chatID, friendlyError(err))
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
	m.ReplyMarkup = activeBetKeyboard()
	h.send(api, m)
}

func activeBetKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("👁 Активная ставка", "active_bet"),
			tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", "cancel_bet"),
		),
	)
}

func (h *Handler) handleActiveBet(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	h.sendActiveBet(ctx, api, msg.Chat.ID, msg.From.ID)
}

func (h *Handler) sendActiveBet(ctx context.Context, api *tgbotapi.BotAPI, chatID, telegramID int64) {
	bet, err := h.selfbets.GetActiveBet(ctx, telegramID)
	if err != nil {
		if errors.Is(err, selfbets.ErrNoActiveBet) {
			m := tgbotapi.NewMessage(chatID, "Активной ставки нет.")
			m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("🎲 Сделать ставку", "bet:0"),
				),
			)
			h.send(api, m)
			return
		}
		h.replyError(api, chatID, "Не удалось получить активную ставку.", err)
		return
	}
	m := tgbotapi.NewMessage(chatID, formatSelfBet("👁 Активная ставка", bet))
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Отменить ставку", "cancel_bet"),
		),
	)
	h.send(api, m)
}

func (h *Handler) handleCancelBet(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	h.doCancelBet(ctx, api, msg.Chat.ID, msg.From.ID)
}

func (h *Handler) doCancelBet(ctx context.Context, api *tgbotapi.BotAPI, chatID, telegramID int64) {
	if err := h.selfbets.CancelActiveBet(ctx, telegramID); err != nil {
		h.sendText(api, chatID, friendlyError(err))
		return
	}
	h.sendText(api, chatID, "✅ Ставка отменена. Монеты возвращены на баланс.")
}

func (h *Handler) handleHistory(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	history, err := h.selfbets.GetHistory(ctx, msg.From.ID, 10)
	if err != nil {
		h.replyError(api, msg.Chat.ID, "Не удалось получить историю.", err)
		return
	}
	if len(history) == 0 {
		h.sendText(api, msg.Chat.ID, "История пустая. Первая ставка: /bet 100")
		return
	}

	var b strings.Builder
	b.WriteString("📋 Последние ставки:\n")
	for _, item := range history {
		fmt.Fprintf(&b, "\n%s #%d | %s | %d монет | ×%s | выплата %d | матч: %s",
			betStatusEmoji(item.Status),
			item.ID,
			predictionLabel(item.Prediction, item.KillsThreshold),
			item.Amount, item.Odds, item.PotentialPayout,
			formatOptionalMatchID(item.TargetMatchID),
		)
	}
	h.sendText(api, msg.Chat.ID, b.String())
}

func (h *Handler) handleTutorial(api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	m := tgbotapi.NewMessage(msg.Chat.ID, tutorialText())
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🚀 Начать", "bet:0"),
		),
	)
	h.send(api, m)
}

func (h *Handler) handleHelp(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	m := tgbotapi.NewMessage(msg.Chat.ID, helpText(h.isAdmin(msg.From.ID)))
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📚 Туториал", "tutorial"),
			tgbotapi.NewInlineKeyboardButtonData("💰 Баланс", "balance"),
		),
	)
	h.send(api, m)
}

// ─── Callback обработчик ─────────────────────────────────────────────────────

func (h *Handler) handleCallback(ctx context.Context, api *tgbotapi.BotAPI, cb *tgbotapi.CallbackQuery) {
	chatID := cb.Message.Chat.ID
	telegramID := cb.From.ID
	data := cb.Data

	switch {
	case data == "tutorial":
		h.send(api, tgbotapi.NewMessage(chatID, tutorialText()))

	case data == "help":
		h.send(api, tgbotapi.NewMessage(chatID, helpText(h.isAdmin(telegramID))))

	case data == "balance":
		user, err := h.users.GetByTelegramID(ctx, telegramID)
		if err == nil {
			if balances, err := h.wallet.GetBalances(ctx, user.ID); err == nil {
				h.sendText(api, chatID, fmt.Sprintf(
					"💰 Доступно: %d\nЗаморожено: %d\nВсего: %d",
					balances.Balance, balances.FrozenBalance, balances.Balance+balances.FrozenBalance,
				))
			}
		}

	case data == "active_bet":
		h.sendActiveBet(ctx, api, chatID, telegramID)

	case data == "cancel_bet":
		h.doCancelBet(ctx, api, chatID, telegramID)

	case data == "history":
		histMsg := &tgbotapi.Message{Chat: cb.Message.Chat, From: cb.From}
		h.handleHistory(ctx, api, histMsg)

	case strings.HasPrefix(data, "bet:"):
		amount, _ := strconv.ParseInt(strings.TrimPrefix(data, "bet:"), 10, 64)
		if amount <= 0 {
			h.send(api, tgbotapi.NewMessage(chatID, "Укажи сумму: /bet <сумма>"))
		} else {
			h.sendBetTypeKeyboard(ctx, api, chatID, amount)
		}

	case strings.HasPrefix(data, "place:win:"):
		if amount, err := strconv.ParseInt(strings.TrimPrefix(data, "place:win:"), 10, 64); err == nil && amount > 0 {
			h.placeBetWin(ctx, api, chatID, telegramID, amount)
		}

	case strings.HasPrefix(data, "kills_menu:"):
		if amount, err := strconv.ParseInt(strings.TrimPrefix(data, "kills_menu:"), 10, 64); err == nil && amount > 0 {
			h.sendKillsThresholdKeyboard(api, chatID, amount)
		}

	case strings.HasPrefix(data, "place:kills:"):
		// place:kills:100:50
		parts := strings.Split(strings.TrimPrefix(data, "place:kills:"), ":")
		if len(parts) == 2 {
			amount, err1 := strconv.ParseInt(parts[0], 10, 64)
			threshold, err2 := strconv.ParseInt(parts[1], 10, 64)
			if err1 == nil && err2 == nil && amount > 0 && threshold > 0 {
				h.placeBetKills(ctx, api, chatID, telegramID, amount, threshold)
			}
		}

	case strings.HasPrefix(data, "fb_menu:"):
		if amount, err := strconv.ParseInt(strings.TrimPrefix(data, "fb_menu:"), 10, 64); err == nil && amount > 0 {
			h.sendFirstBloodKeyboard(api, chatID, amount)
		}

	case strings.HasPrefix(data, "place:fb:"):
		// place:fb:100:radiant
		parts := strings.Split(strings.TrimPrefix(data, "place:fb:"), ":")
		if len(parts) == 2 {
			if amount, err := strconv.ParseInt(parts[0], 10, 64); err == nil && amount > 0 {
				var prediction domain.SelfBetPrediction
				switch parts[1] {
				case "radiant":
					prediction = domain.SelfBetPredictionFirstBloodRadiant
				case "dire":
					prediction = domain.SelfBetPredictionFirstBloodDire
				default:
					return
				}
				h.placeBetFB(ctx, api, chatID, telegramID, amount, prediction)
			}
		}

	// Admin callbacks
	case data == "admin_menu":
		if h.isAdmin(telegramID) {
			h.sendAdminMenu(ctx, api, chatID)
		}

	case data == "admin_toggle_solo":
		if !h.isAdmin(telegramID) || h.adminService == nil {
			return
		}
		newVal, err := h.adminService.ToggleSoloOnly(ctx)
		if err != nil {
			h.sendText(api, chatID, "❌ Ошибка: "+err.Error())
			return
		}
		status := boolIcon(newVal)
		h.sendText(api, chatID, fmt.Sprintf("🎯 Только соло игры: %s", status))
		h.sendAdminMenu(ctx, api, chatID)

	case data == "admin_toggle_hwid":
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

	case data == "admin_prompt_win_odds":
		if h.isAdmin(telegramID) {
			h.setPendingInput(telegramID, &pendingAdminInput{"set_win_odds"})
			h.sendText(api, chatID, "Введи коэффициент для ставки «Победа» (пример: 2.50):")
		}

	case data == "admin_prompt_kills_odds":
		if h.isAdmin(telegramID) {
			h.setPendingInput(telegramID, &pendingAdminInput{"set_kills_odds"})
			h.sendText(api, chatID, "Введи коэффициент для ставки «Тотал килов» (пример: 1.90):")
		}

	case data == "admin_prompt_fb_odds":
		if h.isAdmin(telegramID) {
			h.setPendingInput(telegramID, &pendingAdminInput{"set_fb_odds"})
			h.sendText(api, chatID, "Введи коэффициент для ставки «Первая кровь» (пример: 1.85):")
		}

	case data == "admin_prompt_min_mmr":
		if h.isAdmin(telegramID) {
			h.setPendingInput(telegramID, &pendingAdminInput{"set_min_mmr"})
			h.sendText(api, chatID, "Введи минимальный средний MMR матча (0 = отключить):")
		}
	}
}

func (h *Handler) sendKillsThresholdKeyboard(api *tgbotapi.BotAPI, chatID int64, amount int64) {
	thresholds := []int64{25, 30, 35, 40, 45, 50, 55, 60}
	var rows [][]tgbotapi.InlineKeyboardButton
	var row []tgbotapi.InlineKeyboardButton
	for i, t := range thresholds {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf(">%d", t),
			fmt.Sprintf("place:kills:%d:%d", amount, t),
		))
		if (i+1)%4 == 0 || i == len(thresholds)-1 {
			rows = append(rows, row)
			row = nil
		}
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", fmt.Sprintf("bet:%d", amount)),
	))

	m := tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"💀 Тотал килов, ставка %d монет\nВыбери порог (выиграешь если тотал > N):", amount))
	m.ReplyMarkup = tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	h.send(api, m)
}

func (h *Handler) sendFirstBloodKeyboard(api *tgbotapi.BotAPI, chatID int64, amount int64) {
	m := tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"🩸 Первая кровь, ставка %d монет\nВыбери команду:", amount))
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🌿 Радиант", fmt.Sprintf("place:fb:%d:radiant", amount)),
			tgbotapi.NewInlineKeyboardButtonData("😈 Дайр", fmt.Sprintf("place:fb:%d:dire", amount)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", fmt.Sprintf("bet:%d", amount)),
		),
	)
	h.send(api, m)
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
			"📊 Коэф победа: %s\n"+
			"💀 Коэф тотал: %s\n"+
			"🩸 Коэф ФБ: %s\n"+
			"🎯 Только соло: %s\n"+
			"📈 Мин. ММР: %s\n"+
			"🔒 Привязка железа: %s",
		settings.DefaultOdds, settings.KillsOverOdds, settings.FirstBloodOdds,
		boolIcon(settings.SoloOnlyBets), minMMR, boolIcon(settings.HWIDRequired),
	)

	m := tgbotapi.NewMessage(chatID, text)
	m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📊 Коэф победа", "admin_prompt_win_odds"),
			tgbotapi.NewInlineKeyboardButtonData("💀 Коэф тотал", "admin_prompt_kills_odds"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🩸 Коэф ФБ", "admin_prompt_fb_odds"),
			tgbotapi.NewInlineKeyboardButtonData("📈 Мин. ММР", "admin_prompt_min_mmr"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("🎯 Соло: %s", boolIcon(settings.SoloOnlyBets)),
				"admin_toggle_solo",
			),
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("🔒 HWID: %s", boolIcon(settings.HWIDRequired)),
				"admin_toggle_hwid",
			),
		),
	)
	h.send(api, m)
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

func (h *Handler) handleAdminInput(ctx context.Context, api *tgbotapi.BotAPI, msg *tgbotapi.Message, pending *pendingAdminInput) {
	if h.adminService == nil {
		return
	}
	value := strings.TrimSpace(msg.Text)
	switch pending.action {
	case "set_win_odds":
		if err := h.adminService.SetDefaultOdds(ctx, value); err != nil {
			h.sendText(api, msg.Chat.ID, "❌ Ошибка: "+err.Error())
			return
		}
		h.sendText(api, msg.Chat.ID, fmt.Sprintf("✅ Коэф победа: %s", value))
		h.sendAdminMenu(ctx, api, msg.Chat.ID)
	case "set_kills_odds":
		if err := h.adminService.SetKillsOverOdds(ctx, value); err != nil {
			h.sendText(api, msg.Chat.ID, "❌ Ошибка: "+err.Error())
			return
		}
		h.sendText(api, msg.Chat.ID, fmt.Sprintf("✅ Коэф тотал: %s", value))
		h.sendAdminMenu(ctx, api, msg.Chat.ID)
	case "set_fb_odds":
		if err := h.adminService.SetFirstBloodOdds(ctx, value); err != nil {
			h.sendText(api, msg.Chat.ID, "❌ Ошибка: "+err.Error())
			return
		}
		h.sendText(api, msg.Chat.ID, fmt.Sprintf("✅ Коэф ФБ: %s", value))
		h.sendAdminMenu(ctx, api, msg.Chat.ID)
	case "set_min_mmr":
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
	}
}

func (h *Handler) setPendingInput(telegramID int64, pending *pendingAdminInput) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pendingInputs[telegramID] = pending
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
	h.sendText(api, chatID, message+"\n"+friendlyError(err))
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
	case errors.Is(err, users.ErrNotFound), errors.Is(err, selfbets.ErrUserNotFound):
		return "Сначала создай профиль через /start."
	case errors.Is(err, selfbets.ErrDotaNotLinked):
		return "Сначала привяжи Dota аккаунт: /link_dota <account_id>."
	case errors.Is(err, selfbets.ErrActiveBetExists):
		return "У тебя уже есть активная ставка: /active_bet"
	case errors.Is(err, selfbets.ErrNoActiveBet):
		return "Активной ставки нет."
	case errors.Is(err, selfbets.ErrInvalidAmount), errors.Is(err, wallet.ErrInvalidAmount):
		return "Сумма должна быть положительным числом."
	case errors.Is(err, selfbets.ErrInvalidThreshold):
		return "Порог килов должен быть больше 0."
	case errors.Is(err, wallet.ErrInsufficientFunds):
		return "Недостаточно монет. Проверь: /balance"
	case errors.Is(err, wallet.ErrInsufficientFrozen):
		return "Недостаточно замороженных монет."
	case errors.Is(err, selfbets.ErrBetAlreadyTargeted):
		return "Ставку нельзя отменить — она привязана к матчу."
	case errors.Is(err, selfbets.ErrInvalidAccountID):
		return "Dota account_id должен быть положительным числом."
	case errors.Is(err, selfbets.ErrHWIDRequired):
		return "Требуется привязка железа. Обратись к администратору."
	case errors.Is(err, dota.ErrMatchHistoryPrivate):
		return "История матчей закрыта. В Dota 2: Settings → Social → Expose Public Match Data, сыграй матч, повтори /link_dota."
	case errors.Is(err, dota.ErrSteamAPIKeyRequired):
		return "На сервере не настроен Steam API Key. Напиши администратору."
	case errors.Is(err, dota.ErrProviderUnavailable):
		return "Dota API временно недоступен. Попробуй позже."
	case errors.Is(err, selfbets.ErrMatchResultMissing):
		return "Результат матча ещё не готов. Попробуй позже."
	case errors.Is(err, selfbets.ErrHistoryAdvanced):
		return "В истории появился новый матч — данные обновлены. Повтори ставку."
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
		"1️⃣ Зарегистрируйся: /start",
		"",
		"2️⃣ Найди Dota account_id:",
		"   • dotabuff.com → войди через Steam",
		"   • В адресе: dotabuff.com/players/XXXXXXX",
		"   • Или используй SteamID64 (~76561198...)",
		"",
		"3️⃣ Привяжи аккаунт: /link_dota 123456789",
		"",
		"4️⃣ Сделай ставку: /bet 100",
		"   Откроет меню выбора типа ставки.",
		"",
		"📦 Типы ставок:",
		"🏆 Победа — угадай исход следующего ranked матча",
		"💀 Тотал килов — сумма убийств обеих команд > N",
		"🩸 Первая кровь — какая команда даст первый килл",
		"",
		"5️⃣ Монеты замораживаются до конца матча",
		"6️⃣ Результат определяется автоматически через OpenDota",
		"",
		"⚙️ Активные фильтры (задаёт администратор):",
		"   • Только соло — не учитываются партийные матчи",
		"   • Мин. ММР — не учитываются низкорейтинговые матчи",
		"",
		"🔒 Привязка к железу (HWID):",
		"   Если включена, попроси администратора зарегистрировать",
		"   твой device token перед первой ставкой.",
		"",
		"❓ Все команды: /help",
	}, "\n")
}

func helpText(isAdmin bool) string {
	lines := []string{
		"📖 Команды бота",
		"",
		"👤 Профиль:",
		"/start — создать профиль",
		"/link_dota <id> — привязать Dota аккаунт",
		"/balance — баланс",
		"",
		"🎲 Ставки:",
		"/bet <сумма> — выбрать тип ставки (интерактивно)",
		"/bet_win <сумма> — ставка на победу",
		"/bet_kills <сумма> <порог> — тотал килов > порога",
		"/bet_fb <сумма> radiant|dire — первая кровь",
		"/active_bet — текущая ставка",
		"/cancel_bet — отменить ставку (до привязки к матчу)",
		"/history — история ставок",
		"",
		"📚 Прочее:",
		"/tutorial — как начать",
		"/help — эта справка",
	}

	if isAdmin {
		lines = append(lines,
			"",
			"🔧 Администратор:",
			"/admin — панель управления",
			"/admin_set_odds <коэф> — коэф победа",
			"/admin_set_kills_odds <коэф> — коэф тотал",
			"/admin_set_fb_odds <коэф> — коэф первая кровь",
			"/admin_set_min_mmr <ммр> — мин. ММР (0=откл)",
			"/admin_adjust <tg_id> <delta> — изменить баланс",
			"/admin_block <tg_id> — заблокировать игрока",
			"/admin_unblock <tg_id> — разблокировать игрока",
		)
	}

	return strings.Join(lines, "\n")
}
