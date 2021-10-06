package types

import (
	"math"
	"math/rand"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/stretchr/testify/require"
)

func genByte32() [32]byte {
	var x [32]byte
	rand.Read(x[:])
	return x
}

var (
	txid1 = TransactionID(genByte32())
	txid2 = TransactionID(genByte32())
)

var (
	one   = CalcHash32([]byte("1"))
	two   = CalcHash32([]byte("2"))
	three = CalcHash32([]byte("3"))
)

var (
	atx1 = ATXID(one)
	atx2 = ATXID(two)
	atx3 = ATXID(three)
)

// Make sure we can print out all the relevant log fields for a block.
func TestFields(t *testing.T) {
	t.Skip("this is not a proper test")
	SetLayersPerEpoch(3)
	b := &Block{}
	b.TxIDs = []TransactionID{txid1, txid2, txid1}
	b.ActiveSet = &[]ATXID{atx1, atx2, atx3}
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
		require.Equal(t, NewLayerID(1), NewLayerID(1))
		require.NotEqual(t, NewLayerID(1), NewLayerID(10))
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

func TestLayerID_GetEpoch(t *testing.T) {
	tests := []struct {
		name           string
		layersPerEpoch uint32
		layer          uint32
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
			require.EqualValues(t, tc.epoch, NewLayerID(tc.layer).GetEpoch())
		})
	}
}

func TestLayerID_OrdinalInEpoch(t *testing.T) {
	tests := []struct {
		name           string
		layersPerEpoch uint32
		layer          uint32
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
			require.EqualValues(t, tc.ordinal, NewLayerID(tc.layer).OrdinalInEpoch())
		})
	}
}

func TestLayerID_FirstInEpoch(t *testing.T) {
	tests := []struct {
		name           string
		layersPerEpoch uint32
		layer          uint32
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
			require.EqualValues(t, tc.isFirst, NewLayerID(tc.layer).FirstInEpoch())
		})
	}
}

func TestBlockIDSize(t *testing.T) {
	var id BlockID
	require.Len(t, id.Bytes(), BlockIDSize)
}

func TestLayerIDSize(t *testing.T) {
	var id LayerID
	require.Len(t, id.Bytes(), LayerIDSize)
}
