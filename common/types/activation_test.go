package types

import (
	"bytes"
	"testing"
	"time"

	fuzz "github.com/google/gofuzz"
	"github.com/spacemeshos/go-scale"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
)

var result uint64

func BenchmarkSafeMul(b *testing.B) {
	var c uint64

	for i := 0; i < b.N; i++ {
		int1 := uint64(i * 2)
		int2 := uint64(i + 5)

		c = safeMul(int1, int2)
	}

	result = c // avoid compiler optimizations
}

func BenchmarkMul(b *testing.B) {
	var c uint64

	for i := 0; i < b.N; i++ {
		int1 := uint64(i * 2)
		int2 := uint64(i + 5)

		c = unsafeMul(int1, int2)
	}

	result = c // avoid compiler optimizations
}

func unsafeMul(x, y uint64) uint64 {
	result = x // avoid compiler optimizations
	return x * y
}

func TestRoundEndSerialization(t *testing.T) {
	end := RoundEnd(time.Now())
	var data bytes.Buffer
	_, err := end.EncodeScale(scale.NewEncoder(&data))
	require.NoError(t, err)

	var deserialized RoundEnd
	_, err = deserialized.DecodeScale(scale.NewDecoder(&data))
	require.NoError(t, err)

	require.EqualValues(t, end.IntoTime().Unix(), deserialized.IntoTime().Unix())
}

func layerTester(tb testing.TB, encodable codec.Encodable) LayerID {
	tb.Helper()
	f := fuzz.NewWithSeed(1001)
	f.Fuzz(encodable)
	buf, err := codec.Encode(encodable)
	require.NoError(tb, err)
	var lid LayerID
	require.NoError(tb, codec.Decode(buf, &lid))
	return lid
}

func TestActivationEncoding(t *testing.T) {
	t.Run("layer is first", func(t *testing.T) {
		atx := ActivationTx{}
		lid := layerTester(t, &atx)
		require.Equal(t, atx.PubLayerID, lid)
	})
}
