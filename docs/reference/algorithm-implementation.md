# 算法实现细节

这份文档只描述当前仓库里的实际实现，不保留历史版本方案。

## 成本与分摊

实现位置：
- [pkg/algorithm/cost.go](../../pkg/algorithm/cost.go)

当前实现提供这些能力：
- Pod 月成本计算
- 实际消耗月成本计算
- Gateway 总成本计算
- 按流量权重分摊 Gateway 成本
- 全链路单位成本计算
- 黑洞和核心流量服务判定

task 型服务识别规则：
- 通过 `governance.task_service_patterns` 配置匹配模式（默认：`cron`、`consumer`、`job`、`worker`）
- 名称包含任一模式即为 task 型服务
- task 型服务不参与 Gateway 成本分摊

## Advisor 决策

实现位置：
- [pkg/advisor/engine.go](../../pkg/advisor/engine.go)

关键流程：
1. 先调用成本算法补齐 `RpsWeight`、`GwShareCost`、`FullUnitCost`、`TotalCost`
2. 计算 CPU 和内存建议值
3. 计算当前成本、建议成本、实际成本
4. 打上高风险、黑洞、核心流量、低效等标签
5. 按固定优先级给出动作和原因

优先级顺序：
1. 高风险
2. task 型服务
3. 核心流量保护
4. 黑洞
5. 低频运行
6. 常规缩容/扩容/保持

## 统一分析流程

实现位置：
- [internal/app/analyze/service.go](../../internal/app/analyze/service.go)

当前链路：
1. 采集 Prometheus 指标（一次采集，全部分析共享）
2. 运行资源建议引擎（CPU/内存推荐、风险标签、Gateway 分摊）
3. 运行流量效率引擎（流量密度、浪费率、资源黑洞）
4. 采集 Traefik 健康指标
5. 运行健康检查引擎（一票否决制判定）
6. 渲染统一报告（table/csv/json/markdown）

## 相关配置

关键字段见 [config.yaml](../../config.yaml)：

- `cost.price`
- `cost.count`
- `cost.cpu_cores`
- `cost.memory_gb`
- `cost.resource_weight`
- `gateway_cost.price`
- `gateway_cost.count`
- `governance.target_utilization`
- `governance.cpu_step`
- `governance.memory_step`
- `governance.min_cpu`
- `governance.min_memory`
- `governance.throttle_high_threshold`
- `governance.mem_high_threshold`
- `governance.black_hole_cost_threshold`
- `governance.high_traffic_threshold`
- `governance.task_service_patterns`
