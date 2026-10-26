.PHONY: migrate-up migrate-down web-install web-build web-test fmt lint vet test cover build build-migrate run up down ps logs health

COVER_MIN ?= 80

fmt:
	gofmt -w .

lint:
	golangci-lint run ./...

vet:
	go vet ./...

test:
	go test -race ./...

cover:
	go test -race -coverprofile=coverage.out ./internal/...
	@go tool cover -func=coverage.out | awk -v min=$(COVER_MIN) '/^total:/ { sub("%","",$$3); if ($$3+0 < min) { print "coverage " $$3 "% < " min "%"; exit 1 } else { print "coverage " $$3 "%" } }'

build:
	CGO_ENABLED=0 go build -o bin/tierl ./cmd/tierl

build-migrate:
	CGO_ENABLED=0 go build -o bin/migrate ./cmd/migrate

run:
	go run ./cmd/tierl

# Local database from docker-compose.yml; override DATABASE_URL for another one.
DATABASE_URL ?= postgres://tierl:tierl@localhost:5432/tierl?sslmode=disable

migrate-up:
	TIERL_DATABASE_URL='$(DATABASE_URL)' go run ./cmd/migrate up

migrate-down:
	TIERL_DATABASE_URL='$(DATABASE_URL)' go run ./cmd/migrate down

up:
	docker compose up --build

down:
	docker compose down

ps:
	docker compose ps

logs:
	docker compose logs -f app

health:
	curl -fsS http://localhost:$${TIERL_PORT:-8080}/healthz && echo
	curl -fsS http://localhost:$${TIERL_PORT:-8080}/readyz && echo

web-install:
	cd web && npm ci

web-build:
	cd web && npm run build

web-test:
	cd web && npm run lint && npm run typecheck && npm test
