package main

import (
	"testing"

	"github.com/anivaryam/merge-port/internal/proxy"
)

func TestBuildRoutes_SimpleMode(t *testing.T) {
	tests := []struct {
		name        string
		client      int
		server      int
		prefixes    []string
		wantRoutes  int
		wantErr     string
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
