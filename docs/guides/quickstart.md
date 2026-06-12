# 快速开始

## 1. 构建

```bash
go build -o kops .
```

## 2. 配置

最少需要准备这些配置：

```yaml
namespaces:
  - demo-prod

prometheus:
  address: "https://prom.example.com"
  timeout: 30s
  range: "7d"
  step: "5m"

governance:
  cpu_step: 50
  memory_step: 128
  min_cpu: 100
  min_memory: 128
  cpu_limit_factor: 2.0
  mem_limit_factor: 1.1
  target_utilization: 0.8
  memory_target_utilization: 0.8      # v2.0 新增：内存利用率目标
  throttle_high_threshold: 10
  mem_high_threshold: 0.9
  p95_low_threshold: 0.4
  rps_density_threshold: 100.0         # v2.0 更新：RPS/Core 单位
  black_hole_cost_threshold: 100
  high_traffic_threshold: 0.3
  task_service_patterns:
    - cron
    - consumer
    - job
    - worker

cost:
  price: 1197.42
  count: 4
  cpu_cores: 16
  memory_gb: 64
  resource_weight:
    cpu: 1.0
    memory: 0.0

gateway_cost:
  price: 478.47
  count: 2
```

完整示例见 [config.yaml](../../config.yaml)。

## 3. CLI 分析

```bash
# 一体化分析（资源建议 + 效率 + 健康）
./kops analyze --config config.yaml
./kops analyze --config config.yaml -o markdown
./kops analyze --config config.yaml -o csv
./kops analyze --config config.yaml -o json

# 指定命名空间
./kops analyze -n prod -d 5m -t 0.02
```

## 4. Web Dashboard

```bash
# 启动仪表盘
./kops serve --config config.yaml

# 自定义端口和缓存目录
./kops serve --config config.yaml -p 9090 --cache-dir ./cache
```

访问 `http://localhost:8080` 进入总览页。

### 页面导航

| 页面 | URL | 功能 |
|------|-----|------|
| 总览 | `/` | 统计卡片 + 6 图表 + 趋势对比（本次 vs 上次） |
| 资源推荐 | `/recommendations` | Advisor CPU/内存建议 + 成本 + 风险 + kubectl 命令复制 |
| 流量效率 | `/efficiency` | 流量密度 S/A/B/C 评级 + 资源黑洞 Top 5 |
| 健康状态 | `/health` | Critical/Warning/Healthy/Idle + 健康分 + 无效支出 |
| 集群分析 | `/cluster` | 节点密度 + 伸缩建议 + 按 NS/App 成本归属 |
| 服务详情 | `/service/:ns/:name` | CPU/Mem/RPS 折线图（6h~30d）+ 资源预测 + 推荐对比 |

### 快捷键

按 `?` 查看所有键盘快捷键。

### API 端点

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
| `POST /api/refresh` | 刷新数据缓存 |
| `POST /api/config/reload` | 配置热加载 |
| `GET /api/export/csv` | 导出 CSV |
| `GET /api/export/json` | 导出 JSON |

## 5. 测试

```bash
go test ./...
```

## 6. 输出说明

`kops analyze` 输出三个部分：

- **资源建议**：CPU/内存推荐值、风险标签、Gateway 分摊和节省金额
- **流量效率**：流量密度 (RPS/Core)、浪费率和资源黑洞排序
- **健康检查**：服务健康状态、错误率、P99 延迟和无效投入
