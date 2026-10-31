package netguard

import (
	"errors"
	"net"
	"testing"
)

func TestDeniedAlwaysBlocksReservedRanges(t *testing.T) {
	for _, raw := range []string{
		"0.0.0.0", "100.64.0.1", "169.254.169.254", "192.0.0.8", "198.18.0.1",
		"224.0.0.1", "240.0.0.1", "::", "fe80::1", "ff02::1", "64:ff9b::a00:1",
		"64:ff9b:1::a00:1", "2002:7f00:1::1", "::7f00:1", "fd00:ec2::254",
	} {
		for _, allowPrivate := range []bool{false, true} {
			if !Denied(net.ParseIP(raw), allowPrivate) {
				t.Errorf("Denied(%s, allowPrivate=%v) = false, want true", raw, allowPrivate)
			}
		}
	}
}

func TestDeniedPrivateRangesFollowThePolicy(t *testing.T) {
	for _, raw := range []string{
		"127.0.0.1", "10.0.0.5", "172.16.4.4", "192.168.1.10", "::1", "fd00::1",
		"::ffff:10.0.0.5", "::ffff:127.0.0.1",
	} {
		if !Denied(net.ParseIP(raw), false) {
			t.Errorf("Denied(%s, allowPrivate=false) = false, want true", raw)
		}
		if Denied(net.ParseIP(raw), true) {
			t.Errorf("Denied(%s, allowPrivate=true) = true, want false", raw)
		}
	}
}

func TestDeniedAllowsPublicAddresses(t *testing.T) {
	for _, raw := range []string{"93.184.216.34", "203.0.113.10", "2606:4700::1111"} {
		if Denied(net.ParseIP(raw), false) {
			t.Errorf("Denied(%s) = true, want false", raw)
		}
	}
}

func TestControlRefusesDeniedDialAddresses(t *testing.T) {
	for _, tc := range []struct {
		address string
		denied  bool
	}{
		{"127.0.0.1:80", true},
		{"[::1]:25", true},
		{"[fe80::1%eth0]:25", true},
		{"169.254.169.254:443", true},
		{"203.0.113.10:443", false},
	} {
		err := Control(false)("tcp", tc.address, nil)
		if tc.denied && !errors.Is(err, ErrDenied) {
			t.Errorf("Control(%q) = %v, want ErrDenied", tc.address, err)
		}
		if !tc.denied && err != nil {
			t.Errorf("Control(%q) = %v, want nil", tc.address, err)
		}
	}
	if err := Control(true)("tcp", "127.0.0.1:80", nil); err != nil {
		t.Errorf("Control(allowPrivate=true) = %v, want nil", err)
	}
}
