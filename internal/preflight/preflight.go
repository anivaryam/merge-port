package preflight

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sort"
	"time"

	"github.com/anivaryam/merge-port/internal/proxy"
)

type ListenResult struct {
	Addr      string
	Available bool
	Err       error
	Ephemeral bool
}

type UpstreamResult struct {
	Target    string
	Reachable bool
	Err       error
}

func CheckListenPort(port int) ListenResult {
	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return ListenResult{Addr: addr, Available: false, Err: err, Ephemeral: port == 0}
	}
	ln.Close()
	return ListenResult{Addr: addr, Available: true, Ephemeral: port == 0}
}

func ProbeUpstreams(ctx context.Context, routes []proxy.Route, timeout time.Duration) []UpstreamResult {
	seen := make(map[string]bool)
	var targets []string
	for _, route := range routes {
		if !seen[route.Target] {
			seen[route.Target] = true
			targets = append(targets, route.Target)
		}
	}
	sort.Strings(targets)
	results := make([]UpstreamResult, 0, len(targets))
	for _, target := range targets {
		addr, err := dialAddress(target)
		if err != nil {
			results = append(results, UpstreamResult{Target: target, Err: err})
			continue
		}
		dialer := &net.Dialer{Timeout: timeout}
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			results = append(results, UpstreamResult{Target: target, Err: err})
			continue
		}
		conn.Close()
		results = append(results, UpstreamResult{Target: target, Reachable: true})
	}
	return results
}

func dialAddress(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("target has no host: %s", raw)
	}
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		default:
			port = "80"
		}
	}
	return net.JoinHostPort(host, port), nil
}
