package rangesync_test

import (
	"errors"
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
	require.Empty(t, seq.FirstN(0))
	require.Equal(t, []rangesync.KeyBytes{{1}}, seq.FirstN(1))
	require.Equal(t, []rangesync.KeyBytes{{1}, {2}}, seq.FirstN(2))
	require.Equal(t, []rangesync.KeyBytes{{1}, {2}, {3}}, seq.FirstN(3))
	require.Equal(t, []rangesync.KeyBytes{{1}, {2}, {3}, {4}}, seq.FirstN(4))
	require.Equal(t, []rangesync.KeyBytes{{1}, {2}, {3}, {4}}, seq.FirstN(5))
}

func TestIsEmpty(t *testing.T) {
	empty, err := rangesync.EmptySeqResult().IsEmpty()
	require.NoError(t, err)
	require.True(t, empty)
	sr := rangesync.MakeSeqResult([]rangesync.KeyBytes{{1}})
	empty, err = sr.IsEmpty()
	require.NoError(t, err)
	require.False(t, empty)
	sampleErr := errors.New("error")
	sr = rangesync.ErrorSeqResult(sampleErr)
	_, err = sr.IsEmpty()
	require.ErrorIs(t, err, sampleErr)
	sr = rangesync.SeqResult{
		Seq:   rangesync.Seq(slices.Values([]rangesync.KeyBytes{{1}})),
		Error: rangesync.SeqError(sampleErr),
	}
	_, err = sr.IsEmpty()
	require.ErrorIs(t, err, sampleErr)
}
