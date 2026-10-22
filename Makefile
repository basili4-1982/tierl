.PHONY: fmt lint vet test cover build run up down

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

run:
	go run ./cmd/tierl

up:
	docker compose up --build

down:
	docker compose down
