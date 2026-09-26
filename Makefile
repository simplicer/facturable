GO := go

.PHONY: up down build test test-unit lint run-sello run-api clean compose-config

up:
	docker compose -f infra/docker-compose.yml up -d --build

down:
	docker compose -f infra/docker-compose.yml down

compose-config:
	docker compose -f infra/docker-compose.yml config -q

build:
	$(GO) build ./...

test: test-unit

test-unit:
	$(GO) test ./... -count=1

lint:
	$(GO) vet ./...

run-api:
	$(GO) run ./cmd/api

run-sello:
	$(GO) run ./cmd/sello

clean:
	rm -rf var/* web/.svelte-kit web/build
