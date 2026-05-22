# Repository Guidelines

## Project Structure & Module Organization

`kops` is a Go CLI for Kubernetes resource governance and FinOps analysis. The entrypoint is `main.go`, with Cobra commands in `cmd/` (`advisor`, `health`, `inspect`, `finops`). Core business logic lives under `pkg/`: use `pkg/advisor/` for recommendation and decision flow, `pkg/algorithm/` for deterministic cost and scoring math, `pkg/collector/` for Prometheus and Traefik data collection, `pkg/config/` for config loading, `pkg/model/` for shared structs, and `pkg/finops/` for reporting, forecasting, monitoring, and policy modules. Runtime configuration is centered on `config.yaml`.

## Build, Test, and Development Commands

Use standard Go tooling:

- `go build -o kops .` builds the CLI binary.
- `go test ./...` runs the full test suite.
- `go test ./pkg/algorithm/... -v` runs algorithm-focused tests with verbose output.
- `go test ./pkg/advisor -run TestName` runs a targeted test while iterating.
- `./kops advisor --config config.yaml` runs the main analyzer locally.

## Coding Style & Naming Conventions

Follow idiomatic Go. Format all changes with `gofmt` before review. Keep packages small and cohesive; place reusable domain structs in `pkg/model/`, pure calculations in `pkg/algorithm/`, and command wiring in `cmd/`. Exported identifiers use `PascalCase`; unexported helpers use `camelCase`; test files must end with `_test.go`. Prefer descriptive command names and config keys that match existing terminology such as `target_utilization` and `cpu_step`.

## Testing Guidelines

The repository currently contains Go unit tests in `pkg/algorithm/cost_test.go`; extend that pattern for new logic. Favor table-driven tests for calculation-heavy code and deterministic fixtures for config-driven behavior. When adding metrics or recommendation logic, add tests close to the owning package and run `go test ./...` before submitting.

## Commit & Pull Request Guidelines

Git history is not available in this workspace snapshot, so no repository-specific commit convention could be inferred. Use short, imperative commit subjects such as `add finops forecast validation`. Pull requests should summarize behavior changes, list affected commands or config fields, reference related issues, and include sample CLI output when user-facing reports change.

## Configuration & Safety Notes

Do not hardcode cluster endpoints or credentials. Keep environment-specific values in `config.yaml`, and sanitize any real Prometheus or gateway data before sharing logs, fixtures, or screenshots.
