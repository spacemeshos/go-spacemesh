package opinionhash

import (
	"sync"
)

// Pool is a global OpinionHasher pool. It is meant to amortize allocations
// of OpinionHashers over time by allowing clients to reuse them.
var pool = &sync.Pool{
	New: func() any {
		return New()
	},
}

// GetHasher will get an OpinionHasher from the pool.
// It may or may not allocate a new one. Consumers are not required
// to call Reset() on the hasher before putting it back in
// the pool.
func GetHasher() *OpinionHasher {
	return pool.Get().(*OpinionHasher)
}

// PutHasher resets the hasher and puts it back to the pool.
func PutHasher(hasher *OpinionHasher) {
	hasher.Reset()
	pool.Put(hasher)
}
