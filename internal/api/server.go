// @sentinel-project: sentinel
// @sentinel-path: internal/api/server.go
// Sentinel HTTP API server on 127.0.0.1:8087 (ADR-017).
package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Harshmaury/Sentinel/internal/api/handler"
	"github.com/Harshmaury/Sentinel/internal/collector"
	"github.com/Harshmaury/Sentinel/internal/insight"
)

// Server is the Sentinel HTTP server.
type Server struct {
	http   *http.Server
	logger *log.Logger
}

// NewServer creates the Sentinel HTTP server and registers routes.
func NewServer(
	addr string,
	store *handler.StateStore,
	engine *insight.Engine,
	coll *collector.Collector,
	logger *log.Logger,
) *Server {
	if logger == nil {
		logger = log.Default()
	}

	insightsH := handler.NewInsightsHandler(store, engine, coll)
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health",                handleHealth)
	mux.HandleFunc("GET /insights/system",        insightsH.System)
	mux.HandleFunc("GET /insights/incidents",     insightsH.Incidents)
	mux.HandleFunc("GET /insights/deploy-risk",   insightsH.DeployRisk)

	return &Server{
		http: &http.Server{
			Addr:         addr,
			Handler:      mux,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		logger: logger,
	}
}

// Run starts the server and blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.logger.Printf("Sentinel API listening on %s", s.http.Addr)
		if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("sentinel http: %w", err)
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.logger.Println("Sentinel API shutting down...")
	return s.http.Shutdown(shutdownCtx)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"ok":true,"status":"healthy","service":"sentinel"}`)) //nolint:errcheck
}
