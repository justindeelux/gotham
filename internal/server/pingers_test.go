package server

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

// TestRedisPingRespectsTimeout points the pinger at a TCP listener that accepts
// connections but never answers, then requires the ping to return within the
// health budget instead of hanging on the socket timeout.
func TestRedisPingRespectsTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	// Hold every accepted connection open so the server looks hung rather
	// than closed.
	var (
		mu    sync.Mutex
		conns []net.Conn
	)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range conns {
			_ = conn.Close()
		}
	})

	p := newRedisPinger(ln.Addr().String())
	defer func() { _ = p.Close() }()

	start := time.Now()
	err = p.Ping(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Ping = nil, want a timeout error against a stalled server")
	}
	if elapsed > 4*time.Second {
		t.Fatalf("Ping took %v, want ~%v (context deadline not respected)", elapsed, pingTimeout)
	}
}
