package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type Options struct {
	BaseURL        string
	CandidatePaths []string
	Headers        http.Header
	Timeout        time.Duration
}

type CandidateError struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

type Result struct {
	Source   string              `json:"source"`
	Prefixes []string            `json:"prefixes"`
	Paths    []string            `json:"paths"`
	Errors   []CandidateError    `json:"errors"`
	Examples map[string][]string `json:"-"`
}

func DefaultCandidatePaths() []string {
	return []string{"/openapi.json", "/swagger.json", "/api-docs/swagger.json", "/api/docs/swagger.json", "/api/openapi.json", "/api-docs"}
}

func ParseHeader(raw string) (string, string, error) {
	name, value, ok := strings.Cut(raw, ":")
	if !ok {
		return "", "", fmt.Errorf("invalid header %q: expected Name: value", raw)
	}
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)
	if name == "" {
		return "", "", fmt.Errorf("invalid header %q: header name is empty", raw)
	}
	return name, value, nil
}

func Discover(ctx context.Context, opts Options) (Result, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Second
	}
	candidates := opts.CandidatePaths
	if len(candidates) == 0 {
		candidates = DefaultCandidatePaths()
	}
	client := &http.Client{Timeout: opts.Timeout}
	var result Result
	for _, candidate := range candidates {
		rawURL, err := joinURL(opts.BaseURL, candidate)
		if err != nil {
			result.Errors = append(result.Errors, CandidateError{URL: opts.BaseURL + candidate, Error: err.Error()})
			continue
		}
		paths, err := fetchPaths(ctx, client, rawURL, opts.Headers)
		if err != nil {
			result.Errors = append(result.Errors, CandidateError{URL: rawURL, Error: err.Error()})
			continue
		}
		prefixes, examples := ExtractPrefixes(paths)
		if len(prefixes) == 0 {
			result.Errors = append(result.Errors, CandidateError{URL: rawURL, Error: "no API prefixes found"})
			continue
		}
		result.Source = rawURL
		result.Prefixes = prefixes
		result.Paths = paths
		result.Examples = examples
		return result, nil
	}
	return result, fmt.Errorf("no OpenAPI spec found")
}

func ExtractPrefixes(paths []string) ([]string, map[string][]string) {
	seen := make(map[string]bool)
	examples := make(map[string][]string)
	for _, path := range paths {
		prefix := topPrefix(path)
		if prefix == "/" || prefix == "" {
			continue
		}
		seen[prefix] = true
		examples[prefix] = append(examples[prefix], path)
	}
	prefixes := make([]string, 0, len(seen))
	for prefix := range seen {
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)
	for _, paths := range examples {
		sort.Strings(paths)
	}
	return prefixes, examples
}

func fetchPaths(ctx context.Context, client *http.Client, rawURL string, headers http.Header) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var spec struct {
		Paths map[string]interface{} `json:"paths"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&spec); err != nil {
		return nil, err
	}
	if len(spec.Paths) == 0 {
		return nil, fmt.Errorf("OpenAPI spec has no paths")
	}
	paths := make([]string, 0, len(spec.Paths))
	for path := range spec.Paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func joinURL(base, path string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid base URL %q", base)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimLeft(path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func topPrefix(path string) string {
	trimmed := strings.TrimPrefix(path, "/")
	parts := strings.SplitN(trimmed, "/", 2)
	if parts[0] == "" {
		return "/"
	}
	return "/" + parts[0]
}
