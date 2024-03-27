package hashsync

import (
	"sync"

	"github.com/zeebo/blake3"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// Note: we don't care too much about artificially induced collisions.
// Given that none of the synced hashes are used internally or
// propagated further down the P2P network before the actual contents
// of the objects is received and validated, most an attacker can get
// is partial sync of this node with the attacker node, which doesn't
// pose any serious threat. We could even skip additional hashing
// altogether, but let's make playing the algorithm not too easy.

type Hash32To12Xor struct{}

var _ Monoid = Hash32To12Xor{}

func (m Hash32To12Xor) Identity() any {
	return types.Hash12{}
}

func (m Hash32To12Xor) Op(b, a any) any {
	var r types.Hash12
	h1 := a.(types.Hash12)
	h2 := b.(types.Hash12)
	for n, b := range h1 {
		r[n] = b ^ h2[n]
	}
	return r
}

var hashPool = &sync.Pool{
	New: func() any {
		return blake3.New()
	},
}

func (m Hash32To12Xor) Fingerprint(v any) any {
	// Blake3's New allocates too much memory,
	// so we can't just call types.CalcHash12(h[:]) here
	// TODO: fix types.CalcHash12()
	h := v.(types.Hash32)
	var r types.Hash12
	hasher := hashPool.Get().(*blake3.Hasher)
	defer func() {
		hasher.Reset()
		hashPool.Put(hasher)
	}()
	var hashRes [32]byte
	hasher.Write(h[:])
	hasher.Sum(hashRes[:0])
	copy(r[:], hashRes[:])
	return r
}
