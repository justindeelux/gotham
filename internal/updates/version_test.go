package updates

import "testing"

// TestParseVersion covers the accepted tag shapes.
func TestParseVersion(t *testing.T) {
	cases := map[string]Version{
		"v1.2.3":        {1, 2, 3, ""},
		"1.2.3":         {1, 2, 3, ""},
		"v1.2":          {1, 2, 0, ""},
		"v2":            {2, 0, 0, ""},
		"v1.2.3-rc.1":   {1, 2, 3, "rc.1"},
		"v1.2.3+build7": {1, 2, 3, ""},
	}
	for raw, want := range cases {
		got, err := ParseVersion(raw)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", raw, err)
		}
		if got != want {
			t.Errorf("ParseVersion(%q) = %+v, want %+v", raw, got, want)
		}
	}
	for _, raw := range []string{"", "v", "v1.2.3.4", "v1.x.0", "v-a"} {
		if _, err := ParseVersion(raw); err == nil {
			t.Errorf("ParseVersion(%q) = nil error, want failure", raw)
		}
	}
}

// TestVersionStringAndPrerelease covers the render helpers.
func TestVersionStringAndPrerelease(t *testing.T) {
	release, err := ParseVersion("v1.2.3")
	if err != nil {
		t.Fatalf("ParseVersion: %v", err)
	}
	if release.IsPrerelease() || release.String() != "v1.2.3" {
		t.Errorf("release = %q prerelease=%v", release.String(), release.IsPrerelease())
	}
	pre, err := ParseVersion("1.2.3-rc.1")
	if err != nil {
		t.Fatalf("ParseVersion: %v", err)
	}
	if !pre.IsPrerelease() || pre.String() != "v1.2.3-rc.1" {
		t.Errorf("prerelease = %q prerelease=%v", pre.String(), pre.IsPrerelease())
	}
}

// TestVersionCompare covers ordering, including prerelease rules.
func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.0.0", "v1.0.0", 0},
		{"v1.0.1", "v1.0.0", 1},
		{"v1.0.0", "v1.0.1", -1},
		{"v1.10.0", "v1.9.0", 1},
		{"v2.0.0", "v1.99.99", 1},
		{"v1.0.0", "v1.0.0-rc.1", 1},
		{"v1.0.0-rc.1", "v1.0.0", -1},
		{"v1.0.0-rc.2", "v1.0.0-rc.1", 1},
		{"v1.0.0-rc.10", "v1.0.0-rc.9", 1},
		{"v1.0.0-alpha", "v1.0.0-beta", -1},
		{"v1.0.0-rc", "v1.0.0-rc.1", -1},
	}
	for _, tc := range cases {
		a, err := ParseVersion(tc.a)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", tc.a, err)
		}
		b, err := ParseVersion(tc.b)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", tc.b, err)
		}
		if got := a.Compare(b); got != tc.want {
			t.Errorf("%s.Compare(%s) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
