package dht

import (
	"bytes"
	"crypto/sha256"
	"math/big"
	"sort"
)

// A dht-compatible ID using the XOR keyspace
type ID []byte

func (id ID) Equals(other ID) bool {
	return bytes.Equal(id, other)
}

func (id ID) Distance(id1 ID) *big.Int {
	id2 := id.Xor(id1)
	return big.NewInt(0).SetBytes(id2)
}

func (id ID) Less(o ID) bool {
	for i := 0; i < len(id); i++ {
		if id[i] != o[i] {
			return id[i] < o[i]
		}
	}
	return true
}

func (id ID) Xor(o ID) ID {
	return XOR(id, o)
}

func (id ID) CommonPrefixLen(o ID) int {
	c := id.Xor(o)
	return c.ZeroPrefixLen()
}

func (id ID) ZeroPrefixLen() int {
	for i := 0; i < len(id); i++ {
		for j := 0; j < 8; j++ {
			if (id[i]>>uint8(7-j))&0x1 != 0 {
				return i*8 + j
			}
		}
	}
	return len(id) * 8
}

// Creates a new DHT ID by hashing a node key/id
func NewIdFromNodeKey(key []byte) ID {
	hash := sha256.Sum256([]byte(key))
	return hash[:]
}

// Returns true if id1 is closer to id3 than id2 is
func Closer(id1 ID, id2 ID, id3 ID) bool {
	dist1 := id1.Xor(id3)
	dist2 := id2.Xor(id3)
	return dist1.Less(dist2)
}

func XOR(a, b []byte) []byte {
	c := make([]byte, len(a))
	for i := 0; i < len(a); i++ {
		c[i] = a[i] ^ b[i]
	}
	return c
}

// byDistanceToCenter is a type used to sort ids by proximity to a center.
type byDistanceToCenter struct {
	Center ID
	Keys   []ID
}

func (s byDistanceToCenter) Len() int {
	return len(s.Keys)
}

func (s byDistanceToCenter) Swap(i, j int) {
	s.Keys[i], s.Keys[j] = s.Keys[j], s.Keys[i]
}

func (s byDistanceToCenter) Less(i, j int) bool {
	a := s.Center.Distance(s.Keys[i])
	b := s.Center.Distance(s.Keys[j])
	return a.Cmp(b) == -1
}

// SortByDistance takes a KeySpace, a center id, and a list of ids toSort.
// It returns a new list, where the ids toSort sorted by their
// distance to the center id.
func SortByDistance(center ID, toSort []ID) []ID {

	toSortCopy := make([]ID, len(toSort))
	copy(toSortCopy, toSort)

	bdtc := &byDistanceToCenter{
		Center: center,
		Keys:   toSortCopy,
	}
	sort.Sort(bdtc)
	return bdtc.Keys
}
