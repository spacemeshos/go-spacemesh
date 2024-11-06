package rangesync_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

func TestFingerprint(t *testing.T) {
	var f rangesync.Fingerprint
	require.Equal(t, "000000000000000000000000", f.String())
	require.Equal(t, f, rangesync.EmptyFingerprint())
	f1 := rangesync.MustParseHexFingerprint("0102030405060708090a0b0c")
	require.Equal(t, "0102030405060708090a0b0c", f1.String())
	require.Equal(t, "0102030405", f1.ShortString())
	f1.Update([]byte{
		0x11, 0x22, 0x33, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0xcc,
	})
	require.Equal(t, "1020300405060708090a0bc0", f1.String())
	require.Equal(t, -1, f.Compare(f1))
	require.Equal(t, 0, f1.Compare(f1))
	require.Equal(t, 1, f1.Compare(f))
	var bits []bool
	for i := 0; i < 16; i++ {
		bits = append(bits, f1.BitFromLeft(i))
	}
	require.Equal(t, []bool{
		false, false, false, true,
		false, false, false, false,
		false, false, true, false,
		false, false, false, false,
	}, bits)

	f2 := rangesync.RandomFingerprint()
	f3 := rangesync.RandomFingerprint()
	require.NotEqual(t, f2, f3)

	f4 := rangesync.MustParseHexFingerprint("0102030405060708090a0b0c")
	f5 := rangesync.MustParseHexFingerprint("1112131415161718191a1b1c")
	combined := rangesync.CombineFingerprints(f4, f5)
	require.Equal(t, "101010101010101010101010", combined.String())
}
