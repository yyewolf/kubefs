package kubefs

import (
	"errors"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func parseResourceFilename(filename string) (name string, kind string, group string, version string, ok bool) {
	parts := strings.Split(filename, ".")
	if len(parts) < 5 {
		return "", "", "", "", false
	}
	if parts[len(parts)-1] != "yaml" {
		return "", "", "", "", false
	}
	version = strings.ToLower(strings.TrimSpace(parts[len(parts)-2]))
	group = strings.ToLower(strings.TrimSpace(parts[len(parts)-3]))
	kind = strings.ToLower(strings.TrimSpace(parts[len(parts)-4]))
	name = strings.Join(parts[:len(parts)-4], ".")
	if name == "" || kind == "" || version == "" {
		return "", "", "", "", false
	}
	if group == "core" {
		group = ""
	}
	return name, kind, group, version, true
}

func (k *KubeFS) ResolveResource(group string, version string, kind string) (schema.GroupVersionResource, string, error) {
	var empty schema.GroupVersionResource
	if k.DiscoveryClient == nil {
		return empty, "", errors.New("discovery client not configured")
	}
	group = strings.ToLower(strings.TrimSpace(group))
	version = strings.ToLower(strings.TrimSpace(version))
	kind = strings.ToLower(strings.TrimSpace(kind))
	if group == "core" {
		group = ""
	}

	gv := version
	if group != "" {
		gv = group + "/" + version
	}

	resourceList, err := k.DiscoveryClient.ServerResourcesForGroupVersion(gv)
	if err != nil {
		return empty, "", err
	}

	for _, resource := range resourceList.APIResources {
		if strings.Contains(resource.Name, "/") {
			continue
		}
		if strings.EqualFold(resource.Kind, kind) {
			return schema.GroupVersionResource{Group: group, Version: version, Resource: resource.Name}, resource.Kind, nil
		}
	}

	return empty, "", errors.New("resource kind not found")
}
