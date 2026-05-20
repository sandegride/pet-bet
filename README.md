# Dota Bet Bot

Telegram-бот на Go для виртуальных ставок на Dota 2 матчи.

## Локальный запуск через Docker

Создайте локальный файл окружения:

```sh
cp .env.example .env
```

Заполните `TELEGRAM_BOT_TOKEN` в `.env`. `DATABASE_URL` можно оставить пустым: приложение соберет строку подключения из `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_USER`, `POSTGRES_PASSWORD` и `POSTGRES_DB`.

Запустите проект:

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

Redis добавлен как optional service. Для запуска вместе с Redis используйте:

```sh
docker compose --profile redis up -d --build
```
