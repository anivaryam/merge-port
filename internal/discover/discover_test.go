package discover

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestExtractPrefixesDedupesAndSorts(t *testing.T) {
	prefixes, examples := ExtractPrefixes([]string{"/auth/login", "/api/users", "/api/posts", "/api-docs"})
	want := []string{"/api", "/api-docs", "/auth"}
	if !reflect.DeepEqual(prefixes, want) {
		t.Fatalf("prefixes = %v, want %v", prefixes, want)
	}
	if len(examples["/api"]) != 2 {
		t.Fatalf("api examples = %v, want two", examples["/api"])
	}
}

func TestParseHeaderTrimsAndRejectsInvalid(t *testing.T) {
	name, value, err := ParseHeader("Authorization: Bearer token")
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}
	if name != "Authorization" || value != "Bearer token" {
		t.Fatalf("header = %q:%q", name, value)
	}
	if _, _, err := ParseHeader("broken"); err == nil {
		t.Fatal("expected error for missing colon")
	}
}

func TestDiscoverUsesCustomPathsOnlyAndReportsErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/custom.json" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	result, err := Discover(context.Background(), Options{BaseURL: server.URL, CandidatePaths: []string{"/custom.json"}, Timeout: 2 * time.Second})
	if err == nil {
		t.Fatal("expected discovery error")
	}
	if len(result.Errors) != 1 || result.Errors[0].URL != server.URL+"/custom.json" {
		t.Fatalf("errors = %+v", result.Errors)
	}
}

func TestDiscoverSuccessIncludesSourcePrefixesPathsAndHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("missing auth header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"paths":{"/api/users":{},"/auth/login":{}}}`))
	}))
	defer server.Close()

	result, err := Discover(context.Background(), Options{BaseURL: server.URL, CandidatePaths: []string{"/openapi.json"}, Headers: http.Header{"Authorization": []string{"Bearer token"}}, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if result.Source != server.URL+"/openapi.json" || !reflect.DeepEqual(result.Prefixes, []string{"/api", "/auth"}) || len(result.Paths) != 2 {
		t.Fatalf("result = %+v", result)
	}
}
