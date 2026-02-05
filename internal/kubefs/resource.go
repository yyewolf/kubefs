package kubefs

import (
	"context"
	"fmt"
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
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"
)

type Resource struct {
	Name                 string
	Namespace            *Namespace
	GroupVersionKind     schema.GroupVersionKind
	GroupVersionResource schema.GroupVersionResource

	mu    sync.Mutex
	data  []byte
	dirty bool

	changes   int
	updatedAt time.Time

	fs.Inode
	*dynamic.DynamicClient
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
	fmt.Println("GETATTR")
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
	fmt.Println("Statx")
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
	fmt.Println("Write")
	if offset < 0 {
		fmt.Printf("Invalid offset: %d\n", offset)
		return 0, syscall.EINVAL
	}
	maxInt := int64(^uint(0) >> 1)
	if offset > maxInt {
		fmt.Printf("Offset too large: %d\n", offset)
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
	fmt.Println("Flush")
	return r.flush(ctx)
}

func (r *Resource) Release(ctx context.Context, fh fs.FileHandle) syscall.Errno {
	fmt.Println("Release")
	return r.flush(ctx)
}

func (r *Resource) Setattr(ctx context.Context, fh fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	if in.Valid&fuse.FATTR_SIZE != 0 {
		fmt.Println("Setattr", in.Size, in.Length, in.InHeader)

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
		fmt.Println(len(r.data), string(r.data))
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
	jsonData, err := resource.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return yaml.JSONToYAML(jsonData)
}

func (r *Resource) getResource(ctx context.Context) (*unstructured.Unstructured, error) {
	if r.Namespace.Clusterwide {
		return r.Resource(r.GroupVersionResource).Get(ctx, r.Name, v1.GetOptions{})
	}
	return r.Resource(r.GroupVersionResource).Namespace(r.Namespace.Name).Get(ctx, r.Name, v1.GetOptions{})
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
		fmt.Println("empty")
		return syscall.EINVAL
	}

	jsonData, err := yaml.YAMLToJSON(data)
	if err != nil {
		fmt.Println(err)
		return syscall.EINVAL
	}

	obj := &unstructured.Unstructured{}
	if err := obj.UnmarshalJSON(jsonData); err != nil {
		fmt.Println(err)
		return syscall.EINVAL
	}

	if obj.GroupVersionKind().Empty() {
		obj.SetGroupVersionKind(r.GroupVersionKind)
	}

	if obj.GetName() == "" {
		obj.SetName(r.Name)
	} else if obj.GetName() != r.Name {
		fmt.Printf("Name mismatch: expected %s, got %s\n", r.Name, obj.GetName())
		return syscall.EINVAL
	}

	if r.Namespace.Clusterwide {
		obj.SetNamespace("")
	} else {
		if obj.GetNamespace() == "" {
			obj.SetNamespace(r.Namespace.Name)
		} else if obj.GetNamespace() != r.Namespace.Name {
			fmt.Printf("Namespace mismatch: expected %s, got %s\n", r.Namespace.Name, obj.GetNamespace())
			return syscall.EINVAL
		}
	}

	var updateErr error
	if r.Namespace.Clusterwide {
		_, updateErr = r.Resource(r.GroupVersionResource).Update(ctx, obj, v1.UpdateOptions{})
	} else {
		_, updateErr = r.Resource(r.GroupVersionResource).Namespace(r.Namespace.Name).Update(ctx, obj, v1.UpdateOptions{})
	}
	if updateErr != nil {
		if apierrors.IsNotFound(updateErr) {
			if r.Namespace.Clusterwide {
				_, updateErr = r.Resource(r.GroupVersionResource).Create(ctx, obj, v1.CreateOptions{})
			} else {
				_, updateErr = r.Resource(r.GroupVersionResource).Namespace(r.Namespace.Name).Create(ctx, obj, v1.CreateOptions{})
			}
		}
	}

	if updateErr == nil {
		fmt.Printf("Successfully applied %s/%s\n", r.GroupVersionKind.String(), r.Name)
		return 0
	}
	if apierrors.IsForbidden(updateErr) {
		fmt.Printf("Forbidden: %v\n", updateErr)
		return syscall.EACCES
	}
	if apierrors.IsInvalid(updateErr) {
		fmt.Printf("Invalid: %v\n", updateErr)
		return syscall.EINVAL
	}

	fmt.Printf("Error applying resource: %v\n", updateErr)
	return syscall.EIO
}
