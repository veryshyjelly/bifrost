.PHONY: check fmt-check fmt vet test audit spec-check compose-check
check: fmt-check vet test audit compose-check
fmt-check:
	@test -z "$$(gofmt -l cmd internal)" || { gofmt -l cmd internal; exit 1; }
fmt:
	gofmt -w cmd internal
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
