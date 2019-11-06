package oracle

import (
	"hash/fnv"
	"math"
)

// better little duplication then little dependency

type hasherU32 struct {
}

func newHasherU32() *hasherU32 {
	h := new(hasherU32)

	return h
}

func (h *hasherU32) Hash(values ...[]byte) uint32 {
	fnv := fnv.New32()
	for _, b := range values {
		fnv.Write(b)
	}
	return fnv.Sum32()
}

func (h *hasherU32) MaxValue() uint32 {
	return math.MaxUint32
}
