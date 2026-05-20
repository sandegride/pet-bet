# Pet Bet

Telegram-бот на Go для виртуальных ставок на собственный следующий ranked/competitive матч в Dota 2.

Проект работает только с виртуальными монетами. Здесь нет реальных денег, платежей, депозитов, вывода средств, KYC, bookmaker odds, betting API и ставок на pro-матчи.

## Механика MVP

1. Пользователь пишет `/start`.
2. Привязывает Dota аккаунт: `/link_dota <account_id>`.
3. Бот сохраняет последний известный соревновательный матч как `last_known_match_id`.
4. Пользователь делает ставку на победу в своём следующем ranked/competitive матче: `/bet 100`.
5. Сумма списывается из доступного баланса и замораживается в `frozen_balance`.
6. Worker периодически проверяет историю матчей через Dota provider.
7. Первый новый competitive match после `last_known_match_id` считается целевым матчем ставки.
8. Если пользователь выиграл, frozen amount снимается, а на доступный баланс начисляется `potential_payout`.
9. Если пользователь проиграл, frozen amount окончательно списывается.
10. Бот присылает результат, а пользователь может посмотреть `/history`.

Пример: баланс 1000, ставка 100 с odds `2.00`.

```text
до ставки:      balance=1000, frozen=0
после /bet 100: balance=900,  frozen=100
win:            balance=1100, frozen=0
loss:           balance=900,  frozen=0
```

`last_known_match_id` сохраняется именно при привязке аккаунта, чтобы старые матчи не были приняты за следующий матч после ставки.

## Запуск

Создайте локальный файл окружения:

```sh
cp .env.example .env
```

Заполните в `.env`:

```env
TELEGRAM_BOT_TOKEN=...
DOTA_PROVIDER=mock
```

Для локальной разработки по умолчанию используется `mock` provider. Он генерирует новый ranked match примерно раз в минуту. Для OpenDota:

```env
DOTA_PROVIDER=opendota
OPENDOTA_BASE_URL=https://api.opendota.com/api
DOTA_SYNC_INTERVAL_SECONDS=60
```

Запустите PostgreSQL, миграции, bot и worker:

```sh
make up
```

Логи:

```sh
make logs
```

PostgreSQL shell:

```sh
make psql
```

Остановка:

```sh
make down
```

## Команды бота

```text
/start
/link_dota <account_id>
/balance
/bet <amount>
/active_bet
/cancel_bet
/history
/help
```

Основной flow:

```text
/start
/link_dota 123456789
/balance
/bet 100
/active_bet
```

После следующего ranked/competitive матча worker рассчитает ставку и бот отправит уведомление.

## Dota Provider

Провайдер выбирается через `DOTA_PROVIDER`:

- `mock` — локальная разработка без внешнего API;
- `opendota` — OpenDota API.

OpenDota recent matches endpoint используется для истории матчей игрока, match details endpoint оставлен в provider abstraction для расширения. Worker логирует ошибки provider call и продолжает работу по другим пользователям.

## Миграции

Миграции лежат в `migrations/`. При `make up` запускается one-shot compose service `migrate`.

Вручную:

```sh
make migrate-up
make migrate-down
```
