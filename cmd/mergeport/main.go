package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/anivaryam/merge-port/internal/proxy"
	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	var (
		clientPort  int
		serverPort  int
		listenPort  int
		apiPrefixes []string
		rawRoutes   []string
	)

	rootCmd := &cobra.Command{
		Use:     "merge-port",
		Short:   "Merge client and server ports into one",
		Version: version,
		Long: `A local reverse proxy that merges your client and server
into a single port for easy tunneling.

Simple mode (client + server):
  merge-port --client 3000 --server 3001
  merge-port --client 3000 --server 3001 --api-prefix /api --api-prefix /auth

Route mode (full control):
  merge-port --route /api=3001 --route /=3000
  merge-port --route /api=http://api.local:3001 --route /=3000`,
		Example: `  # Default: /api → server, everything else → client
  merge-port --client 3000 --server 3001

  # Multiple prefixes to the same server
  merge-port --client 3000 --server 3001 --api-prefix /api --api-prefix /auth --api-prefix /ws

  # Custom listen port
  merge-port --client 5173 --server 3001 --port 9000

  # Full custom routing (different backends)
  merge-port --route /api=3001 --route /auth=3002 --route /=3000`,
		RunE: func(cmd *cobra.Command, args []string) error {
			routes, err := buildRoutes(clientPort, serverPort, apiPrefixes, rawRoutes)
			if err != nil {
				return err
			}

			p := proxy.NewProxy(listenPort, routes)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sig := make(chan os.Signal, 1)
			signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sig
				cancel()
			}()

			return p.Run(ctx)
		},
	}

	rootCmd.Flags().IntVar(&clientPort, "client", 0, "client/frontend port (required in simple mode)")
	rootCmd.Flags().IntVar(&serverPort, "server", 0, "server/backend port (required in simple mode)")
	rootCmd.Flags().IntVar(&listenPort, "port", 8080, "port to listen on")
	rootCmd.Flags().StringArrayVar(&apiPrefixes, "api-prefix", nil, "path prefix routed to server (repeatable, default: /api)")
	rootCmd.Flags().StringArrayVar(&rawRoutes, "route", nil, "explicit route as prefix=target (repeatable, e.g. /api=3001)")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func buildRoutes(clientPort, serverPort int, apiPrefixes, rawRoutes []string) ([]proxy.Route, error) {
	routeMode := len(rawRoutes) > 0
	simpleMode := clientPort != 0 || serverPort != 0 || len(apiPrefixes) > 0

	if routeMode && simpleMode {
		return nil, fmt.Errorf("--route cannot be combined with --client, --server, or --api-prefix")
	}

	if routeMode {
		return parseRouteFlags(rawRoutes)
	}

	// Simple mode
	if clientPort == 0 {
		return nil, fmt.Errorf("--client port is required")
	}
	if serverPort == 0 {
		return nil, fmt.Errorf("--server port is required")
	}

	if len(apiPrefixes) == 0 {
		apiPrefixes = []string{"/api"}
	}

	serverTarget := fmt.Sprintf("http://localhost:%d", serverPort)
	clientTarget := fmt.Sprintf("http://localhost:%d", clientPort)

	var routes []proxy.Route
	for _, p := range apiPrefixes {
		if !strings.HasPrefix(p, "/") {
			return nil, fmt.Errorf("api prefix must start with /: %q", p)
		}
		if p == "/" {
			return nil, fmt.Errorf("api prefix cannot be / (use --route for full control)")
		}
		routes = append(routes, proxy.Route{Prefix: p, Target: serverTarget})
	}
	routes = append(routes, proxy.Route{Prefix: "/", Target: clientTarget})

	return routes, nil
}

func parseRouteFlags(rawRoutes []string) ([]proxy.Route, error) {
	var routes []proxy.Route
	for _, raw := range rawRoutes {
		idx := strings.Index(raw, "=")
		if idx < 0 {
			return nil, fmt.Errorf("invalid --route format %q: expected prefix=target (e.g. /api=3001)", raw)
		}
		prefix := raw[:idx]
		target := raw[idx+1:]

		if prefix == "" || !strings.HasPrefix(prefix, "/") {
			return nil, fmt.Errorf("route prefix must start with /: %q", prefix)
		}
		if target == "" {
			return nil, fmt.Errorf("route target is empty for prefix %q", prefix)
		}

		target = normalizeTarget(target)
		routes = append(routes, proxy.Route{Prefix: prefix, Target: target})
	}
	return routes, nil
}

func normalizeTarget(target string) string {
	if isAllDigits(target) {
		return fmt.Sprintf("http://localhost:%s", target)
	}
	if !strings.Contains(target, "://") {
		return "http://" + target
	}
	return target
}

func isAllDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
