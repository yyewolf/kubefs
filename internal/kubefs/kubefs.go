package kubefs

import (
	"context"
	"os"
	"strings"
	"syscall"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	_ "bazil.org/fuse/fs/fstestutil"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

func (k KubeFS) Root() (fs.Node, error) {
	return RootDir{KubeFS: &k}, nil
}

// RootDir implements both Node and Handle for the root directory.
type RootDir struct {
	KubeFS *KubeFS
}

func (RootDir) Attr(ctx context.Context, a *fuse.Attr) error {
	a.Inode = 1
	a.Mode = os.ModeDir | 0o555
	return nil
}

func (r RootDir) Lookup(ctx context.Context, name string) (fs.Node, error) {
	if ns, ok := r.KubeFS.Namespaces[name]; ok {
		return ns, nil
	}
	return nil, syscall.ENOENT
}

func (r RootDir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	dirDirs := make([]fuse.Dirent, 0, len(r.KubeFS.Namespaces))
	for name, ns := range r.KubeFS.Namespaces {
		dirDirs = append(dirDirs, fuse.Dirent{
			Inode: ns.Inode,
			Name:  name,
			Type:  fuse.DT_Dir,
		})
	}
	return dirDirs, nil
}

// Namespace implements both Node and Handle for the NS directory.
type Namespace struct {
	Inode       uint64
	Name        string
	Clusterwide bool

	Root      *KubeFS
	Resources map[string]*Resource
}

func (n *Namespace) Attr(ctx context.Context, a *fuse.Attr) error {
	a.Inode = n.Inode
	a.Mode = os.ModeDir | 0o555
	return nil
}

func (n *Namespace) Lookup(ctx context.Context, name string) (fs.Node, error) {
	if res, ok := n.Resources[name]; ok {
		return res, nil
	}
	return nil, syscall.ENOENT
}

func (n *Namespace) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	// List resources as files
	dirDirs := make([]fuse.Dirent, 0, len(n.Resources))
	for _, res := range n.Resources {
		dirDirs = append(dirDirs, fuse.Dirent{
			Inode: res.Inode,
			Name:  res.Filename(),
			Type:  fuse.DT_File,
		})
	}

	return dirDirs, nil
}

type Resource struct {
	Inode uint64

	Name                 string
	Namespace            *Namespace
	GroupVersionKind     schema.GroupVersionKind
	GroupVersionResource schema.GroupVersionResource

	Root *KubeFS
}

func (r *Resource) Filename() string {
	group := r.GroupVersionKind.Group
	if group == "" {
		group = "core"
	}
	return r.Name + "." + strings.ToLower(r.GroupVersionKind.Kind) + "." + strings.ToLower(group) + "." + r.GroupVersionKind.Version + ".yaml"
}

func (r *Resource) Attr(ctx context.Context, a *fuse.Attr) error {
	a.Inode = r.Inode
	a.Mode = 0o444
	a.Size = 1 * 1024 * 1024 // 1MB, can be adjusted as needed
	return nil
}

func (r *Resource) ReadAll(ctx context.Context) ([]byte, error) {
	var resource *unstructured.Unstructured
	var err error
	if r.Namespace.Clusterwide {
		resource, err = r.Root.client.
			Resource(r.GroupVersionResource).
			Get(ctx, r.Name, v1.GetOptions{})
	} else {
		resource, err = r.Root.client.
			Resource(r.GroupVersionResource).
			Namespace(r.Namespace.Name).
			Get(ctx, r.Name, v1.GetOptions{})
	}
	if err != nil {
		return nil, err
	}

	jsonData, err := resource.MarshalJSON()
	if err != nil {
		return nil, err
	}

	yamlData, err := yaml.JSONToYAML(jsonData)
	if err != nil {
		return nil, err
	}

	return yamlData, nil
}
