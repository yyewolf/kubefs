package kubefs

import (
	"bytes"
	"os"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

type Config struct {
	LogLevel          string       `yaml:"logLevel" json:"logLevel"`
	Scope             string       `yaml:"scope" json:"scope"`
	Namespaces        []string     `yaml:"namespaces" json:"namespaces"`
	AllowRules        []FilterRule `yaml:"allow" json:"allow"`
	DenyRules         []FilterRule `yaml:"deny" json:"deny"`
	AllowCreate       bool         `yaml:"allowCreate" json:"allowCreate"`
	AllowDelete       bool         `yaml:"allowDelete" json:"allowDelete"`
	ShowManagedFields bool         `yaml:"showManagedFields" json:"showManagedFields"`
}

const (
	ScopeCluster   = "cluster"
	ScopeNamespace = "namespace"
)

func DefaultConfig() Config {
	return Config{
		LogLevel:          "info",
		Scope:             ScopeCluster,
		AllowCreate:       false,
		AllowDelete:       false,
		ShowManagedFields: false,
	}
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return DefaultConfig(), err
	}

	return ParseConfig(data)
}

func ParseConfig(data []byte) (Config, error) {
	cfg := DefaultConfig()

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
	cfg.AllowRules = normalizeRules(cfg.AllowRules)
	cfg.DenyRules = normalizeRules(cfg.DenyRules)

	return cfg
}

func normalizeRules(rules []FilterRule) []FilterRule {
	if len(rules) == 0 {
		return nil
	}

	result := make([]FilterRule, 0, len(rules))
	for _, rule := range rules {
		clean := FilterRule{
			ApiGroups: normalizeGroups(rule.ApiGroups),
			Resources: normalizeValues(rule.Resources),
		}
		if len(clean.ApiGroups) == 0 && len(clean.Resources) == 0 {
			continue
		}
		result = append(result, clean)
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

func normalizeGroups(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func normalizeValues(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
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
