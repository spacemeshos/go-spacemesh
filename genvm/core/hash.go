package core

import (
	"github.com/spacemeshos/go-spacemesh/hash"
)

// Hash bytes into sha256 hash.
func Hash(bufs ...[]byte) Hash32 {
	hasher := hash.New()
	for _, buf := range bufs {
		hasher.Write(buf)
	}
	var rst Hash32
	hasher.Sum(rst[:])
	return rst
}
