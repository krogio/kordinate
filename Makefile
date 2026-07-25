.PHONY: run test lint build

run:
	UPSTREAM_MODE=fake go run ./cmd/kordinate

build:
	go build ./...

test:
	go test ./...

lint:
	gofumpt -l -w . && golangci-lint run
