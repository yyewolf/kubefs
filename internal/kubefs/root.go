package kubefs

import (
	"context"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"k8s.io/client-go/dynamic"
)

type KubeFS struct {
	fs.Inode
	*dynamic.DynamicClient
}

func NewKubeFS(client *dynamic.DynamicClient) *KubeFS {
	return &KubeFS{
		DynamicClient: client,
	}
}

var _ = (fs.NodeGetattrer)((*KubeFS)(nil))

func (k *KubeFS) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = 0755
	return 0
}
