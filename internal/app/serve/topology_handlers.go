package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	platformcollector "kops/internal/platform/collector"
)

// handleTopologyPage renders the business topology graph page.
func (s *Server) handleTopologyPage(c *gin.Context) {
	c.HTML(http.StatusOK, "topology.html", &pageData{
		Title:       "业务拓扑",
		Active:      "topology",
		LastUpdated: "",
	})
}

// handleTopologyGraph returns the full graph data as JSON for D3.js rendering.
func (s *Server) handleTopologyGraph(c *gin.Context) {
	if s.topoStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存储未配置，请检查 config.yaml 中 storage 配置"})
		return
	}

	gd, err := s.topoStore.GetGraphData(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gd)
}

// handleTopologyCleanup clears all topology data from local storage.
func (s *Server) handleTopologyCleanup(c *gin.Context) {
	if s.topoStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存储未配置"})
		return
	}

	if err := s.topoStore.ClearAll(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "拓扑数据已清理"})
}

// handleTopologyRefresh triggers a full topology rebuild from live data sources.
func (s *Server) handleTopologyRefresh(c *gin.Context) {
	if s.topoStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存储未配置"})
		return
	}

	data, err := s.loadOrAnalyze()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "分析失败: " + err.Error()})
		return
	}

	if err := s.topoStore.PopulateAll(c.Request.Context(), data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "拓扑写入失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "拓扑数据已刷新"})
}

// handleObservability renders the OTel observability page.
func (s *Server) handleObservability(c *gin.Context) {
	c.HTML(http.StatusOK, "observability.html", &pageData{Title: "OTel 可观测", Active: "observability"})
}

// handleOtelSummary queries Prometheus for OTel metrics summary.
const otelCacheFile = "otel.json"

// collectOtelData queries Prometheus for OTel metrics and returns the full dataset.
func (s *Server) collectOtelData() (gin.H, error) {
	coll := platformcollector.NewCollector(s.cfg)

	// 1. Discover services
	svcQuery := `group by(exported_job)(http_server_request_duration_milliseconds_count{http_route!="",exported_job!~".*_fat"})`
	svcData, _ := coll.QueryInstant(svcQuery)
	otelServices := make(map[string]bool)
	var svcList []string
	for _, r := range svcData {
		if svc := r.Metric["exported_job"]; svc != "" {
			otelServices[svc] = true
			svcList = append(svcList, svc)
		}
	}

	// 2. Service latency
	type svcLatency struct {
		Name    string  `json:"name"`
		P50     float64 `json:"p50"`
		P95     float64 `json:"p95"`
		P99     float64 `json:"p99"`
		RPS     float64 `json:"rps"`
		ErrRate float64 `json:"errRate"`
	}
	var latencies []svcLatency
	for _, svc := range svcList {
		bucket := fmt.Sprintf(`http_server_request_duration_milliseconds_bucket{exported_job="%s",http_route!=""}`, svc)
		counter := fmt.Sprintf(`http_server_request_duration_milliseconds_count{exported_job="%s",http_route!=""}`, svc)
		p50, _ := coll.QueryScalar(fmt.Sprintf(`histogram_quantile(0.50, sum(rate(%s[5m])) by (le))`, bucket))
		p95, _ := coll.QueryScalar(fmt.Sprintf(`histogram_quantile(0.95, sum(rate(%s[5m])) by (le))`, bucket))
		p99, _ := coll.QueryScalar(fmt.Sprintf(`histogram_quantile(0.99, sum(rate(%s[5m])) by (le))`, bucket))
		rps, _ := coll.QueryScalar(fmt.Sprintf(`sum(rate(%s[5m]))`, counter))
		if rps > 0 {
			latencies = append(latencies, svcLatency{svc, p50, p95, p99, rps, 0})
		}
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i].P99 > latencies[j].P99 })

	// 3. Endpoint analysis
	type routeInfo struct {
		Service string  `json:"service"`
		Route   string  `json:"route"`
		P99     float64 `json:"p99"`
		RPS     float64 `json:"rps"`
	}
	routeQ := `topk(50, sum by(exported_job, http_route)(rate(http_server_request_duration_milliseconds_count{http_route!=""}[5m])))`
	routeData, _ := coll.QueryInstant(routeQ)
	var routes []routeInfo
	for _, r := range routeData {
		svc := r.Metric["exported_job"]
		route := r.Metric["http_route"]
		if svc == "" || route == "" {
			continue
		}
		bucket := fmt.Sprintf(`http_server_request_duration_milliseconds_bucket{exported_job="%s",http_route="%s"}`, svc, route)
		p99, _ := coll.QueryScalar(fmt.Sprintf(`histogram_quantile(0.99, sum(rate(%s[5m])) by (le))`, bucket))
		routes = append(routes, routeInfo{svc, route, p99, r.Value})
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].P99 > routes[j].P99 })

	// 4. Coverage
	data, _ := s.loadOrAnalyze()
	var coverage []gin.H
	if data != nil {
		for _, r := range data.AdvisorResults {
			covered := otelServices[r.Deployment]
			if !covered {
				depLower := strings.ToLower(r.Deployment)
				for svc := range otelServices {
					if strings.Contains(strings.ToLower(svc), depLower) || strings.Contains(depLower, strings.ToLower(svc)) {
						covered = true
						break
					}
				}
			}
			coverage = append(coverage, gin.H{"deployment": r.Deployment, "namespace": r.Namespace, "covered": covered})
		}
	}

	return gin.H{"latencies": latencies, "routes": routes, "deps": []gin.H{}, "coverage": coverage}, nil
}

// handleOtelSummary reads cached OTel data, or returns empty if no cache exists.
func (s *Server) handleOtelSummary(c *gin.Context) {
	dir := s.cfg.Storage.Path
	if dir == "" {
		c.JSON(http.StatusOK, gin.H{"latencies": nil, "routes": nil, "deps": nil, "coverage": nil})
		return
	}
	fp := filepath.Join(dir, otelCacheFile)
	raw, err := os.ReadFile(fp)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"latencies": nil, "routes": nil, "deps": nil, "coverage": nil, "prev": nil})
		return
	}
	var result gin.H
	json.Unmarshal(raw, &result)

	// Also load previous snapshot for trend comparison
	prevPath := filepath.Join(dir, "otel_prev.json")
	if prevRaw, prevErr := os.ReadFile(prevPath); prevErr == nil {
		var prevData gin.H
		if json.Unmarshal(prevRaw, &prevData) == nil {
			result["prev"] = prevData
		}
	} else {
		result["prev"] = nil
	}

	c.JSON(http.StatusOK, result)
}

// handleOtelRefresh queries Prometheus live and caches the result.
func (s *Server) handleOtelRefresh(c *gin.Context) {
	data, err := s.collectOtelData()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Save previous as backup, then write new cache
	dir := s.cfg.Storage.Path
	if dir != "" {
		os.MkdirAll(dir, 0755)
		fp := filepath.Join(dir, otelCacheFile)
		prevPath := filepath.Join(dir, "otel_prev.json")
		os.Rename(fp, prevPath) // ignore error if not exists
		raw, _ := json.MarshalIndent(data, "", "  ")
		os.WriteFile(fp, raw, 0644)
	}
	c.JSON(http.StatusOK, data)
}
