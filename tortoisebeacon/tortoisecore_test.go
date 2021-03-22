package tortoisebeacon

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

func TestTortoiseCore(t *testing.T) {
	r := require.New(t)

	type args struct {
		distance           int
		localVotes         map[types.ATXID]bool
		voteWeightsFor     []types.ATXID
		voteWeightsAgainst []types.ATXID
		weakCoin           bool
	}
	tests := []struct {
		name       string
		args       args
		wantValid  map[types.ATXID]bool
		wantGrades map[types.ATXID]int
	}{
		{
			name: "Case 1",
			args: args{
				distance: 0,
				localVotes: map[types.ATXID]bool{
					{1}: true,
					{2}: false,
				},
				voteWeightsFor:     []types.ATXID{{3}},
				voteWeightsAgainst: []types.ATXID{{4}},
				weakCoin:           true,
			},
			wantValid: map[types.ATXID]bool{
				{1}: true,
				{2}: false,
			},
			wantGrades: map[types.ATXID]int{
				{1}: 1,
				{2}: 1,
			},
		},
		{
			name: "Case 2",
			args: args{
				distance: 1,
				localVotes: map[types.ATXID]bool{
					{1}: true,
					{2}: false,
				},
				voteWeightsFor:     []types.ATXID{{3}},
				voteWeightsAgainst: []types.ATXID{{4}},
				weakCoin:           true,
			},
			wantValid: map[types.ATXID]bool{
				{3}: true,
				{4}: true,
			},
			wantGrades: map[types.ATXID]int{
				{3}: 0,
				{4}: 0,
			},
		},
	}

	requirer := require.New(t)
	conf := TestConfig()

	mwc := MockWeakCoin{}

	atxPool := activation.NewAtxMemPool()
	logger := log.NewDefault("TortoiseBeacon")
	genesisTime := time.Now().Add(time.Second * 10)
	ld := time.Duration(10) * time.Second
	types.SetLayersPerEpoch(1)
	clock := timesync.NewClock(timesync.RealClock{}, ld, genesisTime, log.NewDefault("clock"))
	clock.StartNotifying()
	ticker := clock.Subscribe()

	sim := service.NewSimulator()
	n1 := sim.NewNode()

	tb := New(conf, n1, atxPool, nil, mwc, ticker, logger)
	requirer.NotNil(tb)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValid, gotGrades := tb.TortoiseCore(tt.args.distance, tt.args.localVotes, tt.args.voteWeightsFor, tt.args.voteWeightsAgainst, tt.args.weakCoin)

			r.Equal(gotValid, tt.wantValid)
			r.Equal(gotGrades, tt.wantGrades)
		})
	}
}
