package proxy

import (
	"fmt"
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
// Always writes to stdout regardless of Logger settings.
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
