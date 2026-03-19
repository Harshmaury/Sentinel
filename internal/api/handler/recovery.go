// @sentinel-project: sentinel
// @sentinel-path: internal/api/handler/recovery.go
// RecoveryHandler serves GET /insights/recovery-log (ADR-024).
// Returns the history of all recovery actions taken by Sentinel's actuator.
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/Harshmaury/Sentinel/internal/actuator"
)

// RecoveryHandler handles GET /insights/recovery-log.
type RecoveryHandler struct {
	log *actuator.RecoveryLog
}

// NewRecoveryHandler creates a RecoveryHandler.
func NewRecoveryHandler(log *actuator.RecoveryLog) *RecoveryHandler {
	return &RecoveryHandler{log: log}
}

// Log handles GET /insights/recovery-log.
// Returns the last 200 recovery actions, newest first.
func (h *RecoveryHandler) Log(w http.ResponseWriter, r *http.Request) {
	entries := h.log.All()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(struct {
		OK   bool `json:"ok"`
		Data struct {
			Actions []*actuator.RecoveryEntry `json:"actions"`
			Total   int                       `json:"total"`
		} `json:"data"`
	}{
		OK: true,
		Data: struct {
			Actions []*actuator.RecoveryEntry `json:"actions"`
			Total   int                       `json:"total"`
		}{
			Actions: entries,
			Total:   len(entries),
		},
	}) //nolint:errcheck
}
