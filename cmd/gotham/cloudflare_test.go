package main

import (
	"net"
	"testing"
)

func TestCloudflareProxyAddrs(t *testing.T) {
	addrs := []net.IPAddr{
		{IP: net.ParseIP("104.16.1.2")},
		{IP: net.ParseIP("172.67.207.228")},
		{IP: net.ParseIP("2606:4700::6810:1")},
		{IP: net.ParseIP("203.0.113.42")},
		{IP: net.ParseIP("8.8.8.8")},
	}
	got := cloudflareProxyAddrs(addrs)
	want := []string{"104.16.1.2", "172.67.207.228", "2606:4700::6810:1"}
	if len(got) != len(want) {
		t.Fatalf("cloudflareProxyAddrs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cloudflareProxyAddrs = %v, want %v", got, want)
		}
	}
}
