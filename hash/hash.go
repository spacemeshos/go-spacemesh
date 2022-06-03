package hash

import (
	sha256 "github.com/spacemeshos/sha256-simd"
)

var (
	New = sha256.New
	Sum = sha256.Sum256
)
