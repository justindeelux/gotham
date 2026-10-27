package notifications

import "testing"

func TestIncidentEventsMapToSubscribableKeys(t *testing.T) {
	tests := []struct {
		action string
		key    EventKey
	}{
		{"opened", EventIncidentOpened},
		{"severity_changed", EventIncidentSeverity},
		{"recovered", EventIncidentRecovered},
	}
	for _, test := range tests {
		event := Event{Kind: EventIncident, Action: test.action, Name: "shop-web", Severity: "critical"}
		if got := event.Key(); got != test.key {
			t.Errorf("%s key = %q, want %q", test.action, got, test.key)
		}
		if !test.key.Valid() {
			t.Errorf("%s is not subscribable", test.key)
		}
		if event.Summary() == "" {
			t.Errorf("%s has no rendered summary", test.action)
		}
	}
}

func TestRuntimeIncidentEventsUseExplicitKeys(t *testing.T) {
	failure := Event{Kind: EventIncident, Action: "opened", Name: "shop-web", Severity: "critical", KeyOverride: EventRuntimeFailure}
	if got := failure.Key(); got != EventRuntimeFailure {
		t.Fatalf("runtime failure key = %q", got)
	}
	recovery := Event{Kind: EventIncident, Action: "recovered", Name: "shop-web", KeyOverride: EventRuntimeRecovery}
	if got := recovery.Key(); got != EventRuntimeRecovery {
		t.Fatalf("runtime recovery key = %q", got)
	}
	if !EventRuntimeFailure.Valid() || !EventRuntimeRecovery.Valid() {
		t.Fatal("runtime events are not subscribable")
	}
	for _, key := range defaultEvents {
		if key == EventRuntimeFailure || key == EventRuntimeRecovery {
			t.Fatalf("runtime event %q broadened the default subscriptions", key)
		}
	}
	if failure.Summary() == "" {
		t.Fatal("runtime failure has no summary")
	}
}
