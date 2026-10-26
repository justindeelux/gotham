package store

import (
	"reflect"
	"testing"
)

func TestMergePendingChanges(t *testing.T) {
	cases := map[string]struct {
		before  []string
		changes []string
		want    []string
	}{
		"first change":        {nil, []string{"domains"}, []string{"domains"}},
		"adds without repeat": {[]string{"domains"}, []string{"environment"}, []string{"domains", "environment"}},
		"deduplicates":        {[]string{"storage"}, []string{"storage"}, []string{"storage"}},
		"ignores empty":       {[]string{"domains"}, []string{""}, []string{"domains"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := mergePendingChanges(tc.before, tc.changes)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("mergePendingChanges(%v, %v) = %v, want %v", tc.before, tc.changes, got, tc.want)
			}
		})
	}
	if !equalPendingChanges([]string{"a", "b"}, []string{"a", "b"}) {
		t.Error("equalPendingChanges rejects identical sets")
	}
	if equalPendingChanges([]string{"a"}, []string{"a", "b"}) {
		t.Error("equalPendingChanges accepts different sizes")
	}
	if equalPendingChanges([]string{"a"}, []string{"b"}) {
		t.Error("equalPendingChanges accepts different values")
	}
}
