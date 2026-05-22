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
  throttle_high_threshold: 10
  mem_high_threshold: 0.9
  p95_low_threshold: 0.4
  rps_density_threshold: 0.1
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

## 3. 运行

```bash
# 一体化分析（资源建议 + 效率 + 健康）
./kops analyze --config config.yaml
./kops analyze --config config.yaml -o markdown
./kops analyze --config config.yaml -o csv
./kops analyze --config config.yaml -o json
```

## 4. 常用命令

```bash
go test ./...
./kops analyze -n prod -d 5m -t 0.02
```

## 5. 输出说明

`kops analyze` 输出四个部分：

- **资源建议**: CPU/内存推荐值、风险标签、Gateway 分摊和节省金额
- **流量效率**: 流量密度、浪费率和资源黑洞排序
- **资源黑洞 Top 5**: 按浪费金额排序的低效高成本服务
- **健康检查**: 服务健康状态、错误率和无效投入
