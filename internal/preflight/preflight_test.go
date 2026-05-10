package preflight

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/anivaryam/merge-port/internal/proxy"
)

func TestCheckListenPortDetectsUnavailablePort(t *testing.T) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	result := CheckListenPort(port)
	if result.Available || result.Err == nil {
		t.Fatalf("CheckListenPort(%d) = %+v, want unavailable", port, result)
	}
}

func TestProbeUpstreamsUsesTCPAndDeduplicatesTargets(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			conn.Close()
		}
	}()
	target := "http://" + ln.Addr().String()

	results := ProbeUpstreams(context.Background(), []proxy.Route{{Prefix: "/api", Target: target}, {Prefix: "/auth", Target: target}}, 2*time.Second)
	if len(results) != 1 {
		t.Fatalf("ProbeUpstreams len = %d, want 1", len(results))
	}
	if !results[0].Reachable {
		t.Fatalf("ProbeUpstreams result = %+v, want reachable", results[0])
	}
}
