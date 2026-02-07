package kubefs

import (
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

type FilterRule struct {
	ApiGroups []string `yaml:"apiGroups" json:"apiGroups"`
	Resources []string `yaml:"resources" json:"resources"`
}

func (k *KubeFS) AllowsResource(gvr schema.GroupVersionResource) bool {
	cfg := k.GetConfig()
	allowed := matchesAnyRule(cfg.AllowRules, gvr)
	if len(cfg.AllowRules) == 0 {
		allowed = true
	}
	if matchesAnyRule(cfg.DenyRules, gvr) {
		return false
	}
	return allowed
}

func matchesAnyRule(rules []FilterRule, gvr schema.GroupVersionResource) bool {
	for _, rule := range rules {
		if ruleMatches(rule, gvr) {
			return true
		}
	}
	return false
}

func ruleMatches(rule FilterRule, gvr schema.GroupVersionResource) bool {
	if len(rule.ApiGroups) > 0 && !matchGroup(rule.ApiGroups, gvr.Group) {
		return false
	}
	if len(rule.Resources) > 0 && !matchValue(rule.Resources, gvr.Resource) {
		return false
	}
	return true
}

func matchGroup(groups []string, group string) bool {
	if len(groups) == 0 {
		return true
	}
	group = strings.ToLower(strings.TrimSpace(group))
	if group == "" {
		group = "core"
	}
	return matchValue(groups, group)
}

func matchValue(values []string, value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range values {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate == "*" || candidate == value {
			return true
		}
	}
	return false
}
