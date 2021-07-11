package types

import (
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

func TestLayerID_GetEpoch(t *testing.T) {
	tests := []struct {
		name           string
		layersPerEpoch int32
		layer          LayerID
		epoch          int
	}{
		{
			name:           "Case 0",
			layersPerEpoch: 3,
			layer:          0,
			epoch:          0,
		},
		{
			name:           "Case 1",
			layersPerEpoch: 3,
			layer:          1,
			epoch:          0,
		},
		{
			name:           "Case 2",
			layersPerEpoch: 3,
			layer:          2,
			epoch:          0,
		},
		{
			name:           "Case 3",
			layersPerEpoch: 3,
			layer:          3,
			epoch:          1,
		},
		{
			name:           "Case 4",
			layersPerEpoch: 3,
			layer:          4,
			epoch:          1,
		},
		{
			name:           "Case 5",
			layersPerEpoch: 3,
			layer:          5,
			epoch:          1,
		},
		{
			name:           "Case 6",
			layersPerEpoch: 3,
			layer:          6,
			epoch:          2,
		},
		{
			name:           "Case 7",
			layersPerEpoch: 3,
			layer:          7,
			epoch:          2,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			SetLayersPerEpoch(tc.layersPerEpoch)
			require.EqualValues(t, tc.epoch, tc.layer.GetEpoch())
		})
	}
}

func TestLayerID_OrdinalInEpoch(t *testing.T) {
	tests := []struct {
		name           string
		layersPerEpoch int32
		layer          LayerID
		ordinal        int
	}{
		{
			name:           "Case 0",
			layersPerEpoch: 3,
			layer:          0,
			ordinal:        0,
		},
		{
			name:           "Case 1",
			layersPerEpoch: 3,
			layer:          1,
			ordinal:        1,
		},
		{
			name:           "Case 2",
			layersPerEpoch: 3,
			layer:          2,
			ordinal:        2,
		},
		{
			name:           "Case 3",
			layersPerEpoch: 3,
			layer:          3,
			ordinal:        0,
		},
		{
			name:           "Case 4",
			layersPerEpoch: 3,
			layer:          4,
			ordinal:        1,
		},
		{
			name:           "Case 5",
			layersPerEpoch: 3,
			layer:          5,
			ordinal:        2,
		},
		{
			name:           "Case 6",
			layersPerEpoch: 3,
			layer:          6,
			ordinal:        0,
		},
		{
			name:           "Case 7",
			layersPerEpoch: 3,
			layer:          7,
			ordinal:        1,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			SetLayersPerEpoch(tc.layersPerEpoch)
			require.EqualValues(t, tc.ordinal, tc.layer.OrdinalInEpoch())
		})
	}
}

func TestLayerID_FirstInEpoch(t *testing.T) {
	tests := []struct {
		name           string
		layersPerEpoch int32
		layer          LayerID
		isFirst        bool
	}{
		{
			name:           "Case 0",
			layersPerEpoch: 3,
			layer:          0,
			isFirst:        true,
		},
		{
			name:           "Case 1",
			layersPerEpoch: 3,
			layer:          1,
			isFirst:        false,
		},
		{
			name:           "Case 2",
			layersPerEpoch: 3,
			layer:          2,
			isFirst:        false,
		},
		{
			name:           "Case 3",
			layersPerEpoch: 3,
			layer:          3,
			isFirst:        true,
		},
		{
			name:           "Case 4",
			layersPerEpoch: 3,
			layer:          4,
			isFirst:        false,
		},
		{
			name:           "Case 5",
			layersPerEpoch: 3,
			layer:          5,
			isFirst:        false,
		},
		{
			name:           "Case 6",
			layersPerEpoch: 3,
			layer:          6,
			isFirst:        true,
		},
		{
			name:           "Case 7",
			layersPerEpoch: 3,
			layer:          7,
			isFirst:        false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			SetLayersPerEpoch(tc.layersPerEpoch)
			require.EqualValues(t, tc.isFirst, tc.layer.FirstInEpoch())
		})
	}
}
