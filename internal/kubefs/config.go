package kubefs

import (
	"bytes"
	"os"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

type Config struct {
	LogLevel          string   `yaml:"logLevel"`
	Scope             string   `yaml:"scope"`
	Namespaces        []string `yaml:"namespaces"`
	ShowManagedFields bool     `yaml:"showManagedFields"`
}

const (
	ScopeCluster   = "cluster"
	ScopeNamespace = "namespace"
)

func DefaultConfig() Config {
	return Config{
		LogLevel:          "info",
		Scope:             ScopeCluster,
		ShowManagedFields: false,
	}
}

func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}

	if len(bytes.TrimSpace(data)) == 0 {
		return cfg, nil
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}

	cfg = normalizeConfig(cfg)

	return cfg, nil
}

func normalizeConfig(cfg Config) Config {
	defaultCfg := DefaultConfig()

	if strings.TrimSpace(cfg.LogLevel) == "" {
		cfg.LogLevel = defaultCfg.LogLevel
	}

	scope := strings.ToLower(strings.TrimSpace(cfg.Scope))
	if scope != ScopeCluster && scope != ScopeNamespace {
		scope = defaultCfg.Scope
	}
	cfg.Scope = scope

	cfg.Namespaces = normalizeNamespaces(cfg.Namespaces)

	return cfg
}

func normalizeNamespaces(namespaces []string) []string {
	if len(namespaces) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(namespaces))
	result := make([]string, 0, len(namespaces))
	for _, ns := range namespaces {
		ns = strings.ToLower(strings.TrimSpace(ns))
		if ns == "" {
			continue
		}
		if _, exists := seen[ns]; exists {
			continue
		}
		seen[ns] = struct{}{}
		result = append(result, ns)
	}

	sort.Strings(result)
	return result
}
