package hash

import (
	"hash"

	"github.com/minio/sha256-simd"
)

const (
	// Size is an alias to minio sha256.Size (32 bytes).
	Size = sha256.Size
)

// Hash is an alias to stdlib hash.Hash interface.
type Hash = hash.Hash

// New is an alias to minio sha256.New.
var New = sha256.New

// Sum computes sha256.Sum256 from chunks with minio/sha256-simd.
func Sum(chunks ...[]byte) (rst [32]byte) {
	hh := New()
	for _, chunk := range chunks {
		hh.Write(chunk)
	}
	hh.Sum(rst[:0])
	return rst
}
