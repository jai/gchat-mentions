.PHONY: build lint test run install

BIN := bin/gchat-mentions

build:
	go build -o $(BIN) ./cmd/gchat-mentions

lint:
	go vet ./...
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	fi

test:
	go test -race -shuffle=on ./...

run: build
	./$(BIN)

install:
	go install ./cmd/gchat-mentions
