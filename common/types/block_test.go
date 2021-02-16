package types

import (
	"bytes"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/rand"
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
	SetLayersPerEpoch(3)
	b := &Block{}
	b.TxIDs = []TransactionID{txid1, txid2, txid1}
	b.ActiveSet = &[]ATXID{atx1, atx2, atx3}
	log.With().Info("got new block", b.Fields()...)
}

func TestStringToNodeID(t *testing.T) {
	pubkey := genByte32()
	nodeID1 := NodeID{
		Key:          util.Bytes2Hex(pubkey[:]),
		VRFPublicKey: []byte("22222"),
	}
	nodeIDStr := nodeID1.String()
	reversed, _ := StringToNodeID(nodeIDStr)
	if nodeID1.Key != reversed.Key {
		t.Errorf("Node ID deserialization Key does not match")
	}
	if !bytes.Equal(nodeID1.VRFPublicKey, nodeID1.VRFPublicKey) {
		t.Errorf("Node ID deserialization VRF Key does not match")
	}
}

func TestStringToNodeIDTooShort(t *testing.T) {
	pubkey := genByte32()
	_, err := StringToNodeID(string(pubkey[:10]))
	if err == nil {
		t.Errorf("Error should be thrown but is not")
	}
}

func TestStringToNodeIDTooLong(t *testing.T) {
	var x [129]byte
	rand.Read(x[:])
	_, err := StringToNodeID(string(x[:]))
	if err == nil {
		t.Errorf("Error should be thrown but is not")
	}
}

func TestBytesToNodeIDTooShort(t *testing.T) {
	var x [31]byte
	rand.Read(x[:])
	_, err := BytesToNodeID(x[:])
	if err == nil {
		t.Errorf("Error should be thrown but is not")
	}
}

func TestBytesToNodeIDTooLong(t *testing.T) {
	var x [65]byte
	rand.Read(x[:])
	_, err := BytesToNodeID(x[:])
	if err == nil {
		t.Errorf("Error should be thrown but is not")
	}
}
