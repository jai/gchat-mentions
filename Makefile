.PHONY: build lint lint-cli lint-workflows test test-unit-cli all-tests fix run install

BIN := bin/gchat-mentions

build:
	go build -o $(BIN) ./cmd/gchat-mentions

fix:
	@if [ -n "$(FILES)" ]; then \
		go_files=$$(printf '%s\n' $(FILES) | grep '\.go$$' || true); \
		if [ -n "$$go_files" ]; then gofmt -w $$go_files; fi; \
	else \
		gofmt -w .; \
	fi

lint-cli:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "Unformatted Go files:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	go vet ./...
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	fi
	go build ./cmd/gchat-mentions

lint-workflows:
	@if command -v actionlint >/dev/null 2>&1; then \
		actionlint; \
	else \
		echo "actionlint not installed; skipping local workflow lint"; \
	fi

lint: lint-cli lint-workflows

test-unit-cli:
	go test -race -shuffle=on ./...

test: test-unit-cli

all-tests: test-unit-cli

run: build
	./$(BIN)

install:
	go install ./cmd/gchat-mentions
