package kubefs

import (
	"context"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
)

// Namespace implements both Node and Handle for the NS directory.
type Namespace struct {
	Name        string
	Clusterwide bool
	KubeFS      *KubeFS

	fs.Inode
}

var _ = (fs.NodeUnlinker)((*Namespace)(nil))

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
