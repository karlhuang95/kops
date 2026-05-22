package advisor

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	platformpricing "kops/internal/platform/pricing"
	"kops/pkg/config"
	"kops/pkg/model"
	"math"
	"strings"
	"time"
)

type Engine struct {
	cfg             *config.GlobalConfig
	gatewayCostCalc *platformpricing.GatewayCostCalculator
	totalRPS        float64
	gwTotalCost     float64
}

func NewEngine(cfg *config.GlobalConfig) *Engine {
	gatewayCostCalc := platformpricing.NewGatewayCostCalculator(cfg)
	return &Engine{
		cfg:             cfg,
		gatewayCostCalc: gatewayCostCalc,
		gwTotalCost:     gatewayCostCalc.CalculateGatewayTotalCost(),
	}
}

// SetTotalRPS 设置总流量（用于计算流量权重）
func (e *Engine) SetTotalRPS(totalRPS float64) {
	e.totalRPS = totalRPS
}

// Run 执行核心决策与经济核算（含全链路加权分摊 - 新方案）
func (e *Engine) Run(m model.ServiceMetrics) model.AdviceResult {
	res := model.AdviceResult{
		Namespace:      m.Namespace,
		Deployment:     m.Deployment,
		AppGroup:       m.AppGroup,
		OldCPURequest:  int(m.CPURequest),
		OldCPULimit:    int(m.CPULimit),
		OldMemRequest:  int(m.MemRequest),
		OldMemLimit:    int(m.MemLimit),
		CPUUsageMax:    m.CPUUsageMax,
		CPUUsageAvg:    m.CPUUsageAvg,
		MemUsageMax:    m.MemUsageMax,
		ThrottleSecond: m.ThrottleSecond,
		AvgRPS:         m.AvgRPS,
	}

	// --- 0. 计算全链路加权分摊指标（Inspect 模块 - 新方案）---
	// 仅针对 Traffic-based 服务计算流量权重和网关分摊
	metricsCopy := m
	e.gatewayCostCalc.CalculateKopsMetrics(&metricsCopy, e.totalRPS, e.gwTotalCost)
	res.IsGatewayUser = metricsCopy.IsGatewayUser
	res.RpsWeight = metricsCopy.RpsWeight
	res.GwShareCost = metricsCopy.GwShareCost
	res.FullUnitCost = metricsCopy.FullUnitCost
	res.TotalCost = metricsCopy.TotalCost

	// --- 1. 计算建议配置 (资源建议算法) ---
	res.NewCPURequest = e.calculateCPURecommendation(m)
	res.NewMemRequest = e.calculateMemoryRecommendation(m)

	// --- 2. 经济核算 (成本计算模型) ---
	currentCost := platformpricing.PodMonthlyCost(e.cfg, float64(res.OldCPURequest), float64(res.OldMemRequest), m.Replicas)
	recommendedCost := platformpricing.PodMonthlyCost(e.cfg, float64(res.NewCPURequest), float64(res.NewMemRequest), m.Replicas)
	actualCost := platformpricing.PodMonthlyCost(e.cfg, m.CPUUsageAvg, m.MemUsageMax, m.Replicas)

	res.CurrentCost = currentCost.TotalCost
	res.RecommendedCost = recommendedCost.TotalCost
	res.ActualCost = actualCost.TotalCost

	// 浪费/节省金额计算
	res.MonthlySaving = res.CurrentCost - res.RecommendedCost

	// --- 3. 流量与效率评估 ---
	res.Efficiency = e.calculateEfficiency(m)
	res.RPSDensity = e.calculateRPSDensity(m)

	// --- 4. 治理优先级计算（Advisor 模块）---
	res.PriorityScore = e.gatewayCostCalc.CalculatePriority(res.FullUnitCost, res.RpsWeight)

	// --- 5. 业务分类处理逻辑 (Heuristic Rules) ---
	e.detectBlackHole(&res, metricsCopy)
	e.detectHighTrafficService(&res, metricsCopy)
	e.detectHighRiskService(&res, m)
	e.detectInefficientService(&res, m)
	e.makeDecision(&res, m)

	return res
}

// ==================== 资源建议算法 ====================

// calculateCPURecommendation 计算建议 CPU 值
// 新算法：
//  1. 如果 Usage_P95 极低（低频/冷启动），建议配置设为 min_cpu
//  2. 否则：raw_rec_cpu = cpu_p95 / target_utilization
//     rec_cpu = max(ceil(raw_rec_cpu / cpu_step) * cpu_step, min_cpu)
func (e *Engine) calculateCPURecommendation(m model.ServiceMetrics) int {
	return platformpricing.RecommendCPU(e.cfg, m.CPUUsageMax, m.CPURequest)
}

// calculateMemoryRecommendation 计算建议内存值
// 新算法：
// rec_mem = max(ceil(mem_max / memory_step) * memory_step, min_memory)
func (e *Engine) calculateMemoryRecommendation(m model.ServiceMetrics) int {
	return platformpricing.RecommendMemory(e.cfg, m.MemUsageMax)
}

// ==================== 流量与效率评估 ====================

// calculateRPSDensity 计算 RPS 密度
// Score = Avg_RPS / CPU_Usage_Avg
func (e *Engine) calculateRPSDensity(m model.ServiceMetrics) float64 {
	if m.CPUUsageAvg == 0 {
		return 0
	}
	return m.AvgRPS / m.CPUUsageAvg
}

// calculateEfficiency 计算效率分 (0-100)
func (e *Engine) calculateEfficiency(m model.ServiceMetrics) float64 {
	if m.CPURequest == 0 {
		return 100
	}
	// 基础得分：实际峰值 / 设定值
	score := (m.CPUUsageMax / m.CPURequest) * 100
	if score > 100 {
		score = 100
	}

	// 惩罚项：如果发生 Throttling，说明效率虽高但有风险，扣分
	if m.ThrottleSecond > 10 {
		score -= 20
	}
	return math.Max(0, score)
}

// ==================== 业务分类处理逻辑 ====================

// detectBlackHole 检测资源黑洞
// 条件: W_i < 0.01 (权重小于1%) 且 Cost_pod_i > Threshold
func (e *Engine) detectBlackHole(res *model.AdviceResult, m model.ServiceMetrics) {
	if e.gatewayCostCalc.IsBlackHole(m.RpsWeight, m.PodCost) {
		res.IsBlackHole = true
	}
}

// detectHighTrafficService 检测核心流量服务
// 条件: W_i > 0.3 (核心流量服务)，标记为 Protected
func (e *Engine) detectHighTrafficService(res *model.AdviceResult, m model.ServiceMetrics) {
	if e.gatewayCostCalc.IsHighTrafficService(m.RpsWeight) {
		res.IsProtected = true
	}
}

// detectHighRiskService 检测高风险服务
// 条件: Throttle_Sec > 10 或 Mem_Usage_Max / Mem_Limit > 0.9
func (e *Engine) detectHighRiskService(res *model.AdviceResult, m model.ServiceMetrics) {
	if m.ThrottleSecond > e.cfg.Governance.ThrottleHighThreshold ||
		(m.MemLimit > 0 && m.MemUsageMax/m.MemLimit > e.cfg.Governance.MemHighThreshold) {
		res.IsHighRisk = true
	}
}

// detectInefficientService 检测低效服务
// 条件: RPS_Density < Threshold
func (e *Engine) detectInefficientService(res *model.AdviceResult, _ model.ServiceMetrics) {
	if res.RPSDensity < e.cfg.Governance.RPSDensityThreshold {
		res.IsInefficient = true
	}
}

// makeDecision 根据分析结果做出决策（含全链路治理逻辑 - 新方案）
// 新算法：
// 1. Task-based (Cron/Consumer)：仅根据 Pod 利用率缩容，不考虑网关成本
// 2. Traffic-based (Web)：如果 RpsWeight 很高且 FullUnitCost 正常，标记为 Protected
// 3. 如果 Usage_P95 极低：action = "低频运行 (Cold Start)"
// 4. 高风险服务判定
// 5. 否则：比较当前 CPU 和建议 CPU
func (e *Engine) makeDecision(res *model.AdviceResult, m model.ServiceMetrics) {
	// 1. 高风险服务判定（优先级最高）
	if res.IsHighRisk {
		res.Action = "立即扩容 (Immediate Scale Up)"
		if m.ThrottleSecond > e.cfg.Governance.ThrottleHighThreshold {
			res.Reason = "高风险服务：检测到严重 CPU 限流"
		} else {
			res.Reason = "高风险服务：内存使用率接近 Limit，存在 OOM 风险"
		}
		res.RiskLevel = "高"
		return
	}

	// 2. Task-based 服务（Cron/Consumer）：仅根据 Pod 利用率缩容
	// 不走 Traefik，不承担网关成本
	if !res.IsGatewayUser {
		// Cron/Consumer 服务：仅根据 Pod 利用率缩容
		if res.CPUUsageMax < e.cfg.Governance.P95LowThreshold*float64(res.OldCPURequest) {
			res.Action = "缩容 (Cron Low Usage)"
			res.Reason = fmt.Sprintf("Cron/Consumer 服务：CPU 使用率 %.2f%% < %.0f%%，建议缩容",
				(res.CPUUsageMax/float64(res.OldCPURequest))*100,
				e.cfg.Governance.P95LowThreshold*100)
		} else if res.OldCPURequest > res.NewCPURequest {
			res.Action = "缩容 (Cron)"
			res.Reason = "Cron/Consumer 服务：资源配置高于建议值，建议回收资源"
		} else if res.OldCPURequest < res.NewCPURequest {
			res.Action = "扩容 (Cron)"
			res.Reason = "Cron/Consumer 服务：资源配置低于建议值，存在性能风险"
		} else {
			res.Action = "保持 (Cron)"
			res.Reason = "Cron/Consumer 服务：当前配置符合目标利用率要求"
		}
		res.RiskLevel = "低"
		return
	}

	// 3. Traffic-based 服务（Web）：核心流量服务判定（Protected）
	if res.IsProtected {
		if res.OldCPURequest > res.NewCPURequest {
			res.Action = "保持 (Protected)"
			res.Reason = fmt.Sprintf("核心流量服务（流量权重 %.1f%%），即使有闲置也禁止自动缩容以保证网关链路稳定性", res.RpsWeight*100)
		} else {
			res.Action = "保持 (Keep)"
			res.Reason = fmt.Sprintf("核心流量服务（流量权重 %.1f%%），配置合理", res.RpsWeight*100)
		}
		res.RiskLevel = "中"
		return
	}

	// 4. 资源黑洞判定（仅针对 Traffic-based 服务）
	if res.IsBlackHole {
		res.Action = "缩容 (Black Hole)"
		res.Reason = fmt.Sprintf("资源黑洞：流量权重仅 %.2f%% 但成本 %.2f 元/月，建议回收", res.RpsWeight*100, res.TotalCost)
		res.RiskLevel = "低"
		return
	}

	// 5. 低频/冷启动判定（替代僵尸服务）
	isLowUsage := res.CPUUsageMax < e.cfg.Governance.P95LowThreshold*float64(res.OldCPURequest)
	if isLowUsage {
		res.Action = "低频运行 (Cold Start)"
		res.Reason = "检测到低频运行服务：CPU 使用率极低，可能处于冷启动阶段"
		res.RiskLevel = "低"
		return
	}

	// 6. Traffic-based 服务：比较当前 CPU 和建议 CPU
	if res.OldCPURequest > res.NewCPURequest {
		res.Action = "缩容 (Scale Down)"
		res.Reason = "Web 服务：资源配置高于建议值，建议回收资源"
		res.RiskLevel = "低"
	} else if res.OldCPURequest < res.NewCPURequest {
		res.Action = "扩容 (ScaleUp)"
		res.Reason = "Web 服务：资源配置低于建议值，存在性能风险"
		res.RiskLevel = "中"
	} else {
		res.Action = "保持 (Keep)"
		res.Reason = "Web 服务：当前配置符合目标利用率要求"
		res.RiskLevel = "低"
	}

	// 低效服务标记
	if res.IsInefficient {
		res.Reason += fmt.Sprintf(" (注意：RPS 密度较低，全链路单位成本 %.2f 元/万请求)", res.FullUnitCost)
	}
}

// GenerateCSVReport 生成 CSV 报告
func (e *Engine) GenerateCSVReport(results []model.AdviceResult) (string, error) {
	var sb strings.Builder
	writer := csv.NewWriter(&sb)

	if err := writer.Write([]string{"服务名", "命名空间", "当前配置CPU(m)", "建议配置CPU(m)", "当前成本", "建议成本", "实际消耗金额", "治理建议", "浪费金额", "风险等级", "原因"}); err != nil {
		return "", err
	}

	for _, res := range results {
		reasonSuffix := ""
		if res.ThrottleSecond > 0 {
			reasonSuffix = fmt.Sprintf(" (限流%.0fs)", res.ThrottleSecond)
		} else if res.OldMemLimit > 0 && res.MemUsageMax/float64(res.OldMemLimit) > 0.8 {
			reasonSuffix = " (OOM风险)"
		}

		record := []string{
			res.Deployment,
			res.Namespace,
			fmt.Sprintf("%d", res.OldCPURequest),
			fmt.Sprintf("%d", res.NewCPURequest),
			fmt.Sprintf("%.2f", res.CurrentCost),
			fmt.Sprintf("%.2f", res.RecommendedCost),
			fmt.Sprintf("%.2f", res.ActualCost),
			res.Action,
			fmt.Sprintf("%.2f", res.MonthlySaving),
			res.RiskLevel,
			res.Reason + reasonSuffix,
		}
		if err := writer.Write(record); err != nil {
			return "", err
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// GenerateJSONReport 生成 JSON 报告
func (e *Engine) GenerateJSONReport(results []model.AdviceResult) (string, error) {
	bytes, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// GenerateMarkdownReport 生成 Markdown 摘要表格（含全链路成本）
func (e *Engine) GenerateMarkdownReport(results []model.AdviceResult) string {
	var sb strings.Builder

	sb.WriteString("# Kubernetes 资源治理报告（含全链路成本分摊）\n\n")
	sb.WriteString(fmt.Sprintf("生成时间: %s\n\n", time.Now().Format("2006-01-02 15:04:05")))

	// 统计摘要
	total := len(results)
	highRiskCount := 0
	scaleDownCount := 0
	scaleUpCount := 0
	coldStartCount := 0
	protectedCount := 0
	blackHoleCount := 0
	totalSaving := 0.0
	totalGatewayCost := e.gwTotalCost

	for _, res := range results {
		if res.IsHighRisk {
			highRiskCount++
		}
		if res.IsProtected {
			protectedCount++
		}
		if res.IsBlackHole {
			blackHoleCount++
		}
		if strings.Contains(res.Action, "缩容") {
			scaleDownCount++
		}
		if strings.Contains(res.Action, "扩容") {
			scaleUpCount++
		}
		if strings.Contains(res.Action, "低频运行") {
			coldStartCount++
		}
		totalSaving += res.MonthlySaving
	}

	sb.WriteString("## 📊 统计摘要\n\n")
	sb.WriteString(fmt.Sprintf("- 分析服务总数: %d\n", total))
	sb.WriteString(fmt.Sprintf("- 核心流量服务（Protected）: %d\n", protectedCount))
	sb.WriteString(fmt.Sprintf("- 资源黑洞: %d\n", blackHoleCount))
	sb.WriteString(fmt.Sprintf("- 低频运行服务: %d\n", coldStartCount))
	sb.WriteString(fmt.Sprintf("- 高风险服务: %d\n", highRiskCount))
	sb.WriteString(fmt.Sprintf("- 建议缩容: %d\n", scaleDownCount))
	sb.WriteString(fmt.Sprintf("- 建议扩容: %d\n", scaleUpCount))
	sb.WriteString(fmt.Sprintf("- 预计月节省: ￥%.2f\n", totalSaving))
	sb.WriteString(fmt.Sprintf("- Gateway 总成本: ￥%.2f\n\n", totalGatewayCost))

	// 主表格（含全链路成本）
	sb.WriteString("## 🎯 治理建议表（含全链路成本分摊）\n\n")
	sb.WriteString("| 服务名 | 当前配置 | 建议配置 | 流量权重 | Gateway分摊 | 全链路单位成本 | 当前成本 | 建议成本 | 治理建议 | 优先级 |\n")
	sb.WriteString("| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |\n")

	for _, res := range results {
		reasonSuffix := ""
		if res.ThrottleSecond > 0 {
			reasonSuffix = fmt.Sprintf(" (限流 %.0fs)", res.ThrottleSecond)
		} else if res.OldMemLimit > 0 && res.MemUsageMax/float64(res.OldMemLimit) > 0.8 {
			reasonSuffix = " (OOM风险)"
		}

		action := res.Action
		if res.IsProtected {
			action = "🛡️ " + action
		}
		if res.IsBlackHole {
			action = "🕳️ " + action
		}

		sb.WriteString(fmt.Sprintf("| %s | %dm | %dm | %.2f%% | ￥%.2f | ￥%.4f | ￥%.2f | ￥%.2f | %s%s | %.2f |\n",
			res.Deployment,
			res.OldCPURequest,
			res.NewCPURequest,
			res.RpsWeight*100,
			res.GwShareCost,
			res.FullUnitCost,
			res.CurrentCost,
			res.RecommendedCost,
			action,
			reasonSuffix,
			res.PriorityScore))
	}

	sb.WriteString("\n## 📈 详细分析\n\n")
	for _, res := range results {
		sb.WriteString(fmt.Sprintf("### %s\n\n", res.Deployment))
		sb.WriteString(fmt.Sprintf("**当前配置**: CPU %dm / Mem %dMi\n", res.OldCPURequest, res.OldMemRequest))
		sb.WriteString(fmt.Sprintf("**建议配置**: CPU %dm / Mem %dMi\n", res.NewCPURequest, res.NewMemRequest))
		sb.WriteString(fmt.Sprintf("**实际使用**: CPU P95 %.2fm / Avg %.2fm, Mem Max %.2fMi\n",
			res.CPUUsageMax, res.CPUUsageAvg, res.MemUsageMax))
		sb.WriteString(fmt.Sprintf("**流量权重**: %.2f%% (RPS %.2f)\n", res.RpsWeight*100, res.AvgRPS))
		sb.WriteString(fmt.Sprintf("**全链路成本**: Pod成本 ￥%.2f + Gateway分摊 ￥%.2f = 总成本 ￥%.2f\n",
			res.CurrentCost, res.GwShareCost, res.TotalCost))
		sb.WriteString(fmt.Sprintf("**单位成本**: ￥%.4f / 万请求\n", res.FullUnitCost))
		sb.WriteString(fmt.Sprintf("**成本分析**: 当前成本 ￥%.2f, 建议成本 ￥%.2f, 实际消耗 ￥%.2f\n",
			res.CurrentCost, res.RecommendedCost, res.ActualCost))
		sb.WriteString(fmt.Sprintf("**性能指标**: 限流 %.2fs, RPS %.2f\n", res.ThrottleSecond, res.AvgRPS))
		sb.WriteString(fmt.Sprintf("**效率评分**: %.2f/100, RPS密度 %.4f\n", res.Efficiency, res.RPSDensity))
		sb.WriteString(fmt.Sprintf("**治理优先级**: %.2f\n", res.PriorityScore))
		sb.WriteString(fmt.Sprintf("**建议**: %s - %s\n\n", res.Action, res.Reason))
	}

	return sb.String()
}

// GenerateTableReport 生成默认表格报告。
func (e *Engine) GenerateTableReport(results []model.AdviceResult) string {
	var sb strings.Builder

	sb.WriteString(strings.Repeat("-", 180) + "\n")
	sb.WriteString(fmt.Sprintf("%-25s %-12s %-12s %-10s %-12s %-12s %-12s %-20s %-15s\n",
		"服务名",
		"当前配置",
		"建议配置",
		"流量权重",
		"Gateway分摊",
		"当前成本",
		"建议成本",
		"治理建议",
		"浪费金额"))
	sb.WriteString(strings.Repeat("-", 180) + "\n")

	var totalSaving float64
	for _, res := range results {
		totalSaving += res.MonthlySaving

		action := res.Action
		if res.IsProtected {
			action = "🛡️ " + action
		}
		if res.IsBlackHole {
			action = "🕳️ " + action
		}

		sb.WriteString(fmt.Sprintf("%-25s %-12s %-12s %-9.1f%% ￥%-11.2f ￥%-11.2f ￥%-11.2f %-20s ￥%-14.2f\n",
			res.Deployment,
			fmt.Sprintf("%dm", res.OldCPURequest),
			fmt.Sprintf("%dm", res.NewCPURequest),
			res.RpsWeight*100,
			res.GwShareCost,
			res.CurrentCost,
			res.RecommendedCost,
			action,
			res.MonthlySaving))
	}

	sb.WriteString(strings.Repeat("-", 180) + "\n")
	sb.WriteString(fmt.Sprintf("💰 总结：本次治理预计每月可节省总金额: ￥%.2f (目标利用率: %.1f%%)\n",
		totalSaving, e.cfg.Governance.TargetUtilization*100))
	sb.WriteString(fmt.Sprintf("🌉 Gateway 总成本: ￥%.2f\n", e.gwTotalCost))

	return sb.String()
}
