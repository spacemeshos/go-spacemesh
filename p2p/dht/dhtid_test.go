package dht

import (
	"encoding/hex"
	"github.com/spacemeshos/go-spacemesh/assert"
	"math/big"
	"testing"
)

func TestIds(t *testing.T) {
	id1 := NewIdFromBase58String("28Ru2rajv7ZQZ63mHLAcGrZgtG2kEAhKYP53Fp6fFs3At")
	s := id1.Pretty()
	assert.True(t, len(s) > 0, "expected dht id")

	_, err := NewIdFromHexString("xxxx")
	assert.Err(t, err, "expected error")
}

func TestSorting(t *testing.T) {
	id1, _ := NewIdFromHexString("aa726a40a408ff9fbdf627373cab566742114e2fd909eb4af4b6cbec67d6c604")
	id2, _ := NewIdFromHexString("aa726a40a408ff9fbdf627373cab566742114e2fd909eb4af4b6cbec67d6c604")
	id3, _ := NewIdFromHexString("aa826a40a408ff9fbdf627373cab566742114e2fd909eb4af4b6cbec67d6c604")
	id4, _ := NewIdFromHexString("bb826a40a408ff9fbdf627373cab566742114e2fd909eb4af4b6cbec67d6c604")
	id5, _ := NewIdFromHexString("1000000000000000000000000000000000000000000000000000000000000000")

	ids := []ID{id5, id4, id3, id2}

	sorted := id1.SortByDistance(ids)
	assert.Equal(t, len(sorted), len(ids), "expected equal length")
	for i := 0; i < len(sorted)-1; i++ {
		item1 := sorted[i]
		item2 := sorted[i+1]
		assert.True(t, id1.Closer(item1, item2), "unexpected soring order")
	}

	assert.False(t, id1.Less(id1), "unexpected less")
}

func TestDhtIds(t *testing.T) {

	// 256 bits hexa number
	hexData := "b726a40a408ff9fbdf627373cab566742114e2fd909eb4af4b6cbec67d6c6040"

	id1, _ := NewIdFromHexString(hexData)
	id2, _ := NewIdFromHexString(hexData)
	id3, _ := NewIdFromHexString("a726a40a408ff9fbdf627373cab566742114e2fd909eb4af4b6cbec67d6c6040")
	id4, _ := NewIdFromHexString("b626a40a408ff9fbdf627373cab566742114e2fd909eb4af4b6cbec67d6c6040")
	id5, _ := NewIdFromHexString("1000000000000000000000000000000000000000000000000000000000000000")

	assert.Equal(t, len(id1), 32, "Expectd 256 bits / 32 bytes id")
	assert.Equal(t, hex.EncodeToString(id1), hexData, "Unexpected id data")

	assert.True(t, id1.Equals(id2), "expected equal ids")
	assert.False(t, id1.Equals(id3), "expected non-equal ids")

	d := id1.Distance(id1)
	assert.True(t, d.Cmp(big.NewInt(0)) == 0, "expected 0 distance from same id")

	l := id1.CommonPrefixLen(id1)
	assert.Equal(t, l, 256, "expected 256 cpl for id with itself")

	l = id1.CommonPrefixLen(id3)
	assert.Equal(t, l, 3, "expected cpl == 3 bits")

	// cpl(b7... ,b6...) == 7
	l = id1.CommonPrefixLen(id4)
	assert.Equal(t, l, 7, "expected cpl == 7 bits")

	l = id4.CommonPrefixLen(id1)
	assert.Equal(t, l, 7, "expected cpl == 7 bits")

	// test less
	assert.True(t, id3.Less(id1), "expected id3 less than id1")
	assert.False(t, id1.Less(id3), "expected id3 less than id1")

	// test closer
	assert.True(t, id1.Closer(id4, id3), "expected id1 closer to id4 than to id3")
	assert.False(t, id1.Closer(id3, id4), "expected id1 closer to id4 than to id3")

	// test xor - should be 0001 == 0x1
	xorId := id1.Xor(id3)
	assert.True(t, xorId.Equals(id5), "Unexpected xor result")

	// todo: test ID.sortByDistance
}
