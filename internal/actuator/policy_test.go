// @sentinel-project: sentinel
// @sentinel-path: internal/actuator/policy_test.go
// Tests for the Sentinel actuator policy — rule → action mapping (ADR-024).
// Pure function: input is ruleID string, output is RulePolicy.
// No mocks, no side effects.
package actuator

import "testing"

func TestDecide_KnownRules(t *testing.T) {
	tests := []struct {
		ruleID     string
		wantAction string
	}{
		// Only S-001 and S-006 auto-recover — everything else escalates or is info.
		{"S-001", ActionResetAndRestart},
		{"S-002", ActionEscalate},
		{"S-003", ActionEscalate},
		{"S-004", ActionNone},
		{"S-005", ActionEscalate},
		{"S-006", ActionResetAndRestart},
		{"S-007", ActionEscalate},
		{"S-008", ActionEscalate},
	}

	for _, tt := range tests {
		t.Run(tt.ruleID, func(t *testing.T) {
			policy := Decide(tt.ruleID)
			if policy.Action != tt.wantAction {
				t.Errorf("Decide(%q).Action = %q, want %q", tt.ruleID, policy.Action, tt.wantAction)
			}
			if policy.RuleID != tt.ruleID {
				t.Errorf("Decide(%q).RuleID = %q, want %q", tt.ruleID, policy.RuleID, tt.ruleID)
			}
			if policy.Reason == "" {
				t.Errorf("Decide(%q).Reason is empty — every policy must explain why", tt.ruleID)
			}
		})
	}
}

func TestDecide_UnknownRule_EscalatesConservatively(t *testing.T) {
	unknown := []string{"S-099", "G-001", "", "UNKNOWN", "s-001"}
	for _, id := range unknown {
		t.Run(id, func(t *testing.T) {
			policy := Decide(id)
			if policy.Action != ActionEscalate {
				t.Errorf("Decide(%q) unknown rule should escalate, got %q", id, policy.Action)
			}
		})
	}
}

func TestDecide_AutoRecoverRulesAreMinimal(t *testing.T) {
	// Only two rules should ever trigger auto-recovery.
	// This test enforces the bounded automation contract from ADR-024.
	autoRecover := []string{}
	for id, p := range DefaultPolicy {
		if p.Action == ActionResetAndRestart {
			autoRecover = append(autoRecover, id)
		}
	}
	if len(autoRecover) != 2 {
		t.Errorf("expected exactly 2 auto-recover rules (S-001 and S-006), got %d: %v",
			len(autoRecover), autoRecover)
	}
}

func TestDecide_AllDefinedRulesHaveReasons(t *testing.T) {
	for id, p := range DefaultPolicy {
		if p.Reason == "" {
			t.Errorf("DefaultPolicy[%q].Reason is empty — policy decisions must be explained", id)
		}
		if p.RuleID != id {
			t.Errorf("DefaultPolicy[%q].RuleID = %q — key and RuleID must match", id, p.RuleID)
		}
	}
}
