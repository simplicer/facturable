GO := go
VERSION := $(shell cat VERSION)

.PHONY: up down build test test-unit lint analyze web-check version-check acceptance run-sello run-api clean

up:
	docker compose -f infra/docker-compose.yml up -d --build

down:
	docker compose -f infra/docker-compose.yml down

build:
	$(GO) build ./...

test: test-unit

test-unit:
	$(GO) test ./... -count=1

lint: analyze

analyze:
	$(GO) vet ./...

version-check:
	V=$$(cat VERSION); \
	grep -q "$$V" CHANGELOG.md && \
	grep -q "$$V" web/package.json && \
	grep -q "version: $$V" docs/openapi.yaml && \
	echo "version OK: $$V"

acceptance:
	bin/acceptance.sh

bump-patch bin/bump.sh:
	@echo "use bin/bump.sh patch|minor"

run-api:
	$(GO) run ./cmd/api

run-sello:
	$(GO) run ./cmd/sello

clean:
	rm -rf web/.svelte-kit web/build dist
	@echo "NOT deleting var/ (contains your database and certificates)"

e2e: cd web && npx playwright test
