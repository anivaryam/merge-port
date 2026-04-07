package proxy

import (
	"fmt"
	"io"
	"os"
	"time"
)

// Logger controls where and whether request logs are written.
// The startup banner always goes to stdout regardless of Logger settings.
type Logger struct {
	out     io.Writer // destination for request logs; io.Discard when silent
	f       *os.File  // non-nil if writing to a file (closed by Close)
	noColor bool      // true when writing to a file — suppresses ANSI codes
}

// NewLogger creates a Logger.
//   - silent=true, logFile=""  → suppress all request logs
//   - silent=false, logFile="" → write to stdout
//   - logFile != ""            → write to file (silent is ignored, no colors)
func NewLogger(silent bool, logFile string) (*Logger, error) {
	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, fmt.Errorf("cannot open log file %s: %w", logFile, err)
		}
		return &Logger{out: f, f: f, noColor: true}, nil
	}
	if silent {
		return &Logger{out: io.Discard}, nil
	}
	return &Logger{out: os.Stdout}, nil
}

// Close releases any open log file.
func (l *Logger) Close() error {
	if l.f != nil {
		return l.f.Close()
	}
	return nil
}

func (l *Logger) color(code string) string {
	if l.noColor || NoColor {
		return ""
	}
	return code
}

// Request logs a proxied HTTP request.
func (l *Logger) Request(method, path string, status int, target string, elapsed time.Duration) {
	sc := l.color(statusColor(status))
	fmt.Fprintf(l.out, "  %s%-7s%s %s → %s%s%s %s%d%s %s%s%s\n",
		l.color(cBold), method, l.color(cReset),
		path,
		l.color(cDim), target, l.color(cReset),
		sc, status, l.color(cReset),
		l.color(cDim), elapsed.Round(time.Millisecond), l.color(cReset))
}

// WebSocket logs a WebSocket upgrade.
func (l *Logger) WebSocket(path, target string) {
	fmt.Fprintf(l.out, "  %s↑ WS%s    %s → %s%s%s\n",
		l.color(cYellow), l.color(cReset),
		path,
		l.color(cDim), target, l.color(cReset))
}

// Error logs a proxy error.
func (l *Logger) Error(method, path, target string, err error) {
	fmt.Fprintf(l.out, "  %s%-7s%s %s → %s%s%s %s%v%s\n",
		l.color(cBold), method, l.color(cReset),
		path,
		l.color(cDim), target, l.color(cReset),
		l.color(cRed), err, l.color(cReset))
}
