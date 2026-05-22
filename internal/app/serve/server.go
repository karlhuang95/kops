package serve

import (
	"context"
	"embed"
	"fmt"
	"html/template"
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
	router.Use(gin.Logger(), gin.Recovery())

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
	s.router.GET("/", s.handleDashboard)
	s.router.GET("/healthz", s.handleHealthz)
	s.router.GET("/api/analysis", s.handleAnalysisJSON)
	s.router.POST("/api/refresh", s.handleRefresh)
	s.router.GET("/api/export/csv", s.handleExportCSV)
	s.router.GET("/api/export/json", s.handleExportJSON)
	s.router.GET("/service/:namespace/:name", s.handleServiceDetail)
}

// Start begins listening and blocks until the server stops.
func (s *Server) Start() error {
	return s.srv.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
