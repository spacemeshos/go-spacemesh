package core

import (
	"crypto/sha256"

	"github.com/spacemeshos/go-scale"
)

func Hash(bufs ...[]byte) scale.Bytes32 {
	hasher := sha256.New()
	for _, buf := range bufs {
		hasher.Write(buf)
	}
	var rst scale.Bytes32
	r1 := hasher.Sum(nil)
	copy(rst[:], r1)
	return rst
}
