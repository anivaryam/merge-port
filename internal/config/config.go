package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/anivaryam/merge-port/internal/proxy"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Port        int      `yaml:"port,omitempty"`
	Client      int      `yaml:"client,omitempty"`
	Server      int      `yaml:"server,omitempty"`
	APIPrefixes []string `yaml:"api_prefixes,omitempty"`
	Routes      []Route  `yaml:"routes,omitempty"`
	Silent      bool     `yaml:"silent,omitempty"`
	LogFile     string   `yaml:"log_file,omitempty"`
}

type Route struct {
	Prefix string `yaml:"prefix"`
	Target Target `yaml:"target"`
}

type Target string

func Default() Config {
	return Config{Port: 8080, Client: 3000, Server: 3001, APIPrefixes: []string{"/api"}}
}

func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()

	var cfg Config
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, err
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	return cfg, Validate(cfg)
}

func Save(path string, cfg Config, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists; use --force to overwrite", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if err := Validate(cfg); err != nil {
		return err
	}
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}

func Validate(cfg Config) error {
	routeMode := len(cfg.Routes) > 0
	simpleMode := cfg.Client != 0 || cfg.Server != 0 || len(cfg.APIPrefixes) > 0
	if routeMode && simpleMode {
		return fmt.Errorf("routes cannot be combined with client, server, or api_prefixes")
	}
	for _, prefix := range cfg.APIPrefixes {
		if !strings.HasPrefix(prefix, "/") {
			return fmt.Errorf("api prefix must start with /: %q", prefix)
		}
		if prefix == "/" {
			return fmt.Errorf("api prefix cannot be / (use routes for full control)")
		}
	}
	for _, route := range cfg.Routes {
		if route.Prefix == "" || !strings.HasPrefix(route.Prefix, "/") {
			return fmt.Errorf("route prefix must start with /: %q", route.Prefix)
		}
		if route.Target == "" {
			return fmt.Errorf("route target is empty for prefix %q", route.Prefix)
		}
	}
	return nil
}

func ToProxyRoutes(cfg Config) ([]proxy.Route, error) {
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	if len(cfg.Routes) > 0 {
		routes := make([]proxy.Route, 0, len(cfg.Routes))
		for _, route := range cfg.Routes {
			routes = append(routes, proxy.Route{Prefix: route.Prefix, Target: NormalizeTarget(string(route.Target))})
		}
		return routes, nil
	}
	if cfg.Client == 0 {
		return nil, fmt.Errorf("--client port is required for simple mode")
	}
	if cfg.Server == 0 {
		return nil, fmt.Errorf("--server port is required for simple mode")
	}
	prefixes := cfg.APIPrefixes
	if len(prefixes) == 0 {
		prefixes = []string{"/api"}
	}
	routes := make([]proxy.Route, 0, len(prefixes)+1)
	serverTarget := fmt.Sprintf("http://localhost:%d", cfg.Server)
	for _, prefix := range prefixes {
		routes = append(routes, proxy.Route{Prefix: prefix, Target: serverTarget})
	}
	routes = append(routes, proxy.Route{Prefix: "/", Target: fmt.Sprintf("http://localhost:%d", cfg.Client)})
	return routes, nil
}

func NormalizeTarget(target string) string {
	if isAllDigits(target) {
		return fmt.Sprintf("http://localhost:%s", target)
	}
	if !strings.Contains(target, "://") {
		return "http://" + target
	}
	return target
}

func (t *Target) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("target must be a string or integer")
	}
	switch node.ShortTag() {
	case "!!str", "!!int":
		*t = Target(node.Value)
		return nil
	default:
		return fmt.Errorf("target must be a string or integer")
	}
}

func (t Target) MarshalYAML() (interface{}, error) {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: string(t), Style: yaml.DoubleQuotedStyle}, nil
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
