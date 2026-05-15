package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/anivaryam/merge-port/internal/config"
	"github.com/anivaryam/merge-port/internal/discover"
	"github.com/anivaryam/merge-port/internal/lifecycle"
	"github.com/anivaryam/merge-port/internal/preflight"
	"github.com/anivaryam/merge-port/internal/proxy"
	"github.com/spf13/cobra"
)

var version = "dev"

type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }

type routeOptions struct {
	clientPort  int
	serverPort  int
	listenPort  int
	apiPrefixes []string
	rawRoutes   []string
	silent      bool
	logFile     string
	detach      bool
	dryRun      bool
	configPath  string
}

func main() {
	cmd := newRootCommand()
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "Error:", err)
		var uerr usageError
		if errors.As(err, &uerr) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	opts := routeOptions{listenPort: 8080}
	rootCmd := &cobra.Command{
		Use:           "merge-port",
		Short:         "Merge client and server ports into one",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Long: `A local reverse proxy that merges your client and server
into a single port for easy tunneling.

Simple mode (client + server):
  merge-port --client 3000 --server 3001
  merge-port --client 3000 --server 3001 --api-prefix /api --api-prefix /auth

Route mode (full control):
  merge-port --route /api=3001 --route /=3000
  merge-port --route /api=http://api.local:3001 --route /=3000`,
		Example: `  # Default: /api -> server, everything else -> client
  merge-port --client 3000 --server 3001

  # Detect API prefixes from a running backend
  merge-port discover --server 3001

  # Suppress banner and request logs
  merge-port --client 5173 --server 3000 --silent

  # Write request logs to file
  merge-port --client 5173 --server 3000 --log-file /tmp/mp.log

  # Detach from terminal (daemonize)
  merge-port --client 5173 --server 3000 --detach`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoot(cmd, opts)
		},
	}

	addRoutingFlags(rootCmd, &opts)
	rootCmd.Flags().StringVar(&opts.configPath, "config", "", "path to YAML config file")
	rootCmd.Flags().BoolVar(&opts.detach, "detach", false, "daemonize: detach from terminal and run in background")
	rootCmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "print effective routing config and exit without starting the proxy")

	rootCmd.AddCommand(newValidateCommand())
	rootCmd.AddCommand(newInitCommand())
	rootCmd.AddCommand(newDiscoverCommand())
	rootCmd.AddCommand(newStatusCommand())
	rootCmd.AddCommand(newStopCommand())
	return rootCmd
}

func addRoutingFlags(cmd *cobra.Command, opts *routeOptions) {
	cmd.Flags().IntVar(&opts.clientPort, "client", 0, "client/frontend port (required in simple mode)")
	cmd.Flags().IntVar(&opts.serverPort, "server", 0, "server/backend port (required in simple mode)")
	cmd.Flags().IntVar(&opts.listenPort, "port", 8080, "port to listen on")
	cmd.Flags().StringArrayVar(&opts.apiPrefixes, "api-prefix", nil, "path prefix routed to server (repeatable, default: /api)")
	cmd.Flags().StringArrayVar(&opts.rawRoutes, "route", nil, "explicit route as prefix=target (repeatable, e.g. /api=3001)")
	cmd.Flags().BoolVar(&opts.silent, "silent", false, "suppress banner and request log output")
	cmd.Flags().StringVar(&opts.logFile, "log-file", "", "write request logs to FILE instead of stdout")
}

func runRoot(cmd *cobra.Command, opts routeOptions) error {
	cfg, err := effectiveConfig(cmd, opts)
	if err != nil {
		return err
	}
	if opts.dryRun && opts.detach {
		return usageError{fmt.Errorf("--dry-run cannot be combined with --detach")}
	}
	routes, err := config.ToProxyRoutes(cfg)
	if err != nil {
		return usageError{err}
	}
	emitNoCatchAllWarning(cmd.ErrOrStderr(), routes)
	if opts.dryRun {
		printDryRun(cmd.OutOrStdout(), cfg.Port, routes)
		return nil
	}

	if opts.detach {
		return runDetach(cmd, cfg.Port, opts.logFile)
	}

	logger, err := proxy.NewLogger(cfg.Silent, cfg.LogFile)
	if err != nil {
		return err
	}
	defer logger.Close()

	p, err := proxy.NewProxy(cfg.Port, routes, logger)
	if err != nil {
		return err
	}
	p.ShowBanner = !cfg.Silent

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		cancel()
	}()
	return p.Run(ctx)
}

func effectiveConfig(cmd *cobra.Command, opts routeOptions) (config.Config, error) {
	cfg := config.Config{Port: 8080}
	flags := cmd.Flags()
	if flags.Lookup("config") != nil && flags.Changed("config") {
		loaded, err := config.Load(opts.configPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return config.Config{}, usageError{fmt.Errorf("config file not found: %s", opts.configPath)}
			}
			return config.Config{}, err
		}
		cfg = loaded
	} else if flags.Lookup("config") != nil {
		_, statErr := os.Stat(".merge-port.yaml")
		if statErr == nil {
			loaded, err := config.Load(".merge-port.yaml")
			if err != nil {
				return config.Config{}, err
			}
			cfg = loaded
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return config.Config{}, statErr
		}
	}

	if flags.Changed("port") {
		cfg.Port = opts.listenPort
	}
	if flags.Changed("silent") {
		cfg.Silent = opts.silent
	}
	if flags.Changed("log-file") {
		cfg.LogFile = opts.logFile
	}
	simpleChanged := flags.Changed("client") || flags.Changed("server") || flags.Changed("api-prefix")
	if flags.Changed("route") {
		cfg.Client, cfg.Server, cfg.APIPrefixes = 0, 0, nil
		parsed, err := parseConfigRoutes(opts.rawRoutes)
		if err != nil {
			return config.Config{}, usageError{err}
		}
		cfg.Routes = parsed
	} else if simpleChanged {
		cfg.Routes = nil
		if flags.Changed("client") {
			cfg.Client = opts.clientPort
		}
		if flags.Changed("server") {
			cfg.Server = opts.serverPort
		}
		if flags.Changed("api-prefix") {
			cfg.APIPrefixes = opts.apiPrefixes
		}
	}
	if cfg.Port == 0 && !flags.Changed("port") {
		cfg.Port = 8080
	}
	return cfg, nil
}

func parseConfigRoutes(rawRoutes []string) ([]config.Route, error) {
	routes := make([]config.Route, 0, len(rawRoutes))
	for _, raw := range rawRoutes {
		prefix, target, ok := strings.Cut(raw, "=")
		if !ok {
			return nil, fmt.Errorf("invalid --route format %q: expected prefix=target (e.g. /api=3001)", raw)
		}
		if prefix == "" || !strings.HasPrefix(prefix, "/") {
			return nil, fmt.Errorf("route prefix must start with /: %q", prefix)
		}
		if target == "" {
			return nil, fmt.Errorf("route target is empty for prefix %q", prefix)
		}
		routes = append(routes, config.Route{Prefix: prefix, Target: config.Target(target)})
	}
	return routes, nil
}

func printDryRun(w io.Writer, port int, routes []proxy.Route) {
	fmt.Fprintln(w, "merge-port dry run")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Listen: :%d\n", port)
	fmt.Fprintln(w, "Routes:")
	for _, route := range routes {
		fmt.Fprintf(w, "  %-4s -> %s\n", route.Prefix, route.Target)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "No server started.")
}

func newValidateCommand() *cobra.Command {
	opts := routeOptions{listenPort: 8080}
	cmd := &cobra.Command{
		Use:           "validate",
		Short:         "Validate routing, listen port, and upstream reachability",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := effectiveConfig(cmd, opts)
			if err != nil {
				return err
			}
			routes, err := config.ToProxyRoutes(cfg)
			if err != nil {
				return usageError{err}
			}
			emitNoCatchAllWarning(cmd.ErrOrStderr(), routes)
			listen := preflight.CheckListenPort(cfg.Port)
			upstreams := preflight.ProbeUpstreams(cmd.Context(), routes, 2*time.Second)
			passed := listen.Available
			for _, up := range upstreams {
				passed = passed && up.Reachable
			}
			printValidationReport(cmd.OutOrStdout(), passed, listen, upstreams, routes)
			if !passed {
				return fmt.Errorf("validation failed")
			}
			return nil
		},
	}
	addRoutingFlags(cmd, &opts)
	cmd.Flags().StringVar(&opts.configPath, "config", "", "path to YAML config file")
	return cmd
}

func printValidationReport(w io.Writer, passed bool, listen preflight.ListenResult, upstreams []preflight.UpstreamResult, routes []proxy.Route) {
	if passed {
		fmt.Fprintln(w, "merge-port validation passed")
	} else {
		fmt.Fprintln(w, "merge-port validation failed")
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Listen port:")
	if listen.Available {
		fmt.Fprintf(w, "  %s available\n", listen.Addr)
	} else {
		fmt.Fprintf(w, "  %s unavailable: %v\n", listen.Addr, listen.Err)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Upstreams:")
	for _, up := range upstreams {
		if up.Reachable {
			fmt.Fprintf(w, "  %s reachable\n", up.Target)
		} else {
			fmt.Fprintf(w, "  %s unreachable: %v\n", up.Target, up.Err)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Routes:")
	for _, route := range routes {
		fmt.Fprintf(w, "  %-4s -> %s\n", route.Prefix, route.Target)
	}
}

func newInitCommand() *cobra.Command {
	opts := routeOptions{listenPort: 8080}
	var output string
	var force bool
	var dryRun bool
	cmd := &cobra.Command{
		Use:           "init",
		Short:         "Create a .merge-port.yaml config file",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := initConfig(cmd, opts)
			if err != nil {
				return err
			}
			if dryRun {
				return writeConfigYAML(cmd.OutOrStdout(), cfg)
			}
			if err := config.Save(output, cfg, force); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created %s. Run: merge-port\n", output)
			return nil
		},
	}
	cmd.Flags().StringVar(&output, "output", ".merge-port.yaml", "config file to write")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing config file")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print generated config without writing a file")
	addRoutingFlags(cmd, &opts)
	return cmd
}

func initConfig(cmd *cobra.Command, opts routeOptions) (config.Config, error) {
	flags := cmd.Flags()
	cfg := config.Default()
	if flags.Changed("port") {
		cfg.Port = opts.listenPort
	}
	if flags.Changed("silent") {
		cfg.Silent = opts.silent
	}
	if flags.Changed("log-file") {
		cfg.LogFile = opts.logFile
	}
	if flags.Changed("route") {
		routes, err := parseConfigRoutes(opts.rawRoutes)
		if err != nil {
			return config.Config{}, usageError{err}
		}
		cfg.Client, cfg.Server, cfg.APIPrefixes = 0, 0, nil
		cfg.Routes = routes
		return cfg, nil
	}
	if flags.Changed("client") {
		cfg.Client = opts.clientPort
	}
	if flags.Changed("server") {
		cfg.Server = opts.serverPort
	}
	if flags.Changed("api-prefix") {
		cfg.APIPrefixes = opts.apiPrefixes
	}
	return cfg, nil
}

func writeConfigYAML(w io.Writer, cfg config.Config) error {
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("merge-port-dry-run-%d.yaml", time.Now().UnixNano()))
	if err := config.Save(tmp, cfg, true); err != nil {
		return err
	}
	defer os.Remove(tmp)
	b, err := os.ReadFile(tmp)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

func newDiscoverCommand() *cobra.Command {
	var serverPort int
	var baseURL string
	var paths []string
	var rawHeaders []string
	var jsonOutput bool
	var writeConfig string
	var clientPort int
	var listenPort int
	var force bool
	cmd := &cobra.Command{
		Use:           "discover",
		Short:         "Detect API route prefixes from a running server",
		SilenceUsage:  true,
		SilenceErrors: true,
		Example: `  merge-port discover --server 3000
  merge-port discover --url http://localhost:5000
  merge-port discover --server 3000 --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonOutput && writeConfig != "" {
				return usageError{fmt.Errorf("--json cannot be used with --write-config")}
			}
			if (serverPort == 0) == (baseURL == "") {
				return usageError{fmt.Errorf("exactly one of --server or --url is required")}
			}
			if serverPort != 0 {
				baseURL = fmt.Sprintf("http://localhost:%d", serverPort)
			}
			headers := http.Header{}
			for _, raw := range rawHeaders {
				name, value, err := discover.ParseHeader(raw)
				if err != nil {
					return usageError{err}
				}
				headers.Add(name, value)
			}
			result, err := discover.Discover(cmd.Context(), discover.Options{BaseURL: baseURL, CandidatePaths: paths, Headers: headers, Timeout: 5 * time.Second})
			if jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if encErr := enc.Encode(result); encErr != nil {
					return encErr
				}
			}
			if err != nil {
				if jsonOutput {
					return err
				}
				return fmt.Errorf("server at %s did not return an OpenAPI spec: %w", baseURL, err)
			}
			if writeConfig != "" {
				if clientPort == 0 {
					return usageError{fmt.Errorf("--write-config requires --client")}
				}
				cfg := config.Config{Port: listenPort}
				if serverPort != 0 {
					cfg.Client = clientPort
					cfg.Server = serverPort
					cfg.APIPrefixes = result.Prefixes
				} else {
					for _, prefix := range result.Prefixes {
						cfg.Routes = append(cfg.Routes, config.Route{Prefix: prefix, Target: config.Target(baseURL)})
					}
					cfg.Routes = append(cfg.Routes, config.Route{Prefix: "/", Target: config.Target(fmt.Sprintf("%d", clientPort))})
				}
				return config.Save(writeConfig, cfg, force)
			}
			if jsonOutput {
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\nDetected from %s (%s):\n\n", baseURL, result.Source)
			for _, p := range result.Prefixes {
				fmt.Fprintf(cmd.OutOrStdout(), "  --api-prefix %s\n", p)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\nproc-compose.yaml:\n\n  api_prefixes:\n")
			for _, p := range result.Prefixes {
				fmt.Fprintf(cmd.OutOrStdout(), "    - %s\n", p)
			}
			fmt.Fprintln(cmd.OutOrStdout())
			return nil
		},
	}
	cmd.Flags().IntVar(&serverPort, "server", 0, "server port to query")
	cmd.Flags().StringVar(&baseURL, "url", "", "base URL to query")
	cmd.Flags().StringArrayVar(&paths, "openapi-path", nil, "OpenAPI path to probe (repeatable)")
	cmd.Flags().StringArrayVar(&rawHeaders, "header", nil, "HTTP header as Name: value (repeatable)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "print JSON output")
	cmd.Flags().StringVar(&writeConfig, "write-config", "", "write discovered prefixes to config file")
	cmd.Flags().IntVar(&clientPort, "client", 0, "client port for --write-config")
	cmd.Flags().IntVar(&listenPort, "port", 8080, "listen port for --write-config")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite config file for --write-config")
	cmd.MarkFlagsMutuallyExclusive("server", "url")
	return cmd
}

func newStatusCommand() *cobra.Command {
	var port int
	cmd := &cobra.Command{Use: "status", Short: "Show detached merge-port status", RunE: func(cmd *cobra.Command, args []string) error {
		if runtime.GOOS == "windows" {
			return usageError{fmt.Errorf("--detach, status, and stop are not supported on Windows yet.\nRun merge-port in the foreground on Windows.")}
		}
		path := lifecycle.StatePath(defaultStateDir(), port)
		state, err := lifecycle.Read(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				fmt.Fprintf(cmd.OutOrStdout(), "merge-port is not running on port %d\n", port)
				return nil
			}
			return err
		}
		if state.PID <= 0 {
			_ = lifecycle.Remove(path)
			fmt.Fprintf(cmd.OutOrStdout(), "merge-port is not running; removed stale state for port %d\n", port)
			return nil
		}
		exists, permission, err := processExists(state.PID)
		if err != nil && !permission {
			return err
		}
		if lifecycle.IsStartingStale(state, time.Now()) || !exists {
			_ = lifecycle.Remove(path)
			fmt.Fprintf(cmd.OutOrStdout(), "merge-port is not running; removed stale state for port %d\n", port)
			return nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), "merge-port is running")
		fmt.Fprintf(cmd.OutOrStdout(), "PID: %d\nPort: %d\nLog: %s\nStarted: %s\n", state.PID, state.Port, state.LogFile, state.StartedAt.Format(time.RFC3339))
		return nil
	}}
	cmd.Flags().IntVar(&port, "port", 8080, "port to inspect")
	return cmd
}

func newStopCommand() *cobra.Command {
	var port int
	var force bool
	cmd := &cobra.Command{Use: "stop", Short: "Stop detached merge-port", RunE: func(cmd *cobra.Command, args []string) error {
		if runtime.GOOS == "windows" {
			return usageError{fmt.Errorf("--detach, status, and stop are not supported on Windows yet.\nRun merge-port in the foreground on Windows.")}
		}
		path := lifecycle.StatePath(defaultStateDir(), port)
		state, err := lifecycle.Read(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				fmt.Fprintf(cmd.OutOrStdout(), "merge-port is not running on port %d\n", port)
				return nil
			}
			return err
		}
		if state.PID <= 0 {
			_ = lifecycle.Remove(path)
			fmt.Fprintf(cmd.OutOrStdout(), "merge-port is not running; removed stale state for port %d\n", port)
			return nil
		}
		exists, permission, err := processExists(state.PID)
		if permission {
			return fmt.Errorf("merge-port on port %d appears to be running as another user (PID %d).\nStop it from that user account or terminate it manually", port, state.PID)
		}
		if err != nil {
			return err
		}
		if !exists {
			_ = lifecycle.Remove(path)
			fmt.Fprintf(cmd.OutOrStdout(), "merge-port is not running; removed stale state for port %d\n", port)
			return nil
		}
		if err := terminateProcess(state.PID, force); err != nil {
			return err
		}
		_ = lifecycle.Remove(path)
		fmt.Fprintf(cmd.OutOrStdout(), "Stopped merge-port on port %d (PID %d)\n", port, state.PID)
		return nil
	}}
	cmd.Flags().IntVar(&port, "port", 8080, "port to stop")
	cmd.Flags().BoolVar(&force, "force", false, "force kill after graceful stop fails")
	return cmd
}

func runDetach(cmd *cobra.Command, port int, logFile string) error {
	if runtime.GOOS == "windows" {
		return usageError{fmt.Errorf("--detach, status, and stop are not supported on Windows yet.\nRun merge-port in the foreground on Windows.")}
	}
	if port == 0 {
		return usageError{fmt.Errorf("--detach cannot be used with --port 0")}
	}
	listen := preflight.CheckListenPort(port)
	if !listen.Available {
		return fmt.Errorf("listen port unavailable: %w", listen.Err)
	}
	effectiveLog := logFile
	if effectiveLog == "" {
		effectiveLog = fmt.Sprintf("%s/merge-port-%d.log", os.TempDir(), port)
	}
	statePath := lifecycle.StatePath(defaultStateDir(), port)
	if state, err := lifecycle.Read(statePath); err == nil {
		if state.PID <= 0 {
			_ = lifecycle.Remove(statePath)
		} else {
			exists, _, _ := processExists(state.PID)
			if exists {
				return fmt.Errorf("merge-port already running on port %d. Stop it with: merge-port stop --port %d", port, port)
			}
			_ = lifecycle.Remove(statePath)
		}
	}
	reserve := lifecycle.State{Port: port, LogFile: effectiveLog, StartedAt: time.Now(), Status: lifecycle.StatusStarting}
	if err := lifecycle.Reserve(statePath, reserve); err != nil {
		return err
	}
	pid, err := detachProcess(os.Args[1:], effectiveLog)
	if err != nil {
		_ = lifecycle.Remove(statePath)
		return err
	}
	state := lifecycle.State{PID: pid, Port: port, LogFile: effectiveLog, Args: os.Args[1:], StartedAt: time.Now(), Status: lifecycle.StatusRunning}
	if err := lifecycle.WriteAtomic(statePath, state); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "merge-port detached")
	fmt.Fprintf(cmd.OutOrStdout(), "PID: %d\nPort: %d\nLog: %s\nStop: merge-port stop --port %d\n", pid, port, effectiveLog, port)
	return nil
}

func defaultStateDir() string {
	home, _ := os.UserHomeDir()
	return lifecycle.StateDir(runtime.GOOS, home, os.Getenv("XDG_STATE_HOME"), os.TempDir())
}

func detachProcess(osArgs []string, logPath string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	args, err := rewriteDetachArgs(osArgs, logPath)
	if err != nil {
		return 0, err
	}
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return 0, fmt.Errorf("cannot open log file %s: %w", logPath, err)
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdin = nil
	cmd.Stdout = lf
	cmd.Stderr = lf
	setDetachAttr(cmd)
	if err := cmd.Start(); err != nil {
		lf.Close()
		return 0, fmt.Errorf("failed to start background process: %w", err)
	}
	pid := cmd.Process.Pid
	cmd.Process.Release()
	lf.Close()
	return pid, nil
}

func rewriteDetachArgs(osArgs []string, logPath string) ([]string, error) {
	args := stripFlags(osArgs, []string{"--detach", "--silent"}, []string{"--log-file"})
	var err error
	args, err = absolutizeConfigArg(args)
	if err != nil {
		return nil, err
	}
	if !hasConfigArg(args) {
		if _, statErr := os.Stat(".merge-port.yaml"); statErr == nil {
			abs, err := filepath.Abs(".merge-port.yaml")
			if err != nil {
				return nil, err
			}
			args = append(args, "--config", abs)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return nil, statErr
		}
	}
	args = append(args, "--silent", "--log-file", logPath)
	return args, nil
}

func absolutizeConfigArg(args []string) ([]string, error) {
	out := append([]string(nil), args...)
	for i := 0; i < len(out); i++ {
		if out[i] == "--config" {
			if i+1 >= len(out) {
				return nil, usageError{fmt.Errorf("--config requires a value")}
			}
			abs, err := absIfRelative(out[i+1])
			if err != nil {
				return nil, err
			}
			out[i+1] = abs
			continue
		}
		if value, ok := strings.CutPrefix(out[i], "--config="); ok {
			abs, err := absIfRelative(value)
			if err != nil {
				return nil, err
			}
			out[i] = "--config=" + abs
		}
	}
	return out, nil
}

func absIfRelative(path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	return filepath.Abs(path)
}

func hasConfigArg(args []string) bool {
	for _, arg := range args {
		if arg == "--config" || strings.HasPrefix(arg, "--config=") {
			return true
		}
	}
	return false
}

func stripFlags(args, boolFlags, valueFlags []string) []string {
	boolSet := map[string]bool{}
	for _, f := range boolFlags {
		boolSet[f] = true
	}
	valSet := map[string]bool{}
	for _, f := range valueFlags {
		valSet[f] = true
	}
	var out []string
	skip := false
	for _, a := range args {
		if skip {
			skip = false
			continue
		}
		if boolSet[a] {
			continue
		}
		matched := false
		for f := range boolSet {
			if strings.HasPrefix(a, f+"=") {
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		if valSet[a] {
			skip = true
			continue
		}
		for f := range valSet {
			if strings.HasPrefix(a, f+"=") {
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		out = append(out, a)
	}
	return out
}

func buildRoutes(clientPort, serverPort int, apiPrefixes, rawRoutes []string) ([]proxy.Route, error) {
	cfg := config.Config{Port: 8080, Client: clientPort, Server: serverPort, APIPrefixes: apiPrefixes}
	if len(rawRoutes) > 0 {
		if clientPort != 0 || serverPort != 0 || len(apiPrefixes) > 0 {
			return nil, fmt.Errorf("--route cannot be combined with --client, --server, or --api-prefix")
		}
		routes, err := parseConfigRoutes(rawRoutes)
		if err != nil {
			return nil, err
		}
		cfg.Routes = routes
	}
	return config.ToProxyRoutes(cfg)
}

func parseRouteFlags(rawRoutes []string) ([]proxy.Route, error) {
	routes, err := parseConfigRoutes(rawRoutes)
	if err != nil {
		return nil, err
	}
	cfg := config.Config{Routes: routes}
	return config.ToProxyRoutes(cfg)
}

func normalizeTarget(target string) string { return config.NormalizeTarget(target) }

func discoverPrefixes(base string) ([]string, string, error) {
	result, err := discover.Discover(context.Background(), discover.Options{BaseURL: base, Timeout: 5 * time.Second})
	return result.Prefixes, strings.TrimPrefix(result.Source, base), err
}

func tryOpenAPI(client *http.Client, rawURL string) ([]string, bool) {
	ctx := context.Background()
	base, path := splitBasePath(rawURL)
	result, err := discover.Discover(ctx, discover.Options{BaseURL: base, CandidatePaths: []string{path}, Timeout: client.Timeout})
	if err != nil {
		return nil, false
	}
	return result.Prefixes, true
}

func topPrefix(path string) string {
	trimmed := strings.TrimPrefix(path, "/")
	parts := strings.SplitN(trimmed, "/", 2)
	if parts[0] == "" {
		return "/"
	}
	return "/" + parts[0]
}

func splitBasePath(rawURL string) (string, string) {
	parsed, parseErr := http.NewRequest(http.MethodGet, rawURL, nil)
	if parseErr != nil {
		return rawURL, "/"
	}
	base := parsed.URL.Scheme + "://" + parsed.URL.Host
	return base, parsed.URL.Path
}

func hasCatchAllRoute(routes []proxy.Route) bool {
	for _, route := range routes {
		if route.Prefix == "/" {
			return true
		}
	}
	return false
}

func emitNoCatchAllWarning(w io.Writer, routes []proxy.Route) {
	if hasCatchAllRoute(routes) {
		return
	}
	fmt.Fprintln(w, "Warning: no catch-all / route configured; unmatched paths will return 404.")
	fmt.Fprintln(w, "Add --route /=3000 if you want frontend fallback routing.")
}
