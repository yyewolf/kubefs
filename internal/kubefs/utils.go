package kubefs

import (
	"hash/fnv"

	"github.com/google/uuid"
)

func UuidToInode(u string) uint64 {
	uuid := uuid.MustParse(u)
	h := fnv.New64a()
	h.Write(uuid[:])
	return h.Sum64()
}
