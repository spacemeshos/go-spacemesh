package codec

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLenEncoding(t *testing.T) {
	for _, l := range []uint32{
		0,
		1,
		16,
		64,
		65,
		1<<14 - 1,
		1 << 14,
		1 << 20,
		1 << 30,
		1 << 31,
		1<<32 - 1,
	} {
		var b bytes.Buffer
		n, err := EncodeLen(&b, l)
		require.NoError(t, err)
		require.Equal(t, uint32(b.Len()), LenSize(l))
		require.Equal(t, uint32(n), LenSize(l))
		decoded, n, err := DecodeLen(&b)
		require.NoError(t, err)
		require.Equal(t, l, decoded)
		require.Equal(t, uint32(n), LenSize(l))
	}
}
