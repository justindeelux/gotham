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
