.PHONY: check fmt-check fmt vet test audit spec-check compose-check generate generate-check configure up down logs update-providers
check: fmt-check vet test audit compose-check generate-check
fmt-check:
	@test -z "$$(gofmt -l cmd internal migrations)" || { gofmt -l cmd internal migrations; exit 1; }
fmt:
	gofmt -w cmd internal migrations
vet:
	go vet ./...
test:
	go test -race ./...
audit:
	go run ./cmd/repository-check
spec-check:
	openspec validate --all --strict --no-interactive
compose-check:
	docker compose -p bifrost-go --env-file .go.env.example config --quiet

generate:
	go tool sqlc generate
generate-check:
	go tool sqlc diff
configure:
	go run ./cmd/bifrost configure
# Explicit project/env selection avoids inheriting the existing installation's .env.
COMPOSE = docker compose -p bifrost-go --env-file .go.env
up:
	$(COMPOSE) build
	$(COMPOSE) up -d postgres redis
	$(COMPOSE) --profile tools run --rm migrate
	$(COMPOSE) up -d bifrost openwa telegram-api
down:
	$(COMPOSE) down
logs:
	$(COMPOSE) logs -f bifrost openwa

# Resolve current stable module releases independently from the OpenWA gateway tag.
update-providers:
	go get github.com/rmyndharis/OpenWA/sdk/go@latest github.com/go-telegram/bot@latest
	go mod tidy
