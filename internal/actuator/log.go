// @sentinel-project: sentinel
// @sentinel-path: internal/actuator/log.go
// RecoveryLog is an in-memory append-only log of every action Sentinel takes.
// Bounded to the last 200 entries. Thread-safe. Exposed via GET /insights/recovery-log.
package actuator

import (
	"sync"
	"time"
)

const maxLogEntries = 200

// RecoveryEntry records one recovery action taken by Sentinel.
type RecoveryEntry struct {
	RuleID   string    `json:"rule_id"`
	Target   string    `json:"target"`   // service or project ID
	Action   string    `json:"action"`   // ActionResetAndRestart | ActionEscalate
	Outcome  string    `json:"outcome"`  // "success" | "failed" | "skipped"
	Reason   string    `json:"reason"`   // why this action was chosen
	At       time.Time `json:"at"`
	Resolved bool      `json:"resolved"` // updated on next cycle if service recovers
}

// RecoveryLog stores the history of all recovery actions.
type RecoveryLog struct {
	mu      sync.RWMutex
	entries []*RecoveryEntry
}

// NewRecoveryLog creates an empty RecoveryLog.
func NewRecoveryLog() *RecoveryLog { return &RecoveryLog{} }

// Append adds a new entry, evicting the oldest if at capacity.
func (l *RecoveryLog) Append(e *RecoveryEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.entries) >= maxLogEntries {
		l.entries = l.entries[1:]
	}
	l.entries = append(l.entries, e)
}

// All returns a copy of all entries, newest first.
func (l *RecoveryLog) All() []*RecoveryEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]*RecoveryEntry, len(l.entries))
	for i, e := range l.entries {
		cp := *e
		out[len(l.entries)-1-i] = &cp
	}
	return out
}

// MarkResolved sets resolved=true on any unresolved entries for a target.
func (l *RecoveryLog) MarkResolved(target string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.entries {
		if e.Target == target && !e.Resolved {
			e.Resolved = true
		}
	}
}
