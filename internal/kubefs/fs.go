package kubefs

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type KubeFS struct {
	Namespaces map[string]*Namespace

	InodeCounter uint64

	client *dynamic.DynamicClient
}

func (k *KubeFS) EnsureNamespace(name string, clusterwide bool) {
	if _, ok := k.Namespaces[name]; ok {
		return
	}
	k.InodeCounter++
	ns := &Namespace{
		Inode:       k.InodeCounter + 1, // Start from 2, as 1 is reserved for root
		Name:        name,
		Clusterwide: clusterwide,
		Root:        k,
		Resources:   make(map[string]*Resource),
	}
	k.Namespaces[name] = ns
}

func (k *KubeFS) DeleteNamespace(name string) {
	if _, ok := k.Namespaces[name]; !ok {
		return
	}
	delete(k.Namespaces, name)
}

func (k *KubeFS) AddResource(name string, plural string, namespace string, gvk schema.GroupVersionKind) {
	if namespace == "" {
		namespace = "clusterwide"
	}

	k.EnsureNamespace(namespace, namespace == "clusterwide")

	ns, ok := k.Namespaces[namespace]
	if !ok {
		return
	}
	if _, exists := ns.Resources[name]; exists {
		return
	}
	k.InodeCounter++
	res := &Resource{
		Inode:                k.InodeCounter,
		Name:                 name,
		Namespace:            ns,
		GroupVersionKind:     gvk,
		GroupVersionResource: gvk.GroupVersion().WithResource(plural),

		Root: k,
	}

	if ns.Resources == nil {
		ns.Resources = make(map[string]*Resource)
	}

	ns.Resources[res.Filename()] = res
}

func (k *KubeFS) DeleteResource(name string, namespace string, gvk schema.GroupVersionKind) {
	if namespace == "" {
		namespace = "clusterwide"
	}

	ns, ok := k.Namespaces[namespace]
	if !ok {
		return
	}

	res := &Resource{
		Inode:            k.InodeCounter,
		Name:             name,
		Namespace:        ns,
		GroupVersionKind: gvk,
	}

	delete(ns.Resources, res.Filename())
}
