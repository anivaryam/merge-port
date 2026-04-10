package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

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
		silent      bool
		logFile     string
		detach      bool
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

  # Suppress request logs
  merge-port --client 5173 --server 3000 --silent

  # Write request logs to file
  merge-port --client 5173 --server 3000 --log-file /tmp/mp.log

  # Detach from terminal (daemonize)
  merge-port --client 5173 --server 3000 --detach`,
		RunE: func(cmd *cobra.Command, args []string) error {
			routes, err := buildRoutes(clientPort, serverPort, apiPrefixes, rawRoutes)
			if err != nil {
				return err
			}

			if detach {
				if runtime.GOOS == "windows" {
					return fmt.Errorf("--detach is not supported on Windows: background operation is not available")
				}
				effectiveLog := logFile
				if effectiveLog == "" {
					effectiveLog = fmt.Sprintf("%s/merge-port-%d.log", os.TempDir(), listenPort)
				}
				if err := detachProcess(os.Args[1:], effectiveLog); err != nil {
					return err
				}
				fmt.Printf("merge-port detached, logging to %s\n", effectiveLog)
				return nil
			}

			logger, err := proxy.NewLogger(silent, logFile)
			if err != nil {
				return err
			}
			defer logger.Close()

			p, err := proxy.NewProxy(listenPort, routes, logger)
			if err != nil {
				return err
			}

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
	rootCmd.Flags().BoolVar(&silent, "silent", false, "suppress request log output")
	rootCmd.Flags().StringVar(&logFile, "log-file", "", "write request logs to FILE instead of stdout")
	rootCmd.Flags().BoolVar(&detach, "detach", false, "daemonize: detach from terminal and run in background")

	// discover subcommand
	var discoverPort int
	discoverCmd := &cobra.Command{
		Use:   "discover",
		Short: "Detect API route prefixes from a running server",
		Long: `Queries a running server for its OpenAPI spec and prints the
detected route prefixes as merge-port flags and a proc-compose.yaml snippet.`,
		Example: `  merge-port discover --server 3000
  merge-port discover --server 5000`,
		RunE: func(cmd *cobra.Command, args []string) error {
			base := fmt.Sprintf("http://localhost:%d", discoverPort)
			prefixes, source, err := discoverPrefixes(base)
			if err != nil {
				return err
			}
			if len(prefixes) == 0 {
				fmt.Printf("No API prefixes found at %s.\nSpecify them manually with --api-prefix.\n", base)
				return nil
			}

			fmt.Printf("\nDetected from %s (%s):\n\n", base, source)
			for _, p := range prefixes {
				fmt.Printf("  --api-prefix %s\n", p)
			}
			fmt.Printf("\nproc-compose.yaml:\n\n  api_prefixes:\n")
			for _, p := range prefixes {
				fmt.Printf("    - %s\n", p)
			}
			fmt.Println()
			return nil
		},
	}
	discoverCmd.Flags().IntVar(&discoverPort, "server", 0, "server port to query")
	discoverCmd.MarkFlagRequired("server")

	rootCmd.AddCommand(discoverCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// detachProcess re-execs the binary without --detach, redirecting output to logPath.
func detachProcess(osArgs []string, logPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := stripFlags(osArgs, []string{"--detach"}, []string{})

	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("cannot open log file %s: %w", logPath, err)
	}

	cmd := exec.Command(exe, args...)
	cmd.Stdin = nil
	cmd.Stdout = lf
	cmd.Stderr = lf
	setDetachAttr(cmd)

	if err := cmd.Start(); err != nil {
		lf.Close()
		return fmt.Errorf("failed to start background process: %w", err)
	}
	cmd.Process.Release()
	lf.Close()
	return nil
}

// stripFlags removes boolean flags and value flags from args.
func stripFlags(args, boolFlags, valueFlags []string) []string {
	boolSet := make(map[string]bool)
	for _, f := range boolFlags {
		boolSet[f] = true
	}
	valSet := make(map[string]bool)
	for _, f := range valueFlags {
		valSet[f] = true
	}

	var out []string
	skip := false
	for _, a := range args {
		if skip {
			skip = false
			continue
		}
		if boolSet[a] {
			continue
		}
		if valSet[a] {
			skip = true
			continue
		}
		out = append(out, a)
	}
	return out
}

// ─── discover helpers ─────────────────────────────────────────────────────────

func discoverPrefixes(base string) ([]string, string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	endpoints := []string{
		"/openapi.json",
		"/swagger.json",
		"/api-docs/swagger.json",
		"/api/docs/swagger.json",
		"/api/openapi.json",
		"/api-docs",
	}
	for _, ep := range endpoints {
		prefixes, ok := tryOpenAPI(client, base+ep)
		if ok {
			return prefixes, ep, nil
		}
	}
	return nil, "", fmt.Errorf("server at %s did not return an OpenAPI spec — is it running?\nTried: %s",
		base, strings.Join(endpoints, ", "))
}

func tryOpenAPI(client *http.Client, rawURL string) ([]string, bool) {
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var spec struct {
		Paths map[string]interface{} `json:"paths"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&spec); err != nil || len(spec.Paths) == 0 {
		return nil, false
	}
	seen := make(map[string]bool)
	for path := range spec.Paths {
		prefix := topPrefix(path)
		if prefix != "/" && strings.HasPrefix(prefix, "/") {
			seen[prefix] = true
		}
	}
	if len(seen) == 0 {
		return nil, false
	}
	prefixes := make([]string, 0, len(seen))
	for p := range seen {
		prefixes = append(prefixes, p)
	}
	sort.Strings(prefixes)
	return prefixes, true
}

func topPrefix(path string) string {
	trimmed := strings.TrimPrefix(path, "/")
	parts := strings.SplitN(trimmed, "/", 2)
	if parts[0] == "" {
		return "/"
	}
	return "/" + parts[0]
}

// ─── route building ───────────────────────────────────────────────────────────

func buildRoutes(clientPort, serverPort int, apiPrefixes, rawRoutes []string) ([]proxy.Route, error) {
	routeMode := len(rawRoutes) > 0
	simpleMode := clientPort != 0 || serverPort != 0 || len(apiPrefixes) > 0

	if routeMode && simpleMode {
		return nil, fmt.Errorf("--route cannot be combined with --client, --server, or --api-prefix")
	}

	if routeMode {
		return parseRouteFlags(rawRoutes)
	}

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
