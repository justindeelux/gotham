package stats

import (
	"math"
	"strings"
	"testing"
	"time"
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

// netDevFixture is a /proc/net/dev document with loopback, two Ethernet
// interfaces and the two header lines the kernel emits.
const netDevFixture = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 5000000    1000    0    0    0     0          0         0  5000000    1000    0    0    0     0       0          0
  eth0: 2048   20    0    0    0     0          0         0     1024   10    0    0    0     0       0          0
  eth1: 100    1    0    0    0     0          0         0      200    2    0    0    0     0       0          0
`

func TestParseNetDev(t *testing.T) {
	rx, tx, err := parseNetDev(strings.NewReader(netDevFixture))
	if err != nil {
		t.Fatalf("parseNetDev: %v", err)
	}
	// Loopback is excluded: it never leaves the host.
	if rx != 2148 {
		t.Errorf("rx = %d; want 2148", rx)
	}
	if tx != 1224 {
		t.Errorf("tx = %d; want 1224", tx)
	}

	// A malformed, truncated or alias-style line is skipped, not fatal: the
	// readable interfaces still contribute.
	mixed := netDevFixture +
		"  eth2: not-a-number\n" + // non-numeric counter
		"  eth3: 1 2 3\n" + // truncated counter row
		"  eth0:1: 999 999\n" // alias-style name (splits at the first colon)
	rx, tx, err = parseNetDev(strings.NewReader(mixed))
	if err != nil {
		t.Fatalf("parseNetDev(mixed): %v", err)
	}
	if rx != 2148 || tx != 1224 {
		t.Errorf("mixed rx/tx = %d/%d; want 2148/1224 from the parsable lines", rx, tx)
	}

	// Only a document with nothing parsable is an error.
	if _, _, err := parseNetDev(strings.NewReader("  eth2: not-a-number\n")); err == nil {
		t.Error("parseNetDev(all malformed) = nil error; want a no-interfaces error")
	}
	if _, _, err := parseNetDev(strings.NewReader("")); err == nil {
		t.Error("parseNetDev(empty) = nil error; want a no-interfaces error")
	}
}

// TestPseudoBlockDevice pins the /sys/block filter: a RAM-backed zram device
// would otherwise inflate the disk I/O rate.
func TestPseudoBlockDevice(t *testing.T) {
	tests := map[string]bool{
		"loop0":   true,
		"ram0":    true,
		"zram0":   true,
		"sda":     false,
		"vda":     false,
		"nvme0n1": false,
		"dm-0":    false,
	}
	for name, want := range tests {
		if got := pseudoBlockDevice(name); got != want {
			t.Errorf("pseudoBlockDevice(%q) = %v; want %v", name, got, want)
		}
	}
}

// diskStatsFixture is a /proc/diskstats document with a disk, its partition, a
// loop device and a second disk.
const diskStatsFixture = `   8       0 sda 100 0 2048 30 200 0 4096 40 0 0 0 0 0 0
   8       1 sda1 50 0 1024 15 100 0 2048 20 0 0 0 0 0 0
   7       0 loop0 10 0 100 0 10 0 100 0 0 0 0 0 0 0
   8      16 sdb 1 0 8 0 2 0 16 0 0 0 0 0 0 0
`

func TestParseDiskStats(t *testing.T) {
	// The caller passes only real whole devices: sda1 is a partition of sda and
	// loop0 is a pseudo device, so neither must contribute bytes.
	devices := map[string]struct{}{"sda": {}, "sdb": {}}
	read, write, err := parseDiskStats(strings.NewReader(diskStatsFixture), devices)
	if err != nil {
		t.Fatalf("parseDiskStats: %v", err)
	}
	const sector = 512
	wantRead := uint64((2048 + 8) * sector)
	wantWrite := uint64((4096 + 16) * sector)
	if read != wantRead {
		t.Errorf("read = %d; want %d", read, wantRead)
	}
	if write != wantWrite {
		t.Errorf("write = %d; want %d", write, wantWrite)
	}

	if _, _, err := parseDiskStats(strings.NewReader(diskStatsFixture), map[string]struct{}{"nvme0n1": {}}); err == nil {
		t.Error("parseDiskStats(no matching device) = nil error; want a no-devices error")
	}
}

func TestByteRate(t *testing.T) {
	tests := []struct {
		name     string
		current  uint64
		previous uint64
		elapsed  time.Duration
		want     float64
	}{
		{"one second", 2048, 1024, time.Second, 1024},
		{"half second", 2048, 1024, 500 * time.Millisecond, 2048},
		{"unchanged counter", 1024, 1024, time.Second, 0},
		{"counter reset", 512, 1024, time.Second, 0},
		{"zero elapsed", 2048, 1024, 0, 0},
		{"negative elapsed", 2048, 1024, -time.Second, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := byteRate(tt.current, tt.previous, tt.elapsed); got != tt.want {
				t.Errorf("byteRate(%d, %d, %s) = %v; want %v", tt.current, tt.previous, tt.elapsed, got, tt.want)
			}
		})
	}
}

func TestSampleIORatesStartAtZero(t *testing.T) {
	sampler := New()

	first, err := sampler.Sample()
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	// The first sample has no previous counters to compare against, so every
	// rate is zero on every platform (a platform that cannot read a counter
	// keeps reporting zero).
	if first.NetRxBps != 0 || first.NetTxBps != 0 || first.DiskReadBps != 0 || first.DiskWriteBps != 0 {
		t.Errorf("first sample I/O rates = net %v/%v disk %v/%v; want all zero",
			first.NetRxBps, first.NetTxBps, first.DiskReadBps, first.DiskWriteBps)
	}

	second, err := sampler.Sample()
	if err != nil {
		t.Fatalf("second Sample: %v", err)
	}
	for name, value := range map[string]float64{
		"net rx":     second.NetRxBps,
		"net tx":     second.NetTxBps,
		"disk read":  second.DiskReadBps,
		"disk write": second.DiskWriteBps,
	} {
		if math.IsNaN(value) || value < 0 {
			t.Errorf("%s rate = %v; want a finite, non-negative rate", name, value)
		}
	}
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
