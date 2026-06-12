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
)

//go:embed templates/*
var templateFS embed.FS

// Server wraps the Gin router and HTTP server.
type Server struct {
	router   *gin.Engine
	srv      *http.Server
	cfg      *platformconfig.GlobalConfig
	cacheDir string
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

	// API
	s.router.GET("/healthz", s.handleHealthz)
	s.router.GET("/api/prometheus/health", s.handlePrometheusHealth)
	s.router.GET("/api/analysis", s.handleAnalysisJSON)
	s.router.GET("/api/trend", s.handleTrend)
	s.router.GET("/api/cluster/nodes", s.handleClusterNodes)
	s.router.GET("/api/cluster/scaling", s.handleNodeScaling)
	s.router.GET("/api/cost-attribution", s.handleCostAttribution)
	s.router.GET("/api/forecast/:namespace/:name", s.handleForecast)
	s.router.GET("/api/service/:namespace/:name/recommendation", s.handleServiceRecommendation)
	s.router.GET("/api/service/:namespace/:name/timeseries", s.handleServiceTimeSeries)
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
