package hash

import "github.com/minio/sha256-simd"

const (
	// Size is an alias to minio sha256.Size (32 bytes).
	Size = sha256.Size
)

var (
	// New is an alias to minio sha256.New.
	New = sha256.New
)

// Sum computes sha256.Sum256 from chunks with minio/sha256-simd.
func Sum(chunks ...[]byte) (rst [32]byte) {
	hh := New()
	for _, chunk := range chunks {
		hh.Write(chunk)
	}
	hh.Sum(rst[:0])
	return rst
}
