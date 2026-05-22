package advisor

import (
	"fmt"
	platformpricing "kops/internal/platform/pricing"
	"kops/pkg/config"
	"kops/pkg/model"
	"strings"
)

// HealthEngine 健康检查引擎
type HealthEngine struct {
	cfg             *config.GlobalConfig
	gatewayCostCalc *platformpricing.GatewayCostCalculator
}

// NewHealthEngine 创建健康检查引擎
func NewHealthEngine(cfg *config.GlobalConfig) *HealthEngine {
	return &HealthEngine{
		cfg:             cfg,
		gatewayCostCalc: platformpricing.NewGatewayCostCalculator(cfg),
	}
}

// DetermineHealthCode 判定健康状态（一票否决制，含全链路损耗）
func (he *HealthEngine) DetermineHealthCode(metrics model.HealthMetrics, threshold5xx float64) model.HealthStatus {
	status := model.HealthStatus{
		ServiceName:  metrics.ServiceName,
		Namespace:    metrics.Namespace,
		RPS:          metrics.RPS,
		Error5xxRate: metrics.Error5xxRate,
		Error4xxRate: metrics.Error4xxRate,
		P99Latency:   metrics.P99Latency,
		InvalidSpend: 0,
	}

	// 检查是否离线/空转
	if metrics.RPS < 0.001 {
		status.HealthCode = "Idle"
		status.StatusIcon = "⚪"
		status.Diagnosis = "无流量"
		status.Action = "检查服务是否需要下线"
		return status
	}

	// 一票否决制：5xx 错误率超阈值直接红码
	if metrics.Error5xxRate > threshold5xx {
		status.HealthCode = "Critical"
		status.StatusIcon = "🔴"
		status.Diagnosis = fmt.Sprintf("5xx 错误率 %.1f%% 超过阈值 %.1f%%", metrics.Error5xxRate*100, threshold5xx*100)
		status.Action = "立即排查后端服务异常"
		status.InvalidSpend = metrics.MonthlyCost * metrics.Error5xxRate
		// 计算全链路无效投入（含 Gateway 损耗）
		if he.gatewayCostCalc != nil {
			status.WastedSpendTotal = he.gatewayCostCalc.CalculateHealthWaste(metrics.PodCost, metrics.GwShareCost, metrics.Error5xxRate)
			if status.WastedSpendTotal == 0 {
				status.WastedSpendTotal = status.InvalidSpend
			}
		} else {
			status.WastedSpendTotal = status.InvalidSpend
		}
		return status
	}

	// P99 延迟超 2s -> 红码（性能风险，非实际浪费）
	if metrics.P99Latency > 2.0 {
		status.HealthCode = "Critical"
		status.StatusIcon = "🔴"
		status.Diagnosis = fmt.Sprintf("P99 延迟 %.2fs 超过 2s，影响用户体验", metrics.P99Latency)
		status.Action = "排查慢查询/依赖服务超时"
		return status
	}

	// Pod 重启 -> 红码（稳定性风险，非实际浪费）
	if metrics.RestartCount > 0 {
		status.HealthCode = "Critical"
		status.StatusIcon = "🔴"
		status.Diagnosis = fmt.Sprintf("Pod 重启 %d 次，服务不稳定", metrics.RestartCount)
		status.Action = "检查应用崩溃日志/OOM Killer"
		return status
	}

	// 4xx 错误率超 5% -> 黄码
	if metrics.Error4xxRate > 0.05 {
		status.HealthCode = "Warning"
		status.StatusIcon = "🟡"
		status.Diagnosis = fmt.Sprintf("4xx 错误率 %.1f%% 偏高，检查调用方/鉴权", metrics.Error4xxRate*100)
		status.Action = "检查 API 参数、Token 权限"
		status.InvalidSpend = metrics.MonthlyCost * metrics.Error4xxRate
		if he.gatewayCostCalc != nil {
			status.WastedSpendTotal = he.gatewayCostCalc.CalculateHealthWaste(metrics.PodCost, metrics.GwShareCost, metrics.Error4xxRate)
			if status.WastedSpendTotal == 0 {
				status.WastedSpendTotal = status.InvalidSpend
			}
		} else {
			status.WastedSpendTotal = status.InvalidSpend
		}
		return status
	}

	// P99 延迟超 1s -> 黄码（性能风险，非实际浪费）
	if metrics.P99Latency > 1.0 {
		status.HealthCode = "Warning"
		status.StatusIcon = "🟡"
		status.Diagnosis = fmt.Sprintf("P99 延迟 %.2fs 较高，可能影响用户体验", metrics.P99Latency)
		status.Action = "排查慢 SQL/缓存未命中"
		return status
	}

	// CPU 利用率超 85% -> 黄码（容量风险，非实际浪费）
	if metrics.CPUUtilization > 0.85 {
		status.HealthCode = "Warning"
		status.StatusIcon = "🟡"
		status.Diagnosis = fmt.Sprintf("CPU 利用率 %.1f%% 偏高，存在性能瓶颈", metrics.CPUUtilization*100)
		status.Action = "考虑扩容或优化代码"
		return status
	}

	// 所有检查通过 -> 绿码
	status.HealthCode = "Healthy"
	status.StatusIcon = "🟢"
	status.Diagnosis = "运行稳健"
	status.Action = "保持现状"
	return status
}

// GenerateHealthReport 生成健康检查报告（支持多格式输出）
func (he *HealthEngine) GenerateHealthReport(results []model.HealthStatus, outputFormat string) {
	fmt.Print(he.RenderHealthReport(results, outputFormat))
}

// RenderHealthReport 渲染健康检查报告（支持多格式输出）
func (he *HealthEngine) RenderHealthReport(results []model.HealthStatus, outputFormat string) string {
	switch strings.ToLower(outputFormat) {
	case "markdown", "md":
		return he.renderMarkdownReport(results)
	case "csv":
		return he.renderCSVReport(results)
	case "json":
		return he.renderJSONReport(results)
	default:
		return he.renderTableReport(results)
	}
}

// renderTableReport 生成表格格式报告（默认）
func (he *HealthEngine) renderTableReport(results []model.HealthStatus) string {
	var sb strings.Builder
	sb.WriteString("\n" + strings.Repeat("=", 120) + "\n")
	sb.WriteString("                        🏥 服务健康检查报告\n")
	sb.WriteString(strings.Repeat("=", 120) + "\n")

	he.writeTableSummary(&sb, results)
	he.writeTableDetail(&sb, results)
	return sb.String()
}

// renderMarkdownReport 生成 Markdown 格式报告
func (he *HealthEngine) renderMarkdownReport(results []model.HealthStatus) string {
	var sb strings.Builder

	sb.WriteString("# 🏥 服务健康检查报告\n\n")
	he.writeSummary(&sb, results)
	he.writeDetailTable(&sb, results)

	return sb.String()
}

// renderCSVReport 生成 CSV 格式报告
func (he *HealthEngine) renderCSVReport(results []model.HealthStatus) string {
	var sb strings.Builder
	sb.WriteString("Namespace,ServiceName,Status,HealthCode,RPS,Error5xxRate(%),Error4xxRate(%),P99Latency(ms),InvalidSpend(￥),Diagnosis,Action\n")

	for _, res := range results {
		error5xxText := "-"
		error4xxText := "-"
		p99Text := "-"

		if res.RPS >= 0.001 {
			error5xxText = fmt.Sprintf("%.2f", res.Error5xxRate*100)
			error4xxText = fmt.Sprintf("%.2f", res.Error4xxRate*100)
			p99Text = fmt.Sprintf("%.0f", res.P99Latency*1000)
		}

		sb.WriteString(fmt.Sprintf("%s,%s,%s,%s,%.2f,%s,%s,%s,%.2f,%s,%s\n",
			res.Namespace,
			res.ServiceName,
			res.StatusIcon,
			res.HealthCode,
			res.RPS,
			error5xxText,
			error4xxText,
			p99Text,
			res.InvalidSpend,
			res.Diagnosis,
			res.Action))
	}

	return sb.String()
}

// renderJSONReport 生成 JSON 格式报告
func (he *HealthEngine) renderJSONReport(results []model.HealthStatus) string {
	var sb strings.Builder
	sb.WriteString("{\n")
	totalServices := len(results)
	criticalCount := 0
	warningCount := 0
	healthyCount := 0
	idleCount := 0
	totalInvalidSpend := 0.0

	for _, res := range results {
		switch res.HealthCode {
		case "Critical":
			criticalCount++
		case "Warning":
			warningCount++
		case "Healthy":
			healthyCount++
		case "Idle":
			idleCount++
		}
		totalInvalidSpend += res.InvalidSpend
	}

	sb.WriteString("  \"summary\": {\n")
	sb.WriteString(fmt.Sprintf("    \"total_services\": %d,\n", totalServices))
	sb.WriteString(fmt.Sprintf("    \"critical_count\": %d,\n", criticalCount))
	sb.WriteString(fmt.Sprintf("    \"warning_count\": %d,\n", warningCount))
	sb.WriteString(fmt.Sprintf("    \"healthy_count\": %d,\n", healthyCount))
	sb.WriteString(fmt.Sprintf("    \"idle_count\": %d,\n", idleCount))
	sb.WriteString(fmt.Sprintf("    \"total_invalid_spend\": %.2f\n", totalInvalidSpend))
	sb.WriteString("  },\n")
	sb.WriteString("  \"services\": [\n")
	for i, res := range results {
		sb.WriteString(fmt.Sprintf(`    {"namespace": "%s", "service_name": "%s", "status_icon": "%s", "health_code": "%s", "rps": %.2f, "error5xx_rate": %.4f, "error4xx_rate": %.4f, "p99_latency": %.4f, "invalid_spend": %.2f, "diagnosis": "%s", "action": "%s"}%s`+"\n",
			res.Namespace, res.ServiceName, res.StatusIcon, res.HealthCode, res.RPS,
			res.Error5xxRate, res.Error4xxRate, res.P99Latency, res.InvalidSpend,
			res.Diagnosis, res.Action,
			map[bool]string{true: ",", false: ""}[i < len(results)-1]))
	}
	sb.WriteString("  ]\n")
	sb.WriteString("}\n")
	return sb.String()
}

func (he *HealthEngine) writeTableSummary(sb *strings.Builder, results []model.HealthStatus) {
	totalServices := len(results)
	criticalCount := 0
	warningCount := 0
	healthyCount := 0
	idleCount := 0
	totalInvalidSpend := 0.0
	totalWastedSpend := 0.0

	for _, res := range results {
		switch res.HealthCode {
		case "Critical":
			criticalCount++
		case "Warning":
			warningCount++
		case "Healthy":
			healthyCount++
		case "Idle":
			idleCount++
		}
		totalInvalidSpend += res.InvalidSpend
		totalWastedSpend += res.WastedSpendTotal
	}

	sb.WriteString("\n📊 统计摘要\n")
	sb.WriteString(strings.Repeat("-", 120) + "\n")
	sb.WriteString(fmt.Sprintf("  服务总数:   %d\n", totalServices))
	sb.WriteString(fmt.Sprintf("  🔴 红码:    %d (需立即处理)\n", criticalCount))
	sb.WriteString(fmt.Sprintf("  🟡 黄码:    %d (需关注)\n", warningCount))
	sb.WriteString(fmt.Sprintf("  🟢 绿码:    %d (正常)\n", healthyCount))
	sb.WriteString(fmt.Sprintf("  ⚪ 灰码:    %d (无流量)\n", idleCount))
	sb.WriteString(fmt.Sprintf("  无效投入:   ￥%.2f/月\n", totalInvalidSpend))
	sb.WriteString(fmt.Sprintf("  全链路损耗: ￥%.2f/月（含Gateway）\n", totalWastedSpend))
}

// writeSummary 写入统计摘要到 Markdown（含全链路损耗）
func (he *HealthEngine) writeSummary(sb *strings.Builder, results []model.HealthStatus) {
	totalServices := len(results)
	criticalCount := 0
	warningCount := 0
	healthyCount := 0
	idleCount := 0
	totalInvalidSpend := 0.0
	totalWastedSpend := 0.0

	for _, res := range results {
		switch res.HealthCode {
		case "Critical":
			criticalCount++
		case "Warning":
			warningCount++
		case "Healthy":
			healthyCount++
		case "Idle":
			idleCount++
		}
		totalInvalidSpend += res.InvalidSpend
		totalWastedSpend += res.WastedSpendTotal
	}

	sb.WriteString("## 📊 统计摘要\n\n")
	sb.WriteString(fmt.Sprintf("- 服务总数: %d\n", totalServices))
	sb.WriteString(fmt.Sprintf("- 🔴 红码: %d (需立即处理)\n", criticalCount))
	sb.WriteString(fmt.Sprintf("- 🟡 黄码: %d (需关注)\n", warningCount))
	sb.WriteString(fmt.Sprintf("- 🟢 绿码: %d (正常)\n", healthyCount))
	sb.WriteString(fmt.Sprintf("- ⚪ 灰码: %d (无流量)\n", idleCount))
	sb.WriteString(fmt.Sprintf("- 无效投入: ￥%.2f/月\n", totalInvalidSpend))
	sb.WriteString(fmt.Sprintf("- 全链路损耗: ￥%.2f/月（含Gateway）\n\n", totalWastedSpend))
}

func (he *HealthEngine) writeTableDetail(sb *strings.Builder, results []model.HealthStatus) {
	sb.WriteString("\n🎯 详细健康检查表\n")
	sb.WriteString(strings.Repeat("-", 120) + "\n")
	header := fmt.Sprintf("%-6s %-35s %-12s %-10s %-10s %-12s %-20s %-30s",
		"状态", "服务名", "RPS", "5xx率", "P99耗时", "无效投入/月", "诊断建议", "建议操作")
	sb.WriteString(header + "\n")
	sb.WriteString(strings.Repeat("-", 120) + "\n")

	for _, res := range results {
		error5xxText := "-"
		p99Text := "-"
		invalidText := "￥0.00"

		if res.RPS >= 0.001 {
			error5xxText = fmt.Sprintf("%.2f%%", res.Error5xxRate*100)
			p99Text = fmt.Sprintf("%.0fms", res.P99Latency*1000)
		}
		if res.InvalidSpend > 0 {
			invalidText = fmt.Sprintf("￥%.2f", res.InvalidSpend)
		}

		displayName := res.ServiceName
		if res.Namespace != "" {
			displayName = res.Namespace + "/" + res.ServiceName
		}

		sb.WriteString(fmt.Sprintf("%-6s %-35s %-12.1f %-10s %-10s %-20s %-30s %-30s\n",
			res.StatusIcon+" "+res.HealthCode,
			displayName,
			res.RPS,
			error5xxText,
			p99Text,
			invalidText,
			res.Diagnosis,
			res.Action))
	}
	sb.WriteString("\n")
}

// writeDetailTable 写入详细健康检查表到 Markdown
func (he *HealthEngine) writeDetailTable(sb *strings.Builder, results []model.HealthStatus) {
	sb.WriteString("## 🎯 详细健康检查表\n\n")
	sb.WriteString("| 状态 | 服务名 | 命名空间 | RPS | 5xx率 | 4xx率 | P99耗时 | 无效投入/月 | 诊断建议 | 建议操作 |\n")
	sb.WriteString("| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |\n")

	for _, res := range results {
		error5xxText := "-"
		error4xxText := "-"
		p99Text := "-"
		invalidText := "￥0.00"

		if res.RPS >= 0.001 {
			error5xxText = fmt.Sprintf("%.2f%%", res.Error5xxRate*100)
			error4xxText = fmt.Sprintf("%.2f%%", res.Error4xxRate*100)
			p99Text = fmt.Sprintf("%.0fms", res.P99Latency*1000)
		}

		if res.InvalidSpend > 0 {
			invalidText = fmt.Sprintf("￥%.2f", res.InvalidSpend)
		}

		sb.WriteString(fmt.Sprintf("| %s %s | %s | %s | %.1f | %s | %s | %s | %s | %s | %s |\n",
			res.StatusIcon, res.HealthCode, res.ServiceName, res.Namespace, res.RPS,
			error5xxText, error4xxText, p99Text, invalidText, res.Diagnosis, res.Action))
	}
}
