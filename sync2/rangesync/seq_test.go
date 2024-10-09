package rangesync_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

func TestFirst(t *testing.T) {
	seq := rangesync.Seq(slices.Values([]rangesync.KeyBytes{{1}, {2}, {3}, {4}}))
	require.Equal(t, rangesync.KeyBytes{1}, seq.First())
}

func TestGetN(t *testing.T) {
	seq := rangesync.Seq(slices.Values([]rangesync.KeyBytes{{1}, {2}, {3}, {4}}))
	require.Empty(t, seq.GetN(0))
	require.Equal(t, []rangesync.KeyBytes{{1}}, seq.GetN(1))
	require.Equal(t, []rangesync.KeyBytes{{1}, {2}}, seq.GetN(2))
	require.Equal(t, []rangesync.KeyBytes{{1}, {2}, {3}}, seq.GetN(3))
	require.Equal(t, []rangesync.KeyBytes{{1}, {2}, {3}, {4}}, seq.GetN(4))
	require.Equal(t, []rangesync.KeyBytes{{1}, {2}, {3}, {4}}, seq.GetN(5))
}
