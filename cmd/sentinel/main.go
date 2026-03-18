// @sentinel-project: sentinel
// @sentinel-path: cmd/sentinel/main.go
// sentinel is the platform analytical insights daemon (ADR-017).
//
// Startup sequence:
//  1. Config
//  2. Collector (all platform services)
//  3. Insight engine + state store
//  4. Initial analysis pass
//  5. HTTP server (:8087)
//  6. Polling loop (every 30s)
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Harshmaury/Sentinel/internal/api"
	"github.com/Harshmaury/Sentinel/internal/api/handler"
	"github.com/Harshmaury/Sentinel/internal/ai"
	"github.com/Harshmaury/Sentinel/internal/collector"
	"github.com/Harshmaury/Sentinel/internal/config"
	"github.com/Harshmaury/Sentinel/internal/insight"
)

const sentinelVersion = "0.1.0"

func main() {
	logger := log.New(os.Stdout, "[sentinel] ", log.LstdFlags)
	logger.Printf("Sentinel v%s starting", sentinelVersion)
	if err := run(logger); err != nil {
		logger.Fatalf("fatal: %v", err)
	}
	logger.Println("Sentinel stopped cleanly")
}

// sentinelConfig holds resolved runtime configuration.
type sentinelConfig struct {
	httpAddr     string
	atlasAddr    string
	nexusAddr    string
	forgeAddr    string
	guardianAddr string
	serviceToken string
	apiKey       string
}

// loadConfig reads environment variables and logs warnings.
func loadConfig(logger *log.Logger) sentinelConfig {
	cfg := sentinelConfig{
		httpAddr:     config.EnvOrDefault("SENTINEL_HTTP_ADDR", config.DefaultHTTPAddr),
		atlasAddr:    config.EnvOrDefault("ATLAS_HTTP_ADDR", config.DefaultAtlasAddr),
		nexusAddr:    config.EnvOrDefault("NEXUS_HTTP_ADDR", config.DefaultNexusAddr),
		forgeAddr:    config.EnvOrDefault("FORGE_HTTP_ADDR", config.DefaultForgeAddr),
		guardianAddr: config.EnvOrDefault("GUARDIAN_HTTP_ADDR", config.DefaultGuardianAddr),
		serviceToken: config.EnvOrDefault("SENTINEL_SERVICE_TOKEN", ""),
		apiKey:       config.EnvOrDefault("ANTHROPIC_API_KEY", ""),
	}
	if cfg.serviceToken == "" {
		logger.Println("WARNING: SENTINEL_SERVICE_TOKEN not set — upstream auth disabled")
	}
	if cfg.apiKey == "" {
		logger.Println("INFO: ANTHROPIC_API_KEY not set — AI reasoning disabled")
	}
	return cfg
}

func run(logger *log.Logger) error {
	cfg := loadConfig(logger)

	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	coll      := collector.NewCollector(cfg.atlasAddr, cfg.nexusAddr, cfg.forgeAddr, cfg.guardianAddr, cfg.serviceToken)
	engine    := insight.NewEngine()
	stateStore := handler.NewStateStore()
	reasoner  := ai.NewReasoner(cfg.apiKey)

	analyze(ctx, coll, engine, stateStore, logger)
	logger.Printf("✓ Sentinel ready — http=%s atlas=%s nexus=%s forge=%s guardian=%s",
		cfg.httpAddr, cfg.atlasAddr, cfg.nexusAddr, cfg.forgeAddr, cfg.guardianAddr)

	return serveAndWait(ctx, cancel, sigCh, cfg.httpAddr,
		stateStore, engine, coll, reasoner, logger)
}

// serveAndWait starts the HTTP server and polling loop, blocks until shutdown.
func serveAndWait(
	ctx context.Context,
	cancel context.CancelFunc,
	sigCh <-chan os.Signal,
	httpAddr string,
	stateStore *handler.StateStore,
	engine *insight.Engine,
	coll *collector.Collector,
	reasoner *ai.Reasoner,
	logger *log.Logger,
) error {
	srv  := api.NewServer(httpAddr, stateStore, engine, coll, reasoner, logger)
	var wg sync.WaitGroup
	errCh := make(chan error, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := srv.Run(ctx); err != nil && ctx.Err() == nil {
			errCh <- fmt.Errorf("api server: %w", err)
		}
	}()

	wg.Add(1)
	go startPollingLoop(ctx, &wg, coll, engine, stateStore, logger)

	select {
	case sig := <-sigCh:
		logger.Printf("received %s — shutting down", sig)
	case err := <-errCh:
		logger.Printf("component error: %v — shutting down", err)
	}

	cancel()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	<-done
	return nil
}

// startPollingLoop runs the 30-second analysis cycle until ctx is cancelled.
func startPollingLoop(
	ctx context.Context,
	wg *sync.WaitGroup,
	coll *collector.Collector,
	engine *insight.Engine,
	store *handler.StateStore,
	logger *log.Logger,
) {
	defer wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			analyze(ctx, coll, engine, store, logger)
		}
	}
}

// analyze runs one collection + analysis cycle.
// Generates a fresh st-<hex> trace ID per cycle for X-Trace-ID propagation.
func analyze(
	ctx context.Context,
	coll *collector.Collector,
	engine *insight.Engine,
	store *handler.StateStore,
	logger *log.Logger,
) {
	traceID := newTraceID()
	state   := coll.Collect(ctx, traceID)
	report  := engine.Analyze(state)
	store.Set(state, report)
	logger.Printf("analyzed trace=%s — health=%s insights=%d (%s)",
		traceID, report.Health, len(report.Insights), report.Summary)
}

// newTraceID generates a random trace ID for collection cycles.
func newTraceID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("st-%d", time.Now().UnixNano())
	}
	return "st-" + hex.EncodeToString(b)
}
