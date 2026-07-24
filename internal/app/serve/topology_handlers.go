package serve

import (
	"fmt"
	"net/http"
	"sort"

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
func (s *Server) handleOtelSummary(c *gin.Context) {
	coll := platformcollector.NewCollector(s.cfg)

	// 1. Which services have OTel data?
	otelSvcQuery := `group by(service_name)(http_server_duration_milliseconds_count)`
	otelData, _ := coll.QueryInstant(otelSvcQuery)

	otelServices := make(map[string]bool)
	for _, r := range otelData {
		if svc := r.Metric["service_name"]; svc != "" {
			otelServices[svc] = true
		}
	}

	// 2. Service latency (P50/P95/P99) from histogram
	type svcLatency struct {
		Name   string  `json:"name"`
		P50    float64 `json:"p50"`
		P95    float64 `json:"p95"`
		P99    float64 `json:"p99"`
		RPS    float64 `json:"rps"`
		ErrRate float64 `json:"errRate"`
	}

	var latencies []svcLatency
	for svc := range otelServices {
		p50Query := fmt.Sprintf(`histogram_quantile(0.50, sum(rate(http_server_duration_milliseconds_bucket{service_name="%s"}[5m])) by (le))`, svc)
		p95Query := fmt.Sprintf(`histogram_quantile(0.95, sum(rate(http_server_duration_milliseconds_bucket{service_name="%s"}[5m])) by (le))`, svc)
		p99Query := fmt.Sprintf(`histogram_quantile(0.99, sum(rate(http_server_duration_milliseconds_bucket{service_name="%s"}[5m])) by (le))`, svc)
		rpsQuery := fmt.Sprintf(`sum(rate(http_server_duration_milliseconds_count{service_name="%s"}[5m]))`, svc)
		errQuery := fmt.Sprintf(`sum(rate(http_server_duration_milliseconds_count{service_name="%s",http_status_code=~"5.."}[5m])) / sum(rate(http_server_duration_milliseconds_count{service_name="%s"}[5m]))`, svc, svc)

		p50, _ := coll.QueryScalar(p50Query)
		p95, _ := coll.QueryScalar(p95Query)
		p99, _ := coll.QueryScalar(p99Query)
		rps, _ := coll.QueryScalar(rpsQuery)
		errRate, _ := coll.QueryScalar(errQuery)

		if rps > 0 {
			latencies = append(latencies, svcLatency{svc, p50, p95, p99, rps, errRate})
		}
	}

	// Sort by P99 desc
	sort.Slice(latencies, func(i, j int) bool { return latencies[i].P99 > latencies[j].P99 })

	// 3. Client dependencies (service-to-service calls)
	type depLatency struct {
		Source  string  `json:"source"`
		Target  string  `json:"target"`
		P50     float64 `json:"p50"`
		P99     float64 `json:"p99"`
		RPS     float64 `json:"rps"`
		ErrRate float64 `json:"errRate"`
	}

	var deps []depLatency
	clientQuery := `sum by(service_name, net_host_name)(rate(http_client_duration_milliseconds_count[5m]))`
	clientData, _ := coll.QueryInstant(clientQuery)
	for _, r := range clientData {
		src := r.Metric["service_name"]
		tgt := r.Metric["net_host_name"]
		if src == "" || tgt == "" || r.Value < 0.01 {
			continue
		}
		// Get latency for this pair
		p50Q := fmt.Sprintf(`histogram_quantile(0.50, sum(rate(http_client_duration_milliseconds_bucket{service_name="%s",net_host_name="%s"}[5m])) by (le))`, src, tgt)
		p99Q := fmt.Sprintf(`histogram_quantile(0.99, sum(rate(http_client_duration_milliseconds_bucket{service_name="%s",net_host_name="%s"}[5m])) by (le))`, src, tgt)
		errQ := fmt.Sprintf(`sum(rate(http_client_duration_milliseconds_count{service_name="%s",net_host_name="%s",http_status_code=~"5.."}[5m])) / sum(rate(http_client_duration_milliseconds_count{service_name="%s",net_host_name="%s"}[5m]))`, src, tgt, src, tgt)
		p50, _ := coll.QueryScalar(p50Q)
		p99, _ := coll.QueryScalar(p99Q)
		errR, _ := coll.QueryScalar(errQ)
		deps = append(deps, depLatency{src, tgt, p50, p99, r.Value, errR})
	}

	// 4. Deployment coverage
	data, _ := s.loadOrAnalyze()
	var coverage []gin.H
	if data != nil {
		for _, r := range data.AdvisorResults {
			covered := otelServices[r.Deployment] || otelServices[r.Deployment+"-prod"] || otelServices[r.Deployment+"-"+r.Namespace]
			coverage = append(coverage, gin.H{
				"deployment": r.Deployment,
				"namespace":  r.Namespace,
				"covered":    covered,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"latencies": latencies,
		"deps":      deps,
		"coverage":  coverage,
	})
}
