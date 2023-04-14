package types

import (
	"bytes"
	"math"
	"math/rand"
	"testing"

	fuzz "github.com/google/gofuzz"
	"github.com/spacemeshos/go-scale"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
)

func CheckLayerFirstEncoding[T any, H scale.TypePtr[T]](t *testing.T, getLayerID func(object T) LayerID) {
	t.Run("layer is first", func(t *testing.T) {
		var object T
		f := fuzz.NewWithSeed(1001)
		f.Fuzz(&object)

		buf := bytes.NewBuffer(nil)
		enc := scale.NewEncoder(buf)
		_, err := H(&object).EncodeScale(enc)
		require.NoError(t, err)

		lid := LayerID(rand.Uint32())
		require.NoError(t, codec.DecodeSome(buf.Bytes(), &lid))
		require.Equal(t, getLayerID(object), lid)
	})
}

func TestLayerIDWraparound(t *testing.T) {
	var (
		max  = LayerID(math.MaxUint32)
		zero LayerID
	)
	t.Run("Add", func(t *testing.T) {
		require.EqualValues(t, 1, zero.Add(1).Uint32())
		require.Panics(t, func() {
			max.Add(1)
		})
		require.Panics(t, func() {
			LayerID(math.MaxUint32 - 2).Add(math.MaxUint32 - 3)
		})
	})
	t.Run("Sub", func(t *testing.T) {
		require.EqualValues(t, 0, zero.Add(1).Sub(1).Uint32())
		require.Panics(t, func() {
			zero.Sub(1)
		})
		require.Panics(t, func() {
			LayerID(math.MaxUint32 - 2).Sub(math.MaxUint32 - 1)
		})
	})
	t.Run("Mul", func(t *testing.T) {
		require.EqualValues(t, 0, zero.Mul(1).Uint32())
		require.EqualValues(t, 0, LayerID(1).Mul(0).Uint32())
		require.EqualValues(t, 4, LayerID(2).Mul(2).Uint32())
		require.Panics(t, func() {
			max.Mul(2)
		})
	})
	t.Run("Duration", func(t *testing.T) {
		require.EqualValues(t, 1, LayerID(2).Difference(LayerID(1)))
		require.Panics(t, func() {
			LayerID(10).Difference(LayerID(20))
		})
	})
}

func TestLayerIDComparison(t *testing.T) {
	t.Run("After", func(t *testing.T) {
		require.True(t, LayerID(10).After(LayerID(5)))
		require.True(t, !LayerID(10).After(LayerID(10)))
		require.True(t, !LayerID(10).After(LayerID(20)))
	})
	t.Run("Before", func(t *testing.T) {
		require.True(t, LayerID(5).Before(LayerID(10)))
		require.True(t, !LayerID(5).Before(LayerID(5)))
		require.True(t, !LayerID(5).Before(LayerID(3)))
	})
	t.Run("Equal", func(t *testing.T) {
		require.Equal(t, LayerID(1), LayerID(1))
		require.NotEqual(t, LayerID(1), LayerID(10))
	})
}

func TestLayerIDString(t *testing.T) {
	require.Equal(t, "10", LayerID(10).String())
}

func TestLayerIDBinaryEncoding(t *testing.T) {
	lid := LayerID(100)
	buf, err := codec.Encode(&lid)
	require.NoError(t, err)
	decoded := LayerID(0)
	require.NoError(t, codec.Decode(buf, &decoded))
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
			require.EqualValues(t, tc.epoch, LayerID(tc.layer).GetEpoch())
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
			require.EqualValues(t, tc.ordinal, LayerID(tc.layer).OrdinalInEpoch())
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
			require.EqualValues(t, tc.isFirst, LayerID(tc.layer).FirstInEpoch())
		})
	}
}
