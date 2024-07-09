package miner

import (
	"context"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/miner/mocks"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/activeset"
)

type expect struct {
	set    []types.ATXID
	id     types.Hash32
	weight uint64
}

func expectSet(set []types.ATXID, weight uint64) *expect {
	return &expect{
		id:     types.ATXIDList(set).Hash(),
		set:    set,
		weight: weight,
	}
}

type test struct {
	desc       string
	atxs       []*types.ActivationTx
	malfeasent []identity
	blocks     []*types.Block
	ballots    []*types.Ballot
	activesets []*types.EpochActiveSet
	fallbacks  []types.EpochActiveSet

	networkDelay   time.Duration
	goodAtxPercent int
	current        types.LayerID
	target         types.EpochID
	epochStart     *time.Time

	expect    *expect
	expectErr string
}

func unixPtr(sec, nsec int64) *time.Time {
	t := time.Unix(sec, nsec)
	return &t
}

func newTesterActiveSetGenerator(tb testing.TB, cfg config) *testerActiveSetGenerator {
	var (
		db        = sql.InMemory()
		localdb   = localsql.InMemory()
		atxsdata  = atxsdata.New()
		ctrl      = gomock.NewController(tb)
		clock     = mocks.NewMocklayerClock(ctrl)
		wallclock = clockwork.NewFakeClock()
		gen       = newActiveSetGenerator(
			cfg,
			zaptest.NewLogger(tb),
			db,
			localdb,
			atxsdata,
			clock,
			withWallClock(wallclock),
		)
	)
	return &testerActiveSetGenerator{
		tb:        tb,
		gen:       gen,
		db:        db,
		localdb:   localdb,
		atxsdata:  atxsdata,
		ctrl:      ctrl,
		clock:     clock,
		wallclock: wallclock,
	}
}

type testerActiveSetGenerator struct {
	tb  testing.TB
	gen *activeSetGenerator

	db        *sql.Database
	localdb   *localsql.Database
	atxsdata  *atxsdata.Data
	ctrl      *gomock.Controller
	clock     *mocks.MocklayerClock
	wallclock clockwork.FakeClock
}

func TestActiveSetGenerate(t *testing.T) {
	// third epoch first layer
	thirdFirst := types.EpochID(3).FirstLayer()
	// activeset hash from atx ids 1 and 2
	activeSetHash12 := types.ATXIDList([]types.ATXID{{1}, {2}}).Hash()

	for _, tc := range []test{
		{
			desc: "fallback success",
			atxs: []*types.ActivationTx{
				gatx(types.ATXID{1}, 2, types.NodeID{1}, 2),
				gatx(types.ATXID{2}, 2, types.NodeID{1}, 3),
			},
			fallbacks: []types.EpochActiveSet{
				{Epoch: 3, Set: []types.ATXID{{1}, {2}}},
			},
			target: 3,
			expect: expectSet([]types.ATXID{{1}, {2}}, 5*ticks),
		},
		{
			desc: "fallback failure",
			atxs: []*types.ActivationTx{
				gatx(types.ATXID{1}, 2, types.NodeID{1}, 2),
			},
			fallbacks: []types.EpochActiveSet{
				{Epoch: 3, Set: []types.ATXID{{1}, {2}}},
			},
			target:    3,
			expectErr: "atx 3/0200000000 is missing in atxsdata",
		},
		{
			desc: "graded active set > 60",
			atxs: []*types.ActivationTx{
				gatx(types.ATXID{1}, 2, types.NodeID{1}, 2, genAtxWithReceived(time.Unix(20, 0))),
				gatx(types.ATXID{2}, 2, types.NodeID{2}, 2, genAtxWithReceived(time.Unix(20, 0))),
				// the last atx won't be marked as good due to being received only 2 seconds before epoch start
				gatx(types.ATXID{3}, 2, types.NodeID{3}, 2, genAtxWithReceived(time.Unix(28, 0))),
			},
			epochStart:     unixPtr(30, 0),
			networkDelay:   2 * time.Second,
			goodAtxPercent: 60,
			target:         3,
			expect:         expectSet([]types.ATXID{{1}, {2}}, 4*ticks),
		},
		{
			desc: "graded active set < 60",
			atxs: []*types.ActivationTx{
				gatx(types.ATXID{1}, 2, types.NodeID{1}, 2, genAtxWithReceived(time.Unix(20, 0))),
				// two last atx won't be marked as good due to being received only 2 seconds before epoch start
				gatx(types.ATXID{2}, 2, types.NodeID{2}, 2, genAtxWithReceived(time.Unix(28, 0))),
				gatx(types.ATXID{3}, 2, types.NodeID{3}, 2, genAtxWithReceived(time.Unix(28, 0))),
			},
			epochStart:     unixPtr(30, 0),
			networkDelay:   2 * time.Second,
			goodAtxPercent: 60,
			target:         3,
			expectErr:      "failed to generate activeset for epoch 3",
		},
		{
			desc: "graded active set with malicious",
			atxs: []*types.ActivationTx{
				gatx(types.ATXID{1}, 2, types.NodeID{1}, 2, genAtxWithReceived(time.Unix(20, 0))),
				gatx(types.ATXID{2}, 2, types.NodeID{2}, 2, genAtxWithReceived(time.Unix(20, 0))),
				gatx(types.ATXID{3}, 2, types.NodeID{3}, 2, genAtxWithReceived(time.Unix(20, 0))),
			},
			malfeasent: []identity{
				gidentity(types.NodeID{3}, time.Unix(29, 0)),
			},
			epochStart:     unixPtr(30, 0),
			networkDelay:   2 * time.Second,
			goodAtxPercent: 60,
			target:         3,
			expect:         expectSet([]types.ATXID{{1}, {2}}, 4*ticks),
		},
		{
			desc: "graded active set with late malicious",
			atxs: []*types.ActivationTx{
				gatx(types.ATXID{1}, 2, types.NodeID{1}, 2, genAtxWithReceived(time.Unix(20, 0))),
				gatx(types.ATXID{2}, 2, types.NodeID{2}, 2, genAtxWithReceived(time.Unix(20, 0))),
				gatx(types.ATXID{3}, 2, types.NodeID{3}, 2, genAtxWithReceived(time.Unix(20, 0))),
			},
			malfeasent: []identity{
				gidentity(types.NodeID{3}, time.Unix(31, 0)),
			},
			epochStart:     unixPtr(30, 0),
			networkDelay:   2 * time.Second,
			goodAtxPercent: 60,
			target:         3,
			expect:         expectSet([]types.ATXID{{1}, {2}, {3}}, 6*ticks),
		},
		{
			desc:           "graded empty",
			atxs:           []*types.ActivationTx{},
			epochStart:     unixPtr(30, 0),
			networkDelay:   2 * time.Second,
			goodAtxPercent: 60,
			target:         3,
			expectErr:      "empty active set",
		},
		{
			desc: "first block not found",
			atxs: []*types.ActivationTx{
				gatx(types.ATXID{1}, 2, types.NodeID{1}, 2),
				gatx(types.ATXID{2}, 2, types.NodeID{2}, 2),
			},
			epochStart:     unixPtr(0, 0),
			networkDelay:   2 * time.Second,
			goodAtxPercent: 100,
			current:        thirdFirst + 1,
			target:         3,
			expectErr:      "first block in epoch 3 not found",
		},
		{
			desc: "first block success",
			atxs: []*types.ActivationTx{
				gatx(types.ATXID{1}, 2, types.NodeID{1}, 2),
				gatx(types.ATXID{2}, 2, types.NodeID{2}, 2),
			},
			blocks: []*types.Block{
				gblock(thirdFirst, types.ATXID{1}, types.ATXID{2}),
			},
			ballots: []*types.Ballot{
				gballot(types.BallotID{1}, types.ATXID{1}, types.NodeID{1}, thirdFirst, &types.EpochData{
					ActiveSetHash: activeSetHash12,
				}),
				gballot(types.BallotID{2}, types.ATXID{2}, types.NodeID{2}, thirdFirst, &types.EpochData{
					ActiveSetHash: activeSetHash12,
				}),
			},
			activesets: []*types.EpochActiveSet{
				{Epoch: 3, Set: []types.ATXID{{1}, {2}}},
			},
			epochStart:     unixPtr(0, 0),
			networkDelay:   2 * time.Second,
			goodAtxPercent: 100,
			current:        types.EpochID(3).FirstLayer() + 1,
			target:         3,
			expect:         expectSet([]types.ATXID{{1}, {2}}, 4*ticks),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			tester := newTesterActiveSetGenerator(
				t,
				config{networkDelay: tc.networkDelay, goodAtxPercent: tc.goodAtxPercent},
			)
			for _, atx := range tc.atxs {
				require.NoError(t, atxs.Add(tester.db, atx, types.AtxBlob{}))
				tester.atxsdata.AddFromAtx(atx, false)
			}
			for _, identity := range tc.malfeasent {
				require.NoError(
					t,
					identities.SetMalicious(
						tester.db,
						identity.id,
						codec.MustEncode(&identity.proof),
						identity.received,
					),
				)
			}
			for _, block := range tc.blocks {
				require.NoError(t, blocks.Add(tester.db, block))
				require.NoError(t, layers.SetApplied(tester.db, block.LayerIndex, block.ID()))
			}
			for _, ballot := range tc.ballots {
				require.NoError(t, ballots.Add(tester.db, ballot))
			}
			for _, ac := range tc.activesets {
				require.NoError(t, activesets.Add(tester.db, types.ATXIDList(ac.Set).Hash(), ac))
			}
			for _, fallback := range tc.fallbacks {
				tester.gen.updateFallback(fallback.Epoch, fallback.Set)
			}
			if tc.epochStart != nil {
				tester.clock.EXPECT().LayerToTime(tc.target.FirstLayer()).Return(*tc.epochStart)
			}

			id, setWeight, set, err := tester.gen.generate(tc.current, tc.target)
			if tc.expectErr != "" {
				require.ErrorContains(t, err, tc.expectErr)
			} else {
				require.NoError(t, err)
			}
			if tc.expect != nil {
				require.Equal(t, tc.expect.id, id)
				require.Equal(t, tc.expect.weight, setWeight)
				require.Equal(t, tc.expect.set, set)
			}
		})
	}
}

func TestActiveSetEnsure(t *testing.T) {
	const (
		tries  = 10
		target = 3
	)
	ctx := context.Background()
	tester := newTesterActiveSetGenerator(t, config{
		activeSet: ActiveSetPreparation{
			RetryInterval: time.Second,
			Tries:         tries,
		},
	})

	terminated := make(chan struct{})
	expected := []types.ATXID{{1}}
	// verify that it tries compute activeset for configured number of tries
	tester.gen.updateFallback(target, expected)
	tester.clock.EXPECT().CurrentLayer().Return(0).Times(tries)
	go func() {
		tester.gen.ensure(ctx, target)
		terminated <- struct{}{}
	}()
	go func() {
		for i := 0; i < tries; i++ {
			tester.wallclock.BlockUntil(1)
			tester.wallclock.Advance(tester.gen.cfg.activeSet.RetryInterval)
		}
	}()
	_, _, _, err := activeset.Get(tester.localdb, activeset.Tortoise, target)
	require.ErrorIs(t, err, sql.ErrNotFound)
	select {
	case <-terminated:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}

	// interruptible
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	tester.clock.EXPECT().CurrentLayer().Return(0).Times(1)
	tester.gen.ensure(ctx, target)
	_, _, _, err = activeset.Get(tester.localdb, activeset.Tortoise, target)
	require.ErrorIs(t, err, sql.ErrNotFound)

	// computes from first try
	tester.atxsdata.AddFromAtx(gatx(expected[0], 2, types.NodeID{1}, 1), false)
	tester.clock.EXPECT().CurrentLayer().Return(0).Times(1)
	tester.gen.ensure(ctx, target)
	id, weight, set, err := activeset.Get(tester.localdb, activeset.Tortoise, target)
	require.NoError(t, err)
	require.Equal(t, types.ATXIDList(expected).Hash(), id)
	require.Equal(t, expected, set)
	require.Equal(t, 1*ticks, int(weight))
}
