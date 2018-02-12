package dht

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/btcsuite/btcutil/base58"
	"math/big"
	"math/bits"
	"sort"
)

// ID is a dht-compatible ID using the XOR keyspace.
type ID []byte

// Pretty returns a readable string of the ID.
func (id ID) Pretty() string {
	v := hex.EncodeToString(id)
	return fmt.Sprintf("DhtId: %s", v[:8])
}

// NewIDFromNodeKey creates a new dht id from provided binary data.
func NewIDFromNodeKey(key []byte) ID {
	hash := sha256.Sum256([]byte(key))
	return hash[:]
}

// NewIDFromBase58String creates a new dht ID from provided base58 encoded binary data.
func NewIDFromBase58String(s string) ID {
	key := base58.Decode(s)
	return NewIDFromNodeKey(key)
}

// NewIDFromHexString creates a new dht ID from provided hex-encoded string.
func NewIDFromHexString(s string) (ID, error) {
	data, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}
	return ID(data), nil
}

// SortByDistance ids based on xor-distance from ID.
func (id ID) SortByDistance(ids []ID) []ID {
	idsCopy := make([]ID, len(ids))
	copy(idsCopy, ids)
	bdtc := &idsByDistanceToCenter{
		Center: id,
		Ids:    idsCopy,
	}
	sort.Sort(bdtc)
	return bdtc.Ids
}

// Equals returns true iff other equals the ID.
func (id ID) Equals(other ID) bool {
	return bytes.Equal(id, other)
}

// Distance returns the distance between ID and id1 encoded as a big int.
func (id ID) Distance(id1 ID) *big.Int {
	id2 := id.Xor(id1)
	return big.NewInt(0).SetBytes(id2)
}

// Less returns true iff the binary number represented by ID is less than the number represented by o.
func (id ID) Less(o ID) bool {
	for i := 0; i < len(id); i++ {
		if id[i] != o[i] {
			return id[i] < o[i]
		}
	}
	return false
}

// Xor returns a XOR of the ID with o.
func (id ID) Xor(o ID) ID {
	return XOR(id, o)
}

// CommonPrefixLen returns the common shared prefix length in BITS of the binary numbers represented by the ids.
func (id ID) CommonPrefixLen(o ID) int {
	return id.Xor(o).ZeroPrefixLen()
}

// ZeroPrefixLen returns the zero prefix length of the binary number represnted by ID in bits.
func (id ID) ZeroPrefixLen() int {
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
func (id ID) Closer(id1 ID, id2 ID) bool {
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

// Help struct containing a list of ids sorted by the XOR distance from Center ID.
type idsByDistanceToCenter struct {
	Center ID
	Ids    []ID
}

// Len returns the number of ids contained in s.
func (s idsByDistanceToCenter) Len() int {
	return len(s.Ids)
}

// Swap swaps 2 ids in the structure
func (s idsByDistanceToCenter) Swap(i, j int) {
	s.Ids[i], s.Ids[j] = s.Ids[j], s.Ids[i]
}

// Less returns true if the ID indexed by i is closer to the center than the ID indexed by j.
func (s idsByDistanceToCenter) Less(i, j int) bool {
	a := s.Center.Distance(s.Ids[i])
	b := s.Center.Distance(s.Ids[j])
	return a.Cmp(b) == -1
}
