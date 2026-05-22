[中文](README.md) | English

# kops

`kops` is a Kubernetes resource governance and FinOps CLI that generates capacity recommendations, health diagnostics, and cost analysis based on Prometheus metrics.

![](docs/img/kops.png)

## Dependencies

- kube-prometheus-stack v55.5
- traefik 2.11.2

## Directory Structure

- `cmd/`: Cobra command entry points
- `pkg/advisor/`: Resource recommendation, efficiency analysis, and health check engines
- `pkg/algorithm/`: Cost and scoring algorithm wrappers
- `pkg/config/`: Config type aliases
- `pkg/model/`: Domain model type aliases
- `internal/app/analyze/`: Unified analysis orchestration
- `internal/domain/`: Domain type definitions
- `internal/platform/`: Prometheus collection, pricing, config loading
- `docs/`: Design, usage, and product documentation

## Common Commands

```bash
go build -o kops .
go test ./...

# Unified analysis (recommendations + efficiency + health):
./kops analyze --config config.yaml
./kops analyze --config config.yaml -o markdown
./kops analyze -n prod -d 5m -t 0.02
```

## Documentation

- Docs navigation: [docs/README.md](docs/README.md)
- Quick start: [docs/guides/quickstart.md](docs/guides/quickstart.md)
- Algorithm reference: [docs/reference/advisor-algorithm.md](docs/reference/advisor-algorithm.md)
- Health module: [docs/reference/health.md](docs/reference/health.md)
- Product docs: [docs/product/prd.md](docs/product/prd.md)
