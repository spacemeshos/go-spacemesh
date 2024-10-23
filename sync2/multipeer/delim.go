package multipeer

import (
	"encoding/binary"

	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

func getDelimiters(numPeers, keyLen, maxDepth int) (h []rangesync.KeyBytes) {
	if numPeers < 2 {
		return nil
	}
	mask := uint64(0xffffffffffffffff) << (64 - maxDepth)
	inc := (uint64(0x80) << 56) / uint64(numPeers)
	h = make([]rangesync.KeyBytes, numPeers-1)
	for i, v := 0, uint64(0); i < numPeers-1; i++ {
		h[i] = make(rangesync.KeyBytes, keyLen)
		v += inc
		binary.BigEndian.PutUint64(h[i], (v<<1)&mask)
	}
	return h
}
