package connectionpolicy

import "testing"

func TestFromConfigUsesAskByDefaultAndRejectsInvalidValues(t *testing.T) {
	policy, err := FromConfig("")
	if err != nil {
		t.Fatalf("FromConfig(empty) error = %v", err)
	}
	if policy != PolicyAsk {
		t.Errorf("FromConfig(empty) = %q, want ask", policy)
	}
	for _, value := range []string{"ask", "auto", "deny"} {
		if _, err := Parse(value); err != nil {
			t.Errorf("Parse(%q) error = %v", value, err)
		}
	}
	if _, err := FromConfig("xxx"); err == nil || err.Error() != `connection default policy is invalid: "xxx"` {
		t.Errorf("FromConfig(invalid) error = %v, want clear invalid-policy error", err)
	}
}

func TestDecidePrioritizesPresenceAndExistingConnection(t *testing.T) {
	tests := []struct {
		name      string
		policy    Policy
		present   bool
		connected bool
		want      Action
	}{
		{name: "ask", policy: PolicyAsk, present: true, want: ActionAsk},
		{name: "auto", policy: PolicyAuto, present: true, want: ActionConnect},
		{name: "deny", policy: PolicyDeny, present: true, want: ActionDeny},
		{name: "not present", policy: PolicyAuto, want: ActionNone},
		{name: "already connected", policy: PolicyAuto, present: true, connected: true, want: ActionNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Decide(tt.policy, tt.present, tt.connected); got != tt.want {
				t.Errorf("Decide(%q, %t, %t) = %q, want %q", tt.policy, tt.present, tt.connected, got, tt.want)
			}
		})
	}
}
