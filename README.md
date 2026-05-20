# Dota Bet Bot

Telegram-бот на Go для виртуальных ставок на Dota 2 матчи.

Проект сейчас работает только с виртуальными монетами. Реальных денег, платежей, депозитов, вывода средств, KYC и внешних betting API здесь нет.

## Запуск

Создайте локальный файл окружения:

```sh
cp .env.example .env
```

Заполните в `.env`:

```env
TELEGRAM_BOT_TOKEN=...
ADMIN_TELEGRAM_IDS=123456789,987654321
```

`DATABASE_URL` можно оставить пустым: приложение соберёт строку подключения из `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_USER`, `POSTGRES_PASSWORD` и `POSTGRES_DB`.

Запустите PostgreSQL, миграции и бота:

```sh
make up
```

Посмотреть логи:

```sh
make logs
```

Подключиться к PostgreSQL:

```sh
make psql
```

Остановить проект:

```sh
make down
```

Redis добавлен как optional service:

```sh
docker compose --profile redis up -d --build
```

## Команды бота

Пользовательские команды:

```text
/start
/balance
/next
/history
/help
```

Админские команды:

```text
/admin_add_match Team A | Team B | Tournament | 2026-05-25 18:00 | 1.75 | 2.05
/admin_finish_match 1 | Team A
/admin_cancel_match 1
```

Админы задаются через `ADMIN_TELEGRAM_IDS` в `.env`, через запятую.

## Пример flow

1. Пользователь пишет `/start` и получает `INITIAL_BALANCE`.
2. Админ создаёт матч:

```text
/admin_add_match Team Spirit | BetBoom Team | DreamLeague | 2026-05-25 18:00 | 1.75 | 2.05
```

3. Пользователь открывает `/next`, нажимает inline-кнопку ставки и вводит сумму.
4. Админ завершает матч:

```text
/admin_finish_match 1 | Team Spirit
```

5. Бот рассчитывает pending-ставки, начисляет выигрыши через wallet service и пишет все изменения баланса в `transactions`.
6. Пользователь смотрит `/balance` и `/history`.

## Миграции

Миграции лежат в `migrations/`. При `make up` запускается one-shot compose service `migrate`.

Вручную:

```sh
make migrate-up
make migrate-down
```
