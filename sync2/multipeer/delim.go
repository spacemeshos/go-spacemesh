package multipeer

import (
	"encoding/binary"

	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

// getDelimiters generates keys that can be used as range delimiters for splitting the key
// space among the specified number of peers. maxDepth specifies maximum number of high
// non-zero bits to include in resulting keys, which helps avoiding unaligned splits which
// are more expensive for FPTree data structure.
// keyLen specifies key length in bytes.
// The function returns numPeers-1 keys. The ranges are used for split sync, with each
// range being assigned to a separate peer. The first range begins with zero-valued key
// (k0), represented by KeyBytes of length keyLen consisting entirely of zeroes.
// The ranges to scan are:
// [k0,ks[0]); [k0,ks[1]); ... [k0,ks[numPeers-2]); [ks[numPeers-2],0).
func getDelimiters(numPeers, keyLen, maxDepth int) (ks []rangesync.KeyBytes) {
	if numPeers < 2 {
		return nil
	}
	mask := uint64(0xffffffffffffffff) << (64 - maxDepth)
	inc := (uint64(0x80) << 56) / uint64(numPeers)
	ks = make([]rangesync.KeyBytes, numPeers-1)
	for i, v := 0, uint64(0); i < numPeers-1; i++ {
		ks[i] = make(rangesync.KeyBytes, keyLen)
		v += inc
		binary.BigEndian.PutUint64(ks[i], (v<<1)&mask)
	}
	return ks
}
