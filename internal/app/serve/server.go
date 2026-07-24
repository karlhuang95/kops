package serve

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	platformconfig "kops/internal/platform/config"
	topostore "kops/internal/platform/storage"
)

//go:embed templates/*
var templateFS embed.FS

// Server wraps the Gin router and HTTP server.
type Server struct {
	router    *gin.Engine
	srv       *http.Server
	cfg       *platformconfig.GlobalConfig
	cacheDir  string
	topoStore *topostore.TopologyStore
}

// New creates a Gin router, loads embedded templates, registers routes.
func New(cfg *platformconfig.GlobalConfig, port int, cacheDir string) *Server {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()

	// Structured logging and rate limiting
	logger := Logger()
	limiter := NewRateLimiter(10, 30) // 10 req/s, burst 30
	router.Use(StructuredLogging(logger), limiter.Middleware(), gin.Recovery())
	slog.Info("kops server initialized", "port", port, "cache_dir", cacheDir)

	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html"))
	router.SetHTMLTemplate(tmpl)

	s := &Server{
		router:   router,
		cfg:      cfg,
		cacheDir: cacheDir,
		srv: &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: router,
		},
	}

	// Initialize local topology store (non-fatal if unavailable).
	topoStore, err := topostore.NewTopologyStore(context.Background(), cfg)
	if err != nil {
		slog.Warn("topology store unavailable, topology features disabled", "err", err)
	} else {
		s.topoStore = topoStore
	}

	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	// Pages
	s.router.GET("/", s.handleOverview)
	s.router.GET("/recommendations", s.handleRecommendationsPage)
	s.router.GET("/efficiency", s.handleEfficiencyPage)
	s.router.GET("/health", s.handleHealthPage)
	s.router.GET("/cluster", s.handleClusterPage)
	s.router.GET("/service/:namespace/:name", s.handleServiceDetail)

	// Observability
	s.router.GET("/observability", s.handleObservability)
	s.router.GET("/api/otel/summary", s.handleOtelSummary)
	s.router.POST("/api/otel/refresh", s.handleOtelRefresh)

	// Topology
	s.router.GET("/topology", s.handleTopologyPage)
	s.router.GET("/api/topology/graph", s.handleTopologyGraph)
	s.router.POST("/api/topology/refresh", s.handleTopologyRefresh)
	s.router.DELETE("/api/topology/cleanup", s.handleTopologyCleanup)

	// API
	s.router.GET("/healthz", s.handleHealthz)
	s.router.GET("/api/prometheus/health", s.handlePrometheusHealth)
	s.router.GET("/api/analysis", s.handleAnalysisJSON)
	s.router.GET("/api/trend", s.handleTrend)
	s.router.GET("/api/cluster/nodes", s.handleClusterNodes)
	s.router.GET("/api/cluster/env", s.handleClusterEnv)
	s.router.GET("/api/cluster/scaling", s.handleNodeScaling)
	s.router.GET("/api/cost-attribution", s.handleCostAttribution)
	s.router.GET("/api/ingress-ranking", s.handleIngressRanking)
	s.router.GET("/api/slow-requests", s.handleSlowRequests)
	s.router.GET("/api/pod-stability", s.handlePodStability)
	s.router.GET("/api/forecast/:namespace/:name", s.handleForecast)
	s.router.GET("/api/service/:namespace/:name/recommendation", s.handleServiceRecommendation)
	s.router.GET("/api/service/:namespace/:name/timeseries", s.handleServiceTimeSeries)
	s.router.GET("/api/service/:namespace/:name/hpa", s.handleHPA)
	s.router.GET("/api/service/:namespace/:name/predict", s.handlePredict)
	s.router.POST("/api/refresh", s.handleRefresh)
	s.router.POST("/api/config/reload", s.handleConfigReload)
	s.router.GET("/api/export/csv", s.handleExportCSV)
	s.router.GET("/api/export/json", s.handleExportJSON)
}

// Start begins listening and blocks until the server stops.
func (s *Server) Start() error {
	return s.srv.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
