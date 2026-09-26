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

web-check:
	cd web && npm run build

version-check:
	@test "$$(cat VERSION)" = "0.1.0-unstable" || test -n "$$(grep -q "$$(cat VERSION)" CHANGELOG.md && echo ok)"
	@cd web && grep -q "$$(cat ../VERSION)" package.json

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
