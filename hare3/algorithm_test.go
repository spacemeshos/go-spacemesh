package hare3

import (
	"crypto/rand"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

var (
	id1 = randID()
	id2 = randID()
	id3 = randID()

	val1 = randHash20()
	val2 = randHash20()
	val3 = randHash20()

	msgHash1 = randHash20()

	values1 = sortHash20([]types.Hash20{val1})
	values2 = sortHash20([]types.Hash20{val1, val2})
	values3 = sortHash20([]types.Hash20{val1, val2, val3})

	rPre = NewAbsRound(0, -1)
	r0   = NewAbsRound(0, 0)
	r1   = NewAbsRound(0, 1)
	r2   = NewAbsRound(0, 2)
	r3   = NewAbsRound(0, 3)
	r4   = NewAbsRound(0, 4)
)

func randID() types.NodeID {
	var result types.NodeID
	rand.Read(result[:])
	return result
}

func randHash20() types.Hash20 {
	var result types.Hash20
	rand.Read(result[:])
	return result
}
