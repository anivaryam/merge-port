package proxy

import (
	"fmt"
	"time"
)

const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

// PrintBanner prints the startup banner with routing info.
func PrintBanner(port int, routes []Route) {
	fmt.Println()
	fmt.Printf("%s%smerge-port%s %sis running%s on port %s%d%s\n",
		colorBold, colorCyan, colorReset,
		colorGreen, colorReset,
		colorBold, port, colorReset)
	fmt.Println()
	for _, r := range routes {
		fmt.Printf("  %s%-10s%s → %s%s%s\n",
			colorBold, r.Prefix, colorReset,
			colorDim, r.Target, colorReset)
	}
	fmt.Println()
	fmt.Printf("  %sPress Ctrl+C to stop%s\n", colorDim, colorReset)
	fmt.Println()
}

// PrintRequest logs a proxied request with color-coded status.
func PrintRequest(method, path string, status int, target string, elapsed time.Duration) {
	color := statusColor(status)
	fmt.Printf("  %s%-7s%s %s → %s%s%s %s%d%s %s%s%s\n",
		colorBold, method, colorReset,
		path,
		colorDim, target, colorReset,
		color, status, colorReset,
		colorDim, elapsed.Round(time.Millisecond), colorReset)
}

// PrintWebSocket logs a WebSocket upgrade.
func PrintWebSocket(path, target string) {
	fmt.Printf("  %s↑ WS%s    %s → %s%s%s\n",
		colorYellow, colorReset,
		path,
		colorDim, target, colorReset)
}

// PrintError logs a proxy error.
func PrintError(method, path, target string, err error) {
	fmt.Printf("  %s%-7s%s %s → %s%s%s %s%v%s\n",
		colorBold, method, colorReset,
		path,
		colorDim, target, colorReset,
		colorRed, err, colorReset)
}

func statusColor(code int) string {
	switch {
	case code >= 200 && code < 300:
		return colorGreen
	case code >= 300 && code < 400:
		return colorYellow
	default:
		return colorRed
	}
}
