package kubefs

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestAllowsResource_DefaultAllow(t *testing.T) {
	kfs := NewKubeFS(Config{Scope: ScopeCluster})
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}

	if !kfs.AllowsResource(gvr) {
		t.Fatalf("expected resource to be allowed when no allow/deny rules are set")
	}
}

func TestAllowsResource_AllowRulesWithCoreGroup(t *testing.T) {
	cfg := Config{
		Scope: ScopeCluster,
		AllowRules: []FilterRule{
			{ApiGroups: []string{"core"}, Resources: []string{"pods"}},
		},
	}
	kfs := NewKubeFS(cfg)

	allowed := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	denied := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}

	if !kfs.AllowsResource(allowed) {
		t.Fatalf("expected core pods to be allowed")
	}
	if kfs.AllowsResource(denied) {
		t.Fatalf("expected core services to be denied when allow rules are set")
	}
}

func TestAllowsResource_DenyOverridesAllow(t *testing.T) {
	cfg := Config{
		Scope: ScopeCluster,
		AllowRules: []FilterRule{
			{ApiGroups: []string{"core"}, Resources: []string{"pods", "services"}},
		},
		DenyRules: []FilterRule{
			{ApiGroups: []string{"core"}, Resources: []string{"services"}},
		},
	}
	kfs := NewKubeFS(cfg)

	allowed := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	denied := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}

	if !kfs.AllowsResource(allowed) {
		t.Fatalf("expected core pods to be allowed")
	}
	if kfs.AllowsResource(denied) {
		t.Fatalf("expected core services to be denied by deny rules")
	}
}

func TestNormalizeGroups_CoreLiteral(t *testing.T) {
	groups := normalizeGroups([]string{"core", "apps", "core", ""})
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if groups[0] != "apps" || groups[1] != "core" {
		t.Fatalf("unexpected groups: %v", groups)
	}
}
