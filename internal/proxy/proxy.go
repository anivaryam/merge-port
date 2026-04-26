package proxy

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Route defines a path prefix to target mapping.
type Route struct {
	Prefix string
	Target string
}

// Proxy is a reverse proxy that routes requests by path prefix.
type Proxy struct {
	Port            int
	Routes          []Route
	Logger          *Logger
	reverseProxies  map[string]*httputil.ReverseProxy
}

// NewProxy creates a new Proxy. Routes are sorted by prefix length (longest first)
// so that more specific paths match before less specific ones.
// Returns an error if any route target URL is invalid.
// If logger is nil a default stdout logger is used.
func NewProxy(port int, routes []Route, logger *Logger) (*Proxy, error) {
	sorted := make([]Route, len(routes))
	copy(sorted, routes)
	sort.Slice(sorted, func(i, j int) bool {
		return len(sorted[i].Prefix) > len(sorted[j].Prefix)
	})

	for _, r := range sorted {
		if _, err := url.Parse(r.Target); err != nil {
			return nil, fmt.Errorf("invalid target URL %q for prefix %q: %w", r.Target, r.Prefix, err)
		}
	}

	if logger == nil {
		var err error
		logger, err = NewLogger(false, "")
		if err != nil {
			return nil, fmt.Errorf("failed to create default logger: %w", err)
		}
	}

	p := &Proxy{Port: port, Routes: sorted, Logger: logger}
	p.reverseProxies = p.buildReverseProxies()
	return p, nil
}

func (p *Proxy) buildReverseProxies() map[string]*httputil.ReverseProxy {
	result := make(map[string]*httputil.ReverseProxy)
	for _, r := range p.Routes {
		target, _ := url.Parse(r.Target)
		rp := httputil.NewSingleHostReverseProxy(target)

		originalDirector := rp.Director
		rp.Director = func(req *http.Request) {
			originalDirector(req)
			req.Host = target.Host
			req.Header.Del("Origin")
			req.Header.Del("Referer")
		}

		logger := p.Logger
		routeTarget := r.Target
		rp.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
			logger.Error(req.Method, req.URL.Path, routeTarget, err)
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(w, "merge-port: upstream unreachable: %v", err)
		}

		result[r.Prefix] = rp
	}
	return result
}

// Run starts the proxy server and blocks until the context is cancelled.
func (p *Proxy) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", p.handler())

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", p.Port),
		Handler:      mux,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	PrintBanner(p.Port, p.Routes)

	err := server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return fmt.Errorf("server error: %w", err)
}

func (p *Proxy) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_health" {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "ok")
			return
		}

		for _, route := range p.Routes {
			if matchesPrefix(r.URL.Path, route.Prefix) {
				rp := p.reverseProxies[route.Prefix]
				start := time.Now()

				if isWebSocket(r) {
					p.Logger.WebSocket(r.URL.Path, route.Target)
					rp.ServeHTTP(w, r)
					return
				}

				rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
				rp.ServeHTTP(rec, r)
				p.Logger.Request(r.Method, r.URL.Path, rec.status, route.Target, time.Since(start))
				return
			}
		}

		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "merge-port: no matching route")
	}
}

// matchesPrefix checks whether path matches the route prefix at a path boundary.
// This prevents "/api" from matching "/apiv2" or "/api-docs" — only "/api", "/api/",
// and "/api/anything" are valid matches.
func matchesPrefix(path, prefix string) bool {
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	// Root prefix always matches.
	if prefix == "/" {
		return true
	}
	// Exact match or next char is a slash (path boundary).
	return len(path) == len(prefix) || path[len(prefix)] == '/'
}

func isWebSocket(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// statusRecorder wraps http.ResponseWriter to capture the status code
// written, enabling logging of the actual response status.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

// WriteHeader captures the status code and delegates to the underlying ResponseWriter.
func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

// Flush delegates to the underlying ResponseWriter if it supports flushing.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack delegates to the underlying ResponseWriter if it supports hijacking.
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := r.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("upstream ResponseWriter does not support hijacking")
}
