package advisor

import (
	"fmt"
	platformpricing "kops/internal/platform/pricing"
	"kops/pkg/config"
	"kops/pkg/model"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
)

type EfficiencyEngine struct {
	cfg             *config.GlobalConfig
	gatewayCostCalc *platformpricing.GatewayCostCalculator
	totalRPS        float64
	gwTotalCost     float64
}

func NewEfficiencyEngine(cfg *config.GlobalConfig) *EfficiencyEngine {
	gatewayCostCalc := platformpricing.NewGatewayCostCalculator(cfg)
	return &EfficiencyEngine{
		cfg:             cfg,
		gatewayCostCalc: gatewayCostCalc,
		gwTotalCost:     gatewayCostCalc.CalculateGatewayTotalCost(),
	}
}

// SetTotalRPS 设置总流量（用于计算流量权重）
func (e *EfficiencyEngine) SetTotalRPS(totalRPS float64) {
	e.totalRPS = totalRPS
}

// Analyze 分析服务的流量效能和资源利用情况（含全链路成本）
func (e *EfficiencyEngine) Analyze(m model.ServiceMetrics) model.EfficiencyResult {
	res := model.EfficiencyResult{
		Namespace:   m.Namespace,
		ServiceName: m.Deployment,
		AppGroup:    m.AppGroup,
		CurrentCPU:  int(m.CPURequest),
		CurrentMem:  int(m.MemRequest),
		UsageCPUP95: m.CPUUsageMax,
		UsageCPUAvg: m.CPUUsageAvg,
		UsageMemMax: m.MemUsageMax,
		AvgRPS:      m.AvgRPS,
		Replicas:    m.Replicas,
	}

	// 0. 计算全链路加权分摊指标
	metricsCopy := m
	e.gatewayCostCalc.CalculateKopsMetrics(&metricsCopy, e.totalRPS, e.gwTotalCost)

	// 1. 计算流量密度 (RPS per Core)
	cpuCoreAvg := m.CPUUsageAvg / 1000.0
	if cpuCoreAvg > 0 {
		res.TrafficDensity = m.AvgRPS / cpuCoreAvg
	}
	res.TrafficDensityRank = e.calculateRank(res.TrafficDensity)

	// 2. 计算建议配置
	res.RecCPU = e.calculateRecCPU(m)
	res.RecMem = e.calculateRecMem(m)

	// 3. 计算成本（含 CPU+内存，与 Advisor 引擎一致）
	res.CurrentCost = platformpricing.PodMonthlyCost(e.cfg, m.CPURequest, m.MemRequest, m.Replicas).TotalCost
	res.RecCost = platformpricing.PodMonthlyCost(e.cfg, float64(res.RecCPU), float64(res.RecMem), m.Replicas).TotalCost
	res.ActualCost = platformpricing.PodMonthlyCost(e.cfg, m.CPUUsageAvg, m.MemUsageMax, m.Replicas).TotalCost

	// 加入 Gateway 分摊成本
	res.CurrentCost += metricsCopy.GwShareCost
	res.RecCost += metricsCopy.GwShareCost

	res.MonthlySaving = res.CurrentCost - res.RecCost

	// 4. 计算浪费金额（"烧掉"的冤枉钱）
	res.WasteAmount = res.CurrentCost - res.ActualCost

	// 5. 计算浪费比例
	if res.CurrentCost > 0 {
		res.WasteRatio = 1 - (res.ActualCost / res.CurrentCost)
	}

	// 6. 生成治理建议（考虑全链路单位成本）
	res.Action = e.makeActionWithFullCost(m, metricsCopy)
	res.Reason = e.makeReasonWithFullCost(m, res, metricsCopy)

	return res
}

// calculateRank 计算流量密度等级
func (e *EfficiencyEngine) calculateRank(density float64) string {
	if density > 2000 {
		return "S (极高)"
	} else if density > 500 {
		return "A (正常)"
	} else if density > 50 {
		return "B (低效)"
	}
	return "C (极低/空转)"
}

// calculateRecCPU 计算建议 CPU 配置
func (e *EfficiencyEngine) calculateRecCPU(m model.ServiceMetrics) int {
	return platformpricing.RecommendCPU(e.cfg, m.CPUUsageMax, m.CPURequest)
}

// calculateRecMem 计算建议内存配置
func (e *EfficiencyEngine) calculateRecMem(m model.ServiceMetrics) int {
	return platformpricing.RecommendMemory(e.cfg, m.MemUsageMax)
}

// makeActionWithFullCost 生成治理动作（含全链路成本）
func (e *EfficiencyEngine) makeActionWithFullCost(m model.ServiceMetrics, fullMetrics model.ServiceMetrics) string {
	// 资源黑洞判定
	if e.gatewayCostCalc.IsBlackHole(fullMetrics.RpsWeight, fullMetrics.PodCost) {
		return "资源黑洞回收"
	}

	// 核心流量服务判定
	if e.gatewayCostCalc.IsHighTrafficService(fullMetrics.RpsWeight) {
		return "保持（核心服务）"
	}

	// 内存黑洞检测：RPS 极低但内存占用高
	if m.AvgRPS < 0.01 && m.MemUsageMax > 512 {
		return "内存占用预警"
	}

	// 低频运行
	if m.CPUUsageMax < 5 && m.AvgRPS < 0.01 {
		return "低频运行"
	}

	// 性能扩容
	if m.CPURequest > 0 && m.CPUUsageMax/m.CPURequest > 0.8 {
		return "性能扩容"
	}

	// 分步缩容
	if m.CPUUsageMax > 0 && m.CPURequest/m.CPUUsageMax > 2 {
		return "分步缩容"
	}

	return "保持"
}

// makeReasonWithFullCost 生成建议理由（含全链路成本）
func (e *EfficiencyEngine) makeReasonWithFullCost(m model.ServiceMetrics, res model.EfficiencyResult, fullMetrics model.ServiceMetrics) string {
	if res.Action == "资源黑洞回收" {
		return fmt.Sprintf("流量权重仅 %.2f%% 但 Pod 成本 %.2f 元/月，全链路单位成本 %.4f 元/万请求，建议回收资源",
			fullMetrics.RpsWeight*100, fullMetrics.PodCost, fullMetrics.FullUnitCost)
	}
	if res.Action == "保持（核心服务）" {
		return fmt.Sprintf("核心流量服务（权重 %.1f%%），Gateway 分摊成本 %.2f 元/月，全链路单位成本 %.4f 元/万请求",
			fullMetrics.RpsWeight*100, fullMetrics.GwShareCost, fullMetrics.FullUnitCost)
	}
	if res.Action == "内存占用预警" {
		return fmt.Sprintf("RPS 极低 (<0.01) 但内存占用高达 %.0f MiB，可能存在内存泄漏、缓存设计不合理或基础镜像过大", m.MemUsageMax)
	}
	if res.Action == "低频运行" {
		return "CPU 使用率极低且 RPS 近乎为 0，建议保持最小配置"
	}
	if res.Action == "性能扩容" {
		utilization := 0.0
		if m.CPURequest > 0 {
			utilization = (m.CPUUsageMax / m.CPURequest) * 100
		}
		return fmt.Sprintf("CPU P95 利用率已达 %.1f%%，超过 80%% 阈值，存在性能瓶颈风险", utilization)
	}
	if res.Action == "分步缩容" {
		ratio := 0.0
		if m.CPUUsageMax > 0 {
			ratio = m.CPURequest / m.CPUUsageMax
		}
		return fmt.Sprintf("当前配置是实际使用的 %.1f 倍，建议回收冗余资源", ratio)
	}
	return "配置合理，保持现状"
}

// GenerateEfficiencyReport 生成效率分析报告（支持多格式输出）
func (e *EfficiencyEngine) GenerateEfficiencyReport(results []model.EfficiencyResult, blackHoles []model.ResourceBlackHole, outputFormat string) {
	fmt.Print(e.RenderEfficiencyReport(results, blackHoles, outputFormat))
}

// RenderEfficiencyReport 渲染效率分析报告（支持多格式输出）
func (e *EfficiencyEngine) RenderEfficiencyReport(results []model.EfficiencyResult, blackHoles []model.ResourceBlackHole, outputFormat string) string {
	switch strings.ToLower(outputFormat) {
	case "markdown", "md":
		return e.renderMarkdownReport(results, blackHoles)
	case "csv":
		return e.renderCSVReport(results, blackHoles)
	case "json":
		return e.renderJSONReport(results, blackHoles)
	default:
		return e.renderTableReport(results, blackHoles)
	}
}

// renderTableReport 生成表格格式报告（默认）
func (e *EfficiencyEngine) renderTableReport(results []model.EfficiencyResult, blackHoles []model.ResourceBlackHole) string {
	var sb strings.Builder
	sortedResults := copyEfficiencyResults(results)
	sb.WriteString("\n" + strings.Repeat("=", 100) + "\n")
	sb.WriteString("                     📊 流量效能与资源黑洞分析报告\n")
	sb.WriteString(strings.Repeat("=", 100) + "\n")
	e.writeTableSummary(&sb, sortedResults, blackHoles)
	e.writeTableTrafficDistribution(&sb, sortedResults)
	e.writeTableTop5BlackHoles(&sb, blackHoles)
	e.writeTableDetail(&sb, sortedResults, true)
	return sb.String()
}

// renderMarkdownReport 生成 Markdown 格式报告
func (e *EfficiencyEngine) renderMarkdownReport(results []model.EfficiencyResult, blackHoles []model.ResourceBlackHole) string {
	var sb strings.Builder

	sb.WriteString("# 流量效能与资源黑洞分析报告\n\n")
	sb.WriteString(fmt.Sprintf("分析时间: %s\n\n", time.Now().Format("2006-01-02 15:04:05")))

	// 统计摘要
	e.writeSummary(&sb, results, blackHoles)

	// 流量密度统计
	e.writeTrafficDistribution(&sb, results)

	// Top 5 资源黑洞
	e.writeTop5BlackHoles(&sb, blackHoles)

	// 详细效能分析表
	e.writeDetailTable(&sb, results)

	return sb.String()
}

// renderCSVReport 生成 CSV 格式报告
func (e *EfficiencyEngine) renderCSVReport(results []model.EfficiencyResult, blackHoles []model.ResourceBlackHole) string {
	var sb strings.Builder
	sb.WriteString("=== 资源黑洞 ===\n")
	sb.WriteString("Rank,ServiceName,Namespace,CurrentCost,ActualCost,WasteRatio(%),WasteAmount\n")
	for i, bh := range blackHoles {
		if i >= 5 {
			break
		}
		sb.WriteString(fmt.Sprintf("%d,%s,%s,￥%.2f,￥%.2f,%.1f%%,￥%.2f\n",
			i+1, bh.ServiceName, bh.Namespace, bh.CurrentCost, bh.ActualCost,
			bh.WasteRatio*100, bh.WasteAmount))
	}

	sb.WriteString("\n=== 详细服务分析 ===\n")
	sb.WriteString("Namespace,ServiceName,CurrentCPU(m),RecCPU(m),CurrentMem(MiB),RecMem(MiB),TrafficDensity,Rank,CurrentCost,RecCost,ActualCost,WasteAmount,MonthlySaving,WasteRatio(%),Action,Reason\n")

	for _, res := range results {
		savingText := fmt.Sprintf("￥%.2f", res.MonthlySaving)
		if res.MonthlySaving < 0 {
			savingText = fmt.Sprintf("需追加投入 ￥%.2f", math.Abs(res.MonthlySaving))
		}

		sb.WriteString(fmt.Sprintf("%s,%s,%d,%d,%d,%d,%.2f,%s,￥%.2f,￥%.2f,￥%.2f,￥%.2f,%s,%.1f%%,%s,%s\n",
			res.Namespace,
			res.ServiceName,
			res.CurrentCPU,
			res.RecCPU,
			res.CurrentMem,
			res.RecMem,
			res.TrafficDensity,
			res.TrafficDensityRank,
			res.CurrentCost,
			res.RecCost,
			res.ActualCost,
			res.WasteAmount,
			savingText,
			res.WasteRatio*100,
			res.Action,
			res.Reason,
		))
	}

	return sb.String()
}

// renderJSONReport 生成 JSON 格式报告
func (e *EfficiencyEngine) renderJSONReport(results []model.EfficiencyResult, blackHoles []model.ResourceBlackHole) string {
	var sb strings.Builder
	sb.WriteString("{\n")
	sb.WriteString("  \"summary\": {\n")
	totalServices := len(results)
	totalSaving := 0.0
	totalWaste := 0.0
	for _, res := range results {
		totalSaving += res.MonthlySaving
	}
	for _, bh := range blackHoles {
		totalWaste += bh.WasteAmount
	}
	sb.WriteString(fmt.Sprintf("    \"total_services\": %d,\n", totalServices))
	sb.WriteString(fmt.Sprintf("    \"black_hole_count\": %d,\n", len(blackHoles)))
	sb.WriteString(fmt.Sprintf("    \"total_monthly_saving\": %.2f,\n", totalSaving))
	sb.WriteString(fmt.Sprintf("    \"total_waste_amount\": %.2f\n", totalWaste))
	sb.WriteString("  },\n")
	sb.WriteString("  \"black_holes\": [\n")

	for i, bh := range blackHoles {
		if i >= 5 {
			break
		}
		sb.WriteString(fmt.Sprintf(`    {"rank": %d, "service": "%s", "namespace": "%s", "current_cost": %.2f, "actual_cost": %.2f, "waste_ratio": %.4f, "waste_amount": %.2f}%s`+"\n",
			i+1, bh.ServiceName, bh.Namespace, bh.CurrentCost, bh.ActualCost, bh.WasteRatio, bh.WasteAmount,
			map[bool]string{true: ",", false: ""}[i < min(4, len(blackHoles)-1)]))
	}

	sb.WriteString("  ],\n")
	sb.WriteString("  \"services\": [\n")
	for i, res := range results {
		savingText := fmt.Sprintf("￥%.2f", res.MonthlySaving)
		if res.MonthlySaving < 0 {
			savingText = fmt.Sprintf("需追加投入 ￥%.2f", math.Abs(res.MonthlySaving))
		}
		sb.WriteString(fmt.Sprintf(`    {"namespace": "%s", "service": "%s", "current_cpu": %d, "rec_cpu": %d, "current_mem": %d, "rec_mem": %d, "traffic_density": %.2f, "rank": "%s", "current_cost": %.2f, "rec_cost": %.2f, "actual_cost": %.2f, "waste_amount": %.2f, "monthly_saving": "%s", "waste_ratio": %.4f, "action": "%s", "reason": "%s"}%s`+"\n",
			res.Namespace, res.ServiceName, res.CurrentCPU, res.RecCPU, res.CurrentMem, res.RecMem,
			res.TrafficDensity, res.TrafficDensityRank, res.CurrentCost, res.RecCost, res.ActualCost, res.WasteAmount,
			savingText, res.WasteRatio, res.Action, res.Reason,
			map[bool]string{true: ",", false: ""}[i < len(results)-1]))
	}
	sb.WriteString("  ]\n")
	sb.WriteString("}\n")
	return sb.String()
}

func (e *EfficiencyEngine) writeTableSummary(sb *strings.Builder, results []model.EfficiencyResult, blackHoles []model.ResourceBlackHole) {
	totalServices := len(results)
	totalSaving := 0.0
	totalWaste := 0.0

	for _, res := range results {
		totalSaving += res.MonthlySaving
	}
	for _, bh := range blackHoles {
		totalWaste += bh.WasteAmount
	}

	sb.WriteString("\n📊 统计摘要\n")
	sb.WriteString("─" + strings.Repeat("─", 90) + "\n")
	sb.WriteString(fmt.Sprintf("  分析服务总数: %d\n", totalServices))
	sb.WriteString(fmt.Sprintf("  发现资源黑洞: %d\n", len(blackHoles)))
	sb.WriteString(fmt.Sprintf("  预计月节省:   ￥%.2f\n", totalSaving))
	sb.WriteString(fmt.Sprintf("  资源黑洞浪费: ￥%.2f\n", totalWaste))
}

// writeSummary 写入统计摘要到 Markdown
func (e *EfficiencyEngine) writeSummary(sb *strings.Builder, results []model.EfficiencyResult, blackHoles []model.ResourceBlackHole) {
	totalServices := len(results)
	totalSaving := 0.0
	blackHoleCount := len(blackHoles)
	totalWaste := 0.0

	for _, res := range results {
		totalSaving += res.MonthlySaving
	}

	for _, bh := range blackHoles {
		totalWaste += bh.WasteAmount
	}

	sb.WriteString("## 📊 统计摘要\n\n")
	sb.WriteString(fmt.Sprintf("- 分析服务总数: %d\n", totalServices))
	sb.WriteString(fmt.Sprintf("- 发现资源黑洞: %d\n", blackHoleCount))
	sb.WriteString(fmt.Sprintf("- 预计月节省: ￥%.2f\n", totalSaving))
	sb.WriteString(fmt.Sprintf("- 资源黑洞浪费: ￥%.2f\n\n", totalWaste))
}

func (e *EfficiencyEngine) writeTableTrafficDistribution(sb *strings.Builder, results []model.EfficiencyResult) {
	rankCount := make(map[string]int)
	for _, res := range results {
		rankCount[res.TrafficDensityRank]++
	}

	sb.WriteString("\n📈 流量密度分布\n")
	sb.WriteString("─" + strings.Repeat("─", 90) + "\n")
	sb.WriteString(fmt.Sprintf("  S (极高):   %d 个 (>2000 RPS/Core)\n", rankCount["S (极高)"]))
	sb.WriteString(fmt.Sprintf("  A (正常):   %d 个 (500-2000 RPS/Core)\n", rankCount["A (正常)"]))
	sb.WriteString(fmt.Sprintf("  B (低效):   %d 个 (50-500 RPS/Core)\n", rankCount["B (低效)"]))
	sb.WriteString(fmt.Sprintf("  C (极低):   %d 个 (≤50 RPS/Core)\n", rankCount["C (极低/空转)"]))
}

// writeTrafficDistribution 写入流量密度分布到 Markdown
func (e *EfficiencyEngine) writeTrafficDistribution(sb *strings.Builder, results []model.EfficiencyResult) {
	rankCount := make(map[string]int)
	for _, res := range results {
		rankCount[res.TrafficDensityRank]++
	}

	sb.WriteString("## 📈 流量密度分布\n\n")
	sb.WriteString("| 等级 | 说明 | 数量 |\n")
	sb.WriteString("| :--- | :--- | :--- |\n")
	sb.WriteString(fmt.Sprintf("| 🌟 S | 极高 (>2000 RPS/Core) | %d |\n", rankCount["S (极高)"]))
	sb.WriteString(fmt.Sprintf("| A | 正常 (500-2000 RPS/Core) | %d |\n", rankCount["A (正常)"]))
	sb.WriteString(fmt.Sprintf("| B | 低效 (50-500 RPS/Core) | %d |\n", rankCount["B (低效)"]))
	sb.WriteString(fmt.Sprintf("| ⚠️ C | 极低/空转 (≤50 RPS/Core) | %d |\n\n", rankCount["C (极低/空转)"]))
}

func (e *EfficiencyEngine) writeTableTop5BlackHoles(sb *strings.Builder, blackHoles []model.ResourceBlackHole) {
	sb.WriteString("\n🕳️ Top 5 资源黑洞\n")
	sb.WriteString("─" + strings.Repeat("─", 90) + "\n")
	sb.WriteString(fmt.Sprintf("%-5s %-20s %-15s %-12s %-12s %-12s %-12s\n", "排名", "服务名", "命名空间", "当前成本", "实际消耗", "浪费比例", "浪费金额"))
	sb.WriteString("─" + strings.Repeat("─", 90) + "\n")

	for i, bh := range blackHoles {
		if i >= 5 {
			break
		}
		sb.WriteString(fmt.Sprintf("%-5d %-20s %-15s ￥%-10.2f ￥%-10.2f %-11.1f%% ￥%.2f\n",
			i+1, bh.ServiceName, bh.Namespace, bh.CurrentCost, bh.ActualCost,
			bh.WasteRatio*100, bh.WasteAmount))
	}
}

// writeTop5BlackHoles 写入 Top 5 资源黑洞到 Markdown
func (e *EfficiencyEngine) writeTop5BlackHoles(sb *strings.Builder, blackHoles []model.ResourceBlackHole) {
	sb.WriteString("## 🕳️ Top 5 资源黑洞\n\n")
	sb.WriteString("| 排名 | 服务名 | 命名空间 | 当前成本 | 实际消耗 | 浪费比例 | 浪费金额 |\n")
	sb.WriteString("| :--- | :--- | :--- | :--- | :--- | :--- | :--- |\n")

	for i, bh := range blackHoles {
		if i >= 5 {
			break
		}
		sb.WriteString(fmt.Sprintf("| %d | %s | %s | ￥%.2f | ￥%.2f | %.1f%% | ￥%.2f |\n",
			i+1, bh.ServiceName, bh.Namespace, bh.CurrentCost, bh.ActualCost,
			bh.WasteRatio*100, bh.WasteAmount))
	}
	sb.WriteString("\n")
}

func (e *EfficiencyEngine) writeTableDetail(sb *strings.Builder, results []model.EfficiencyResult, sortByWasteRatio bool) {
	sb.WriteString("\n🎯 详细效能分析表\n")
	sb.WriteString("─" + strings.Repeat("─", 140) + "\n")
	header := fmt.Sprintf("%-20s %-8s %-8s %-12s %-10s %-10s %-10s %-10s %-12s %-15s",
		"服务名", "当前CPU", "建议CPU", "流量密度", "等级", "当前成本", "建议成本", "实际消耗", "浪费金额", "治理建议")
	sb.WriteString(header + "\n")
	sb.WriteString("─" + strings.Repeat("─", 140) + "\n")

	view := copyEfficiencyResults(results)
	if sortByWasteRatio {
		sort.Slice(view, func(i, j int) bool {
			return view[i].WasteRatio > view[j].WasteRatio
		})
	}

	for _, res := range view {
		rank := res.TrafficDensityRank
		if strings.HasPrefix(rank, "S") {
			rank = "🌟 " + rank
		} else if strings.HasPrefix(rank, "C") {
			rank = "⚠️ " + rank
		}

		savingText := fmt.Sprintf("￥%.2f", res.MonthlySaving)
		if res.MonthlySaving < 0 {
			savingText = fmt.Sprintf("需追加 ￥%.2f", math.Abs(res.MonthlySaving))
		}

		sb.WriteString(fmt.Sprintf("%-20s %-8d %-8d %-12.2f %-10s %-10s %-10s %-10s %-12s %-15s\n",
			res.ServiceName,
			res.CurrentCPU,
			res.RecCPU,
			res.TrafficDensity,
			rank,
			fmt.Sprintf("￥%.2f", res.CurrentCost),
			fmt.Sprintf("￥%.2f", res.RecCost),
			fmt.Sprintf("￥%.2f", res.ActualCost),
			fmt.Sprintf("￥%.2f", res.WasteAmount),
			res.Action+" "+savingText,
		))
	}
	sb.WriteString("\n")
}

// writeDetailTable 写入详细效能分析表到 Markdown
func (e *EfficiencyEngine) writeDetailTable(sb *strings.Builder, results []model.EfficiencyResult) {
	sb.WriteString("## 🎯 详细效能分析表\n\n")
	sb.WriteString("| 服务名 | 当前CPU | 建议CPU | 当前内存 | 建议内存 | 流量密度 | 等级 | 当前成本 | 建议成本 | 实际消耗 | 浪费金额 | 月节省/投入 | 浪费比例 | 治理建议 | 理由 |\n")
	sb.WriteString("| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |\n")

	// 按浪费比例排序
	view := copyEfficiencyResults(results)
	sort.Slice(view, func(i, j int) bool {
		return view[i].WasteRatio > view[j].WasteRatio
	})

	for _, res := range view {
		// 等级标记
		rank := res.TrafficDensityRank
		if strings.HasPrefix(rank, "S") {
			rank = "🌟 " + rank
		} else if strings.HasPrefix(rank, "C") {
			rank = "⚠️ " + rank
		}

		// 月节省/投入
		savingText := fmt.Sprintf("￥%.2f", res.MonthlySaving)
		if res.MonthlySaving < 0 {
			savingText = fmt.Sprintf("需追加投入 ￥%.2f", math.Abs(res.MonthlySaving))
		}

		sb.WriteString(fmt.Sprintf("| %s | %dm | %dm | %dMiB | %dMiB | %.2f | %s | ￥%.2f | ￥%.2f | ￥%.2f | ￥%.2f | %s | %.1f%% | %s | %s |\n",
			res.ServiceName,
			res.CurrentCPU,
			res.RecCPU,
			res.CurrentMem,
			res.RecMem,
			res.TrafficDensity,
			rank,
			res.CurrentCost,
			res.RecCost,
			res.ActualCost,
			res.WasteAmount,
			savingText,
			res.WasteRatio*100,
			res.Action,
			res.Reason))
	}
}

func copyEfficiencyResults(results []model.EfficiencyResult) []model.EfficiencyResult {
	return slices.Clone(results)
}
