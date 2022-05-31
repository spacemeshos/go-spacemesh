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
	hasher.Sum(rst[:])
	return rst
}
