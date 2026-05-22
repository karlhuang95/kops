# 🏥 Health 检查说明

## 功能概述

`kops analyze` 中的健康检查部分是一个"业务保障雷达"，通过分析 Traefik 监控指标，检查服务的健康状态并计算因错误导致的无效投入金额。

### 核心价值

当你把红码服务（特别是带金额损失的）截图发给研发时，响应速度绝对比发 CPU 利用率快得多。

## 健康状态分级

| 状态 | 图标 | 判定条件 | 处理优先级 |
|------|------|----------|-----------|
| 🔴 **红码** (Critical) | 🔴 | 5xx 错误率 > 阈值 <br> 或 P99 延迟 > 2s <br> 或 Pod 重启 | **立即处理** |
| 🟡 **黄码** (Warning) | 🟡 | 4xx 错误率 > 5% <br> 或 P99 延迟 > 1s <br> 或 CPU 利用率 > 85% | **需关注** |
| 🟢 **绿码** (Healthy) | 🟢 | 以上皆无，且有流量 | 正常运行 |
| ⚪ **灰码** (Idle) | ⚪ | RPS < 0.001 (无流量) | 检查是否需要下线 |

## 无效投入计算

### 何时计算无效投入

无效投入仅在实际产生错误流量时才计算：

| 判定条件 | 无效投入 | 说明 |
|----------|---------|------|
| 5xx 错误率超阈值 | `MonthlyCost × Error5xxRate` | 服务器错误导致的实际浪费 |
| 4xx 错误率 > 5% | `MonthlyCost × Error4xxRate` | 客户端错误导致的实际浪费 |
| P99 延迟超 2s | 0 | 性能风险指标，非实际浪费 |
| Pod 重启 | 0 | 稳定性风险指标，非实际浪费 |
| CPU 利用率 > 85% | 0 | 容量风险指标，非实际浪费 |
| 运行稳健 | 0 | 无浪费 |

### 全链路损耗

对于有错误率的情况，`WastedSpendTotal` 包含 Pod 成本 + Gateway 分摊成本的错误比例：`(PodCost + GwShareCost) × ErrorRate`

## 命令用法

### 基本用法

```bash
# 一体化分析（含健康检查）
./kops analyze --config config.yaml

# 自定义分析时间窗口
./kops analyze -d 10m

# 自定义 5xx 错误率阈值
./kops analyze -t 0.05  # 5%

# 指定输出格式
./kops analyze -o markdown
```

### 参数说明

| 参数 | 短参数 | 说明 | 默认值 |
|------|--------|------|--------|
| `--namespace` | `-n` | 指定要检查的命名空间 | 配置中的 namespaces |
| `--duration` | `-d` | 分析时间窗口 | `5m` |
| `--threshold` | `-t` | 5xx 错误率报警阈值 | `0.02` (2%) |
| `--output` | `-o` | 报告输出格式 | `table` |

### 输出格式说明

| 格式 | 说明 | 使用场景 |
|------|------|----------|
| `table` | 表格格式（默认） | 终端直接查看 |
| `csv` | CSV 格式 | 数据导入、分析 |
| `json` | JSON 格式 | 程序处理、API 集成 |
| `markdown` / `md` | Markdown 文档格式 | 生成报告文档 |

## 诊断建议说明

### 🔴 红码场景

1. **5xx 错误率超阈值**
   - 诊断：`5xx 错误率 XX% 超过阈值 X%`
   - 建议：`立即排查后端服务异常`
   - 无效投入：`MonthlyCost × Error5xxRate`（实际浪费）

2. **P99 延迟超 2s**
   - 诊断：`P99 延迟 X.XXs 超过 2s，影响用户体验`
   - 建议：`排查慢查询/依赖服务超时`
   - 无效投入：0（性能风险，非实际浪费）

3. **Pod 重启**
   - 诊断：`Pod 重启 X 次，服务不稳定`
   - 建议：`检查应用崩溃日志/OOM Killer`
   - 无效投入：0（稳定性风险，非实际浪费）

### 🟡 黄码场景

1. **4xx 错误率偏高**
   - 诊断：`4xx 错误率 X.X% 偏高，检查调用方/鉴权`
   - 建议：`检查 API 参数、Token 权限`
   - 无效投入：`MonthlyCost × Error4xxRate`（实际浪费）

2. **P99 延迟较高**
   - 诊断：`P99 延迟 X.XXs 较高，可能影响用户体验`
   - 建议：`排查慢 SQL/缓存未命中`
   - 无效投入：0（性能风险，非实际浪费）

3. **CPU 利用率偏高**
   - 诊断：`CPU 利用率 XX.X% 偏高，存在性能瓶颈`
   - 建议：`考虑扩容或优化代码`
   - 无效投入：0（容量风险，非实际浪费）

### 🟢 绿码场景

- 诊断：`运行稳健`
- 建议：`保持现状`
- 无效投入：0

### ⚪ 灰码场景

- 诊断：`无流量`
- 建议：`检查服务是否需要下线`
- 无效投入：0

## Traefik 监控指标说明

### 使用的 PromQL 查询

| 维度 | PromQL 模板 | 目的 |
|------|-------------|------|
| **流量基数** | `sum(irate(traefik_service_requests_total{service=~"$SVC.*"}[$D]))` | 计算分母 |
| **5xx 错误** | `sum(irate(traefik_service_requests_total{service=~"$SVC.*", code=~"5.."}[$D]))` | 逻辑健康检查 |
| **4xx 错误** | `sum(irate(traefik_service_requests_total{service=~"$SVC.*", code=~"4.."}[$D]))` | 调用方/鉴权检查 |
| **P99 响应** | `histogram_quantile(0.99, sum(irate(traefik_service_request_duration_seconds_bucket{service=~"$SVC.*"}[$D])) by (le))` | 用户体验检查 |

### irate 函数说明

`irate()` 用于计算**瞬时速率**，适合短时间窗口（如 5m）的流量变化分析：

```
irate(range_vector) = (last - prev) / (timestamp_last - timestamp_prev)
```

## 使用场景

### 场景 1：日常巡检

```bash
# 每天早上运行一次，查看整体健康状态
./kops analyze --config config.yaml

# 生成 Markdown 报告文档
./kops analyze --config config.yaml -o markdown > report.md
```

### 场景 2：故障排查

```bash
# 发现某个服务异常，使用更短的时间窗口分析
./kops analyze -n production -d 1m -t 0.01  # 1% 阈值

# 导出 JSON 格式，用于告警系统处理
./kops analyze -n production -o json | jq '.health_statuses[] | select(.health_code == "Critical")'
```

### 场景 3：成本分析

```bash
# 导出 CSV 格式，用于 Excel 分析
./kops analyze -n production -o csv > analysis_data.csv
```

## 技术实现

### 核心组件

1. **采集层** (`internal/platform/collector/`)
   - Prometheus 指标采集（CPU/内存/RPS）
   - Traefik 健康指标采集

2. **健康引擎** (`pkg/advisor/health.go`)
   - 实现 `DetermineHealthCode` 函数
   - 一票否决制的健康判定逻辑
   - 仅对错误率条件计算无效投入

3. **编排层** (`internal/app/analyze/service.go`)
   - 统一编排采集、分析、渲染流程

### 状态判定逻辑（一票否决制）

```go
// 按优先级检查，任一条件满足立即返回
if 5xx 错误率 > 阈值 {
    InvalidSpend = MonthlyCost × Error5xxRate  // 实际浪费
    return 🔴 Critical
}
if P99 延迟 > 2s {
    InvalidSpend = 0  // 性能风险，非浪费
    return 🔴 Critical
}
if Pod 重启 {
    InvalidSpend = 0  // 稳定性风险，非浪费
    return 🔴 Critical
}
if 4xx 错误率 > 5% {
    InvalidSpend = MonthlyCost × Error4xxRate  // 实际浪费
    return 🟡 Warning
}
// ... 其他黄码条件（InvalidSpend = 0）
InvalidSpend = 0  // 绿码无浪费
return 🟢 Healthy
```

## 常见问题

### Q1: 为什么有些服务显示 "无流量"？

A: 这些服务的 RPS < 0.001，可能是：
- 新部署的服务
- 已下线但未删除的服务
- 测试/备用服务

建议：确认是否需要下线以节省成本

### Q2: 无效投入金额如何计算？

A: 仅当服务存在 5xx 或 4xx 错误率超标时计算无效投入。P99 延迟、Pod 重启、CPU 利用率等风险指标的无效投入为 0——它们是需要关注的信号，但不直接产生浪费。

### Q3: 为什么红码服务的无效投入这么高？

A: 红码由 5xx 高错误率触发时，大量请求失败意味着投入的资源被浪费。P99 延迟或 Pod 重启触发的红码则不含无效投入——它们是稳定性/性能问题而非直接浪费。

---

**将红码服务截图发给研发，响应速度绝对比发 CPU 利用率快得多！** 🚀
