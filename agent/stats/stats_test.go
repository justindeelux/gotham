package stats

import (
	"math"
	"testing"
)

func TestSample(t *testing.T) {
	sampler := New()

	first, err := sampler.Sample()
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if first.TotalMem == 0 {
		t.Error("TotalMem = 0; want a positive total")
	}
	if first.TotalDisk == 0 {
		t.Error("TotalDisk = 0; want a positive total")
	}
	assertFraction(t, "mem", first.Mem)
	assertFraction(t, "disk", first.Disk)
	assertFraction(t, "cpu", first.CPU)

	second, err := sampler.Sample()
	if err != nil {
		t.Fatalf("second Sample: %v", err)
	}
	assertFraction(t, "cpu", second.CPU)
	if second.TotalMem != first.TotalMem {
		t.Errorf("TotalMem changed between samples: %d != %d", second.TotalMem, first.TotalMem)
	}
}

func TestHelpers(t *testing.T) {
	if TotalMemory() == 0 {
		t.Error("TotalMemory() = 0; want positive")
	}
	if TotalDisk("/") == 0 {
		t.Error("TotalDisk(/) = 0; want positive")
	}
	// OSVersion is best-effort; it must not panic.
	_ = OSVersion()
}

func TestClampFraction(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		want float64
	}{
		{"negative", -0.5, 0},
		{"zero", 0, 0},
		{"mid", 0.42, 0.42},
		{"one", 1, 1},
		{"above", 1.5, 1},
		{"NaN", math.NaN(), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clampFraction(tt.in); got != tt.want {
				t.Errorf("clampFraction(%v) = %v; want %v", tt.in, got, tt.want)
			}
		})
	}
}

func assertFraction(t *testing.T, name string, value float64) {
	t.Helper()
	if math.IsNaN(value) || value < 0 || value > 1 {
		t.Errorf("%s = %v; want a fraction in 0..1", name, value)
	}
}
