package hash

import "github.com/minio/sha256-simd"

const (
	// Size is an alias to minio sha256.Size (32 bytes).
	Size = sha256.Size
)

var (
	// New is an alias to minio sha256.New.
	New = sha256.New
	// Sum is an lias to minio sha256.Sum256.
	Sum = sha256.Sum256
)
