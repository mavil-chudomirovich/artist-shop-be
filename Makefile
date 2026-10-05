run:
	go run ./cmd/api

build:
	go build -o bin/api ./cmd/api
	go build -o bin/migrate ./cmd/migrate

test:
	go test ./...

lint:
	@files="$$(gofmt -l .)"; if [ -n "$$files" ]; then echo "unformatted files:"; echo "$$files"; exit 1; fi
	go vet ./...
	golangci-lint run

migrate-up:
	go run ./cmd/migrate up

migrate-down:
	go run ./cmd/migrate down

migrate-status:
	go run ./cmd/migrate status

tidy:
	go mod tidy

.PHONY: run build test lint migrate-up migrate-down migrate-status tidy
