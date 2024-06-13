package hash

import (
	"hash"

	"github.com/zeebo/blake3"
)

// Hash is an alias to stdlib hash.Hash interface.
type Hash = hash.Hash

// New is an alias to blake3.New.
var New = blake3.New

// Sum computes 256-bit hash from chunks with blake3.
func Sum(chunks ...[]byte) (rst [32]byte) {
	hh := GetHasher()
	for _, chunk := range chunks {
		hh.Write(chunk)
	}
	hh.Sum(rst[:0])

	// reset the hasher and put it back in the pool
	hh.Reset()
	PutHasher(hh)
	return rst
}

func Sum20(chunks ...[]byte) (rst [20]byte) {
	hh := GetHasher()
	for _, chunk := range chunks {
		hh.Write(chunk)
	}
	hh.Digest().Read(rst[:])

	// reset the hasher and put it back in the pool
	hh.Reset()
	PutHasher(hh)
	return rst
}
