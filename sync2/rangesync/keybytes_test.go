package rangesync_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

func TestKeyBytes(t *testing.T) {
	require.Equal(t, "010203040506", rangesync.KeyBytes([]byte{1, 2, 3, 4, 5, 6}).String())
	require.Equal(t, "0102030405", rangesync.KeyBytes([]byte{1, 2, 3, 4, 5, 6}).ShortString())
	a := rangesync.MustParseHexKeyBytes("010203040506")
	require.Equal(t, "010203040506", a.String())
	b := a.Clone()
	a.Zero()
	require.Equal(t, "000000000000", a.String())
	require.True(t, a.IsZero())
	require.Equal(t, "010203040506", b.String())
	require.False(t, b.IsZero())
	require.Equal(t, -1, a.Compare(b))
	require.Equal(t, 1, b.Compare(a))
	require.Equal(t, 0, a.Compare(a))
	var bits []bool
	for i := 0; i < 16; i++ {
		bits = append(bits, b.BitFromLeft(i))
	}
	require.Equal(t, []bool{
		false, false, false, false,
		false, false, false, true,
		false, false, false, false,
		false, false, true, false,
	}, bits)

	c := rangesync.RandomKeyBytes(6)
	require.Len(t, c, 6)
	d := rangesync.RandomKeyBytes(6)
	require.Len(t, d, 6)
	require.NotEqual(t, c, d)
}

func TestIncKeyBytes(t *testing.T) {
	for _, tc := range []struct {
		id, expected string
		overflow     bool
	}{
		{
			id:       "00000000",
			expected: "00000001",
			overflow: false,
		},
		{
			id:       "000000ff",
			expected: "00000100",
			overflow: false,
		},
		{
			id:       "ffffffff",
			expected: "00000000",
			overflow: true,
		},
	} {
		id := rangesync.MustParseHexKeyBytes(tc.id)
		require.Equal(t, tc.overflow, id.Inc())
		expected := rangesync.MustParseHexKeyBytes(tc.expected)
		require.Equal(t, expected, id)
	}
}

func TestTrimKeyBytes(t *testing.T) {
	for _, tc := range []struct {
		k, expected string
		bit         int
	}{
		{
			k:        "010203040506",
			expected: "010203040506",
			bit:      48,
		},
		{
			k:        "010203040506",
			expected: "010203040506",
			bit:      128,
		},
		{
			k:        "a10203040506",
			expected: "a00000000000",
			bit:      4,
		},
		{
			k:        "a1b203040506",
			expected: "a1b000000000",
			bit:      12,
		},
		{
			k:        "a1b203040506",
			expected: "a1b200000000",
			bit:      16,
		},
		{
			k:        "a1b203040506",
			expected: "a1b203000000",
			bit:      24,
		},
		{
			k:        "a1b203040506",
			expected: "a1b203000000",
			bit:      28,
		},
		{
			k:        "a1b203040506",
			expected: "a1b203040000",
			bit:      32,
		},
	} {
		k := rangesync.MustParseHexKeyBytes(tc.k)
		k.Trim(tc.bit)
		expected := rangesync.MustParseHexKeyBytes(tc.expected)
		require.Equal(t, expected, k)
	}
}
