package rangesync_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

var errBadItem = errors.New("bad item")

func mkFakeSeq(items []string) rangesync.SeqResult {
	var err error
	return rangesync.SeqResult{
		Seq: func(yield func(rangesync.KeyBytes) bool) {
			for {
				for _, item := range items {
					if item == "ERROR" {
						err = errBadItem
						return
					}
					if !yield(rangesync.KeyBytes(item)) {
						return
					}
				}
			}
		},
		Error: func() error {
			return err
		},
	}
}

func TestMinHash(t *testing.T) {
	for _, tc := range []struct {
		name       string
		a, b       []string
		errA       error
		sampleSize int
		errB       error
		sim        float64
	}{
		{
			name:       "2 empty sets",
			a:          nil,
			b:          nil,
			sampleSize: 4,
			sim:        1,
		},
		{
			name:       "empty A",
			a:          []string{"abcde", "fghij", "klmno", "pqrst", "uvwxy"},
			b:          nil,
			sampleSize: 4,
			sim:        0,
		},
		{
			name:       "empty B",
			a:          nil,
			b:          []string{"abcde", "fghij", "klmno", "pqrst", "uvwxy"},
			sampleSize: 4,
			sim:        0,
		},
		{
			name:       "identical",
			a:          []string{"abcde", "fghij", "klmno", "pqrst", "uvwxy"},
			b:          []string{"abcde", "fghij", "klmno", "pqrst", "uvwxy"},
			sampleSize: 4,
			sim:        1,
		},
		{
			name:       "different",
			a:          []string{"abcde", "fghij", "klmno", "pqrst", "uvwxy"},
			b:          []string{"abcde", "fghij", "klmno", "fffff"},
			sampleSize: 4,
			sim:        0.75,
		},
		{
			name:       "identical short",
			a:          []string{"abcde", "fghij", "klmno"},
			b:          []string{"abcde", "fghij", "klmno"},
			sampleSize: 4,
			sim:        1,
		},
		{
			name:       "different short",
			a:          []string{"abcde", "klmno"},
			b:          []string{"abcde", "fffff"},
			sampleSize: 4,
			sim:        0.5,
		},
		{
			name:       "errors",
			a:          []string{"abcde", "ERROR", "klmno", "pqrst", "uvwxy"},
			errA:       errBadItem,
			b:          []string{"abcde", "fghij", "klmno", "ERROR", "uvwxy"},
			errB:       errBadItem,
			sampleSize: 4,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sampleA, err := rangesync.Sample(mkFakeSeq(tc.a), len(tc.a), tc.sampleSize)
			if tc.errA != nil {
				require.ErrorIs(t, err, tc.errA)
			} else {
				require.NoError(t, err)
			}
			sampleB, err := rangesync.Sample(mkFakeSeq(tc.b), len(tc.b), tc.sampleSize)
			if tc.errB != nil {
				require.ErrorIs(t, err, tc.errB)
			} else {
				require.NoError(t, err)
			}
			if tc.errA == nil && tc.errB == nil {
				require.InDelta(t, tc.sim, rangesync.CalcSim(sampleA, sampleB), 1e-9)
			}
		})
	}
}
