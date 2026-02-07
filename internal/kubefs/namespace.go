package kubefs

import (
	"context"
	"fmt"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Namespace implements both Node and Handle for the NS directory.
type Namespace struct {
	Name        string
	Clusterwide bool
	KubeFS      *KubeFS

	fs.Inode
}

var _ = (fs.NodeUnlinker)((*Namespace)(nil))
var _ = (fs.NodeCreater)((*Namespace)(nil))

func (n *Namespace) Unlink(ctx context.Context, name string) syscall.Errno {
	if n.KubeFS == nil {
		return syscall.EIO
	}
	if !n.KubeFS.GetConfig().AllowDelete {
		return syscall.EPERM
	}
	child := n.GetChild(name)
	if child == nil {
		return syscall.ENOENT
	}
	resource, ok := child.Operations().(*Resource)
	if !ok {
		return syscall.EPERM
	}

	if errno := resource.deleteResource(ctx); errno != 0 {
		return errno
	}

	n.RmChild(name)
	Infof("Deleted %s", resource.logRef())
	return 0
}

func (n *Namespace) Create(ctx context.Context, name string, flags uint32, mode uint32, out *fuse.EntryOut) (*fs.Inode, fs.FileHandle, uint32, syscall.Errno) {
	Debugf("Create requested: %s/%s", n.Name, name)
	if n.KubeFS == nil || n.KubeFS.DynamicClient == nil || n.KubeFS.DiscoveryClient == nil {
		Errorf("Create failed: discovery client not ready for %s/%s", n.Name, name)
		return nil, nil, 0, syscall.EIO
	}
	if !n.KubeFS.GetConfig().AllowCreate {
		Warnf("Create blocked (allowCreate=false): %s/%s", n.Name, name)
		return nil, nil, 0, syscall.EPERM
	}
	if n.Clusterwide && !n.KubeFS.IsClusterScope() {
		return nil, nil, 0, syscall.EPERM
	}
	if !n.Clusterwide && !n.KubeFS.AllowsNamespace(n.Name) {
		return nil, nil, 0, syscall.EPERM
	}
	if n.GetChild(name) != nil {
		Warnf("Create failed: %s/%s already exists", n.Name, name)
		return nil, nil, 0, syscall.EEXIST
	}

	resourceName, kindName, groupName, version, ok := parseResourceFilename(name)
	if !ok {
		Warnf("Create failed: invalid filename %s/%s", n.Name, name)
		return nil, nil, 0, syscall.EINVAL
	}

	gvr, kind, err := n.KubeFS.ResolveResource(groupName, version, kindName)
	if err != nil {
		Warnf("Failed to resolve resource for %s: %v", name, err)
		return nil, nil, 0, syscall.EINVAL
	}
	if !n.KubeFS.AllowsResource(gvr) {
		Warnf("Create blocked by filters: %s/%s", n.Name, name)
		return nil, nil, 0, syscall.EPERM
	}

	res := &Resource{
		Name:                 resourceName,
		Namespace:            n,
		GroupVersionKind:     schema.GroupVersionKind{Group: gvr.Group, Version: gvr.Version, Kind: kind},
		GroupVersionResource: gvr,
		KubeFS:               n.KubeFS,
		updatedAt:            time.Now(),
		dirty:                true,
	}

	res.data = []byte(buildResourceSkeleton(res))

	inode := n.NewPersistentInode(ctx, res, fs.StableAttr{Mode: fuse.S_IFREG})
	n.AddChild(name, inode, false)
	out.Attr.Mode = fuse.S_IFREG | 0664
	Infof("Created %s", res.logRef())
	return inode, res, fuse.FOPEN_DIRECT_IO, 0
}

func buildResourceSkeleton(res *Resource) string {
	apiVersion := res.GroupVersionKind.Version
	if res.GroupVersionKind.Group != "" {
		apiVersion = res.GroupVersionKind.Group + "/" + res.GroupVersionKind.Version
	}

	if res.Namespace != nil && !res.Namespace.Clusterwide {
		return fmt.Sprintf("apiVersion: %s\nkind: %s\nmetadata:\n  name: %s\n  namespace: %s\n", apiVersion, res.GroupVersionKind.Kind, res.Name, res.Namespace.Name)
	}

	return fmt.Sprintf("apiVersion: %s\nkind: %s\nmetadata:\n  name: %s\n", apiVersion, res.GroupVersionKind.Kind, res.Name)
}
