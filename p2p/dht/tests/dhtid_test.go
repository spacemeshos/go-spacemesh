package tests

import (
	"encoding/hex"
	"github.com/UnrulyOS/go-unruly/assert"
	//"github.com/UnrulyOS/go-unruly/p2p"
	"github.com/UnrulyOS/go-unruly/p2p/dht"
	"math/big"
	"testing"
)

func TestDhtIds(t *testing.T) {

	hexData := "b726a40a408ff9fbdf627373cab566742114e2fd909eb4af4b6cbec67d6c6040"

	id1 := dht.NewIdFromHexString(hexData)
	id2 := dht.NewIdFromHexString(hexData)
	id3 := dht.NewIdFromHexString("a726a40a408ff9fbdf627373cab566742114e2fd909eb4af4b6cbec67d6c6040")
	id4 := dht.NewIdFromHexString("b626a40a408ff9fbdf627373cab566742114e2fd909eb4af4b6cbec67d6c6040")
	id5 := dht.NewIdFromHexString("1000000000000000000000000000000000000000000000000000000000000000")

	assert.Equal(t, len(id1), 32, "Expectd 256 bits / 32 bytes id")
	assert.Equal(t, hex.EncodeToString(id1), hexData, "Unexpected id data")

	assert.True(t, id1.Equals(id2), "exepected equal ids")
	assert.False(t, id1.Equals(id3), "exepected not equal ids")

	d := id1.Distance(id1)
	assert.True(t, d.Cmp(big.NewInt(0)) == 0, "expected 0 distance from same id")

	l := id1.CommonPrefixLen(id1)
	assert.Equal(t, l, 256, "expected 256 cpl for id with itself")

	l = id1.CommonPrefixLen(id3)
	assert.Equal(t, l, 3, "expected cpl == 3")

	// cpl(b7... ,b6...) == 7
	l = id1.CommonPrefixLen(id4)
	assert.Equal(t, l, 7, "expected cpl == 7")

	l = id4.CommonPrefixLen(id1)
	assert.Equal(t, l, 7, "expected cpl == 7")

	xorId := id1.Xor(id3)
	assert.True(t, xorId.Equals(id5), "Unexpected xor result")
}