// @sentinel-project: sentinel
// @sentinel-path: internal/actuator/policy.go
// RecoveryPolicy maps Sentinel rule IDs to recovery actions (ADR-024).
// Pure function — no side effects, no state, fully deterministic.
//
// Only S-001 and S-006 trigger auto-recovery actions.
// All other rules produce escalation-only outcomes.
// This is bounded automation: Sentinel cannot act on build failures,
// agent connectivity, or code-level problems.
package actuator

// Action constants — what the actuator may do.
const (
	ActionResetAndRestart = "reset+restart" // reset service then start project
	ActionEscalate        = "escalate"      // log and surface, no auto-action
	ActionNone            = "none"          // info-level, no action needed
)

// RulePolicy holds the recovery strategy for one Sentinel rule.
type RulePolicy struct {
	RuleID  string
	Action  string
	Reason  string // why this action was chosen
}

// DefaultPolicy is the authoritative rule → action mapping (ADR-024 §2).
var DefaultPolicy = map[string]RulePolicy{
	"S-001": {
		RuleID: "S-001",
		Action: ActionResetAndRestart,
		Reason: "crash cascade may be transient — reset and restart affected services",
	},
	"S-002": {
		RuleID: "S-002",
		Action: ActionEscalate,
		Reason: "deploy caused crash — do not restart blindly, require human review",
	},
	"S-003": {
		RuleID: "S-003",
		Action: ActionEscalate,
		Reason: "dependency risk requires configuration fix, not restart",
	},
	"S-004": {
		RuleID: "S-004",
		Action: ActionNone,
		Reason: "stale project is informational — not a failure requiring action",
	},
	"S-005": {
		RuleID: "S-005",
		Action: ActionEscalate,
		Reason: "denial rate requires project registration or nexus.yaml fix",
	},
	"S-006": {
		RuleID: "S-006",
		Action: ActionResetAndRestart,
		Reason: "service stuck in maintenance — most common transient failure, safe to reset",
	},
	"S-007": {
		RuleID: "S-007",
		Action: ActionEscalate,
		Reason: "build failure requires code fix — automation cannot repair broken builds",
	},
	"S-008": {
		RuleID: "S-008",
		Action: ActionEscalate,
		Reason: "agent offline — cannot restart engxa remotely from Sentinel",
	},
}

// Decide returns the recovery policy for a given rule ID.
// Returns ActionEscalate for unknown rules — safe default.
func Decide(ruleID string) RulePolicy {
	if p, ok := DefaultPolicy[ruleID]; ok {
		return p
	}
	return RulePolicy{
		RuleID: ruleID,
		Action: ActionEscalate,
		Reason: "unknown rule — escalating conservatively",
	}
}
