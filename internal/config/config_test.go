package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anivaryam/merge-port/internal/proxy"
)

func TestLoadRouteTargetsAcceptStringAndInt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".merge-port.yaml")
	content := []byte("port: 8080\nroutes:\n  - prefix: /api\n    target: 3001\n  - prefix: /auth\n    target: \"3002\"\n")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := string(cfg.Routes[0].Target); got != "3001" {
		t.Fatalf("numeric target = %q, want 3001", got)
	}
	if got := string(cfg.Routes[1].Target); got != "3002" {
		t.Fatalf("string target = %q, want 3002", got)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".merge-port.yaml")
	if err := os.WriteFile(path, []byte("port: 8080\napiPrefixes:\n  - /api\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "field apiPrefixes not found") {
		t.Fatalf("Load error = %v, want unknown field", err)
	}
}

func TestSaveOmitsDefaultsAndPreservesRouteOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".merge-port.yaml")
	cfg := Config{
		Port: 8080,
		Routes: []Route{
			{Prefix: "/api", Target: Target("3001")},
			{Prefix: "/", Target: Target("5173")},
		},
	}

	if err := Save(path, cfg, false); err != nil {
		t.Fatalf("Save: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	if strings.Contains(out, "silent: false") || strings.Contains(out, "log_file") {
		t.Fatalf("saved defaults should be omitted:\n%s", out)
	}
	if !strings.Contains(out, "target: \"3001\"") {
		t.Fatalf("bare port target should be quoted:\n%s", out)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Routes[0].Prefix != "/api" || loaded.Routes[1].Prefix != "/" {
		t.Fatalf("route order changed: %+v", loaded.Routes)
	}
}

func TestToProxyRoutesNormalizesTargets(t *testing.T) {
	routes, err := ToProxyRoutes(Config{Routes: []Route{{Prefix: "/api", Target: Target("3001")}, {Prefix: "/", Target: Target("http://localhost:5173")}}})
	if err != nil {
		t.Fatalf("ToProxyRoutes: %v", err)
	}
	want := []proxy.Route{{Prefix: "/api", Target: "http://localhost:3001"}, {Prefix: "/", Target: "http://localhost:5173"}}
	for i := range want {
		if routes[i] != want[i] {
			t.Fatalf("route %d = %+v, want %+v", i, routes[i], want[i])
		}
	}
}

func TestValidateRejectsMixedModes(t *testing.T) {
	err := Validate(Config{Client: 3000, Routes: []Route{{Prefix: "/api", Target: Target("3001")}}})
	if err == nil || !strings.Contains(err.Error(), "routes cannot be combined") {
		t.Fatalf("Validate error = %v", err)
	}
}
