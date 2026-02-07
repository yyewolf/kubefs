package kubefs

import (
	"context"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (k *KubeFS) AddNamespace(ctx context.Context, name string, clusterwide bool) {
	inode := k.GetChild(name)
	if inode != nil {
		return
	}

	ns := &Namespace{
		Name:        name,
		Clusterwide: clusterwide,
		KubeFS:      k,
	}
	k.AddChild(name, k.NewPersistentInode(ctx, ns, fs.StableAttr{Mode: fuse.S_IFDIR}), false)
}

func (k *KubeFS) RemoveNamespace(ctx context.Context, name string) {
	inode := k.GetChild(name)
	if inode != nil {
		return
	}
	k.RmChild(name)
}

func (k *KubeFS) AddResource(ctx context.Context, name string, plural string, namespace string, gvk schema.GroupVersionKind) {
	gvr := gvk.GroupVersion().WithResource(plural)
	if !k.AllowsResource(gvr) {
		return
	}
	if namespace == "" {
		if !k.IsClusterScope() {
			return
		}
		namespace = "clusterwide"
	} else if !k.AllowsNamespace(namespace) {
		return
	}

	k.AddNamespace(ctx, namespace, namespace == "clusterwide")

	nsInode := k.GetChild(namespace)
	if nsInode == nil {
		return
	}

	ns := nsInode.Operations().(*Namespace)

	res := &Resource{
		Name:                 name,
		Namespace:            ns,
		GroupVersionKind:     gvk,
		GroupVersionResource: gvr,
		KubeFS:               k,
	}

	if child := nsInode.GetChild(res.Filename()); child != nil {
		go func() {
			child.Operations().(*Resource).changes++
			child.Operations().(*Resource).updatedAt = time.Now()

			child.NotifyContent(0, 0)
		}()
		return
	}

	nsInode.AddChild(res.Filename(), k.NewPersistentInode(ctx, res, fs.StableAttr{Mode: fuse.S_IFREG}), false)
}

func (k *KubeFS) DeleteResource(ctx context.Context, name string, plural string, namespace string, gvk schema.GroupVersionKind) {
	gvr := gvk.GroupVersion().WithResource(plural)
	if !k.AllowsResource(gvr) {
		return
	}
	if namespace == "" {
		if !k.IsClusterScope() {
			return
		}
		namespace = "clusterwide"
	} else if !k.AllowsNamespace(namespace) {
		return
	}

	res := &Resource{
		Name:                 name,
		GroupVersionKind:     gvk,
		GroupVersionResource: gvr,
		KubeFS:               k,
	}

	nsInode := k.GetChild(namespace)
	if nsInode == nil {
		return
	}

	filename := res.Filename()
	nsInode.RmChild(filename)
}
