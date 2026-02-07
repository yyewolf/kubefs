package kubefs

import (
	"context"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

type Resource struct {
	Name                 string
	Namespace            *Namespace
	GroupVersionKind     schema.GroupVersionKind
	GroupVersionResource schema.GroupVersionResource
	KubeFS               *KubeFS

	mu    sync.Mutex
	data  []byte
	dirty bool

	changes   int
	updatedAt time.Time

	fs.Inode
}

func (r *Resource) Filename() string {
	group := r.GroupVersionKind.Group
	if group == "" {
		group = "core"
	}
	return r.Name + "." + strings.ToLower(r.GroupVersionKind.Kind) + "." + strings.ToLower(group) + "." + r.GroupVersionKind.Version + ".yaml"
}

var _ = (fs.NodeGetattrer)((*Resource)(nil))
var _ = (fs.FileStatxer)((*Resource)(nil))
var _ = (fs.NodeOpener)((*Resource)(nil))
var _ = (fs.NodeReader)((*Resource)(nil))
var _ = (fs.NodeWriter)((*Resource)(nil))
var _ = (fs.NodeSetattrer)((*Resource)(nil))
var _ = (fs.NodeFlusher)((*Resource)(nil))
var _ = (fs.NodeReleaser)((*Resource)(nil))

func (r *Resource) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	Tracef("Getattr %s", r.Filename())
	out.Mode = fuse.S_IFREG | 0664
	out.Uid = uint32(1000)
	out.Gid = uint32(1000)

	out.Mtime = uint64(r.updatedAt.UnixNano())
	out.Ctime = out.Mtime
	out.Atime = out.Mtime

	out.Size = 1024*1024 + uint64(r.changes)
	return 0
}

func (r *Resource) Statx(ctx context.Context, flags uint32, mask uint32, out *fuse.StatxOut) syscall.Errno {
	Tracef("Statx %s", r.Filename())
	out.Mode = fuse.S_IFREG | 0664
	out.Uid = uint32(1000)
	out.Gid = uint32(1000)

	out.Mtime = fuse.SxTime{Sec: uint64(r.updatedAt.Unix()), Nsec: uint32(r.updatedAt.Nanosecond())}
	out.Ctime = out.Mtime
	out.Atime = out.Mtime

	out.Size = 1024*1024 + uint64(r.changes)
	return 0
}

func (r *Resource) Open(ctx context.Context, flags uint32) (fh fs.FileHandle, fuseFlags uint32, errno syscall.Errno) {
	r.mu.Lock()
	if r.data == nil || !r.dirty {
		data, err := r.fetchYAML(ctx)
		if err != nil {
			r.mu.Unlock()
			return nil, 0, syscall.EACCES
		}
		r.data = data
	}
	r.mu.Unlock()
	return r, fuse.FOPEN_DIRECT_IO, fs.OK
}

func (r *Resource) Read(ctx context.Context, _ fs.FileHandle, dest []byte, offset int64) (fuse.ReadResult, syscall.Errno) {
	r.mu.Lock()
	resp := r.data
	if resp == nil || !r.dirty {
		data, err := r.fetchYAML(ctx)
		if err != nil {
			r.mu.Unlock()
			return nil, syscall.EACCES
		}
		r.data = data
		resp = r.data
	}
	r.mu.Unlock()

	if offset > int64(len(resp)) {
		return fuse.ReadResultData(nil), 0
	}

	resp = resp[offset:]
	if len(dest) < len(resp) {
		resp = resp[:len(dest)]
	}

	copy(dest, resp)
	dest = dest[:len(resp)]
	return fuse.ReadResultData(dest), 0
}

func (r *Resource) Write(ctx context.Context, fh fs.FileHandle, data []byte, offset int64) (uint32, syscall.Errno) {
	Tracef("Write %s offset=%d size=%d", r.Filename(), offset, len(data))
	if offset < 0 {
		Warnf("Invalid offset for %s: %d", r.Filename(), offset)
		return 0, syscall.EINVAL
	}
	maxInt := int64(^uint(0) >> 1)
	if offset > maxInt {
		Warnf("Offset too large for %s: %d", r.Filename(), offset)
		return 0, syscall.EINVAL
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	end := int(offset) + len(data)
	if end > len(r.data) {
		newData := make([]byte, end)
		copy(newData, r.data)
		r.data = newData
	}
	copy(r.data[offset:], data)
	r.dirty = true
	r.changes++
	return uint32(len(data)), 0
}

func (r *Resource) Flush(ctx context.Context, fh fs.FileHandle) syscall.Errno {
	Tracef("Flush %s", r.Filename())
	return r.flush(ctx)
}

func (r *Resource) Release(ctx context.Context, fh fs.FileHandle) syscall.Errno {
	Tracef("Release %s", r.Filename())
	return r.flush(ctx)
}

func (r *Resource) Setattr(ctx context.Context, fh fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	if in.Valid&fuse.FATTR_SIZE != 0 {
		Tracef("Setattr %s size=%d", r.Filename(), in.Size)

		maxInt := int64(^uint(0) >> 1)
		if in.Size > uint64(maxInt) {
			return syscall.EINVAL
		}
		newSize := int(in.Size)
		r.mu.Lock()
		if newSize < len(r.data) {
			r.data = r.data[:newSize]
		} else if newSize > len(r.data) {
			newData := make([]byte, newSize)
			copy(newData, r.data)
			r.data = newData
		}
		Tracef("Setattr %s newSize=%d", r.Filename(), len(r.data))
		r.dirty = true
		r.mu.Unlock()
	}

	return r.Getattr(ctx, fh, out)
}

func (r *Resource) fetchYAML(ctx context.Context) ([]byte, error) {
	resource, err := r.getResource(ctx)
	if err != nil {
		return nil, err
	}
	r.maybeStripManagedFields(resource)
	jsonData, err := resource.MarshalJSON()
	if err != nil {
		return nil, err
	}
	Debugf("Fetched %s", r.logRef())
	return yaml.JSONToYAML(jsonData)
}

func (r *Resource) getResource(ctx context.Context) (*unstructured.Unstructured, error) {
	client := r.KubeFS.DynamicClient
	if r.Namespace.Clusterwide {
		return client.Resource(r.GroupVersionResource).Get(ctx, r.Name, v1.GetOptions{})
	}
	return client.Resource(r.GroupVersionResource).Namespace(r.Namespace.Name).Get(ctx, r.Name, v1.GetOptions{})
}

func (r *Resource) flush(ctx context.Context) syscall.Errno {
	r.mu.Lock()
	if !r.dirty {
		r.mu.Unlock()
		return 0
	}
	data := make([]byte, len(r.data))
	copy(data, r.data)
	r.dirty = false
	r.mu.Unlock()

	go func() {
		time.Sleep(20 * time.Millisecond)
		r.WriteCache(0, data)
	}()

	return r.applyYAML(ctx, data)
}

func (r *Resource) applyYAML(ctx context.Context, data []byte) syscall.Errno {
	if len(data) == 0 {
		Warnf("Empty write for %s", r.logRef())
		return syscall.EINVAL
	}

	jsonData, err := yaml.YAMLToJSON(data)
	if err != nil {
		Errorf("Invalid YAML for %s: %v", r.logRef(), err)
		return syscall.EINVAL
	}

	obj := &unstructured.Unstructured{}
	if err := obj.UnmarshalJSON(jsonData); err != nil {
		Errorf("Invalid JSON for %s: %v", r.logRef(), err)
		return syscall.EINVAL
	}
	r.maybeStripManagedFields(obj)

	if obj.GroupVersionKind().Empty() {
		obj.SetGroupVersionKind(r.GroupVersionKind)
	}

	if obj.GetName() == "" {
		obj.SetName(r.Name)
	} else if obj.GetName() != r.Name {
		Warnf("Name mismatch for %s: expected %s, got %s", r.logRef(), r.Name, obj.GetName())
		return syscall.EINVAL
	}

	if r.Namespace.Clusterwide {
		obj.SetNamespace("")
	} else {
		if obj.GetNamespace() == "" {
			obj.SetNamespace(r.Namespace.Name)
		} else if obj.GetNamespace() != r.Namespace.Name {
			Warnf("Namespace mismatch for %s: expected %s, got %s", r.logRef(), r.Namespace.Name, obj.GetNamespace())
			return syscall.EINVAL
		}
	}

	var updateErr error
	client := r.KubeFS.DynamicClient
	if r.Namespace.Clusterwide {
		_, updateErr = client.Resource(r.GroupVersionResource).Update(ctx, obj, v1.UpdateOptions{})
	} else {
		_, updateErr = client.Resource(r.GroupVersionResource).Namespace(r.Namespace.Name).Update(ctx, obj, v1.UpdateOptions{})
	}
	if updateErr != nil {
		if apierrors.IsNotFound(updateErr) {
			if r.Namespace.Clusterwide {
				_, updateErr = client.Resource(r.GroupVersionResource).Create(ctx, obj, v1.CreateOptions{})
			} else {
				_, updateErr = client.Resource(r.GroupVersionResource).Namespace(r.Namespace.Name).Create(ctx, obj, v1.CreateOptions{})
			}
		}
	}

	if updateErr == nil {
		Infof("Applied %s", r.logRef())
		return 0
	}
	if apierrors.IsForbidden(updateErr) {
		Errorf("Forbidden applying %s: %v", r.logRef(), updateErr)
		return syscall.EACCES
	}
	if apierrors.IsInvalid(updateErr) {
		Errorf("Invalid resource %s: %v", r.logRef(), updateErr)
		return syscall.EINVAL
	}

	Errorf("Error applying %s: %v", r.logRef(), updateErr)
	return syscall.EIO
}

func (r *Resource) deleteResource(ctx context.Context) syscall.Errno {
	if r.KubeFS == nil || r.KubeFS.DynamicClient == nil {
		return syscall.EIO
	}
	client := r.KubeFS.DynamicClient
	var err error
	if r.Namespace.Clusterwide {
		err = client.Resource(r.GroupVersionResource).Delete(ctx, r.Name, v1.DeleteOptions{})
	} else {
		err = client.Resource(r.GroupVersionResource).Namespace(r.Namespace.Name).Delete(ctx, r.Name, v1.DeleteOptions{})
	}
	if err == nil || apierrors.IsNotFound(err) {
		return 0
	}
	if apierrors.IsForbidden(err) {
		Errorf("Forbidden deleting %s: %v", r.logRef(), err)
		return syscall.EACCES
	}
	if apierrors.IsInvalid(err) {
		Errorf("Invalid delete for %s: %v", r.logRef(), err)
		return syscall.EINVAL
	}
	Errorf("Error deleting %s: %v", r.logRef(), err)
	return syscall.EIO
}

func (r *Resource) maybeStripManagedFields(obj *unstructured.Unstructured) {
	if obj == nil || r.shouldShowManagedFields() {
		return
	}
	unstructured.RemoveNestedField(obj.Object, "metadata", "managedFields")
}

func (r *Resource) shouldShowManagedFields() bool {
	if r.KubeFS == nil {
		return DefaultConfig().ShowManagedFields
	}
	return r.KubeFS.GetConfig().ShowManagedFields
}

func (r *Resource) logRef() string {
	if r.Namespace == nil {
		return r.GroupVersionKind.String() + "/" + r.Name
	}
	if r.Namespace.Clusterwide {
		return r.GroupVersionKind.String() + "/" + r.Name
	}
	return r.GroupVersionKind.String() + "/" + r.Namespace.Name + "/" + r.Name
}
