package peersync

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRound(t *testing.T) {
	for _, tc := range []struct {
		desc      string
		round     round
		responses []Response
		received  []int64
		notReady  bool
		offset    time.Duration
	}{
		{
			desc: "EvenMedian",
			responses: []Response{
				{Timestamp: 30},
				{Timestamp: 10},
				{Timestamp: 20},
			},
			received: []int64{0, 0, 0},
			offset:   20,
		},
		{
			desc: "OddMedian",
			responses: []Response{
				{Timestamp: 30},
				{Timestamp: 10},
				{Timestamp: 20},
				{Timestamp: 40},
			},
			received: []int64{0, 0, 0, 0},
			offset:   25,
		},
		{
			desc:  "AdjustedByRTT",
			round: round{Timestamp: 50},
			responses: []Response{
				{Timestamp: 100},
				{Timestamp: 100},
				{Timestamp: 100},
			},
			received: []int64{140, 140, 120},
			offset:   5,
		},
		{
			desc: "OneResponse",
			responses: []Response{
				{Timestamp: 100},
			},
			received: []int64{0},
			offset:   100,
		},
		{
			desc: "TwoResponses",
			responses: []Response{
				{Timestamp: 100},
				{Timestamp: 120},
			},
			received: []int64{0, 0},
			offset:   110,
		},
		{
			desc: "NoResponses",
		},
		{
			desc:  "RoundMismatch",
			round: round{ID: 1, RequiredResponses: 2},
			responses: []Response{
				{Timestamp: 100, ID: 1},
				{Timestamp: 100, ID: 2},
			},
			received: []int64{0, 0},
			notReady: true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			round := tc.round
			for i, resp := range tc.responses {
				round.AddResponse(resp, tc.received[i])
			}
			if tc.notReady {
				require.False(t, round.Ready())
			} else {
				require.True(t, round.Ready())
				require.Equal(t, tc.offset, round.Offset())
			}
		})
	}
}
