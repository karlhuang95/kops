package serve

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	appanalyze "kops/internal/app/analyze"
	platformcollector "kops/internal/platform/collector"
	platformconfig "kops/internal/platform/config"
	advisorpkg "kops/pkg/advisor"
)

type chartData struct {
	HealthLabels         []string  `json:"healthLabels"`
	HealthValues         []int     `json:"healthValues"`
	HealthColors         []string  `json:"healthColors"`
	RiskLabels           []string  `json:"riskLabels"`
	RiskValues           []int     `json:"riskValues"`
	RiskColors           []string  `json:"riskColors"`
	RankLabels           []string  `json:"rankLabels"`
	RankValues           []int     `json:"rankValues"`
	RankColors           []string  `json:"rankColors"`
	CostLabels           []string  `json:"costLabels"`
	CostCurrent          []float64 `json:"costCurrent"`
	CostRec              []float64 `json:"costRec"`
	NSCostLabels         []string  `json:"nsCostLabels"`
	NSCostCurrent        []float64 `json:"nsCostCurrent"`
	NSCostRec            []float64 `json:"nsCostRec"`
	CostBreakdownLabels  []string  `json:"costBreakdownLabels"`
	CostBreakdownValues  []float64 `json:"costBreakdownValues"`
}

func buildChartData(data *appanalyze.AnalysisData) template.JS {
	cd := chartData{
		HealthLabels: []string{"严重", "警告", "健康", "空闲"},
		HealthColors: []string{"#d93025", "#e37400", "#1e8e3e", "#9aa0a6"},
		RiskLabels:   []string{"高风险", "中风险", "低风险"},
		RiskColors:   []string{"#d93025", "#e37400", "#1e8e3e"},
		RankLabels:   []string{"S (极高)", "A (正常)", "B (低效)", "C (极低)"},
		RankColors:   []string{"#1e8e3e", "#1a73e8", "#e37400", "#9aa0a6"},
	}

	// Health distribution.
	healthCount := map[string]int{}
	for _, h := range data.HealthStatuses {
		healthCount[h.HealthCode]++
	}
	cd.HealthValues = []int{healthCount["Critical"], healthCount["Warning"], healthCount["Healthy"], healthCount["Idle"]}

	// Risk distribution (actual values: "高"/"中"/"低").
	riskCount := map[string]int{}
	for _, r := range data.AdvisorResults {
		riskCount[r.RiskLevel]++
	}
	cd.RiskValues = []int{riskCount["高"], riskCount["中"], riskCount["低"]}

	// Efficiency rank distribution (actual values: "S (极高)"/"A (正常)"/"B (低效)"/"C (极低/空转)").
	rankCount := map[string]int{}
	for _, e := range data.EfficiencyResults {
		switch {
		case strings.HasPrefix(e.TrafficDensityRank, "S"):
			rankCount["S"]++
		case strings.HasPrefix(e.TrafficDensityRank, "A"):
			rankCount["A"]++
		case strings.HasPrefix(e.TrafficDensityRank, "B"):
			rankCount["B"]++
		case strings.HasPrefix(e.TrafficDensityRank, "C"):
			rankCount["C"]++
		}
	}
	cd.RankValues = []int{rankCount["S"], rankCount["A"], rankCount["B"], rankCount["C"]}

	// Top 10 by current cost — compare current vs recommended cost.
	type costEntry struct {
		name    string
		current float64
		rec     float64
	}
	entries := make([]costEntry, 0, len(data.AdvisorResults))
	for _, r := range data.AdvisorResults {
		entries = append(entries, costEntry{r.Deployment, r.CurrentCost, r.RecommendedCost})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].current > entries[j].current })
	n := 10
	if len(entries) < n {
		n = len(entries)
	}
	for i := 0; i < n; i++ {
		cd.CostLabels = append(cd.CostLabels, entries[i].name)
		cd.CostCurrent = append(cd.CostCurrent, entries[i].current)
		cd.CostRec = append(cd.CostRec, entries[i].rec)
	}

	// Namespace-level cost aggregation
	nsCostCurrent := map[string]float64{}
	nsCostRec := map[string]float64{}
	for _, r := range data.AdvisorResults {
		nsCostCurrent[r.Namespace] += r.CurrentCost
		nsCostRec[r.Namespace] += r.RecommendedCost
	}
	type nsEntry struct{ ns string; cur, rec float64 }
	var nsEntries []nsEntry
	for ns, cur := range nsCostCurrent {
		nsEntries = append(nsEntries, nsEntry{ns, cur, nsCostRec[ns]})
	}
	sort.Slice(nsEntries, func(i, j int) bool { return nsEntries[i].cur > nsEntries[j].cur })
	for _, e := range nsEntries {
		cd.NSCostLabels = append(cd.NSCostLabels, e.ns)
		cd.NSCostCurrent = append(cd.NSCostCurrent, e.cur)
		cd.NSCostRec = append(cd.NSCostRec, e.rec)
	}

	// Cost breakdown: total Pod cost vs total Gateway cost
	var totalPodCost, totalGwCost float64
	for _, r := range data.AdvisorResults {
		totalPodCost += r.CurrentCost
		totalGwCost += r.GwShareCost
	}
	if totalGwCost > 0 {
		cd.CostBreakdownLabels = []string{"Pod 成本", "Gateway 分摊"}
		cd.CostBreakdownValues = []float64{totalPodCost, totalGwCost}
	}

	b, _ := json.Marshal(cd)
	return template.JS(b)
}

// loadOrAnalyze returns cached analysis data, or runs a fresh analysis and caches it.
func (s *Server) loadOrAnalyze() (*appanalyze.AnalysisData, error) {
	cache := NewCacheManager(s.cacheDir)
	if cache.Exists() {
		cf, err := cache.Load()
		if err == nil && cf.Data != nil {
			return cf.Data, nil
		}
	}
	params := appanalyze.AnalyzeParams{
		Config:       s.cfg,
		OutputFormat: "json",
		Duration:     "5m",
	}
	data, err := appanalyze.Analyze(params)
	if err != nil {
		return nil, err
	}
	now := time.Now().Format(time.RFC3339)
	_ = cache.Save(&cacheFile{CachedAt: now, Duration: "5m", Data: data})
	return data, nil
}

// pageData holds common template fields.
type pageData struct {
	Title          string
	Active         string
	LastUpdated    string
	ErrorMessage   string
	Stats          *overviewStats
	ChartJS        template.JS
	Results        interface{}
	BlackHoles     interface{}
	HealthWarning  string
	CriticalCount  int
	WarningCount   int
	HealthyCount   int
	IdleCount      int
	TotalInvalidSpend float64
	AvgScore       string
}

type overviewStats struct {
	ServiceCount   int
	TotalSaving    float64
	CriticalCount  int
	WarningCount   int
	BlackHoleCount int
	TotalRPS       float64
	AvgHealthScore string
}

func buildOverviewStats(data *appanalyze.AnalysisData) *overviewStats {
	var scoreTotal float64
	var scoreCount int
	for _, h := range data.HealthStatuses {
		if h.HealthCode != "Idle" {
			scoreTotal += h.HealthScore
			scoreCount++
		}
	}
	avgScore := "0"
	if scoreCount > 0 {
		avgScore = fmt.Sprintf("%.0f", scoreTotal/float64(scoreCount))
	}
	return &overviewStats{
		ServiceCount:   len(data.AdvisorResults),
		TotalSaving:    data.TotalMonthlySaving,
		CriticalCount:  data.CriticalCount,
		WarningCount:   data.WarningCount,
		BlackHoleCount: len(data.BlackHoles),
		TotalRPS:       data.TotalRPS,
		AvgHealthScore: avgScore,
	}
}

func buildHealthStats(data *appanalyze.AnalysisData) (critical, warning, healthy, idle int, totalInvalid float64, avgScore string) {
	var scoreTotal float64
	var scoreCount int
	for _, h := range data.HealthStatuses {
		switch h.HealthCode {
		case "Critical": critical++
		case "Warning": warning++
		case "Healthy": healthy++
		case "Idle": idle++
		}
		totalInvalid += h.InvalidSpend
		if h.HealthCode != "Idle" {
			scoreTotal += h.HealthScore
			scoreCount++
		}
	}
	if scoreCount > 0 {
		avgScore = fmt.Sprintf("%.0f", scoreTotal/float64(scoreCount))
	} else {
		avgScore = "0"
	}
	return
}

// ====== Page handlers ======

func (s *Server) handleOverview(c *gin.Context) {
	data, err := s.loadOrAnalyze()
	ns := c.Query("namespace")
	if ns != "" && data != nil {
		data = filterAnalysisByNamespace(data, ns)
	}
	stats := buildOverviewStats(data)
	if err != nil {
		c.HTML(http.StatusOK, "overview.html", &pageData{Title: "总览", Active: "overview", ErrorMessage: err.Error()})
		return
	}
	c.HTML(http.StatusOK, "overview.html", &pageData{
		Title: "总览", Active: "overview", LastUpdated: time.Now().Format(time.RFC3339),
		Stats: stats, ChartJS: buildChartData(data),
	})
}

func (s *Server) handleRecommendationsPage(c *gin.Context) {
	data, err := s.loadOrAnalyze()
	if err != nil {
		c.HTML(http.StatusOK, "recommendations.html", &pageData{Title: "资源推荐", Active: "recommendations", ErrorMessage: err.Error()})
		return
	}
	c.HTML(http.StatusOK, "recommendations.html", &pageData{
		Title: "资源推荐", Active: "recommendations", LastUpdated: time.Now().Format(time.RFC3339),
		Results: data.AdvisorResults,
	})
}

func (s *Server) handleEfficiencyPage(c *gin.Context) {
	data, err := s.loadOrAnalyze()
	if err != nil {
		c.HTML(http.StatusOK, "efficiency.html", &pageData{Title: "流量效率", Active: "efficiency", ErrorMessage: err.Error()})
		return
	}
	c.HTML(http.StatusOK, "efficiency.html", &pageData{
		Title: "流量效率", Active: "efficiency", LastUpdated: time.Now().Format(time.RFC3339),
		Results: data.EfficiencyResults, BlackHoles: data.BlackHoles,
	})
}

func (s *Server) handleHealthPage(c *gin.Context) {
	data, err := s.loadOrAnalyze()
	if err != nil {
		c.HTML(http.StatusOK, "health.html", &pageData{Title: "健康状态", Active: "health", ErrorMessage: err.Error()})
		return
	}
	critical, warning, healthy, idle, totalInvalid, avgScore := buildHealthStats(data)
	c.HTML(http.StatusOK, "health.html", &pageData{
		Title: "健康状态", Active: "health", LastUpdated: time.Now().Format(time.RFC3339),
		Results: data.HealthStatuses, HealthWarning: data.HealthWarning,
		CriticalCount: critical, WarningCount: warning, HealthyCount: healthy, IdleCount: idle,
		TotalInvalidSpend: totalInvalid, AvgScore: avgScore,
	})
}

func (s *Server) handleClusterPage(c *gin.Context) {
	c.HTML(http.StatusOK, "cluster.html", &pageData{Title: "集群分析", Active: "cluster", LastUpdated: time.Now().Format(time.RFC3339)})
}

// filterAnalysisByNamespace returns a copy of data filtered to only include a specific namespace.
func filterAnalysisByNamespace(data *appanalyze.AnalysisData, ns string) *appanalyze.AnalysisData {
	if ns == "" || data == nil {
		return data
	}
	filtered := &appanalyze.AnalysisData{
		HealthWarning: data.HealthWarning,
		TotalRPS:      data.TotalRPS,
	}

	for _, r := range data.AdvisorResults {
		if r.Namespace == ns {
			filtered.AdvisorResults = append(filtered.AdvisorResults, r)
			filtered.TotalMonthlySaving += r.MonthlySaving
		}
	}
	for _, e := range data.EfficiencyResults {
		if e.Namespace == ns {
			filtered.EfficiencyResults = append(filtered.EfficiencyResults, e)
		}
	}
	for _, bh := range data.BlackHoles {
		if bh.Namespace == ns {
			filtered.BlackHoles = append(filtered.BlackHoles, bh)
		}
	}
	for _, h := range data.HealthStatuses {
		if h.Namespace == ns {
			filtered.HealthStatuses = append(filtered.HealthStatuses, h)
			switch h.HealthCode {
			case "Critical":
				filtered.CriticalCount++
			case "Warning":
				filtered.WarningCount++
			}
		}
	}
	// Recalculate total RPS for the filtered namespace
	var filteredRPS float64
	for _, r := range filtered.AdvisorResults {
		filteredRPS += r.AvgRPS
	}
	filtered.TotalRPS = filteredRPS

	return filtered
}

func (s *Server) handleHealthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) handleTrend(c *gin.Context) {
	cache := NewCacheManager(s.cacheDir)
	history, err := cache.LoadHistory()
	if err != nil || len(history) < 2 {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "trends": nil, "message": "需要至少两次分析才能显示趋势"})
		return
	}

	prev := history[len(history)-2]
	curr := history[len(history)-1]

	type trendData struct {
		PrevTime             string  `json:"prevTime"`
		CurrTime             string  `json:"currTime"`
		PrevServiceCount     int     `json:"prevServiceCount"`
		CurrServiceCount     int     `json:"currServiceCount"`
		PrevMonthlySaving    float64 `json:"prevMonthlySaving"`
		CurrMonthlySaving    float64 `json:"currMonthlySaving"`
		PrevCriticalCount    int     `json:"prevCriticalCount"`
		CurrCriticalCount    int     `json:"currCriticalCount"`
		PrevBlackHoleCount   int     `json:"prevBlackHoleCount"`
		CurrBlackHoleCount   int     `json:"currBlackHoleCount"`
		SavingDelta          float64 `json:"savingDelta"`
		CriticalDelta        int     `json:"criticalDelta"`
		BlackHoleDelta       int     `json:"blackHoleDelta"`
	}

	if curr.Data == nil || prev.Data == nil {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "trends": nil})
		return
	}

	td := trendData{
		PrevTime:           prev.CachedAt,
		CurrTime:           curr.CachedAt,
		PrevServiceCount:   len(prev.Data.AdvisorResults),
		CurrServiceCount:   len(curr.Data.AdvisorResults),
		PrevMonthlySaving:  prev.Data.TotalMonthlySaving,
		CurrMonthlySaving:  curr.Data.TotalMonthlySaving,
		PrevCriticalCount:  prev.Data.CriticalCount,
		CurrCriticalCount:  curr.Data.CriticalCount,
		PrevBlackHoleCount: len(prev.Data.BlackHoles),
		CurrBlackHoleCount: len(curr.Data.BlackHoles),
		SavingDelta:        curr.Data.TotalMonthlySaving - prev.Data.TotalMonthlySaving,
		CriticalDelta:      curr.Data.CriticalCount - prev.Data.CriticalCount,
		BlackHoleDelta:     len(curr.Data.BlackHoles) - len(prev.Data.BlackHoles),
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "trends": td})
}

func (s *Server) handleConfigReload(c *gin.Context) {
	newCfg, err := platformconfig.LoadConfig(c.Query("config"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": err.Error()})
		return
	}
	s.cfg = newCfg
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "configuration reloaded"})
}

func (s *Server) handlePrometheusHealth(c *gin.Context) {
	coll := platformcollector.NewCollector(s.cfg)
	start := time.Now()
	_, err := coll.QueryRange("up", time.Now().Add(-1*time.Minute), time.Now(), 30*time.Second)
	latency := time.Since(start)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  "error",
			"error":   err.Error(),
			"latency": latency.String(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"latency": latency.String(),
	})
}

func (s *Server) handleRefresh(c *gin.Context) {
	params := appanalyze.AnalyzeParams{
		Config:       s.cfg,
		Namespace:    c.Query("namespace"),
		OutputFormat: "json",
		Duration:     c.DefaultQuery("duration", "5m"),
		Threshold5xx: c.DefaultQuery("threshold", "0.02"),
	}

	data, err := appanalyze.Analyze(params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "error": err.Error()})
		return
	}

	now := time.Now().Format(time.RFC3339)
	cache := NewCacheManager(s.cacheDir)
	_ = cache.Save(&cacheFile{
		CachedAt: now,
		Duration: params.Duration,
		Data:     data,
	})

	// Check and send alerts if configured
	if s.cfg.FinOps.Monitoring.Enabled && len(s.cfg.FinOps.Monitoring.AlertChannels) > 0 {
		for _, ch := range s.cfg.FinOps.Monitoring.AlertChannels {
			if ch.Type == "webhook" {
				if url, ok := ch.Config["url"].(string); ok && url != "" {
					notifier := NewAlertNotifier(url)
					notifier.CheckAndAlert(data)
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "cached_at": now})
}

func (s *Server) handleAnalysisJSON(c *gin.Context) {
	params := appanalyze.AnalyzeParams{
		Config:       s.cfg,
		Namespace:    c.Query("namespace"),
		OutputFormat: "json",
		Duration:     c.DefaultQuery("duration", "5m"),
		Threshold5xx: c.DefaultQuery("threshold", "0.02"),
	}

	data, err := appanalyze.Analyze(params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, data)
}

func (s *Server) handleExportCSV(c *gin.Context) {
	data := s.getCachedData()
	if data == nil {
		c.String(http.StatusServiceUnavailable, "no cached data available")
		return
	}
	csv := appanalyze.ExportCSV(data)
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=kops_analysis.csv")
	c.String(http.StatusOK, csv)
}

func (s *Server) handleExportJSON(c *gin.Context) {
	data := s.getCachedData()
	if data == nil {
		c.String(http.StatusServiceUnavailable, "no cached data available")
		return
	}
	jsonStr := appanalyze.ExportJSON(data)
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=kops_analysis.json")
	c.String(http.StatusOK, jsonStr)
}

func (s *Server) getCachedData() *appanalyze.AnalysisData {
	cache := NewCacheManager(s.cacheDir)
	cf, err := cache.Load()
	if err != nil || cf.Data == nil {
		return nil
	}
	return cf.Data
}

// ====== Cluster & Node handlers ======

func (s *Server) handleClusterNodes(c *gin.Context) {
	coll := platformcollector.NewCollector(s.cfg)
	cm, err := coll.CollectClusterMetrics()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cm)
}

func (s *Server) handleNodeScaling(c *gin.Context) {
	targetCPU := 0.7
	targetMem := 0.7
	if s.cfg.Governance.TargetUtilization > 0 {
		targetCPU = s.cfg.Governance.TargetUtilization
	}
	if s.cfg.Governance.MemoryTargetUtilization > 0 {
		targetMem = s.cfg.Governance.MemoryTargetUtilization
	}

	coll := platformcollector.NewCollector(s.cfg)
	result := coll.CollectNodeScalingRecommendation(targetCPU, targetMem)
	c.JSON(http.StatusOK, result)
}

func (s *Server) handleCostAttribution(c *gin.Context) {
	namespaces := s.cfg.Namespaces
	if ns := c.Query("namespace"); ns != "" {
		namespaces = []string{ns}
	}
	labelKeys := c.QueryArray("label")
	if len(labelKeys) == 0 {
		// Default: attribute by common labels
		labelKeys = []string{"app", "team", "owner", "cost-center"}
	}

	coll := platformcollector.NewCollector(s.cfg)
	result := coll.CollectLabelCosts(namespaces, labelKeys)

	// Convert map to sorted slice
	type entry struct {
		Key   string                         `json:"key"`
		Value *platformcollector.LabelCostEntry `json:"value"`
	}
	var entries []entry
	for k, v := range result {
		entries = append(entries, entry{Key: k, Value: v})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Value.TotalCost > entries[j].Value.TotalCost
	})

	c.JSON(http.StatusOK, gin.H{"attributions": entries})
}

func (s *Server) handleForecast(c *gin.Context) {
	ns := c.Param("namespace")
	name := c.Param("name")
	daysStr := c.DefaultQuery("days", "7")
	days := 7
	if d, err := strconv.Atoi(daysStr); err == nil && d > 0 && d <= 90 {
		days = d
	}

	coll := platformcollector.NewCollector(s.cfg)
	cpuPts, memPts, _, err := coll.CollectServiceHistory(ns, name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Simple forecast: use current value with linear projection
	type forecastPoint struct {
		Timestamp int64   `json:"t"`
		Value     float64 `json:"v"`
		IsForecast bool   `json:"isForecast,omitempty"`
	}

	var cpuForecast []forecastPoint
	var memForecast []forecastPoint

	// Use last known values for projection
	var lastCPU, lastMem float64
	if len(cpuPts) > 0 {
		lastCPU = cpuPts[len(cpuPts)-1].Value
	}
	if len(memPts) > 0 {
		lastMem = memPts[len(memPts)-1].Value
	}

	// Generate simple projection points (7-day horizon)
	now := time.Now().Unix()
	for i := 0; i <= days; i++ {
		ts := now + int64(i*86400)
		// Simple 5% growth projection per week
		growth := 1.0 + float64(i)*0.05/float64(days)
		cpuForecast = append(cpuForecast, forecastPoint{Timestamp: ts, Value: lastCPU * growth, IsForecast: true})
		memForecast = append(memForecast, forecastPoint{Timestamp: ts, Value: lastMem * growth, IsForecast: true})
	}

	// Add current actual points
	var cpuActual []forecastPoint
	var memActual []forecastPoint
	for _, p := range cpuPts {
		cpuActual = append(cpuActual, forecastPoint{Timestamp: p.Timestamp, Value: p.Value})
	}
	for _, p := range memPts {
		memActual = append(memActual, forecastPoint{Timestamp: p.Timestamp, Value: p.Value})
	}

	c.JSON(http.StatusOK, gin.H{
		"cpuActual":   cpuActual,
		"memActual":   memActual,
		"cpuForecast": cpuForecast,
		"memForecast": memForecast,
		"lastCPU":     lastCPU,
		"lastMem":     lastMem,
		"days":        days,
	})
}

// handleServiceTimeSeries returns CPU/memory/RPS time-series for a service.
func (s *Server) handleServiceTimeSeries(c *gin.Context) {
	ns := c.Param("namespace")
	name := c.Param("name")
	duration := c.DefaultQuery("duration", "6h")
	step := c.DefaultQuery("step", "5m")

	d, err := time.ParseDuration(duration)
	if err != nil {
		d = 6 * time.Hour
	}
	sd, err := time.ParseDuration(step)
	if err != nil {
		sd = 5 * time.Minute
	}

	end := time.Now()
	start := end.Add(-d)

	coll := platformcollector.NewCollector(s.cfg)

	// Match pods starting with deploy name, but exclude consumer/cron/job/worker sub-deployments.
	cpuQuery := fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{namespace="%s", pod=~"^%s-.*", pod!~".*-consumer-.*|.*-cron-.*|.*-job-.*|.*-worker-.*", container!=""}[5m])) * 1000`, ns, name)
	cpuPoints, _ := coll.QueryRange(cpuQuery, start, end, sd)

	memQuery := fmt.Sprintf(`sum(container_memory_working_set_bytes{namespace="%s", pod=~"^%s-.*", pod!~".*-consumer-.*|.*-cron-.*|.*-job-.*|.*-worker-.*", container!=""}) / 1024 / 1024`, ns, name)
	memPoints, _ := coll.QueryRange(memQuery, start, end, sd)

	rpsQuery := fmt.Sprintf(`sum(rate(traefik_service_requests_total{exported_service=~"%s-%s-.*"}[5m]))`, ns, name)
	rpsPoints, _ := coll.QueryRange(rpsQuery, start, end, sd)

	c.JSON(http.StatusOK, gin.H{
		"cpuPoints": cpuPoints,
		"memPoints": memPoints,
		"rpsPoints": rpsPoints,
	})
}

// handleServiceRecommendation returns Advisor recommendation for a single service.
func (s *Server) handleServiceRecommendation(c *gin.Context) {
	ns := c.Param("namespace")
	name := c.Param("name")

	coll := platformcollector.NewCollector(s.cfg)
	// Collect metrics for this single service
	m := coll.CollectSingleService(ns, name)

	if m.CPURequest <= 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found or no metrics available"})
		return
	}

	// Run Advisor engine
	advisorEngine := advisorpkg.NewEngine(s.cfg)
	// For single service, totalRPS is just this service's RPS
	advisorEngine.SetTotalRPS(m.AvgRPS)
	result := advisorEngine.Run(m)

	// Include replicas count in response
	c.JSON(http.StatusOK, gin.H{
		"advice":   result,
		"replicas": m.Replicas,
		"podCount": m.Replicas,
	})
}

// detailPageData holds chart data for a single service detail page.
type detailPageData struct {
	ServiceName string
	Namespace   string
	Duration    string
	CPUPoints   template.JS
	MemPoints   template.JS
	RPSPoints   template.JS
}

func (s *Server) handleServiceDetail(c *gin.Context) {
	ns := c.Param("namespace")
	name := c.Param("name")
	duration := c.DefaultQuery("duration", "6h")
	step := c.DefaultQuery("step", "5m")

	d, err := time.ParseDuration(duration)
	if err != nil {
		d = 6 * time.Hour
	}
	sd, err := time.ParseDuration(step)
	if err != nil {
		sd = 5 * time.Minute
	}

	end := time.Now()
	start := end.Add(-d)

	coll := platformcollector.NewCollector(s.cfg)

	// CPU usage time series (millicores) — only main deployment pods, exclude cron/consumer
	cpuQuery := fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{namespace="%s", pod=~"^%s-.*", pod!~".*-consumer-.*|.*-cron-.*|.*-job-.*|.*-worker-.*", container!=""}[%s])) * 1000`, ns, name, step)
	cpuPoints, _ := coll.QueryRange(cpuQuery, start, end, sd)

	// Memory usage time series (MiB) — only main deployment pods
	memQuery := fmt.Sprintf(`sum(container_memory_working_set_bytes{namespace="%s", pod=~"^%s-.*", pod!~".*-consumer-.*|.*-cron-.*|.*-job-.*|.*-worker-.*", container!=""}) / 1024 / 1024`, ns, name)
	memPoints, _ := coll.QueryRange(memQuery, start, end, sd)

	// RPS time series
	rpsQuery := fmt.Sprintf(`sum(rate(traefik_service_requests_total{exported_service=~"%s-%s-.*"}[%s]))`, ns, name, step)
	rpsPoints, _ := coll.QueryRange(rpsQuery, start, end, sd)

	type point struct {
		T int64   `json:"t"`
		V float64 `json:"v"`
	}
	toJS := func(pts []platformcollector.TimeSeriesPoint) template.JS {
		out := make([]point, 0, len(pts))
		for _, p := range pts {
			out = append(out, point{T: p.Timestamp, V: p.Value})
		}
		b, _ := json.Marshal(out)
		return template.JS(b)
	}

	c.HTML(http.StatusOK, "detail.html", &detailPageData{
		ServiceName: name,
		Namespace:   ns,
		Duration:    duration,
		CPUPoints:   toJS(cpuPoints),
		MemPoints:   toJS(memPoints),
		RPSPoints:   toJS(rpsPoints),
	})
}
