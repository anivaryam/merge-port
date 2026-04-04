package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/anivaryam/merge-port/internal/proxy"
	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	var (
		clientPort int
		serverPort int
		listenPort int
		apiPrefix  string
	)

	rootCmd := &cobra.Command{
		Use:     "merge-port",
		Short:   "Merge client and server ports into one",
		Version: version,
		Long: `A local reverse proxy that merges your client and server
into a single port for easy tunneling.

Example:
  merge-port --client 3000 --server 3001
  merge-port --client 5173 --server 8080 --port 9000 --api-prefix /api/v1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if clientPort == 0 {
				return fmt.Errorf("--client port is required")
			}
			if serverPort == 0 {
				return fmt.Errorf("--server port is required")
			}

			routes := []proxy.Route{
				{Prefix: apiPrefix, Target: fmt.Sprintf("http://localhost:%d", serverPort)},
				{Prefix: "/", Target: fmt.Sprintf("http://localhost:%d", clientPort)},
			}

			p := proxy.NewProxy(listenPort, routes)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sig := make(chan os.Signal, 1)
			signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sig
				cancel()
			}()

			return p.Run(ctx)
		},
	}

	rootCmd.Flags().IntVar(&clientPort, "client", 0, "client/frontend port (required)")
	rootCmd.Flags().IntVar(&serverPort, "server", 0, "server/backend port (required)")
	rootCmd.Flags().IntVar(&listenPort, "port", 8080, "port to listen on")
	rootCmd.Flags().StringVar(&apiPrefix, "api-prefix", "/api", "path prefix to route to server")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
