# Changelog

## v2.0 — Dashboard 重构 & 功能增强 (2026-06-12)

### 🏗️ 架构变更

#### Dashboard 拆分为独立页面
- 原单页面 5 个 Tab → 拆分为独立路由页面
- `/` 总览、`/recommendations` 资源推荐、`/efficiency` 流量效率、`/health` 健康状态、`/cluster` 集群分析
- 共享导航栏 + 独立内容区，每页只加载必要数据
- 页面间导航秒切，不再一次性加载全部数据

#### 模板重构
- 新增 `head.html`、`nav.html`、`scripts.html` 共享模板
- 新增 `overview.html`、`recommendations.html`、`efficiency.html`、`health.html`、`cluster.html`
- 删除旧的 1200 行单体 `dashboard.html`

---

### 🐛 Bug 修复

| 问题 | 修复 |
|------|------|
| Advisor 引擎 CPURequest=0 时除零 | 过滤条件改为 `CPURequest <= 0` 跳过 |
| `calculateEfficiency` 对 CPURequest=0 返回 100% | 改为返回 0（未配置=最差效率） |
| 节流惩罚固定 -20 分 | 改为比例惩罚（每 10s 扣 5 分，上限 40 分） |
| Health 引擎 CPUUtilization/RestartCount 从未填充 | Traefik 采集器补充 PromQL 查询 |
| `CalculateHealthScore` 为死代码 | 增强为多维评分并集成到 HealthEngine |
| Prometheus Timeout 配置未生效 | 改为解析配置中的 timeout 值 |
| 命名空间过滤不生效 | 缓存命中后增加 `filterAnalysisByNamespace` |
| 命名空间下拉框切换后丢失选项 | 下拉列表改为从配置文件读取 |
| 折线图横坐标全部显示 08:00 | `QueryRange` timestamp 解析修复（float64 科学计数法） |
| CPU/内存查询 `pod!~` 正则缺少关闭引号 | 修复 8 处 PromQL 语法错误 |

---

### ✨ 新功能

#### 页面功能
- **总览页**：6 张图表 + 趋势对比卡片（显示本次 vs 上次分析的变化）
- **资源推荐页**：独立页面，含筛选芯片、列排序、行展开详情、kubectl 命令一键复制
- **流量效率页**：独立页面，含黑洞 Top 5
- **健康状态页**：独立页面，含统计卡片、健康分进度条
- **集群分析页**：节点密度、伸缩建议、成本归属（AJAX 加载）

#### 服务详情页
- CPU/内存/RPS 折线图，支持 6h/24h/7d/30d 时间切换
- 资源推荐对比卡片（当前 vs 推荐，含月节省金额）
- 7 天资源预测折线图
- 统计卡片显示 Pod 数量（CPU 总和 ×N Pod）
- 横坐标北京时间

#### 后端新增 API
| 端点 | 说明 |
|------|------|
| `GET /api/service/:ns/:name/timeseries` | 单服务时间序列 |
| `GET /api/service/:ns/:name/recommendation` | 单服务 Advisor 推荐 |
| `GET /api/cluster/nodes` | 节点密度数据 |
| `GET /api/cluster/scaling` | 节点伸缩建议 |
| `GET /api/cost-attribution` | 成本归属（按 NS + App） |
| `GET /api/forecast/:ns/:name` | 资源预测 |
| `GET /api/trend` | 趋势对比（本次 vs 上次分析） |
| `POST /api/config/reload` | 配置热加载 |

#### 工程化
- 结构化日志（`slog` JSON 输出）
- API 限流（Token bucket，10 req/s，burst 30）
- Webhook 告警通知
- 分析历史快照（保留最近 10 次）
- Prometheus 连通性状态指示器

---

### 🔧 算法改进

| 改进 | 说明 |
|------|------|
| 统一黑洞检测 | 同时检查利用率 + 流量两个维度 |
| 统一成本口径 | Advisor CurrentCost 包含 Gateway 分摊 |
| 统一密度单位 | RPSDensity 改为 RPS/Core（与 Efficiency 一致） |
| 内存利用率目标 | 新增 `memory_target_utilization` 配置 |
| RestartCount 分级 | 1-3 次 Warning，>3 次 Critical |
| Pod 精准匹配 | `pod=~"^deploy-.*"` + `pod!~"consumer\|cron\|job\|worker"` 排除非主服务 Pod |

---

### 🎨 UI 增强

- 暗色模式（CSS 变量 + localStorage 持久化）
- 列排序（点击表头）
- 快捷筛选芯片（高风险/可节省/S 级/严重等）
- 行展开面板（免跳转查看详情）
- kubectl 命令一键复制
- 键盘快捷键（`?` 查看）
- 差异条可视化（CPU 变化量）
- 异常红点（高风险/高错误率服务）
- 暗色模式切换
- 响应式布局

---

### 📁 文件变更统计

- 修改：22 个文件
- 新增：10 个文件（模板 7 + kube.go + middleware.go + notifier.go）
- 删除：1 个文件（dashboard.html）
- 净增：~1000 行
