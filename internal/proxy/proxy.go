package proxy

import (
	"bufio"
	"context"
	"fmt"
	"log"
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
	Port   int
	Routes []Route
	Logger *Logger
}

// NewProxy creates a new Proxy. Routes are sorted by prefix length (longest first)
// so that more specific paths match before less specific ones.
// If logger is nil a default stdout logger is used.
func NewProxy(port int, routes []Route, logger *Logger) *Proxy {
	sorted := make([]Route, len(routes))
	copy(sorted, routes)
	sort.Slice(sorted, func(i, j int) bool {
		return len(sorted[i].Prefix) > len(sorted[j].Prefix)
	})
	if logger == nil {
		logger, _ = NewLogger(false, "")
	}
	return &Proxy{Port: port, Routes: sorted, Logger: logger}
}

// Run starts the proxy server and blocks until the context is cancelled.
func (p *Proxy) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", p.handler())

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", p.Port),
		Handler: mux,
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
	reverseProxies := make(map[string]*httputil.ReverseProxy)

	for _, r := range p.Routes {
		target, err := url.Parse(r.Target)
		if err != nil {
			log.Fatalf("[proxy] invalid target URL %q: %v", r.Target, err)
		}

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
			fmt.Fprintf(w, "merge-port: upstream %s unreachable: %v", routeTarget, err)
		}

		reverseProxies[r.Prefix] = rp
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_health" {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "ok")
			return
		}

		for _, route := range p.Routes {
			if strings.HasPrefix(r.URL.Path, route.Prefix) {
				rp := reverseProxies[route.Prefix]
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

func isWebSocket(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// statusRecorder wraps http.ResponseWriter to capture the status code.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := r.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("upstream ResponseWriter does not support hijacking")
}
