package kubefs

import (
	"context"
	"sync"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
)

type KubeFS struct {
	fs.Inode
	*dynamic.DynamicClient
	DiscoveryClient discovery.DiscoveryInterface
	Config          Config
	configMu        sync.RWMutex
}

func NewKubeFS(config Config) *KubeFS {
	return &KubeFS{
		Config: config,
	}
}

func (k *KubeFS) SetConfig(config Config) {
	k.configMu.Lock()
	k.Config = config
	k.configMu.Unlock()
}

func (k *KubeFS) GetConfig() Config {
	k.configMu.RLock()
	defer k.configMu.RUnlock()
	return k.Config
}

func (k *KubeFS) IsClusterScope() bool {
	cfg := k.GetConfig()
	return cfg.Scope == ScopeCluster
}

func (k *KubeFS) AllowedNamespaces() []string {
	if k.IsClusterScope() {
		return nil
	}
	return k.GetConfig().Namespaces
}

func (k *KubeFS) AllowsNamespace(name string) bool {
	if k.IsClusterScope() {
		return true
	}
	for _, ns := range k.GetConfig().Namespaces {
		if ns == name {
			return true
		}
	}
	return false
}

var _ = (fs.NodeGetattrer)((*KubeFS)(nil))

func (k *KubeFS) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = 0755
	return 0
}
