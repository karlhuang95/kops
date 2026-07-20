package serve

import (
	"net/http"

	"github.com/gin-gonic/gin"
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
