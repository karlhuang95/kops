package analyze

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	appcommon "kops/internal/app/common"
	advisordomain "kops/internal/domain/advisor"
	healthdomain "kops/internal/domain/health"
	metricsdomain "kops/internal/domain/metrics"
	platformcollector "kops/internal/platform/collector"
	platformconfig "kops/internal/platform/config"
	platformpricing "kops/internal/platform/pricing"
	advisorpkg "kops/pkg/advisor"
)

// AnalyzeParams holds all input parameters.
type AnalyzeParams struct {
	Config       *platformconfig.GlobalConfig
	Namespace    string
	OutputFormat string
	Duration     string
	Threshold5xx string
}

// AnalysisData holds the complete structured results from all three analysis engines.
type AnalysisData struct {
	AdvisorResults     []advisordomain.AdviceResult
	EfficiencyResults  []advisordomain.EfficiencyResult
	BlackHoles         []advisordomain.ResourceBlackHole
	HealthStatuses     []healthdomain.HealthStatus
	HealthWarning      string
	TotalRPS           float64
	TotalMonthlySaving float64
	CriticalCount      int
	WarningCount       int
}

// Analyze runs the full collection and analysis pipeline, returning structured data.
func Analyze(params AnalyzeParams) (*AnalysisData, error) {
	namespaces := appcommon.ResolveTargetNamespaces(params.Config, params.Namespace)

	rawMetrics, err := collectPromMetrics(params.Config, namespaces)
	if err != nil {
		return nil, fmt.Errorf("metric collection failed: %w", err)
	}

	totalRPS := platformpricing.CalculateTotalTrafficRPS(params.Config, rawMetrics)

	advisorEngine := advisorpkg.NewEngine(params.Config)
	advisorEngine.SetTotalRPS(totalRPS)
	advisorResults := buildAdviceResults(advisorEngine, rawMetrics)

	efficiencyEngine := advisorpkg.NewEfficiencyEngine(params.Config)
	efficiencyEngine.SetTotalRPS(totalRPS)
	efficiencyResults, blackHoles := buildEfficiencyResults(params.Config, efficiencyEngine, rawMetrics)

	healthStatuses, err := collectHealthStatuses(params)
	var healthWarning string
	if err != nil {
		healthWarning = err.Error()
	}

	var totalSaving float64
	for _, r := range advisorResults {
		totalSaving += r.MonthlySaving
	}
	var criticalCount, warningCount int
	for _, h := range healthStatuses {
		switch h.HealthCode {
		case "Critical":
			criticalCount++
		case "Warning":
			warningCount++
		}
	}

	return &AnalysisData{
		AdvisorResults:     advisorResults,
		EfficiencyResults:  efficiencyResults,
		BlackHoles:         blackHoles,
		HealthStatuses:     healthStatuses,
		HealthWarning:      healthWarning,
		TotalRPS:           totalRPS,
		TotalMonthlySaving: totalSaving,
		CriticalCount:      criticalCount,
		WarningCount:       warningCount,
	}, nil
}

// Run orchestrates collection and analysis, returns the formatted report.
func Run(params AnalyzeParams) (string, error) {
	data, err := Analyze(params)
	if err != nil {
		return "", err
	}
	if data.HealthWarning != "" {
		fmt.Printf("⚠️  health check skipped: %v\n", data.HealthWarning)
	}
	return renderAnalysisData(data, params.Config, params.OutputFormat), nil
}

func renderAnalysisData(data *AnalysisData, cfg *platformconfig.GlobalConfig, format string) string {
	advisorEngine := advisorpkg.NewEngine(cfg)
	advisorEngine.SetTotalRPS(data.TotalRPS)
	efficiencyEngine := advisorpkg.NewEfficiencyEngine(cfg)
	efficiencyEngine.SetTotalRPS(data.TotalRPS)
	healthEngine := advisorpkg.NewHealthEngine(cfg)
	return renderCombinedReport(advisorEngine, efficiencyEngine, healthEngine,
		data.AdvisorResults, data.EfficiencyResults, data.BlackHoles,
		data.HealthStatuses, format)
}

func collectPromMetrics(cfg *platformconfig.GlobalConfig, namespaces []string) ([]metricsdomain.ServiceMetrics, error) {
	coll := platformcollector.NewCollector(cfg)
	return coll.CollectAll(namespaces)
}

func buildAdviceResults(engine *advisorpkg.Engine, metrics []metricsdomain.ServiceMetrics) []advisordomain.AdviceResult {
	results := make([]advisordomain.AdviceResult, 0, len(metrics))
	for _, m := range metrics {
		if m.CPURequest <= 0 {
			continue
		}
		results = append(results, engine.Run(m))
	}
	return results
}

func buildEfficiencyResults(cfg *platformconfig.GlobalConfig, engine *advisorpkg.EfficiencyEngine, metrics []metricsdomain.ServiceMetrics) ([]advisordomain.EfficiencyResult, []advisordomain.ResourceBlackHole) {
	results := make([]advisordomain.EfficiencyResult, 0, len(metrics))
	blackHoles := make([]advisordomain.ResourceBlackHole, 0)

	for _, m := range metrics {
		if m.CPURequest <= 0 {
			continue
		}
		results = append(results, engine.Analyze(m))
		if bh, ok := identifyResourceBlackHole(m, cfg); ok {
			blackHoles = append(blackHoles, bh)
		}
	}

	sort.Slice(blackHoles, func(i, j int) bool {
		return blackHoles[i].WasteAmount > blackHoles[j].WasteAmount
	})

	return results, blackHoles
}

func identifyResourceBlackHole(m metricsdomain.ServiceMetrics, cfg *platformconfig.GlobalConfig) (advisordomain.ResourceBlackHole, bool) {
	currentCost := platformpricing.PodMonthlyCost(cfg, m.CPURequest, m.MemRequest, m.Replicas).TotalCost
	actualCost := platformpricing.PodMonthlyCost(cfg, m.CPUUsageAvg, m.MemUsageMax, m.Replicas).TotalCost

	// 成本门槛：必须超过黑洞判定阈值
	if currentCost <= cfg.Governance.BlackHoleCostThreshold {
		return advisordomain.ResourceBlackHole{}, false
	}

	// 统一黑洞检测：同时检查利用率维度和流量维度
	isUtilizationBlackHole := actualCost/currentCost < 0.1

	// 流量黑洞检测：有成本但流量极低
	isTrafficBlackHole := false
	if m.AvgRPS < 0.001 && currentCost > cfg.Governance.BlackHoleCostThreshold {
		isTrafficBlackHole = true
	}

	if !isUtilizationBlackHole && !isTrafficBlackHole {
		return advisordomain.ResourceBlackHole{}, false
	}

	wasteAmount := currentCost - actualCost
	wasteRatio := 1.0 - actualCost/currentCost
	if isTrafficBlackHole && !isUtilizationBlackHole {
		// 纯流量黑洞（有成本但无流量）：浪费按全额计算
		wasteAmount = currentCost
		wasteRatio = 1.0
	}

	return advisordomain.ResourceBlackHole{
		ServiceName: m.Deployment,
		Namespace:   m.Namespace,
		CurrentCost: currentCost,
		ActualCost:  actualCost,
		WasteRatio:  wasteRatio,
		WasteAmount: wasteAmount,
	}, true
}

func collectHealthStatuses(params AnalyzeParams) ([]healthdomain.HealthStatus, error) {
	duration, err := time.ParseDuration(params.Duration)
	if err != nil {
		return nil, fmt.Errorf("invalid duration %q: %w", params.Duration, err)
	}

	threshold5xx := 0.02
	if _, err := fmt.Sscanf(params.Threshold5xx, "%f", &threshold5xx); err != nil {
		threshold5xx = 0.02
	}

	namespaces := appcommon.ResolveTargetNamespaces(params.Config, params.Namespace)
	healthEngine := advisorpkg.NewHealthEngine(params.Config)
	traefikCollector := platformcollector.NewTraefikCollector(params.Config)

	// Collect raw metrics grouped by (namespace, serviceName).
	type svcKey struct {
		ns, name string
	}
	grouped := make(map[svcKey]*healthdomain.HealthMetrics)

	for _, ns := range namespaces {
		services, err := traefikCollector.CollectServiceList(ns)
		if err != nil {
			continue
		}
		for _, svc := range services {
			m, err := traefikCollector.CollectHealthMetrics(svc, duration, ns)
			if err != nil {
				continue
			}
			m.Namespace = ns
			m.MonthlyCost = traefikCollector.GetServiceMonthlyCost(svc, ns)

			key := svcKey{ns, m.ServiceName}
			if existing, ok := grouped[key]; ok {
				// Aggregate: sum RPS, accumulate error counts via RPS * rate
				totalRPS := existing.RPS + m.RPS
				if totalRPS > 0 {
					existing.Error5xxRate = (existing.Error5xxRate*existing.RPS + m.Error5xxRate*m.RPS) / totalRPS
					existing.Error4xxRate = (existing.Error4xxRate*existing.RPS + m.Error4xxRate*m.RPS) / totalRPS
				}
				existing.RPS = totalRPS
				if m.P99Latency > existing.P99Latency {
					existing.P99Latency = m.P99Latency
				}
			} else {
				grouped[key] = &m
			}
		}
	}

	allResults := make([]healthdomain.HealthStatus, 0, len(grouped))
	for _, m := range grouped {
		allResults = append(allResults, healthEngine.DetermineHealthCode(*m, threshold5xx))
	}

	appcommon.SortHealthStatuses(allResults)
	return allResults, nil
}

func renderCombinedReport(
	advisorEngine *advisorpkg.Engine,
	efficiencyEngine *advisorpkg.EfficiencyEngine,
	healthEngine *advisorpkg.HealthEngine,
	advisorResults []advisordomain.AdviceResult,
	efficiencyResults []advisordomain.EfficiencyResult,
	blackHoles []advisordomain.ResourceBlackHole,
	healthStatuses []healthdomain.HealthStatus,
	outputFormat string,
) string {
	switch strings.ToLower(outputFormat) {
	case "json":
		return renderCombinedJSON(advisorResults, efficiencyResults, blackHoles, healthStatuses)
	case "csv":
		return renderCombinedCSV(advisorResults, efficiencyResults, blackHoles, healthStatuses)
	case "markdown", "md":
		return renderCombinedMarkdown(advisorEngine, efficiencyEngine, healthEngine, advisorResults, efficiencyResults, blackHoles, healthStatuses)
	default:
		return renderCombinedTable(advisorEngine, efficiencyEngine, healthEngine, advisorResults, efficiencyResults, blackHoles, healthStatuses)
	}
}

func renderCombinedTable(
	advisorEngine *advisorpkg.Engine,
	efficiencyEngine *advisorpkg.EfficiencyEngine,
	healthEngine *advisorpkg.HealthEngine,
	advisorResults []advisordomain.AdviceResult,
	efficiencyResults []advisordomain.EfficiencyResult,
	blackHoles []advisordomain.ResourceBlackHole,
	healthStatuses []healthdomain.HealthStatus,
) string {
	var sb strings.Builder

	sb.WriteString(strings.Repeat("=", 120) + "\n")
	sb.WriteString("                    kops analyze — Kubernetes Resource & Health Analysis\n")
	sb.WriteString(strings.Repeat("=", 120) + "\n")

	// Section 1: Resource Recommendations (Advisor)
	sb.WriteString("\n==== 1. Resource Recommendations ====\n")
	sb.WriteString(advisorEngine.GenerateTableReport(advisorResults))
	sb.WriteString("\n")

	// Section 2: Traffic Efficiency (Inspect)
	sb.WriteString("\n==== 2. Traffic Efficiency ====\n")
	sb.WriteString(efficiencyEngine.RenderEfficiencyReport(efficiencyResults, blackHoles, "table"))
	sb.WriteString("\n")

	// Section 3: Health Status
	sb.WriteString("\n==== 3. Health Status ====\n")
	sb.WriteString(healthEngine.RenderHealthReport(healthStatuses, "table"))

	return sb.String()
}

func mustMarshal(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

func renderCombinedJSON(
	advisorResults []advisordomain.AdviceResult,
	efficiencyResults []advisordomain.EfficiencyResult,
	blackHoles []advisordomain.ResourceBlackHole,
	healthStatuses []healthdomain.HealthStatus,
) string {
	var parts []string
	parts = append(parts, fmt.Sprintf(`"advisor_results": %s`, mustMarshal(advisorResults)))
	parts = append(parts, fmt.Sprintf(`"efficiency_results": %s`, mustMarshal(efficiencyResults)))
	parts = append(parts, fmt.Sprintf(`"black_holes": %s`, mustMarshal(blackHoles)))
	parts = append(parts, fmt.Sprintf(`"health_statuses": %s`, mustMarshal(healthStatuses)))
	return "{\n  " + strings.Join(parts, ",\n  ") + "\n}\n"
}

func renderCombinedCSV(
	advisorResults []advisordomain.AdviceResult,
	efficiencyResults []advisordomain.EfficiencyResult,
	blackHoles []advisordomain.ResourceBlackHole,
	healthStatuses []healthdomain.HealthStatus,
) string {
	var sb strings.Builder

	// Advisor section
	sb.WriteString("=== Resource Recommendations ===\n")
	sb.WriteString("Service,Namespace,OldCPU(m),NewCPU(m),OldMem(MiB),NewMem(MiB),CurrentCost,RecommendedCost,ActualCost,MonthlySaving,RiskLevel,Action,Reason\n")
	for _, r := range advisorResults {
		sb.WriteString(fmt.Sprintf("%s,%s,%d,%d,%d,%d,%.2f,%.2f,%.2f,%.2f,%s,%s,%s\n",
			r.Deployment, r.Namespace, r.OldCPURequest, r.NewCPURequest, r.OldMemRequest, r.NewMemRequest,
			r.CurrentCost, r.RecommendedCost, r.ActualCost, r.MonthlySaving, r.RiskLevel, r.Action, r.Reason))
	}

	// Efficiency section
	sb.WriteString("\n=== Traffic Efficiency ===\n")
	sb.WriteString("Service,Namespace,CurrentCPU(m),RecCPU(m),TrafficDensity,Rank,CurrentCost,RecCost,ActualCost,WasteAmount,WasteRatio,Action,Reason\n")
	for _, r := range efficiencyResults {
		sb.WriteString(fmt.Sprintf("%s,%s,%d,%d,%.2f,%s,%.2f,%.2f,%.2f,%.2f,%.4f,%s,%s\n",
			r.ServiceName, r.Namespace, r.CurrentCPU, r.RecCPU, r.TrafficDensity, r.TrafficDensityRank,
			r.CurrentCost, r.RecCost, r.ActualCost, r.WasteAmount, r.WasteRatio, r.Action, r.Reason))
	}

	// Black Holes section
	sb.WriteString("\n=== Top 5 Resource Black Holes ===\n")
	sb.WriteString("Rank,Service,Namespace,CurrentCost,ActualCost,WasteRatio,WasteAmount\n")
	for i, bh := range blackHoles {
		if i >= 5 {
			break
		}
		sb.WriteString(fmt.Sprintf("%d,%s,%s,%.2f,%.2f,%.4f,%.2f\n",
			i+1, bh.ServiceName, bh.Namespace, bh.CurrentCost, bh.ActualCost, bh.WasteRatio, bh.WasteAmount))
	}

	// Health section
	sb.WriteString("\n=== Health Status ===\n")
	sb.WriteString("Namespace,Service,Status,HealthCode,RPS,Error5xx(%),Error4xx(%),P99Latency(ms),InvalidSpend,Diagnosis,Action\n")
	for _, r := range healthStatuses {
		e5xx, e4xx, p99 := "-", "-", "-"
		if r.RPS >= 0.001 {
			e5xx = fmt.Sprintf("%.2f", r.Error5xxRate*100)
			e4xx = fmt.Sprintf("%.2f", r.Error4xxRate*100)
			p99 = fmt.Sprintf("%.0f", r.P99Latency*1000)
		}
		sb.WriteString(fmt.Sprintf("%s,%s,%s,%s,%.2f,%s,%s,%s,%.2f,%s,%s\n",
			r.Namespace, r.ServiceName, r.StatusIcon, r.HealthCode, r.RPS, e5xx, e4xx, p99, r.InvalidSpend, r.Diagnosis, r.Action))
	}

	return sb.String()
}

func renderCombinedMarkdown(
	advisorEngine *advisorpkg.Engine,
	efficiencyEngine *advisorpkg.EfficiencyEngine,
	healthEngine *advisorpkg.HealthEngine,
	advisorResults []advisordomain.AdviceResult,
	efficiencyResults []advisordomain.EfficiencyResult,
	blackHoles []advisordomain.ResourceBlackHole,
	healthStatuses []healthdomain.HealthStatus,
) string {
	var sb strings.Builder

	sb.WriteString("# kops analyze — Kubernetes Resource & Health Analysis\n\n")

	sb.WriteString("## 1. Resource Recommendations\n\n")
	sb.WriteString(advisorEngine.GenerateMarkdownReport(advisorResults))
	sb.WriteString("\n")

	sb.WriteString("## 2. Traffic Efficiency\n\n")
	sb.WriteString(efficiencyEngine.RenderEfficiencyReport(efficiencyResults, blackHoles, "markdown"))
	sb.WriteString("\n")

	sb.WriteString("## 3. Health Status\n\n")
	sb.WriteString(healthEngine.RenderHealthReport(healthStatuses, "markdown"))

	return sb.String()
}

// ExportCSV returns the analysis data as a CSV string.
func ExportCSV(data *AnalysisData) string {
	return renderCombinedCSV(data.AdvisorResults, data.EfficiencyResults, data.BlackHoles, data.HealthStatuses)
}

// ExportJSON returns the analysis data as a JSON string.
func ExportJSON(data *AnalysisData) string {
	return renderCombinedJSON(data.AdvisorResults, data.EfficiencyResults, data.BlackHoles, data.HealthStatuses)
}
