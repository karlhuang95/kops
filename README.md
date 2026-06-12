中文 | [English](README_EN.md)


# kops

`kops` 是一个面向 Kubernetes 的资源治理和 FinOps CLI，基于 Prometheus 指标生成容量建议、健康诊断和成本分析。

![](docs/img/kops.png)

## 依赖

- kube-prometheus-stack v55.5
- traefik 2.11.2

## 功能

### CLI 分析

```bash
# 一体化分析（资源建议 + 效率 + 健康）
./kops analyze --config config.yaml
./kops analyze --config config.yaml -o markdown
./kops analyze -n prod -d 5m -t 0.02
```

### Web Dashboard

```bash
# 启动 Web 仪表盘
./kops serve --config config.yaml

# 自定义端口
./kops serve --config config.yaml -p 9090
```

访问 `http://localhost:8080`：

| 页面 | URL | 功能 |
|------|-----|------|
| 总览 | `/` | 6 张图表 + 统计卡片 + 趋势对比 |
| 资源推荐 | `/recommendations` | CPU/内存建议 + 成本 + 风险等级 + kubectl 命令 |
| 流量效率 | `/efficiency` | 流量密度 S/A/B/C 评级 + 资源黑洞 |
| 健康状态 | `/health` | Critical/Warning/Healthy/Idle 健康检查 |
| 集群分析 | `/cluster` | 节点密度 + 伸缩建议 + 成本归属 |
| 服务详情 | `/service/:ns/:name` | CPU/内存/RPS 折线图 + 资源预测 |

Dashboard 特性：
- 暗色模式、列排序、筛选芯片、行展开详情
- 键盘快捷键（`?` 查看）、kubectl 命令一键复制
- Prometheus 连通性状态指示器
- 自动刷新、CSV/JSON 导出

## 目录

```
cmd/                          # Cobra 命令入口
internal/
├── app/
│   ├── analyze/              # 统一分析编排
│   ├── common/               # 共享工具
│   └── serve/                # Web 服务 + 模板 + 缓存 + 告警
├── domain/                   # 领域类型 (advisor/health/metrics)
└── platform/
    ├── collector/            # Prometheus + Traefik + K8s 采集
    ├── config/               # 配置加载与验证
    └── pricing/              # 成本模型 + 推荐算法
pkg/
├── advisor/                  # 资源建议、效率分析、健康检查引擎
├── algorithm/                # 成本与评分算法
├── config/                   # 配置类型别名
└── model/                    # 领域类型别名
docs/                         # 设计文档
```

## 常用命令

```bash
go build -o kops .
go test ./...

# 一体化分析:
./kops analyze --config config.yaml
./kops analyze --config config.yaml -o markdown
./kops analyze -n prod -d 5m -t 0.02
```

## 配置

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
  memory_target_utilization: 0.8   # 新增：内存利用率目标
  black_hole_cost_threshold: 100.0

cost:
  price: 1197.42
  cpu_cores: 16
  memory_gb: 64

gateway_cost:
  price: 478.47
  count: 3
```

## API 端点

| 端点 | 说明 |
|------|------|
| `GET /api/analysis` | 全量分析 JSON |
| `GET /api/trend` | 趋势对比 |
| `GET /api/cluster/nodes` | 节点密度 |
| `GET /api/cluster/scaling` | 伸缩建议 |
| `GET /api/cost-attribution` | 成本归属 |
| `GET /api/forecast/:ns/:name` | 资源预测 |
| `GET /api/service/:ns/:name/recommendation` | 单服务推荐 |
| `GET /api/service/:ns/:name/timeseries` | 单服务时间序列 |
| `POST /api/refresh` | 刷新缓存 |
| `POST /api/config/reload` | 配置热加载 |
| `GET /api/export/csv` / `json` | 导出 |

## 文档

- 快速开始: [docs/guides/quickstart.md](docs/guides/quickstart.md)
- 算法说明: [docs/reference/advisor-algorithm.md](docs/reference/advisor-algorithm.md)
- 健康模块: [docs/reference/health.md](docs/reference/health.md)
- 术语表: [docs/GLOSSARY.md](docs/GLOSSARY.md)

## 变更记录

### v2.0 (2026-06-12)

**架构：** Dashboard 从单页面 5 Tab 重构为独立路由页面，新增 7 个 API 端点。

**修复：** Advisor 除零、Efficiency 评分异常、Health 字段缺失、Prometheus timeout 不生效、QueryRange timestamp 解析错误、pod!~ 正则引号缺失（8 处 PromQL 语法错误）等 10+ Bug。

**新增：** 节点密度分析、伸缩建议、成本归属、资源预测、趋势对比、结构化日志、API 限流、Webhook 告警、暗色模式、列排序、筛选芯片、kubectl 命令复制。

**算法：** 统一黑洞检测（利用率+流量）、统一成本口径（含 Gateway 分摊）、统一密度单位（RPS/Core）、内存利用率目标、Pod 精准匹配排除 consumer/cron/job/worker。

详见 [CHANGELOG.md](CHANGELOG.md)
