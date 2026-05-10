package proxy

import (
	"fmt"
	"os"
)

// NoColor disables ANSI escape codes in all output.
// Initialized from the NO_COLOR env var (https://no-color.org).
var NoColor = os.Getenv("NO_COLOR") != ""

func ansi(code string) string {
	if NoColor {
		return ""
	}
	return code
}

const (
	cReset  = "\033[0m"
	cGreen  = "\033[32m"
	cYellow = "\033[33m"
	cRed    = "\033[31m"
	cCyan   = "\033[36m"
	cBold   = "\033[1m"
	cDim    = "\033[2m"
)

// PrintBanner prints the startup banner with routing info.
// Always writes to stdout regardless of Logger settings.
func PrintBanner(port int, routes []Route) {
	fmt.Println()
	fmt.Printf("%s%smerge-port%s %sis running%s on port %s%d%s\n",
		ansi(cBold), ansi(cCyan), ansi(cReset),
		ansi(cGreen), ansi(cReset),
		ansi(cBold), port, ansi(cReset))
	fmt.Println()
	for _, r := range routes {
		fmt.Printf("  %s%-10s%s → %s%s%s\n",
			ansi(cBold), r.Prefix, ansi(cReset),
			ansi(cDim), r.Target, ansi(cReset))
	}
	fmt.Println()
	fmt.Printf("  %sPress Ctrl+C to stop%s\n", ansi(cDim), ansi(cReset))
	fmt.Println()
}

func statusColor(code int) string {
	switch {
	case code >= 200 && code < 300:
		return cGreen
	case code >= 300 && code < 400:
		return cYellow
	default:
		return cRed
	}
}
