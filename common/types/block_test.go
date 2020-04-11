package types

import (
	"github.com/spacemeshos/go-spacemesh/rand"
	"testing"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func genByte32() [32]byte {
	var x [32]byte
	rand.Read(x[:])
	return x
}

var txid1 = TransactionID(genByte32())
var txid2 = TransactionID(genByte32())
var txid3 = TransactionID(genByte32())

var one = CalcHash32([]byte("1"))
var two = CalcHash32([]byte("2"))
var three = CalcHash32([]byte("3"))

var atx1 = ATXID(one)
var atx2 = ATXID(two)
var atx3 = ATXID(three)

// Make sure we can print out all the relevant log fields for a block
func TestFields(t *testing.T) {
	b := &Block{}
	b.TxIDs = []TransactionID{txid1, txid2, txid1}
	b.ATXIDs = []ATXID{atx1, atx2, atx3}

	for i := 0; i <= AtxsPerBlockLimit; i++ {
		b.ATXIDs = append(b.ATXIDs, atx1)
	}

	b.TxIDs = []TransactionID{}
	b.ATXIDs = []ATXID{}
	t.Log(b.Fields())
}
