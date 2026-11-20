# Repository Workflow

This repository follows the personal-project CI standard with lightweight deviations approved for a single-binary Go CLI.

## Runners

- CI runs on GitHub-hosted `ubuntu-latest` runners.

## Components

| Component | What it is | Release tag |
| --- | --- | --- |
| `cli` | The `gchat-mentions` Go binary (`cmd/gchat-mentions/`, `internal/`) | Not configured yet |

## Test Matrix

| Component | Lint | Unit | Integration | UI | Deploy |
| --- | --- | --- | --- | --- | --- |
| `cli` | `lint-cli`: push to `main` | `test-unit-cli`: push to `main` | Grouped under `test-unit-cli` (see deviations) | None | None |

## Make Targets

| Target | Runs |
| --- | --- |
| `make lint-cli` | `gofmt` check, `go vet ./...`, `golangci-lint` (when installed), `go build ./cmd/gchat-mentions` |
| `make lint-workflows` | `actionlint` (when installed) |
| `make lint` | `lint-cli` and `lint-workflows` |
| `make test-unit-cli` | `go test -race -shuffle=on ./...` |
| `make all-tests` | `test-unit-cli` |
| `make fix FILES=<files>` | `gofmt -w` on Go files |
| `make build` | Builds `bin/gchat-mentions` |

## Deviations from the Standard

- **Push to `main` only (no PR workflow):** Workflows run on pushes to `main` without pull-request triggers. Approved by Jai, 2026-11-20.
- **No RC or release pipeline yet:** `generate-release-candidate`, `release`, `git-cliff`, and external clock schedules are omitted while the CLI is kept lightweight. Approved by Jai, 2026-11-20.
- **Local `t.TempDir()` file tests in `test-unit-cli`:** Config, state, and CLI tests use isolated `t.TempDir()` directories with zero network or external processes and run in ~10ms, so they stay under `test-unit-cli` instead of a separate `test-integration-cli` workflow. Approved by Jai, 2026-11-21.
- **No `.claude/hooks/block-no-verify.py`:** Omitted from this public repository; `.pre-commit-config.yaml` enforces local pre-commit lint and pre-push tests. Approved by Jai, 2026-11-21.
