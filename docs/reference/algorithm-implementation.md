# 算法实现细节

这份文档只描述当前仓库里的实际实现，不保留历史版本方案。

## 成本与分摊

实现位置：
- [pkg/algorithm/cost.go](../../pkg/algorithm/cost.go)
- [internal/platform/pricing/cost.go](../../internal/platform/pricing/cost.go)

当前实现提供这些能力：
- Pod 月成本计算（CPU + 内存）
- 实际消耗月成本计算
- Gateway 总成本计算
- 按流量权重分摊 Gateway 成本
- 全链路单位成本计算
- 黑洞和核心流量服务判定
- 健康评分计算（多维：5xx 率、4xx 率、P99 延迟、重启次数、CPU 利用率）

task 型服务识别规则：
- 通过 `governance.task_service_patterns` 配置匹配模式（默认：`cron`、`consumer`、`job`、`worker`）
- 名称包含任一模式即为 task 型服务
- task 型服务不参与 Gateway 成本分摊

**v2.0 更新：** Pod 精准匹配使用 `pod=~"^deploy-.*"` + `pod!~"consumer|cron|job|worker"` 排除子部署 Pod，避免 CPU/内存指标被 CronJob/Consumer 污染。

## 资源推荐算法

实现位置：
- [internal/platform/pricing/recommendation.go](../../internal/platform/pricing/recommendation.go)

**CPU 推荐：**
```
raw = P95_Usage / target_utilization
rec = max(ceil(raw / cpu_step) * cpu_step, min_cpu)
若 Usage < P95LowThreshold * Request → 判定为冷启动，返回 min_cpu
```

**内存推荐（v2.0 更新）：**
```
raw = Max_Usage / memory_target_utilization    # 新增利用率目标，留出缓冲
rec = max(ceil(raw / memory_step) * memory_step, min_memory)
```

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
1. 高风险（节流 > 10s 或 内存利用率 > 90%）
2. task 型服务（Cron/Consumer/Job/Worker）
3. 核心流量保护（RpsWeight > 30%）
4. 黑洞（低流量高成本 或 利用率 < 10%）
5. 低频运行（CPU 使用率 < P95LowThreshold * Request）
6. 常规缩容/扩容/保持

**v2.0 更新：**
- 节流惩罚改为比例制：每 10s 扣 5 分，上限 40 分（之前固定 -20）
- 效率评分：CPURequest=0 返回 0（之前错误返回 100）
- 黑洞检测统一：同时检查利用率维度（actual/current < 10%）和流量维度（RPS < 0.001）
- RestartCount 分级：1-3 次 Warning，>3 次 Critical（之前 >0 直接 Critical）

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

**v2.0 新增：**
- Web Dashboard 独立页面模式（`kops serve`）
- 节点密度分析（`/api/cluster/nodes`）
- 节点伸缩建议（`/api/cluster/scaling`）
- 成本归属（`/api/cost-attribution`）
- 资源预测（`/api/forecast/:ns/:name`）
- 趋势对比（`/api/trend`）
- 配置热加载（`POST /api/config/reload`）
- 分析历史快照（保留最近 10 次）

## 相关配置

关键字段见 [config.yaml](../../config.yaml)：

- `cost.price`、`cost.count`、`cost.cpu_cores`、`cost.memory_gb`
- `cost.resource_weight`
- `gateway_cost.price`、`gateway_cost.count`
- `governance.target_utilization`
- `governance.memory_target_utilization`（v2.0 新增）
- `governance.cpu_step`、`governance.memory_step`
- `governance.min_cpu`、`governance.min_memory`
- `governance.throttle_high_threshold`、`governance.mem_high_threshold`
- `governance.p95_low_threshold`
- `governance.rps_density_threshold`（v2.0 更新：RPS/Core 单位，默认 100）
- `governance.black_hole_cost_threshold`、`governance.high_traffic_threshold`
- `governance.task_service_patterns`
