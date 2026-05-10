package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/anivaryam/merge-port/internal/lifecycle"
	"github.com/anivaryam/merge-port/internal/proxy"
)

func executeCommand(args ...string) (string, string, error) {
	cmd := newRootCommand()
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestRootHelpMentionsDiscover(t *testing.T) {
	out, _, err := executeCommand("--help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	if !strings.Contains(out, "merge-port discover --server 3001") {
		t.Fatalf("help output missing discover example:\n%s", out)
	}
}

func TestRootMissingClientHasActionableError(t *testing.T) {
	_, _, err := executeCommand()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--client port is required") {
		t.Fatalf("error = %v", err)
	}
}

func TestDryRunDetachConflict_ReturnsUsageError(t *testing.T) {
	_, _, err := executeCommand("--dry-run", "--detach")
	if err == nil {
		t.Fatal("expected error for --dry-run --detach")
	}
	var uerr usageError
	if !errors.As(err, &uerr) {
		t.Fatalf("expected usageError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "--dry-run cannot be combined with --detach") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDryRunPrintsRoutesWithoutStarting(t *testing.T) {
	out, _, err := executeCommand("--client", "5173", "--server", "3001", "--dry-run")
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !strings.Contains(out, "merge-port dry run") || !strings.Contains(out, "/api -> http://localhost:3001") {
		t.Fatalf("unexpected dry-run output:\n%s", out)
	}
}

func TestDryRunRouteModeWithoutCatchAllWarnsAndPrintsDryRun(t *testing.T) {
	out, errOut, err := executeCommand("--route", "/api=3001", "--dry-run")
	if err != nil {
		t.Fatalf("dry-run route mode: %v", err)
	}
	if !strings.Contains(out, "merge-port dry run") || !strings.Contains(out, "/api -> http://localhost:3001") {
		t.Fatalf("unexpected dry-run output:\n%s", out)
	}
	if !strings.Contains(errOut, "Warning: no catch-all / route configured; unmatched paths will return 404.") {
		t.Fatalf("missing no-catch-all warning on stderr:\n%s", errOut)
	}
}

func TestDryRunPortZeroIsPreservedWhenExplicit(t *testing.T) {
	out, _, err := executeCommand("--client", "5173", "--server", "3001", "--dry-run", "--port", "0")
	if err != nil {
		t.Fatalf("dry-run port zero: %v", err)
	}
	if !strings.Contains(out, "Listen: :0") {
		t.Fatalf("expected explicit port 0 in dry-run output:\n%s", out)
	}
}

func TestDryRunUsesDefaultPortWhenAbsent(t *testing.T) {
	out, _, err := executeCommand("--client", "5173", "--server", "3001", "--dry-run")
	if err != nil {
		t.Fatalf("dry-run default port: %v", err)
	}
	if !strings.Contains(out, "Listen: :8080") {
		t.Fatalf("expected default port 8080 in dry-run output:\n%s", out)
	}
}

func TestDetachPortZeroReturnsUsageError(t *testing.T) {
	_, _, err := executeCommand("--client", "5173", "--server", "3001", "--port", "0", "--detach")
	if err == nil {
		t.Fatal("expected --port 0 --detach error")
	}
	var uerr usageError
	if !errors.As(err, &uerr) {
		t.Fatalf("expected usageError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "--detach cannot be used with --port 0") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateLoadsExplicitConfigAndUsesReachableUpstreams(t *testing.T) {
	client := listenForTest(t)
	server := listenForTest(t)
	listenPort := freePortForTest(t)
	path := filepath.Join(t.TempDir(), "merge-port.yaml")
	content := fmt.Sprintf("port: %d\nclient: %d\nserver: %d\n", listenPort, portOf(t, client), portOf(t, server))
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	out, _, err := executeCommand("validate", "--config", path)
	if err != nil {
		t.Fatalf("validate explicit config: %v\n%s", err, out)
	}
	if !strings.Contains(out, "merge-port validation passed") || !strings.Contains(out, fmt.Sprintf(":%d available", listenPort)) {
		t.Fatalf("unexpected validate output:\n%s", out)
	}
}

func TestValidateMissingExplicitConfigIsUsageError(t *testing.T) {
	_, _, err := executeCommand("validate", "--config", filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("expected missing config error")
	}
	var uerr usageError
	if !errors.As(err, &uerr) {
		t.Fatalf("expected usageError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "config file not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateLoadsImplicitCWDConfig(t *testing.T) {
	client := listenForTest(t)
	server := listenForTest(t)
	listenPort := freePortForTest(t)
	dir := t.TempDir()
	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("port: %d\nclient: %d\nserver: %d\n", listenPort, portOf(t, client), portOf(t, server))
	if err := os.WriteFile(".merge-port.yaml", []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	out, _, err := executeCommand("validate")
	if err != nil {
		t.Fatalf("validate implicit config: %v\n%s", err, out)
	}
	if !strings.Contains(out, "merge-port validation passed") || !strings.Contains(out, fmt.Sprintf(":%d available", listenPort)) {
		t.Fatalf("unexpected validate output:\n%s", out)
	}
}

func TestValidateRoutingFlagsOverrideConfig(t *testing.T) {
	client := listenForTest(t)
	server := listenForTest(t)
	listenPort := freePortForTest(t)
	path := filepath.Join(t.TempDir(), "merge-port.yaml")
	content := fmt.Sprintf("port: %d\nclient: 1\nserver: 2\n", listenPort)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	out, _, err := executeCommand("validate", "--config", path, "--client", fmt.Sprint(portOf(t, client)), "--server", fmt.Sprint(portOf(t, server)))
	if err != nil {
		t.Fatalf("validate overrides config: %v\n%s", err, out)
	}
	if strings.Contains(out, "localhost:1") || strings.Contains(out, "localhost:2") {
		t.Fatalf("validate did not override config routes:\n%s", out)
	}
}

func TestValidateRouteModeWithoutCatchAllWarnsAndPrintsReport(t *testing.T) {
	server := listenForTest(t)
	listenPort := freePortForTest(t)
	out, errOut, err := executeCommand("validate", "--route", fmt.Sprintf("/api=%d", portOf(t, server)), "--port", fmt.Sprint(listenPort))
	if err != nil {
		t.Fatalf("validate route mode: %v\n%s", err, out)
	}
	if !strings.Contains(out, "merge-port validation passed") || !strings.Contains(out, fmt.Sprintf(":%d available", listenPort)) {
		t.Fatalf("unexpected validation report:\n%s", out)
	}
	if !strings.Contains(errOut, "Warning: no catch-all / route configured; unmatched paths will return 404.") {
		t.Fatalf("missing no-catch-all warning on stderr:\n%s", errOut)
	}
}

func TestValidatePortZeroChecksEphemeralListenPort(t *testing.T) {
	server := listenForTest(t)
	out, _, err := executeCommand("validate", "--route", fmt.Sprintf("/api=%d", portOf(t, server)), "--port", "0")
	if err != nil {
		t.Fatalf("validate port zero: %v\n%s", err, out)
	}
	if !strings.Contains(out, ":0 available") {
		t.Fatalf("expected validate to preserve explicit port 0:\n%s", out)
	}
}

func TestInitDryRunWritesNoFile(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	defer os.Chdir(old)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	out, _, err := executeCommand("init", "--dry-run", "--client", "5173")
	if err != nil {
		t.Fatalf("init dry-run: %v", err)
	}
	if !strings.Contains(out, "client: 5173") || !strings.Contains(out, "server: 3001") {
		t.Fatalf("unexpected config:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".merge-port.yaml")); !os.IsNotExist(err) {
		t.Fatalf("init --dry-run wrote file: %v", err)
	}
}

func TestDiscoverWriteConfigFromServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/openapi.json" {
			w.Write([]byte(`{"paths":{"/api/users":{},"/auth/login":{}}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	port := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	path := filepath.Join(t.TempDir(), ".merge-port.yaml")
	out, _, err := executeCommand("discover", "--server", port, "--client", "5173", "--write-config", path)
	_ = out
	if err != nil {
		t.Fatalf("discover write config: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	if !strings.Contains(content, "client: 5173") || !strings.Contains(content, "server:") || !strings.Contains(content, "/api") {
		t.Fatalf("unexpected config:\n%s", content)
	}
}

func TestDiscoverJSONCannotBeUsedWithWriteConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".merge-port.yaml")
	_, _, err := executeCommand("discover", "--server", "3001", "--client", "5173", "--json", "--write-config", path)
	if err == nil {
		t.Fatal("expected --json --write-config error")
	}
	var uerr usageError
	if !errors.As(err, &uerr) {
		t.Fatalf("expected usageError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "--json cannot be used with --write-config") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("discover wrote config despite invalid flags: %v", statErr)
	}
}

func TestStopMissingStatePrintsFriendlyNotRunning(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	port := freePortForTest(t)
	out, _, err := executeCommand("stop", "--port", fmt.Sprint(port))
	if err != nil {
		t.Fatalf("stop missing state: %v", err)
	}
	want := fmt.Sprintf("merge-port is not running on port %d", port)
	if !strings.Contains(out, want) {
		t.Fatalf("expected friendly not-running message %q, got:\n%s", want, out)
	}
	if strings.Contains(out, "no such file") || strings.Contains(out, "open ") {
		t.Fatalf("raw filesystem error leaked:\n%s", out)
	}
}

func TestStatusPIDZeroStartingStateIsNotRunning(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	port := freePortForTest(t)
	writeStateForTest(t, port, lifecycle.State{PID: 0, Port: port, Status: lifecycle.StatusStarting, StartedAt: time.Now()})

	out, _, err := executeCommand("status", "--port", fmt.Sprint(port))
	if err != nil {
		t.Fatalf("status pid zero: %v", err)
	}
	if !strings.Contains(out, fmt.Sprintf("merge-port is not running; removed stale state for port %d", port)) {
		t.Fatalf("expected stale pid-zero state to be treated as not running:\n%s", out)
	}
	if strings.Contains(out, "PID: 0") || strings.Contains(out, "merge-port is running") {
		t.Fatalf("pid-zero state treated as running:\n%s", out)
	}
}

func TestStopPIDZeroStartingStateIsNotRunning(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	port := freePortForTest(t)
	writeStateForTest(t, port, lifecycle.State{PID: 0, Port: port, Status: lifecycle.StatusStarting, StartedAt: time.Now()})

	out, _, err := executeCommand("stop", "--port", fmt.Sprint(port))
	if err != nil {
		t.Fatalf("stop pid zero: %v", err)
	}
	if !strings.Contains(out, fmt.Sprintf("merge-port is not running; removed stale state for port %d", port)) {
		t.Fatalf("expected stale pid-zero state to be treated as not running:\n%s", out)
	}
	if strings.Contains(out, "PID 0") || strings.Contains(out, "Stopped merge-port") {
		t.Fatalf("pid-zero state treated as signalable:\n%s", out)
	}
}

func TestBuildRoutes_SimpleMode(t *testing.T) {
	tests := []struct {
		name       string
		client     int
		server     int
		prefixes   []string
		wantRoutes int
		wantErr    string
	}{
		{
			name:       "default prefix",
			client:     3000,
			server:     3001,
			prefixes:   nil,
			wantRoutes: 2, // /api + /
		},
		{
			name:       "custom single prefix",
			client:     3000,
			server:     3001,
			prefixes:   []string{"/api/v1"},
			wantRoutes: 2,
		},
		{
			name:       "multiple prefixes",
			client:     3000,
			server:     3001,
			prefixes:   []string{"/api", "/auth", "/ws"},
			wantRoutes: 4, // 3 prefixes + /
		},
		{
			name:    "missing client",
			client:  0,
			server:  3001,
			wantErr: "--client",
		},
		{
			name:    "missing server",
			client:  3000,
			server:  0,
			wantErr: "--server",
		},
		{
			name:     "prefix without slash",
			client:   3000,
			server:   3001,
			prefixes: []string{"api"},
			wantErr:  "must start with /",
		},
		{
			name:     "prefix is root slash",
			client:   3000,
			server:   3001,
			prefixes: []string{"/"},
			wantErr:  "cannot be /",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routes, err := buildRoutes(tt.client, tt.server, tt.prefixes, nil)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(routes) != tt.wantRoutes {
				t.Fatalf("expected %d routes, got %d: %+v", tt.wantRoutes, len(routes), routes)
			}
		})
	}
}

func TestBuildRoutes_SimpleMode_Targets(t *testing.T) {
	routes, err := buildRoutes(3000, 3001, []string{"/api", "/auth"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First two routes should point to server
	for _, r := range routes[:2] {
		if r.Target != "http://localhost:3001" {
			t.Errorf("prefix %q: expected target http://localhost:3001, got %q", r.Prefix, r.Target)
		}
	}
	// Last route should be catch-all to client
	last := routes[len(routes)-1]
	if last.Prefix != "/" || last.Target != "http://localhost:3000" {
		t.Errorf("expected catch-all / → http://localhost:3000, got %q → %q", last.Prefix, last.Target)
	}
}

func TestBuildRoutes_RouteMode(t *testing.T) {
	tests := []struct {
		name       string
		rawRoutes  []string
		wantRoutes int
		wantFirst  proxy.Route
	}{
		{
			name:       "bare ports",
			rawRoutes:  []string{"/api=3001", "/=3000"},
			wantRoutes: 2,
			wantFirst:  proxy.Route{Prefix: "/api", Target: "http://localhost:3001"},
		},
		{
			name:       "full URL",
			rawRoutes:  []string{"/api=http://api.local:3001"},
			wantRoutes: 1,
			wantFirst:  proxy.Route{Prefix: "/api", Target: "http://api.local:3001"},
		},
		{
			name:       "host without scheme",
			rawRoutes:  []string{"/api=api.local:3001"},
			wantRoutes: 1,
			wantFirst:  proxy.Route{Prefix: "/api", Target: "http://api.local:3001"},
		},
		{
			name:       "single route no catch-all",
			rawRoutes:  []string{"/api=3001"},
			wantRoutes: 1,
			wantFirst:  proxy.Route{Prefix: "/api", Target: "http://localhost:3001"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routes, err := buildRoutes(0, 0, nil, tt.rawRoutes)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(routes) != tt.wantRoutes {
				t.Fatalf("expected %d routes, got %d", tt.wantRoutes, len(routes))
			}
			if routes[0] != tt.wantFirst {
				t.Fatalf("expected first route %+v, got %+v", tt.wantFirst, routes[0])
			}
		})
	}
}

func TestBuildRoutes_MutualExclusivity(t *testing.T) {
	tests := []struct {
		name     string
		client   int
		server   int
		prefixes []string
		routes   []string
	}{
		{
			name:   "route with client",
			client: 3000,
			routes: []string{"/api=3001"},
		},
		{
			name:   "route with server",
			server: 3001,
			routes: []string{"/api=3001"},
		},
		{
			name:     "route with api-prefix",
			prefixes: []string{"/api"},
			routes:   []string{"/api=3001"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildRoutes(tt.client, tt.server, tt.prefixes, tt.routes)
			if err == nil {
				t.Fatal("expected mutual exclusivity error, got nil")
			}
			if !contains(err.Error(), "cannot be combined") {
				t.Fatalf("expected error containing 'cannot be combined', got %q", err.Error())
			}
		})
	}
}

func TestParseRouteFlags_Errors(t *testing.T) {
	tests := []struct {
		name    string
		raw     []string
		wantErr string
	}{
		{
			name:    "no equals",
			raw:     []string{"api3001"},
			wantErr: "expected prefix=target",
		},
		{
			name:    "empty prefix",
			raw:     []string{"=3001"},
			wantErr: "must start with /",
		},
		{
			name:    "prefix no slash",
			raw:     []string{"api=3001"},
			wantErr: "must start with /",
		},
		{
			name:    "empty target",
			raw:     []string{"/api="},
			wantErr: "target is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseRouteFlags(tt.raw)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestNormalizeTarget(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"3001", "http://localhost:3001"},
		{"api.local:3001", "http://api.local:3001"},
		{"http://api.local:3001", "http://api.local:3001"},
		{"https://secure.local", "https://secure.local"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeTarget(tt.input)
			if got != tt.want {
				t.Fatalf("normalizeTarget(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildRoutes_NeitherMode(t *testing.T) {
	_, err := buildRoutes(0, 0, nil, nil)
	if err == nil {
		t.Fatal("expected error when no flags provided, got nil")
	}
}

// ─── discover helpers ─────────────────────────────────────────────────────────

func TestTopPrefix(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/api/users", "/api"},
		{"/api", "/api"},
		{"/", "/"},
		{"api/users", "/api"}, // no leading slash
		{"/auth/login", "/auth"},
		{"/v1/users/123", "/v1"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := topPrefix(tt.path)
			if got != tt.want {
				t.Errorf("topPrefix(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestTryOpenAPI_NotOK(t *testing.T) {
	client := &http.Client{Timeout: 5 * time.Second}
	prefixes, ok := tryOpenAPI(client, "http://localhost:9999/nonexistent")
	if ok {
		t.Errorf("tryOpenAPI should return false for unreachable server")
	}
	if prefixes != nil {
		t.Errorf("tryOpenAPI should return nil prefixes on failure, got %v", prefixes)
	}
}

func TestTryOpenAPI_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	prefixes, ok := tryOpenAPI(client, server.URL+"/openapi.json")
	if ok {
		t.Errorf("tryOpenAPI should return false for non-200 status")
	}
	if prefixes != nil {
		t.Errorf("tryOpenAPI should return nil prefixes on failure, got %v", prefixes)
	}
}

func TestTryOpenAPI_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	prefixes, ok := tryOpenAPI(client, server.URL+"/openapi.json")
	if ok {
		t.Errorf("tryOpenAPI should return false for invalid JSON")
	}
	if prefixes != nil {
		t.Errorf("tryOpenAPI should return nil prefixes on failure, got %v", prefixes)
	}
}

func TestTryOpenAPI_EmptyPaths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"paths": {}}`))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	prefixes, ok := tryOpenAPI(client, server.URL+"/openapi.json")
	if ok {
		t.Errorf("tryOpenAPI should return false for empty paths")
	}
	if prefixes != nil {
		t.Errorf("tryOpenAPI should return nil prefixes on failure, got %v", prefixes)
	}
}

func TestTryOpenAPI_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"paths": {
				"/api/users": {"get": {}},
				"/api/posts": {"post": {}},
				"/auth/login": {"post": {}}
			}
		}`))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	prefixes, ok := tryOpenAPI(client, server.URL+"/openapi.json")
	if !ok {
		t.Fatalf("tryOpenAPI should return true for valid OpenAPI spec")
	}
	if len(prefixes) != 2 {
		t.Errorf("expected 2 prefixes, got %d: %v", len(prefixes), prefixes)
	}
	if prefixes[0] != "/api" || prefixes[1] != "/auth" {
		t.Errorf("prefixes = %v, want [/api, /auth]", prefixes)
	}
}

func TestDiscoverPrefixes_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/openapi.json" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"paths": {"/api/users": {}, "/api/posts": {}}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	prefixes, source, err := discoverPrefixes(server.URL)
	if err != nil {
		t.Fatalf("discoverPrefixes failed: %v", err)
	}
	if source != "/openapi.json" {
		t.Errorf("source = %q, want /openapi.json", source)
	}
	if len(prefixes) != 1 || prefixes[0] != "/api" {
		t.Errorf("prefixes = %v, want [/api]", prefixes)
	}
}

func TestDiscoverPrefixes_FallbackToSwagger(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/openapi.json":
			w.WriteHeader(http.StatusNotFound)
		case "/swagger.json":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"paths": {"/auth/token": {}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	prefixes, source, err := discoverPrefixes(server.URL)
	if err != nil {
		t.Fatalf("discoverPrefixes failed: %v", err)
	}
	if source != "/swagger.json" {
		t.Errorf("source = %q, want /swagger.json", source)
	}
	if len(prefixes) != 1 || prefixes[0] != "/auth" {
		t.Errorf("prefixes = %v, want [/auth]", prefixes)
	}
}

func TestDiscoverPrefixes_AllEndpointsFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, _, err := discoverPrefixes(server.URL)
	if err == nil {
		t.Fatal("discoverPrefixes should return error when all endpoints fail")
	}
}

func TestDiscoverPrefixes_ServerUnreachable(t *testing.T) {
	_, _, err := discoverPrefixes("http://localhost:99999")
	if err == nil {
		t.Fatal("discoverPrefixes should return error for unreachable server")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func listenForTest(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln
}

func freePortForTest(t *testing.T) int {
	t.Helper()
	ln := listenForTest(t)
	port := portOf(t, ln)
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func portOf(t *testing.T, ln net.Listener) int {
	t.Helper()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener addr = %T", ln.Addr())
	}
	return addr.Port
}

func writeStateForTest(t *testing.T, port int, state lifecycle.State) {
	t.Helper()
	path := lifecycle.StatePath(defaultStateDir(), port)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(f).Encode(state); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDetachPIDZeroStartingStateDoesNotReportAlreadyRunningWhenPortUnavailable(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	ln := listenForTest(t)
	port := portOf(t, ln)

	writeStateForTest(t, port, lifecycle.State{PID: 0, Port: port, Status: lifecycle.StatusStarting, StartedAt: time.Now()})

	_, _, err := executeCommand("--client", "5173", "--server", "3001", "--port", fmt.Sprint(port), "--detach")
	if err == nil {
		ln.Close()
		t.Fatal("runDetach succeeded but port is occupied - child may be running")
	}
	if strings.Contains(err.Error(), "already running") {
		ln.Close()
		t.Fatalf("runDetach incorrectly treated PID 0 state as running process:\n%v", err)
	}
	ln.Close()
}

func TestDetachPIDZeroStateIsRemovedBeforeFailedChildStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("detach is unsupported on Windows")
	}
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	ln := listenForTest(t)
	port := portOf(t, ln)
	ln.Close()
	logDir := t.TempDir()

	writeStateForTest(t, port, lifecycle.State{PID: 0, Port: port, Status: lifecycle.StatusStarting, StartedAt: time.Now()})

	_, _, err := executeCommand("--client", "5173", "--server", "3001", "--port", fmt.Sprint(port), "--detach", "--log-file", logDir)
	if err == nil {
		t.Fatal("expected child start to fail because log path is a directory")
	}
	if strings.Contains(err.Error(), "already running") {
		t.Fatalf("runDetach reported already running for PID 0 state: %v", err)
	}
	if !strings.Contains(err.Error(), "cannot open log file") {
		t.Fatalf("unexpected detach error: %v", err)
	}

	statePath := lifecycle.StatePath(defaultStateDir(), port)
	if _, statErr := os.Stat(statePath); !os.IsNotExist(statErr) {
		t.Fatalf("state file should be removed after failed child start: %v", statErr)
	}
}
