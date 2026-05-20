COMPOSE := docker compose
MIGRATE_IMAGE ?= migrate/migrate:v4.17.1
MIGRATIONS_DIR ?= migrations
DOCKER_NETWORK ?= dota_bet_bot_network

ifneq (,$(wildcard .env))
include .env
export
endif

POSTGRES_HOST ?= postgres
POSTGRES_PORT ?= 5432
POSTGRES_USER ?= bot
POSTGRES_PASSWORD ?= bot
POSTGRES_DB ?= dota_bet_bot
MIGRATE_DATABASE_URL := $(if $(DATABASE_URL),$(DATABASE_URL),postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@$(POSTGRES_HOST):$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable)
MIGRATE_STEPS ?= 1

.PHONY: up down logs build restart migrate-up migrate-down psql

up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f bot worker postgres

build:
	$(COMPOSE) build

restart:
	$(COMPOSE) restart bot

migrate-up:
	@if [ ! -d "$(MIGRATIONS_DIR)" ]; then \
		echo "No $(MIGRATIONS_DIR) directory yet."; \
		exit 0; \
	fi
	docker run --rm \
		--network $(DOCKER_NETWORK) \
		-v "$(CURDIR)/$(MIGRATIONS_DIR):/migrations:ro" \
		$(MIGRATE_IMAGE) \
		-path=/migrations \
		-database "$(MIGRATE_DATABASE_URL)" \
		up

migrate-down:
	@if [ ! -d "$(MIGRATIONS_DIR)" ]; then \
		echo "No $(MIGRATIONS_DIR) directory yet."; \
		exit 0; \
	fi
	docker run --rm \
		--network $(DOCKER_NETWORK) \
		-v "$(CURDIR)/$(MIGRATIONS_DIR):/migrations:ro" \
		$(MIGRATE_IMAGE) \
		-path=/migrations \
		-database "$(MIGRATE_DATABASE_URL)" \
		down $(MIGRATE_STEPS)

psql:
	$(COMPOSE) exec postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB)
