package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ─── matchesPrefix ───────────────────────────────────────────────────────────

func TestMatchesPrefix(t *testing.T) {
	tests := []struct {
		path   string
		prefix string
		want   bool
	}{
		// Exact match
		{"/api", "/api", true},
		{"/health", "/health", true},

		// Path under prefix
		{"/api/users", "/api", true},
		{"/api/users/123", "/api", true},
		{"/api/", "/api", true},

		// Partial match — should NOT match (the critical bug)
		{"/apiv2", "/api", false},
		{"/api-docs", "/api", false},
		{"/api-docs.json", "/api", false},
		{"/apis", "/api", false},
		{"/application", "/app", false},
		{"/authz", "/auth", false},

		// Root prefix matches everything
		{"/anything", "/", true},
		{"/", "/", true},
		{"/deeply/nested/path", "/", true},

		// Longer prefix
		{"/api/auth/login", "/api/auth", true},
		{"/api/authorize", "/api/auth", false},
	}

	for _, tt := range tests {
		t.Run(tt.path+"_vs_"+tt.prefix, func(t *testing.T) {
			got := matchesPrefix(tt.path, tt.prefix)
			if got != tt.want {
				t.Errorf("matchesPrefix(%q, %q) = %v, want %v", tt.path, tt.prefix, got, tt.want)
			}
		})
	}
}

// ─── Integration tests ──────────────────────────────────────────────────────

func setupTestProxy(t *testing.T, routes []Route) http.Handler {
	t.Helper()
	logger, _ := NewLogger(true, "") // silent
	p, err := NewProxy(0, routes, logger)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	return http.HandlerFunc(p.handler())
}

func TestProxy_BasicRouting(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("BACKEND"))
	}))
	defer backend.Close()

	frontend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("FRONTEND"))
	}))
	defer frontend.Close()

	handler := setupTestProxy(t, []Route{
		{Prefix: "/api", Target: backend.URL},
		{Prefix: "/", Target: frontend.URL},
	})

	tests := []struct {
		path string
		want string
	}{
		{"/api/users", "BACKEND"},
		{"/api", "BACKEND"},
		{"/", "FRONTEND"},
		{"/about", "FRONTEND"},
		{"/static/app.js", "FRONTEND"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			body := rec.Body.String()
			if body != tt.want {
				t.Errorf("GET %s = %q, want %q", tt.path, body, tt.want)
			}
		})
	}
}

func TestProxy_PrefixBoundary(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("BACKEND"))
	}))
	defer backend.Close()

	frontend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("FRONTEND"))
	}))
	defer frontend.Close()

	handler := setupTestProxy(t, []Route{
		{Prefix: "/api", Target: backend.URL},
		{Prefix: "/", Target: frontend.URL},
	})

	// These should NOT match /api — they are different path segments
	tests := []struct {
		path string
		want string
	}{
		{"/apiv2", "FRONTEND"},
		{"/api-docs", "FRONTEND"},
		{"/api-docs.json", "FRONTEND"},
		{"/apis", "FRONTEND"},
		{"/application", "FRONTEND"},
		// These SHOULD match /api
		{"/api", "BACKEND"},
		{"/api/", "BACKEND"},
		{"/api/users", "BACKEND"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			body := rec.Body.String()
			if body != tt.want {
				t.Errorf("GET %s = %q, want %q", tt.path, body, tt.want)
			}
		})
	}
}

func TestProxy_LongestPrefixFirst(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("AUTH"))
	}))
	defer authServer.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("API"))
	}))
	defer apiServer.Close()

	// Routes given in any order — NewProxy sorts by length
	handler := setupTestProxy(t, []Route{
		{Prefix: "/api", Target: apiServer.URL},
		{Prefix: "/api/auth", Target: authServer.URL},
	})

	tests := []struct {
		path string
		want string
	}{
		{"/api/auth/login", "AUTH"},
		{"/api/auth", "AUTH"},
		{"/api/users", "API"},
		{"/api", "API"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			body := rec.Body.String()
			if body != tt.want {
				t.Errorf("GET %s = %q, want %q", tt.path, body, tt.want)
			}
		})
	}
}

func TestProxy_HealthEndpoint(t *testing.T) {
	handler := setupTestProxy(t, []Route{
		{Prefix: "/", Target: "http://localhost:1"},
	})

	req := httptest.NewRequest("GET", "/_health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("/_health status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if body != "ok" {
		t.Errorf("/_health body = %q, want %q", body, "ok")
	}
}

func TestProxy_NoMatchReturns404(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("BACKEND"))
	}))
	defer backend.Close()

	// Only /api route, no catch-all /
	handler := setupTestProxy(t, []Route{
		{Prefix: "/api", Target: backend.URL},
	})

	req := httptest.NewRequest("GET", "/other", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /other status = %d, want 404", rec.Code)
	}
}

func TestProxy_WebSocketUpgrade(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the Upgrade header was forwarded
		if r.Header.Get("Upgrade") == "websocket" {
			w.Write([]byte("WS_OK"))
		} else {
			w.Write([]byte("NOT_WS"))
		}
	}))
	defer backend.Close()

	handler := setupTestProxy(t, []Route{
		{Prefix: "/", Target: backend.URL},
	})

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if body != "WS_OK" {
		t.Errorf("WebSocket request body = %q, want %q", body, "WS_OK")
	}
}

func TestNewProxy_InvalidTarget(t *testing.T) {
	_, err := NewProxy(8080, []Route{
		{Prefix: "/api", Target: "://invalid"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid target URL, got nil")
	}
}

func TestProxy_StatusCapture(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("created"))
	}))
	defer backend.Close()

	handler := setupTestProxy(t, []Route{
		{Prefix: "/", Target: backend.URL},
	})

	req := httptest.NewRequest("POST", "/resources", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != "created" {
		t.Errorf("body = %q, want %q", body, "created")
	}
}
