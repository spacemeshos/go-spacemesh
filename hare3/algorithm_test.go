package hare3

import (
	"crypto/rand"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

var (
	id1 = randHash20()
	id2 = randHash20()
	id3 = randHash20()

	val1 = randHash20()
	val2 = randHash20()
	val3 = randHash20()

	values1 = []types.Hash20{val1}
	values2 = []types.Hash20{val1, val2}
	values3 = []types.Hash20{val1, val2, val3}

	rPre = NewAbsRound(0, -1)
	r0   = NewAbsRound(0, 0)
	r1   = NewAbsRound(0, 1)
	r2   = NewAbsRound(0, 2)
)

func randHash20() types.Hash20 {
	var result types.Hash20
	rand.Read(result[:])
	return result
}
