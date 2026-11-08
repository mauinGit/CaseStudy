SWAG_VERSION := v1.16.4
TEST_DATABASE_URL ?= postgres://gdgoc:gdgoc@localhost:5432/gdgoc?sslmode=disable

.PHONY: up down run test test-race lint docs

## up: start API + PostgreSQL (migrations and seed run automatically)
up:
	docker compose up -d --build

## down: stop containers and delete the local database volume
down:
	docker compose down -v

## run: run the API locally (needs a .env, see .env.example)
run:
	go run ./cmd/api

## test: run all tests against PostgreSQL (start it first with `make up`)
test:
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -count=1 ./...

test-race:
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -race -count=1 ./...

lint:
	test -z "$$(gofmt -l .)"
	go vet ./...

## docs: regenerate docs/docs.go (served at /docs) and openapi.yaml
docs:
	go run github.com/swaggo/swag/cmd/swag@$(SWAG_VERSION) init -g cmd/api/main.go -o docs --outputTypes go,yaml --parseInternal
	mv docs/swagger.yaml openapi.yaml
