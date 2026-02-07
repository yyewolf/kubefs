package kubefs

import "testing"

func TestParseConfig_Empty(t *testing.T) {
	cfg, err := ParseConfig(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("expected default log level, got %q", cfg.LogLevel)
	}
	if cfg.Scope != ScopeCluster {
		t.Fatalf("expected default scope %q, got %q", ScopeCluster, cfg.Scope)
	}
}

func TestParseConfig_NormalizesRulesAndNamespaces(t *testing.T) {
	data := []byte("" +
		"logLevel: ''\n" +
		"scope: namespace\n" +
		"namespaces: [Dev, qa, dev]\n" +
		"allow:\n" +
		"  - apiGroups: [core, apps, apps]\n" +
		"    resources: [Pods, deployments, pods]\n" +
		"deny:\n" +
		"  - apiGroups: [apps]\n" +
		"    resources: [deployments]\n")

	cfg, err := ParseConfig(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.LogLevel != "info" {
		t.Fatalf("expected default log level, got %q", cfg.LogLevel)
	}
	if cfg.Scope != ScopeNamespace {
		t.Fatalf("expected scope %q, got %q", ScopeNamespace, cfg.Scope)
	}
	if len(cfg.Namespaces) != 2 || cfg.Namespaces[0] != "dev" || cfg.Namespaces[1] != "qa" {
		t.Fatalf("unexpected namespaces: %v", cfg.Namespaces)
	}

	if len(cfg.AllowRules) != 1 {
		t.Fatalf("expected 1 allow rule, got %d", len(cfg.AllowRules))
	}
	allow := cfg.AllowRules[0]
	if len(allow.ApiGroups) != 2 || allow.ApiGroups[0] != "apps" || allow.ApiGroups[1] != "core" {
		t.Fatalf("unexpected allow apiGroups: %v", allow.ApiGroups)
	}
	if len(allow.Resources) != 2 || allow.Resources[0] != "deployments" || allow.Resources[1] != "pods" {
		t.Fatalf("unexpected allow resources: %v", allow.Resources)
	}

	if len(cfg.DenyRules) != 1 {
		t.Fatalf("expected 1 deny rule, got %d", len(cfg.DenyRules))
	}
	deny := cfg.DenyRules[0]
	if len(deny.ApiGroups) != 1 || deny.ApiGroups[0] != "apps" {
		t.Fatalf("unexpected deny apiGroups: %v", deny.ApiGroups)
	}
	if len(deny.Resources) != 1 || deny.Resources[0] != "deployments" {
		t.Fatalf("unexpected deny resources: %v", deny.Resources)
	}
}
