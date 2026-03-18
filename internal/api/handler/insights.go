// @sentinel-project: sentinel
// @sentinel-path: internal/api/handler/insights.go
// InsightsHandler serves GET /insights/* endpoints (ADR-017).
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Harshmaury/Sentinel/internal/collector"
	"github.com/Harshmaury/Sentinel/internal/insight"
)

// StateStore holds the latest platform state and system report in memory.
type StateStore struct {
	mu     sync.RWMutex
	state  *collector.PlatformState
	report *insight.SystemReport
}

// NewStateStore creates an empty StateStore.
func NewStateStore() *StateStore { return &StateStore{} }

// Set updates the stored state and report atomically.
func (s *StateStore) Set(state *collector.PlatformState, report *insight.SystemReport) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
	s.report = report
}

// Get returns the latest state and report.
func (s *StateStore) Get() (*collector.PlatformState, *insight.SystemReport) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state, s.report
}

// InsightsHandler handles GET /insights/* routes.
type InsightsHandler struct {
	store      *StateStore
	engine     *insight.Engine
	coll       *collector.Collector
}

// NewInsightsHandler creates an InsightsHandler.
func NewInsightsHandler(store *StateStore, engine *insight.Engine, coll *collector.Collector) *InsightsHandler {
	return &InsightsHandler{store: store, engine: engine, coll: coll}
}

// System handles GET /insights/system — full platform health report.
func (h *InsightsHandler) System(w http.ResponseWriter, r *http.Request) {
	_, report := h.store.Get()
	if report == nil {
		report = &insight.SystemReport{
			Health:      insight.HealthHealthy,
			Summary:     "no data collected yet",
			Insights:    []*insight.Insight{},
			CollectedAt: time.Now().UTC(),
		}
	}
	respondOK(w, report)
}

// Incidents handles GET /insights/incidents — clusters error-level insights.
func (h *InsightsHandler) Incidents(w http.ResponseWriter, r *http.Request) {
	_, report := h.store.Get()
	var incidents []*insight.Incident

	if report != nil {
		seen := map[string]*insight.Incident{}
		for _, ins := range report.Insights {
			if ins.Severity != insight.SeverityError {
				continue
			}
			inc, ok := seen[ins.RuleID]
			if !ok {
				inc = &insight.Incident{
					ID:       ins.RuleID,
					Severity: ins.Severity,
					Title:    ins.Title,
					Evidence: []*insight.Insight{},
				}
				seen[ins.RuleID] = inc
				incidents = append(incidents, inc)
			}
			inc.Evidence = append(inc.Evidence, ins)
		}
	}

	if incidents == nil {
		incidents = []*insight.Incident{}
	}
	respondOK(w, map[string]any{"incidents": incidents})
}

// DeployRisk handles GET /insights/deploy-risk.
// Performs a live collection then runs deploy risk analysis.
func (h *InsightsHandler) DeployRisk(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	traceID := deployRiskTraceID()
	state   := h.coll.Collect(ctx, traceID)
	risk    := h.engine.DeployRisk(state)
	respondOK(w, risk)
}

// deployRiskTraceID generates a trace ID for on-demand deploy-risk collections.
func deployRiskTraceID() string {
	return fmt.Sprintf("st-dr-%d", time.Now().UnixNano())
}

// ── RESPONSE HELPERS ──────────────────────────────────────────────────────────

type apiResponse struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

func respondOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(apiResponse{OK: true, Data: data}) //nolint:errcheck
}
