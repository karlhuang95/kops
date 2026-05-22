package serve

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	appanalyze "kops/internal/app/analyze"
	platformcollector "kops/internal/platform/collector"
)

type dashboardData struct {
	Analysis      *appanalyze.AnalysisData
	LastUpdated   string
	ErrorMessage  string
	Duration      string
	FromCache     bool
	ChartJS       template.JS
	NamespaceList []string
	CurrentNS     string
}

type chartData struct {
	HealthLabels []string  `json:"healthLabels"`
	HealthValues []int     `json:"healthValues"`
	HealthColors []string  `json:"healthColors"`
	RiskLabels   []string  `json:"riskLabels"`
	RiskValues   []int     `json:"riskValues"`
	RiskColors   []string  `json:"riskColors"`
	RankLabels   []string  `json:"rankLabels"`
	RankValues   []int     `json:"rankValues"`
	RankColors   []string  `json:"rankColors"`
	CostLabels   []string  `json:"costLabels"`
	CostCurrent  []float64 `json:"costCurrent"`
	CostRec      []float64 `json:"costRec"`
}

func collectNamespaces(data *appanalyze.AnalysisData) []string {
	seen := map[string]bool{}
	for _, r := range data.AdvisorResults {
		seen[r.Namespace] = true
	}
	for _, e := range data.EfficiencyResults {
		seen[e.Namespace] = true
	}
	for _, h := range data.HealthStatuses {
		seen[h.Namespace] = true
	}
	var out []string
	for ns := range seen {
		out = append(out, ns)
	}
	sort.Strings(out)
	return out
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

	b, _ := json.Marshal(cd)
	return template.JS(b)
}

func (s *Server) handleDashboard(c *gin.Context) {
	params := appanalyze.AnalyzeParams{
		Config:       s.cfg,
		Namespace:    c.Query("namespace"),
		OutputFormat: "html",
		Duration:     c.DefaultQuery("duration", "5m"),
		Threshold5xx: c.DefaultQuery("threshold", "0.02"),
	}
	currentNS := c.Query("namespace")

	cache := NewCacheManager(s.cacheDir)

	if cache.Exists() {
		cf, err := cache.Load()
		if err == nil && cf.Data != nil {
			c.HTML(http.StatusOK, "dashboard.html", &dashboardData{
				Analysis:      cf.Data,
				LastUpdated:   cf.CachedAt,
				Duration:      cf.Duration,
				FromCache:     true,
				ChartJS:       buildChartData(cf.Data),
				NamespaceList: collectNamespaces(cf.Data),
				CurrentNS:     currentNS,
			})
			return
		}
	}

	data, err := appanalyze.Analyze(params)
	if err != nil {
		c.HTML(http.StatusOK, "dashboard.html", &dashboardData{
			ErrorMessage: err.Error(),
			LastUpdated:  time.Now().Format(time.RFC3339),
			Duration:     params.Duration,
		})
		return
	}

	now := time.Now().Format(time.RFC3339)
	_ = cache.Save(&cacheFile{
		CachedAt: now,
		Duration: params.Duration,
		Data:     data,
	})

	c.HTML(http.StatusOK, "dashboard.html", &dashboardData{
		Analysis:      data,
		LastUpdated:   now,
		Duration:      params.Duration,
		FromCache:     false,
		ChartJS:       buildChartData(data),
		NamespaceList: collectNamespaces(data),
		CurrentNS:     currentNS,
	})
}

func (s *Server) handleHealthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
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

	// CPU usage time series (millicores)
	cpuQuery := fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{namespace="%s", pod=~"%s-.*", container!=""}[%s])) * 1000`, ns, name, step)
	cpuPoints, _ := coll.QueryRange(cpuQuery, start, end, sd)

	// Memory usage time series (MiB)
	memQuery := fmt.Sprintf(`sum(container_memory_working_set_bytes{namespace="%s", pod=~"%s-.*", container!=""}) / 1024 / 1024`, ns, name)
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
