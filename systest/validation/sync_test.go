package validation

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSyncValidation(t *testing.T) {
	type item struct {
		err    error
		id     int
		synced bool
	}
	type tcase struct {
		desc           string
		size, tolerate int
		items          []item
	}
	for _, tc := range []tcase{
		{
			desc: "sanity",
			size: 3, tolerate: 1,
			items: []item{
				{nil, 1, true},
				{nil, 1, true},
				{nil, 1, true},
			},
		},
		{
			desc: "error",
			size: 3, tolerate: 1,
			items: []item{
				{nil, 1, false},
				{nil, 2, true},
				{nil, 2, true},
				{errors.New("node 1 not synced in 2 periods"), 1, false},
			},
		},
		{
			desc: "resets to 0",
			size: 3, tolerate: 1,
			items: []item{
				{nil, 2, false},
				{nil, 2, true},
				{nil, 2, false},
				{nil, 2, true},
				{nil, 2, false},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			sv := SyncValidation{failures: make([]int, tc.size), tolerate: tc.tolerate}
			for _, item := range tc.items {
				require.Equal(t, item.err, sv.OnData(item.id, item.synced))
			}
		})
	}
}
