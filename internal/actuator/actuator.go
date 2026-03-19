// @sentinel-project: sentinel
// @sentinel-path: internal/actuator/actuator.go
// Actuator executes recovery actions on behalf of Sentinel (ADR-024).
// It is the ONLY component in the observer layer with write capability.
// Write authority is strictly limited to:
//   POST /services/:id/reset   (ADR-023)
//   POST /projects/:id/start   (ADR-005)
//
// Three safety constraints prevent runaway healing:
//   A. Cooldown: 5 minutes between resets for the same service
//   B. Max attempts: 3 per service per hour, then escalate
//   C. Verify: check recovery on next cycle before counting success
package actuator

import (
	"bytes"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Harshmaury/Sentinel/internal/insight"
	canon "github.com/Harshmaury/Canon/identity"
)

const (
	cooldownDuration = 5 * time.Minute
	maxAttemptsPerHour = 3
	attemptWindow      = 1 * time.Hour
)

// serviceRecord tracks cooldown and attempt history for one service.
type serviceRecord struct {
	lastReset time.Time
	attempts  []time.Time // timestamps of recent attempts
}

// Actuator executes bounded recovery actions and tracks constraints.
type Actuator struct {
	nexusAddr    string
	serviceToken string
	client       *http.Client
	log          *RecoveryLog
	mu           sync.Mutex
	records      map[string]*serviceRecord
}

// NewActuator creates an Actuator.
func NewActuator(nexusAddr, serviceToken string, log *RecoveryLog) *Actuator {
	return &Actuator{
		nexusAddr:    nexusAddr,
		serviceToken: serviceToken,
		client:       &http.Client{Timeout: 10 * time.Second},
		log:          log,
		records:      make(map[string]*serviceRecord),
	}
}

// React evaluates each insight and executes allowed recovery actions.
// Called once per collection cycle after Analyze().
func (a *Actuator) React(insights []*insight.Insight) {
	for _, ins := range insights {
		policy := Decide(ins.RuleID)
		if policy.Action != ActionResetAndRestart {
			continue // escalate-only or none — no action taken
		}
		for _, subject := range ins.Subjects {
			a.attemptRecovery(ins.RuleID, subject, policy)
		}
	}
}

// attemptRecovery applies constraints then executes reset+restart.
func (a *Actuator) attemptRecovery(ruleID, serviceID string, policy RulePolicy) {
	a.mu.Lock()
	rec := a.getRecord(serviceID)

	if time.Since(rec.lastReset) < cooldownDuration {
		a.mu.Unlock()
		return // constraint A: cooldown
	}
	if a.recentAttempts(rec) >= maxAttemptsPerHour {
		a.mu.Unlock()
		a.log.Append(&RecoveryEntry{
			RuleID:  ruleID,
			Target:  serviceID,
			Action:  ActionEscalate,
			Outcome: "skipped",
			Reason:  fmt.Sprintf("max attempts (%d/hr) reached — escalating", maxAttemptsPerHour),
			At:      time.Now().UTC(),
		})
		return // constraint B: max attempts
	}

	rec.lastReset = time.Now().UTC()
	rec.attempts = append(rec.attempts, time.Now().UTC())
	a.mu.Unlock()

	outcome, err := a.resetAndRestart(serviceID)
	entry := &RecoveryEntry{
		RuleID:  ruleID,
		Target:  serviceID,
		Action:  ActionResetAndRestart,
		Outcome: outcome,
		Reason:  policy.Reason,
		At:      time.Now().UTC(),
	}
	if err != nil {
		entry.Outcome = "failed"
	}
	a.log.Append(entry)
}

// resetAndRestart calls Nexus reset then project start for a service.
func (a *Actuator) resetAndRestart(serviceID string) (string, error) {
	if err := a.post("/services/" + serviceID + "/reset"); err != nil {
		return "failed", fmt.Errorf("reset %s: %w", serviceID, err)
	}
	// Derive project ID from service ID (service = "<project>-daemon")
	projectID := deriveProjectID(serviceID)
	if err := a.post("/projects/" + projectID + "/start"); err != nil {
		return "failed", fmt.Errorf("start %s: %w", projectID, err)
	}
	return "success", nil
}

// post sends an authenticated POST to Nexus with an empty body.
func (a *Actuator) post(path string) error {
	req, err := http.NewRequest(http.MethodPost,
		a.nexusAddr+path, bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(canon.ServiceTokenHeader, a.serviceToken)
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// VerifyRecovery checks if previously acted-on services have recovered.
// Called each cycle after React() with current running service IDs.
func (a *Actuator) VerifyRecovery(runningServiceIDs map[string]bool) {
	for id := range runningServiceIDs {
		a.log.MarkResolved(id)
	}
}

// getRecord returns or creates a serviceRecord for a service ID.
// Must be called with a.mu held.
func (a *Actuator) getRecord(serviceID string) *serviceRecord {
	if r, ok := a.records[serviceID]; ok {
		return r
	}
	r := &serviceRecord{}
	a.records[serviceID] = r
	return r
}

// recentAttempts counts attempts within the attempt window.
// Must be called with a.mu held.
func (a *Actuator) recentAttempts(rec *serviceRecord) int {
	cutoff := time.Now().UTC().Add(-attemptWindow)
	count := 0
	for _, t := range rec.attempts {
		if t.After(cutoff) {
			count++
		}
	}
	return count
}

// deriveProjectID converts "atlas-daemon" → "atlas".
// Convention: service IDs are "<project>-daemon".
func deriveProjectID(serviceID string) string {
	const suffix = "-daemon"
	if len(serviceID) > len(suffix) &&
		serviceID[len(serviceID)-len(suffix):] == suffix {
		return serviceID[:len(serviceID)-len(suffix)]
	}
	return serviceID
}
