package validation

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConsensusValidation(t *testing.T) {
	type item struct {
		consensus, state string
	}
	type iter struct {
		err   error
		items []*item
	}
	type tcase struct {
		desc           string
		size, tolerate int
		iterations     []iter
	}
	for _, tc := range []tcase{
		{
			"sanity no error",
			3, 1,
			[]iter{
				{
					nil,
					[]*item{
						{"a", "a"},
						{"a", "a"},
						{"a", "a"},
					},
				},
			},
		},
		{
			"sanity error",
			3, 0,
			[]iter{
				{
					errors.New("node 2 failed to reach consensus in 1 period(s)"),
					[]*item{
						{"a", "a"},
						{"a", "a"},
						{"b", "b"},
					},
				},
			},
		},
		{
			"state error",
			3, 0,
			[]iter{
				{
					errors.New("node 1 failed to reach consensus in 1 period(s)"),
					[]*item{
						{"a", "a"},
						{"a", "b"},
						{"a", "a"},
					},
				},
			},
		},
		{
			"no response",
			3, 1,
			[]iter{
				{
					nil,
					[]*item{
						{"a", "a"},
						{"a", "a"},
						nil,
					},
				},
				{
					errors.New("node 2 failed to reach consensus in 2 period(s)"),
					[]*item{
						{"a", "a"},
						nil,
						nil,
					},
				},
				{
					errors.New("node 1 failed to reach consensus in 2 period(s)"),
					[]*item{
						{"a", "a"},
						nil,
						nil,
					},
				},
			},
		},
		{
			"minority fails regardless of state",
			5, 0,
			[]iter{
				{
					errors.New("node 0 failed to reach consensus in 1 period(s)"),
					[]*item{
						{"a", "b"},
						{"a", "b"},
						{"b", "b"},
						{"b", "b"},
						{"b", "b"},
					},
				},
			},
		},
		{
			"equal size clusters fail",
			5, 0,
			[]iter{
				{
					errors.New("node 0 failed to reach consensus in 1 period(s)"),
					[]*item{
						{"a", "a"},
						{"a", "a"},
						{"b", "b"},
						{"b", "b"},
					},
				},
			},
		},
		{
			"counter reset on consensus",
			3, 1,
			[]iter{
				{
					nil,
					[]*item{
						{"a", "a"},
						{"b", "b"},
						{"a", "a"},
					},
				},
				{
					nil,
					[]*item{
						{"a", "a"},
						{"a", "a"},
						{"a", "a"},
					},
				},
				{
					nil,
					[]*item{
						{"a", "a"},
						{"b", "b"},
						{"a", "a"},
					},
				},
			},
		},
		{
			"inconsistency in state counts as a failure",
			3, 1,
			[]iter{
				{
					nil,
					[]*item{
						{"b", "b"},
						{"a", "a"},
						{"a", "a"},
					},
				},
				{
					errors.New("node 0 failed to reach consensus in 2 period(s)"),
					[]*item{
						{"a", "b"},
						{"a", "a"},
						{"a", "a"},
					},
				},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			cv := NewConsensusValidation(tc.size, tc.tolerate)
			for _, iteration := range tc.iterations {
				iter := cv.Next()
				for i, item := range iteration.items {
					if item == nil {
						iter.OnData(i, nil)
					} else {
						iter.OnData(i, &ConsensusData{
							Consensus: []byte(item.consensus),
							State:     []byte(item.state),
						})
					}
				}
				require.Equal(t, iteration.err, cv.Complete(iter))
			}
		})
	}
}
