// @sentinel-project: sentinel
// @sentinel-path: internal/api/handler/explain.go
// ExplainHandler handles GET /insights/explain (ADR-018).
// Calls the AI reasoning layer on the current Phase 1 report and
// returns narrative reasoning alongside structured insights.
// Degrades gracefully if AI is unavailable.
package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/Harshmaury/Sentinel/internal/ai"
	"github.com/Harshmaury/Sentinel/internal/insight"
)

// ExplainResponse is the full response from GET /insights/explain.
type ExplainResponse struct {
	Health             string            `json:"health"`
	AIReasoning        string            `json:"ai_reasoning"`
	AIAvailable        bool              `json:"ai_available"`
	StructuredInsights []*insight.Insight `json:"structured_insights"`
	CollectedAt        time.Time         `json:"collected_at"`
}

// ExplainHandler handles GET /insights/explain.
type ExplainHandler struct {
	store    *StateStore
	reasoner *ai.Reasoner
	logger   *log.Logger
}

// NewExplainHandler creates an ExplainHandler.
func NewExplainHandler(store *StateStore, reasoner *ai.Reasoner, logger *log.Logger) *ExplainHandler {
	if logger == nil {
		logger = log.Default()
	}
	return &ExplainHandler{store: store, reasoner: reasoner, logger: logger}
}

// Explain handles GET /insights/explain.
//
// Concurrency safety: report is retrieved from StateStore under RLock and its
// fields are snapshotted into local variables BEFORE the Anthropic call begins.
// This ensures the 25-second AI call works on an immutable point-in-time
// snapshot even if the 30-second polling loop calls StateStore.Set() during
// the Anthropic round-trip. (FIX-001b)
func (h *ExplainHandler) Explain(w http.ResponseWriter, r *http.Request) {
	_, report := h.store.Get()

	// Snapshot all fields we will use in the response BEFORE the AI call.
	// After this point we never touch the report pointer again.
	var (
		snapHealth      string
		snapInsights    []*insight.Insight
		snapCollectedAt time.Time
	)

	if report == nil {
		snapHealth      = insight.HealthHealthy
		snapInsights    = []*insight.Insight{}
		snapCollectedAt = time.Now().UTC()
	} else {
		snapHealth      = report.Health
		snapInsights    = report.Insights
		snapCollectedAt = report.CollectedAt
	}

	// Build a stable SystemReport from the snapshot for the Reasoner.
	// The Reasoner receives a value we own — not a pointer into StateStore.
	stable := &insight.SystemReport{
		Health:      snapHealth,
		Summary:     "",   // Reasoner does not use Summary
		Insights:    snapInsights,
		CollectedAt: snapCollectedAt,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	reasoning, aiAvailable, err := h.reasoner.ExplainReport(ctx, stable)
	if err != nil {
		h.logger.Printf("AI reasoning failed: %v — returning Phase 1 only", err)
		aiAvailable = false
		reasoning   = ""
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"ok": true,
		"data": ExplainResponse{
			Health:             snapHealth,
			AIReasoning:        reasoning,
			AIAvailable:        aiAvailable,
			StructuredInsights: snapInsights,
			CollectedAt:        snapCollectedAt,
		},
	})
}
