[中文](README.md) | English

# kops

`kops` is a Kubernetes resource governance and FinOps CLI that generates capacity recommendations, health diagnostics, and cost analysis based on Prometheus metrics.

![](docs/img/kops.png)

## Dependencies

- kube-prometheus-stack v55.5
- traefik 2.11.2

## Features

### CLI Analysis

```bash
# Unified analysis (resource + efficiency + health)
./kops analyze --config config.yaml
./kops analyze --config config.yaml -o markdown
./kops analyze -n prod -d 5m -t 0.02
```

### Web Dashboard

```bash
# Start web dashboard
./kops serve --config config.yaml

# Custom port
./kops serve --config config.yaml -p 9090
```

Visit `http://localhost:8080`:

| Page | URL | Description |
|------|-----|-------------|
| Overview | `/` | 6 charts + stat cards + trend comparison |
| Recommendations | `/recommendations` | CPU/Memory advice + cost + risk + kubectl command |
| Efficiency | `/efficiency` | Traffic density S/A/B/C ranking + black holes |
| Health | `/health` | Critical/Warning/Healthy/Idle health checks |
| Cluster | `/cluster` | Node density + scaling advice + cost attribution |
| Service Detail | `/service/:ns/:name` | CPU/Mem/RPS time series + resource forecast |

Dashboard Features:
- Dark mode, column sorting, chip filters, expandable rows
- Keyboard shortcuts (`?` for help), one-click kubectl patch copy
- Prometheus connectivity indicator
- Auto-refresh, CSV/JSON export

## Directory Structure

```
cmd/                          # Cobra command entry points
internal/
├── app/
│   ├── analyze/              # Unified analysis orchestration
│   ├── common/               # Shared utilities
│   └── serve/                # Web server + templates + cache + alerts
├── domain/                   # Domain types (advisor/health/metrics)
└── platform/
    ├── collector/            # Prometheus + Traefik + K8s collectors
    ├── config/               # Config loading and validation
    └── pricing/              # Cost model + recommendation algorithms
pkg/
├── advisor/                  # Resource advice, efficiency, health engines
├── algorithm/                # Cost & scoring algorithms
├── config/                   # Config type aliases
└── model/                    # Domain type aliases
docs/                         # Documentation
```

## Common Commands

```bash
go build -o kops .
go test ./...

# Unified analysis:
./kops analyze --config config.yaml
./kops analyze --config config.yaml -o markdown
./kops analyze -n prod -d 5m -t 0.02
```

## Configuration

```yaml
# config.yaml
namespaces:
  - web-prod
  - demo-prod

prometheus:
  address: "https://prom.example.com"
  timeout: 30s

governance:
  cpu_step: 50
  memory_step: 128
  min_cpu: 100
  min_memory: 128
  target_utilization: 0.8
  memory_target_utilization: 0.8   # New: memory utilization target
  black_hole_cost_threshold: 100.0

cost:
  price: 1197.42
  cpu_cores: 16
  memory_gb: 64

gateway_cost:
  price: 478.47
  count: 3
```

## API Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /api/analysis` | Full analysis JSON |
| `GET /api/trend` | Trend comparison |
| `GET /api/cluster/nodes` | Node density |
| `GET /api/cluster/scaling` | Scaling recommendations |
| `GET /api/cost-attribution` | Cost attribution |
| `GET /api/forecast/:ns/:name` | Resource forecast |
| `GET /api/service/:ns/:name/recommendation` | Single service recommendation |
| `GET /api/service/:ns/:name/timeseries` | Single service time series |
| `POST /api/refresh` | Refresh cache |
| `POST /api/config/reload` | Hot reload config |
| `GET /api/export/csv` / `json` | Export |

## Documentation

- Quickstart: [docs/guides/quickstart.md](docs/guides/quickstart.md)
- Algorithm: [docs/reference/advisor-algorithm.md](docs/reference/advisor-algorithm.md)
- Health module: [docs/reference/health.md](docs/reference/health.md)
- Glossary: [docs/GLOSSARY.md](docs/GLOSSARY.md)

## Changelog

### v2.0 (2026-06-12)

**Architecture:** Dashboard refactored from single-page 5-tab to independent routed pages, 7 new API endpoints.

**Bug Fixes:** Division by zero, Efficiency returning 100%, Health engine dead fields, Prometheus timeout not applied, QueryRange timestamp parsing, pod!~ regex missing quotes (8 PromQL syntax errors), and more.

**New Features:** Node density analysis, scaling recommendations, cost attribution, resource forecasting, trend comparison, structured logging, API rate limiting, webhook alerts, dark mode, column sorting, chip filters, kubectl command copy.

**Algorithm:** Unified black hole detection (utilization + traffic), unified cost model (includes Gateway share), unified density units (RPS/Core), memory utilization target, precise pod matching (excludes consumer/cron/job/worker).

See [CHANGELOG.md](CHANGELOG.md) for details.
