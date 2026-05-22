# Advisor 算法说明

`kops analyze` 中的资源建议部分分成四步。

## 1. 流量与 Gateway 分摊

实现位置：
- [pkg/algorithm/cost.go](../../pkg/algorithm/cost.go)
- [pkg/advisor/engine.go](../../pkg/advisor/engine.go)

规则：
- 匹配 `task_service_patterns` 中任一模式的服务视为 task 型服务，不参与 Gateway 成本分摊
- 默认模式：`cron`、`consumer`、`job`、`worker`，可在配置中自定义
- 其他服务按 `AvgRPS / totalTrafficRPS` 计算流量权重
- Gateway 分摊成本 = `rpsWeight * gatewayTotalCost`

## 2. 资源建议

CPU：
- 当 `CPUUsageMax < p95_low_threshold * CPURequest` 时，直接建议 `min_cpu`
- 否则按 `CPUUsageMax / target_utilization` 计算，再按 `cpu_step` 向上对齐

内存：
- 按 `MemUsageMax` 计算，再按 `memory_step` 向上对齐
- 最低不小于 `min_memory`

## 3. 风险和标签

高风险：
- `ThrottleSecond > throttle_high_threshold`
- 或 `MemUsageMax / MemLimit > mem_high_threshold`

核心流量服务：
- `rpsWeight > high_traffic_threshold`

资源黑洞：
- `rpsWeight < 0.01`
- 且 `PodCost > black_hole_cost_threshold`

低效服务：
- `RPSDensity < rps_density_threshold`

## 4. 决策顺序

判定顺序是固定的：

1. 高风险服务优先，直接给扩容建议
2. task 型服务按利用率和建议值判断缩容/扩容/保持
3. 核心流量服务优先保护，不自动缩容
4. 命中黑洞标签时给缩容建议
5. 低频服务标记为 `Cold Start`
6. 其余服务按当前配置和建议配置比较

## 关键配置

见 [config.yaml](../../config.yaml) 中这些字段：

- `governance.target_utilization`
- `governance.cpu_step`
- `governance.memory_step`
- `governance.min_cpu`
- `governance.min_memory`
- `governance.throttle_high_threshold`
- `governance.mem_high_threshold`
- `governance.p95_low_threshold`
- `governance.rps_density_threshold`
- `governance.black_hole_cost_threshold`
- `governance.high_traffic_threshold`
- `governance.task_service_patterns`
- `gateway_cost.price`
- `gateway_cost.count`
