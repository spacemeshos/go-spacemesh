package hash

import (
	"sync"

	"github.com/zeebo/blake3"
)

// Pool is a global blake3 hasher pool. It is meant to amortize allocations
// of blake3 hashers over time by allowing clients to reuse them.
var pool = &sync.Pool{
	New: func() any {
		return blake3.New()
	},
}

// GetHasher will get a blake3 hasher from the pool.
// It may or may not allocate a new one. Consumers are not required
// to call Reset() on the hasher before putting it back in
// the pool.
func GetHasher() *blake3.Hasher {
	return pool.Get().(*blake3.Hasher)
}

// PutHasher resets the hasher and puts it back to the pool.
func PutHasher(hasher *blake3.Hasher) {
	hasher.Reset()
	pool.Put(hasher)
}
