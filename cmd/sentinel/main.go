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
	"fmt"
	"log"
	"os"
	"path/filepath"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Harshmaury/Sentinel/internal/actuator"
	"github.com/Harshmaury/Sentinel/internal/api"
	"github.com/Harshmaury/Sentinel/internal/api/handler"
	"github.com/Harshmaury/Sentinel/internal/ai"
	"github.com/Harshmaury/Sentinel/internal/collector"
	"github.com/Harshmaury/Sentinel/internal/config"
	"github.com/Harshmaury/Sentinel/internal/insight"
)

const sentinelVersion = "0.3.0"

func main() {
	logger := log.New(os.Stdout, "[sentinel] ", log.LstdFlags)
	logger.Printf("Sentinel v%s starting", sentinelVersion)
	if err := run(logger); err != nil {
		logger.Fatalf("fatal: %v", err)
	}
	logger.Println("Sentinel stopped cleanly")
}

func run(logger *log.Logger) error {
	// ── 1. CONFIG ────────────────────────────────────────────────────────────
	httpAddr     := config.EnvOrDefault("SENTINEL_HTTP_ADDR", config.DefaultHTTPAddr)
	atlasAddr    := config.EnvOrDefault("ATLAS_HTTP_ADDR", config.DefaultAtlasAddr)
	nexusAddr    := config.EnvOrDefault("NEXUS_HTTP_ADDR", config.DefaultNexusAddr)
	forgeAddr    := config.EnvOrDefault("FORGE_HTTP_ADDR", config.DefaultForgeAddr)
	guardianAddr := config.EnvOrDefault("GUARDIAN_HTTP_ADDR", config.DefaultGuardianAddr)
	serviceToken := config.EnvOrDefault("SENTINEL_SERVICE_TOKEN", "")
	if serviceToken == "" {
		logger.Println("WARNING: SENTINEL_SERVICE_TOKEN not set — upstream auth disabled")
	}
	apiKey := config.EnvOrDefault("ANTHROPIC_API_KEY", "")
	if apiKey == "" {
		logger.Println("INFO: ANTHROPIC_API_KEY not set — AI reasoning disabled, /insights/explain returns Phase 1 only")
	}

	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// ── 2. COLLECTOR ─────────────────────────────────────────────────────────
	coll := collector.NewCollector(atlasAddr, nexusAddr, forgeAddr, guardianAddr, serviceToken)

	// ── 3. ENGINE + STORE ─────────────────────────────────────────────────────
	engine     := insight.NewEngine()
	stateStore := handler.NewStateStore()
	reasoner   := ai.NewReasoner(apiKey) // Phase 2: nil-safe, disabled if no API key

	// ── 4. ACTUATOR (ADR-024: self-healing — bounded write authority) ─────────
	recovLog := actuator.NewRecoveryLogWithPath(recoveryLogPath(logger), logger)
	act      := actuator.NewActuator(nexusAddr, serviceToken, recovLog)

	// ── 5. INITIAL ANALYSIS ──────────────────────────────────────────────────
	analyze(ctx, coll, engine, stateStore, act, logger)
	logger.Printf("✓ Sentinel ready — http=%s atlas=%s nexus=%s forge=%s guardian=%s",
		httpAddr, atlasAddr, nexusAddr, forgeAddr, guardianAddr)

	// ── 6. HTTP SERVER ───────────────────────────────────────────────────────
	srv := api.NewServer(httpAddr, stateStore, engine, coll, reasoner, recovLog, logger)

	var wg sync.WaitGroup
	errCh := make(chan error, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := srv.Run(ctx); err != nil && ctx.Err() == nil {
			errCh <- fmt.Errorf("api server: %w", err)
		}
	}()

	// ── 6. POLLING LOOP ───────────────────────────────────────────────────────
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				analyze(ctx, coll, engine, stateStore, act, logger)
			}
		}
	}()

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
	_ = recovLog.Close()
	return nil
}

// recoveryLogPath returns the path for the sentinel recovery log.
func recoveryLogPath(logger *log.Logger) string {
	home, err := os.UserHomeDir()
	if err != nil {
		logger.Printf("WARNING: cannot resolve home dir for recovery log: %v", err)
		return ""
	}
	dir := filepath.Join(home, ".nexus")
	if err := os.MkdirAll(dir, 0755); err != nil {
		logger.Printf("WARNING: cannot create .nexus dir: %v", err)
		return ""
	}
	return filepath.Join(dir, "recovery.log")
}

// analyze runs one collection + analysis + recovery cycle (ADR-024).
func analyze(
	ctx context.Context,
	coll *collector.Collector,
	engine *insight.Engine,
	store *handler.StateStore,
	act *actuator.Actuator,
	logger *log.Logger,
) {
	state  := coll.Collect(ctx)
	report := engine.Analyze(state)

	// ADR-024: verify previous recoveries then react to current insights.
	running := runningServiceIDs(state)
	act.VerifyRecovery(running)
	act.React(report.Insights)

	store.Set(state, report)
	logger.Printf("analyzed — health=%s insights=%d (%s)",
		report.Health, len(report.Insights), report.Summary)
}

// runningServiceIDs returns a set of service IDs currently actual=running.
func runningServiceIDs(state *collector.PlatformState) map[string]bool {
	out := make(map[string]bool, len(state.Services))
	for _, svc := range state.Services {
		if svc.ActualState == "running" {
			out[svc.ID] = true
		}
	}
	return out
}
