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
	Health            string            `json:"health"`
	AIReasoning       string            `json:"ai_reasoning"`
	AIAvailable       bool              `json:"ai_available"`
	StructuredInsights []*insight.Insight `json:"structured_insights"`
	CollectedAt       time.Time         `json:"collected_at"`
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
// Fetches the current Phase 1 report, calls the AI reasoner, returns combined output.
func (h *ExplainHandler) Explain(w http.ResponseWriter, r *http.Request) {
	_, report := h.store.Get()
	if report == nil {
		report = &insight.SystemReport{
			Health:      insight.HealthHealthy,
			Summary:     "no data collected yet",
			Insights:    []*insight.Insight{},
			CollectedAt: time.Now().UTC(),
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	reasoning, aiAvailable, err := h.reasoner.ExplainReport(ctx, report)
	if err != nil {
		h.logger.Printf("AI reasoning failed: %v — returning Phase 1 only", err)
		aiAvailable = false
		reasoning = ""
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"ok": true,
		"data": ExplainResponse{
			Health:             report.Health,
			AIReasoning:        reasoning,
			AIAvailable:        aiAvailable,
			StructuredInsights: report.Insights,
			CollectedAt:        report.CollectedAt,
		},
	})
}
