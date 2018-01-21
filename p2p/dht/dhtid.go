package dht

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/btcsuite/btcutil/base58"
	"math/big"
	"sort"
)

// A dht-compatible ID using the XOR keyspace
type ID []byte

func (id ID) Pretty() string {
	v := hex.EncodeToString(id)
	return fmt.Sprintf("DhtId: %s", v[:8])
}

func NewIdFromNodeKey(key []byte) ID {
	hash := sha256.Sum256([]byte(key))
	return hash[:]
}

func NewIdFromBase58String(s string) ID {
	key := base58.Decode(s)
	return NewIdFromNodeKey(key)
}

func NewIdFromHexString(s string) (ID, error) {
	data, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}
	return ID(data), nil
}

// Sort ids based on distance from id
func (id ID) SortByDistance(ids []ID) []ID {
	idsCopy := make([]ID, len(ids))
	copy(idsCopy, ids)
	bdtc := &byDistanceToCenter{
		Center: id,
		Ids:    idsCopy,
	}
	sort.Sort(bdtc)
	return bdtc.Ids
}

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
	return false
}

func (id ID) Xor(o ID) ID {
	return XOR(id, o)
}

// Common shared prefix length in bits
func (id ID) CommonPrefixLen(o ID) int {
	return id.Xor(o).ZeroPrefixLen()
}

// Zero prefix length in bits
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

// Returns true if id1 is closer to id3 than id2
func (id ID) Closer(id1 ID, id2 ID) bool {
	dist1 := id.Xor(id1)
	dist2 := id.Xor(id2)
	return dist1.Less(dist2)
}

func XOR(a, b []byte) []byte {
	c := make([]byte, len(a))
	for i := 0; i < len(a); i++ {
		c[i] = a[i] ^ b[i]
	}
	return c
}

type byDistanceToCenter struct {
	Center ID
	Ids    []ID
}

func (s byDistanceToCenter) Len() int {
	return len(s.Ids)
}

func (s byDistanceToCenter) Swap(i, j int) {
	s.Ids[i], s.Ids[j] = s.Ids[j], s.Ids[i]
}

func (s byDistanceToCenter) Less(i, j int) bool {
	a := s.Center.Distance(s.Ids[i])
	b := s.Center.Distance(s.Ids[j])
	return a.Cmp(b) == -1
}
