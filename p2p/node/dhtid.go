package node

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"math/bits"
	"sort"

	"github.com/btcsuite/btcutil/base58"
)

// DhtID is a dht-compatible DhtID using the XOR keyspace.
type DhtID []byte

// Pretty returns a readable string of the DhtID.
func (id DhtID) Pretty() string {
	v := hex.EncodeToString(id)
	return fmt.Sprintf("DhtId: %s", v[:8])
}

func (id DhtID) String() string {
	return hex.EncodeToString(id)
}

// NewDhtID creates a new dht id from provided binary data.
func NewDhtID(key []byte) DhtID {
	hash := sha256.Sum256([]byte(key))
	return hash[:]
}

// NewDhtIDFromBase58 creates a new dht DhtID from provided base58 encoded binary data.
func NewDhtIDFromBase58(s string) DhtID {
	key := base58.Decode(s)
	return NewDhtID(key)
}

// NewDhtIDFromHex creates a new dht DhtID from provided hex-encoded string.
func NewDhtIDFromHex(s string) (DhtID, error) {
	data, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}
	return DhtID(data), nil
}

// SortByDistance ids based on xor-distance from DhtID.
func (id DhtID) SortByDistance(ids []DhtID) []DhtID {
	idsCopy := make([]DhtID, len(ids))
	copy(idsCopy, ids)
	bdtc := &idsByDistanceToCenter{
		Center: id,
		Ids:    idsCopy,
	}
	sort.Sort(bdtc)
	return bdtc.Ids
}

// Equals returns true iff other equals the DhtID.
func (id DhtID) Equals(other DhtID) bool {
	return bytes.Equal(id, other)
}

// Distance returns the distance between DhtID and id1 encoded as a big int.
func (id DhtID) Distance(id1 DhtID) *big.Int {
	id2 := id.Xor(id1)
	return big.NewInt(0).SetBytes(id2)
}

// Less returns true iff the binary number represented by DhtID is less than the number represented by o.
func (id DhtID) Less(o DhtID) bool {
	for i := 0; i < len(id); i++ {
		if id[i] != o[i] {
			return id[i] < o[i]
		}
	}
	return false
}

// Xor returns a XOR of the DhtID with o.
func (id DhtID) Xor(o DhtID) DhtID {
	return XOR(id, o)
}

// CommonPrefixLen returns the common shared prefix length in BITS of the binary numbers represented by the ids.
// tradeoff the pretty func we had with this more efficient one. (faster than libp2p and eth)
func (id DhtID) CommonPrefixLen(o DhtID) int {
	for i := 0; i < len(id); i++ {
		lz := bits.LeadingZeros8(id[i] ^ o[i])
		if lz != 8 {
			return i*8 + lz
		}
	}
	return len(id) * 8
}

// ZeroPrefixLen returns the zero prefix length of the binary number represnted by DhtID in bits.
func (id DhtID) ZeroPrefixLen() int {
	zpl := 0
	for i, b := range id {
		zpl = bits.LeadingZeros8(b)
		if zpl != 8 {
			return i*8 + zpl
		}
	}
	return len(id) * 8
}

// Closer returns true if id1 is closer to id3 than id2 using XOR arithmetic.
func (id DhtID) Closer(id1 DhtID, id2 DhtID) bool {
	dist1 := id.Xor(id1)
	dist2 := id.Xor(id2)
	return dist1.Less(dist2)
}

// XOR is a helper method used to return a byte slice which is the XOR of 2 provided byte slices.
func XOR(a, b []byte) []byte {
	c := make([]byte, len(a))
	for i := 0; i < len(a); i++ {
		c[i] = a[i] ^ b[i]
	}
	return c
}

// Help struct containing a list of ids sorted by the XOR distance from Center DhtID.
type idsByDistanceToCenter struct {
	Center DhtID
	Ids    []DhtID
}

// Len returns the number of ids contained in s.
func (s idsByDistanceToCenter) Len() int {
	return len(s.Ids)
}

// Swap swaps 2 ids in the structure
func (s idsByDistanceToCenter) Swap(i, j int) {
	s.Ids[i], s.Ids[j] = s.Ids[j], s.Ids[i]
}

// Less returns true if the DhtID indexed by i is closer to the center than the DhtID indexed by j.
func (s idsByDistanceToCenter) Less(i, j int) bool {
	a := s.Center.Distance(s.Ids[i])
	b := s.Center.Distance(s.Ids[j])
	return a.Cmp(b) == -1
}
