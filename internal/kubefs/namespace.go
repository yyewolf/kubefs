package kubefs

import (
	"github.com/hanwen/go-fuse/v2/fs"
	"k8s.io/client-go/dynamic"
)

// Namespace implements both Node and Handle for the NS directory.
type Namespace struct {
	Name        string
	Clusterwide bool

	fs.Inode
	*dynamic.DynamicClient
}
