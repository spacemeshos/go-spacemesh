package types

import (
	"math"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/require"
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
	reversed, err := StringToNodeID(nodeIDStr)

	r := require.New(t)
	r.NoError(err, "Error converting string to NodeID")
	r.Equal(nodeID1.Key, reversed.Key, "Node ID deserialization Key does not match")
	r.Equal(nodeID1.VRFPublicKey, reversed.VRFPublicKey, "Node ID deserialization VRF Key does not match")

	// Test too short
	_, err = StringToNodeID(string(pubkey[:10]))
	r.Error(err, "Expected error converting too-short string to NodeID")

	// Test too long
	var x [129]byte
	rand.Read(x[:])
	_, err = StringToNodeID(string(x[:]))
	r.Error(err, "Expected error converting too-long string to NodeID")
}

func TestBytesToNodeID(t *testing.T) {
	pubkey := genByte32()
	nodeID1 := NodeID{
		Key:          util.Bytes2Hex(pubkey[:]),
		VRFPublicKey: []byte("222222"),
	}

	// Test correct length
	bytes := nodeID1.ToBytes()
	reversed, err := BytesToNodeID(bytes)

	r := require.New(t)
	r.NoError(err, "Error converting bytes to NodeID")
	r.Equal(nodeID1.Key, reversed.Key, "NodeID Key does not match")
	r.Equal(nodeID1.VRFPublicKey, reversed.VRFPublicKey, "NodeID VRF Key does not match")

	// Test too short
	var x [31]byte
	rand.Read(x[:])
	_, err = BytesToNodeID(x[:])
	r.Error(err, "Expected error converting too-short byte array to NodeID")

	// Test too long
	var y [65]byte
	rand.Read(y[:])
	_, err = BytesToNodeID(y[:])
	r.Error(err, "Expected error converting too-long byte array to NodeID")
}

func TestLayerIDWraparound(t *testing.T) {
	var (
		max  = NewLayerID(math.MaxUint32)
		zero LayerID
	)
	t.Run("Add", func(t *testing.T) {
		require.EqualValues(t, 1, zero.Add(1).Uint32())
		require.Panics(t, func() {
			max.Add(1)
		})
		require.Panics(t, func() {
			LayerID{}.Add(math.MaxUint32 - 2).Add(math.MaxUint32 - 3)
		})
	})
	t.Run("Sub", func(t *testing.T) {
		require.EqualValues(t, 0, zero.Add(1).Sub(1).Uint32())
		require.Panics(t, func() {
			zero.Sub(1)
		})
		require.Panics(t, func() {
			LayerID{}.Add(math.MaxUint32 - 2).Sub(math.MaxUint32 - 1)
		})
	})
	t.Run("Mul", func(t *testing.T) {
		require.EqualValues(t, 0, zero.Mul(1).Uint32())
		require.EqualValues(t, 0, LayerID{}.Add(1).Mul(0).Uint32())
		require.EqualValues(t, 4, LayerID{}.Add(2).Mul(2).Uint32())
		require.Panics(t, func() {
			max.Mul(2)
		})
	})
	t.Run("Duration", func(t *testing.T) {
		require.EqualValues(t, 1, NewLayerID(2).Difference(NewLayerID(1)))
		require.Panics(t, func() {
			NewLayerID(10).Difference(NewLayerID(20))
		})
	})
}

func TestLayerIDComparison(t *testing.T) {
	t.Run("After", func(t *testing.T) {
		require.True(t, NewLayerID(10).After(NewLayerID(5)))
		require.True(t, !NewLayerID(10).After(NewLayerID(10)))
		require.True(t, !NewLayerID(10).After(NewLayerID(20)))
	})
	t.Run("Before", func(t *testing.T) {
		require.True(t, NewLayerID(5).Before(NewLayerID(10)))
		require.True(t, !NewLayerID(5).Before(NewLayerID(5)))
		require.True(t, !NewLayerID(5).Before(NewLayerID(3)))
	})
	t.Run("Equal", func(t *testing.T) {
		require.True(t, NewLayerID(1) == NewLayerID(1))
		require.True(t, NewLayerID(1) != NewLayerID(10))
	})
}

func TestLayerIDString(t *testing.T) {
	require.Equal(t, "10", NewLayerID(10).String())
}

func TestLayerIDBinaryEncoding(t *testing.T) {
	lid := NewLayerID(100)
	buf, err := InterfaceToBytes(lid)
	require.NoError(t, err)
	decoded := LayerID{}
	require.NoError(t, BytesToInterface(buf, &decoded))
	require.Equal(t, lid, decoded)
}
