// @sentinel-project: sentinel
// @sentinel-path: internal/actuator/log.go
// Phase 3: RecoveryLog now persists to disk as append-only JSON lines.
// Every Append() writes one JSON line to the log file. The in-memory
// ring buffer (last 200 entries) is retained for fast API reads.
// The on-disk log is never truncated — it is the permanent audit trail.
//
// Log path: ~/.nexus/recovery.log (set via NewRecoveryLogWithPath).
// If path is empty (NewRecoveryLog), writes in-memory only — backward compatible.
package actuator

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"
)

const maxLogEntries = 200

// RecoveryEntry records one recovery action taken by Sentinel.
type RecoveryEntry struct {
	RuleID   string    `json:"rule_id"`
	Target   string    `json:"target"`
	Action   string    `json:"action"`
	Outcome  string    `json:"outcome"`
	Reason   string    `json:"reason"`
	At       time.Time `json:"at"`
	Resolved bool      `json:"resolved"`
}

// RecoveryLog stores the history of all recovery actions.
// Thread-safe. In-memory ring buffer + optional disk persistence.
type RecoveryLog struct {
	mu      sync.RWMutex
	entries []*RecoveryEntry
	file    *os.File    // nil = memory-only mode
	logger  *log.Logger // nil-safe
}

// NewRecoveryLog creates a memory-only RecoveryLog (backward compatible).
func NewRecoveryLog() *RecoveryLog { return &RecoveryLog{} }

// NewRecoveryLogWithPath creates a RecoveryLog that also writes to logPath.
// Each entry is appended as a single JSON line. Created if absent.
// Falls back to memory-only with a WARNING if the file cannot be opened.
func NewRecoveryLogWithPath(logPath string, logger *log.Logger) *RecoveryLog {
	l := &RecoveryLog{logger: logger}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		if logger != nil {
			logger.Printf("WARNING: recovery log unavailable (%s): %v — memory-only", logPath, err)
		}
		return l
	}
	l.file = f
	return l
}

// Append adds an entry to the ring buffer and writes it to disk if enabled.
func (l *RecoveryLog) Append(e *RecoveryEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.entries) >= maxLogEntries {
		l.entries = l.entries[1:]
	}
	l.entries = append(l.entries, e)
	l.writeToDisk(e)
}

// writeToDisk writes one entry as a JSON line. Must be called with mu held.
func (l *RecoveryLog) writeToDisk(e *RecoveryEntry) {
	if l.file == nil {
		return
	}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	data = append(data, '\n')
	if _, err := l.file.Write(data); err != nil {
		if l.logger != nil {
			l.logger.Printf("WARNING: recovery log write: %v", err)
		}
	}
}

// All returns a copy of all in-memory entries, newest first.
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

// MarkResolved sets resolved=true on unresolved entries for a target.
func (l *RecoveryLog) MarkResolved(target string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.entries {
		if e.Target == target && !e.Resolved {
			e.Resolved = true
		}
	}
}

// Close flushes and closes the underlying log file if open.
func (l *RecoveryLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}
