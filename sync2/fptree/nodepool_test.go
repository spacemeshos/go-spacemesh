package fptree

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

func TestNodePool(t *testing.T) {
	var np nodePool
	require.Zero(t, np.nodeCount())
	idx1 := np.add(rangesync.MustParseHexFingerprint("000000000000000000000001"), 1, noIndex, noIndex,
		rangesync.KeyBytes("foo"), noIndex)
	idx2 := np.add(rangesync.MustParseHexFingerprint("000000000000000000000002"), 1, noIndex, noIndex,
		rangesync.KeyBytes("bar"), noIndex)
	idx3 := np.add(rangesync.MustParseHexFingerprint("000000000000000000000003"), 2, idx1, idx2, nil, noIndex)

	require.Equal(t, nodeIndex(0), idx1)
	require.Equal(t, rangesync.KeyBytes("foo"), np.value(idx1))
	require.Equal(t, noIndex, np.left(idx1))
	require.Equal(t, noIndex, np.right(idx1))
	require.True(t, np.leaf(idx1))
	require.Equal(t, uint32(1), np.count(idx1))
	count, fp, leaf := np.info(idx1)
	require.Equal(t, uint32(1), count)
	require.Equal(t, rangesync.MustParseHexFingerprint("000000000000000000000001"), fp)
	require.True(t, leaf)
	require.Equal(t, uint32(1), np.refCount(idx1))

	require.Equal(t, nodeIndex(1), idx2)
	require.Equal(t, rangesync.KeyBytes("bar"), np.value(idx2))
	require.Equal(t, noIndex, np.left(idx2))
	require.Equal(t, noIndex, np.right(idx2))
	require.True(t, np.leaf(idx2))
	require.Equal(t, uint32(1), np.count(idx2))
	count, fp, leaf = np.info(idx2)
	require.Equal(t, uint32(1), count)
	require.Equal(t, rangesync.MustParseHexFingerprint("000000000000000000000002"), fp)
	require.True(t, leaf)
	require.Equal(t, uint32(1), np.refCount(idx2))

	require.Equal(t, nodeIndex(2), idx3)
	require.Nil(t, nil, idx3)
	require.Equal(t, idx1, np.left(idx3))
	require.Equal(t, idx2, np.right(idx3))
	require.False(t, np.leaf(idx3))
	require.Equal(t, uint32(2), np.count(idx3))
	count, fp, leaf = np.info(idx3)
	require.Equal(t, uint32(2), count)
	require.Equal(t, rangesync.MustParseHexFingerprint("000000000000000000000003"), fp)
	require.False(t, leaf)
	require.Equal(t, uint32(1), np.refCount(idx3))

	require.Equal(t, 3, np.nodeCount())

	np.ref(idx2)
	require.Equal(t, uint32(2), np.refCount(idx2))

	np.release(idx3)
	require.Equal(t, 1, np.nodeCount())
	require.Equal(t, uint32(1), np.refCount(idx2))
	count, fp, leaf = np.info(idx2)
	require.Equal(t, uint32(1), count)
	require.Equal(t, rangesync.MustParseHexFingerprint("000000000000000000000002"), fp)
	require.True(t, leaf)

	require.Equal(t, idx2, np.add(
		rangesync.MustParseHexFingerprint("000000000000000000000004"), 1, noIndex, noIndex,
		rangesync.KeyBytes("bar2"), idx2))
	count, fp, leaf = np.info(idx2)
	require.Equal(t, uint32(1), count)
	require.Equal(t, rangesync.MustParseHexFingerprint("000000000000000000000004"), fp)
	require.True(t, leaf)
	require.Equal(t, rangesync.KeyBytes("bar2"), np.value(idx2))

	np.release(idx2)
	require.Zero(t, np.nodeCount())
}
