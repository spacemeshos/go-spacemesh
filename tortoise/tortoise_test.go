package tortoise

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/spacemeshos/fixed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/result"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/beacons"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/tortoise/opinionhash"
	"github.com/spacemeshos/go-spacemesh/tortoise/sim"
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(4)

	res := m.Run()
	os.Exit(res)
}

const (
	defaultTestLayerSize  = 3
	defaultTestWindowSize = 30
	defaultVoteDelays     = 6
	numValidBlock         = 1
)

var (
	defaultTestHdist = DefaultConfig().Hdist
	defaultTestZdist = DefaultConfig().Zdist
)

func TestLayerPatterns(t *testing.T) {
	const size = 10 // more blocks means a longer test
	t.Run("Good", func(t *testing.T) {
		s := sim.New(sim.WithLayerSize(size))
		s.Setup()

		cfg := defaultTestConfig()
		cfg.LayerSize = size
		tortoise := tortoiseFromSimState(
			t,
			s.GetState(0),
			WithConfig(cfg),
			WithLogger(zaptest.NewLogger(t)),
		)

		var (
			last     types.LayerID
			verified types.LayerID
		)
		for _, lid := range sim.GenLayers(s,
			sim.WithSequence(5),
		) {
			last = lid
			tortoise.TallyVotes(lid)
			verified = tortoise.LatestComplete()
		}
		require.Equal(t, last.Sub(1), verified)
	})

	t.Run("HealAfterBad", func(t *testing.T) {
		s := sim.New(sim.WithLayerSize(size))
		s.Setup()

		cfg := defaultTestConfig()
		cfg.LayerSize = size
		cfg.Hdist = 4
		cfg.Zdist = cfg.Hdist
		tortoise := tortoiseFromSimState(
			t,
			s.GetState(0),
			WithConfig(cfg),
			WithLogger(zaptest.NewLogger(t)),
		)

		var (
			last    types.LayerID
			genesis = types.GetEffectiveGenesis()
		)
		for _, lid := range sim.GenLayers(s,
			sim.WithSequence(5),
			sim.WithSequence(2, sim.WithEmptyHareOutput()),
		) {
			last = lid
			tortoise.TallyVotes(lid)
		}
		require.Equal(t, genesis.Add(4), tortoise.LatestComplete())

		for _, lid := range sim.GenLayers(s,
			sim.WithSequence(2),
		) {
			last = lid
			tortoise.TallyVotes(lid)
		}
		require.Equal(t, last.Sub(1), tortoise.LatestComplete())
	})

	t.Run("HealAfterBadGoodBadGoodBad", func(t *testing.T) {
		s := sim.New(sim.WithLayerSize(size))
		s.Setup()

		cfg := defaultTestConfig()
		cfg.LayerSize = size
		tortoise := tortoiseFromSimState(
			t,
			s.GetState(0),
			WithConfig(cfg),
			WithLogger(zaptest.NewLogger(t)),
		)

		var (
			last     types.LayerID
			verified types.LayerID
		)
		for _, lid := range sim.GenLayers(s,
			sim.WithSequence(5),
			sim.WithSequence(1, sim.WithEmptyHareOutput()),
			sim.WithSequence(2),
			sim.WithSequence(2, sim.WithEmptyHareOutput()),
			sim.WithSequence(30),
		) {
			last = lid
			tortoise.TallyVotes(lid)
			verified = tortoise.LatestComplete()
		}
		require.Equal(t, last.Sub(1), verified)
	})
}

func TestAbstainsInMiddle(t *testing.T) {
	const size = 4
	s := sim.New(
		sim.WithLayerSize(size),
	)
	s.Setup(sim.WithSetupMinerRange(size, size))

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Hdist = 10
	cfg.Zdist = 3
	tortoise := tortoiseFromSimState(t, s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)))

	var last, verified types.LayerID
	for i := 0; i < 5; i++ {
		last = s.Next(sim.WithNumBlocks(1), sim.WithVoteGenerator(tortoiseVoting(tortoise)))
		tortoise.TallyVotes(last)
		verified = tortoise.LatestComplete()
	}
	require.Equal(t, last.Sub(1), verified)
	expected := last

	for i := 0; i < 2; i++ {
		tortoise.TallyVotes(s.Next(
			sim.WithNumBlocks(1),
			sim.WithVoteGenerator(tortoiseVoting(tortoise)),
			sim.WithoutHareOutput(),
		))
	}
	for i := 0; i < int(cfg.Zdist); i++ {
		tortoise.TallyVotes(s.Next(
			sim.WithNumBlocks(1),
			sim.WithVoteGenerator(tortoiseVoting(tortoise)),
		))
	}

	// local opinion will be decided after zdist layers
	// at that point verifying tortoise consistency will be revisited
	require.Equal(t, expected, tortoise.LatestComplete())
}

func TestAbstainLateBlock(t *testing.T) {
	const size = 4
	s := sim.New(
		sim.WithLayerSize(size),
	)
	s.Setup(sim.WithSetupMinerRange(size, size))

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Hdist = 2
	cfg.Zdist = 1
	tortoise := tortoiseFromSimState(t, s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)))

	s.Next(sim.WithNumBlocks(1))
	s.Next(sim.WithNumBlocks(0))
	last := s.Next(
		sim.WithNumBlocks(1),
		sim.WithoutHareOutput(),
		sim.WithVoteGenerator(abstainVoting),
	)
	tortoise.TallyVotes(last)

	events := tortoise.Updates()
	require.Len(t, events, 4)
	require.Equal(t, events[1].Layer, last.Sub(2))
	for _, v := range events[1].Blocks {
		require.True(t, v.Valid)
	}
	require.True(t, tortoise.OnApplied(events[3].Layer, events[3].Opinion))

	block := types.BlockHeader{ID: types.BlockID{1}, LayerID: last.Sub(1)}
	tortoise.OnBlock(block)
	tortoise.TallyVotes(last)

	events = tortoise.Updates()
	require.Len(t, events, 1)
}

func TestEncodeAbstainVotesForZdist(t *testing.T) {
	const (
		size  = 4
		zdist = 3
	)
	s := sim.New(sim.WithLayerSize(size))
	s.Setup()

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Zdist = zdist
	tortoise := tortoiseFromSimState(t, s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)))

	var (
		last     types.LayerID
		verified types.LayerID
	)
	for _, lid := range sim.GenLayers(s,
		sim.WithSequence(1),
	) {
		last = lid
		tortoise.TallyVotes(lid)
		verified = tortoise.LatestComplete()
	}
	require.Equal(t, last.Sub(1), verified)

	var start types.LayerID
	for i := 1; i <= zdist; i++ {
		current := last.Add(uint32(i))
		votes, err := tortoise.EncodeVotes(context.Background(), EncodeVotesWithCurrent(current))
		require.NoError(t, err)
		require.Len(t, votes.Support, 1)
		require.Empty(t, votes.Against)

		if i > zdist+1 {
			require.Len(t, votes.Abstain, zdist, "abstain is limited by zdist: %+v", votes.Abstain)
			start = current.Sub(zdist)
		} else {
			require.Len(t, votes.Abstain, i-1, "abstain is less then zdist: %+v", votes.Abstain)
			start = last.Add(1)
		}
		for j, lid := range votes.Abstain {
			require.Equal(t, start.Add(uint32(j)), lid)
		}
	}
}

func TestEncodeAbstainVotesDelayedHare(t *testing.T) {
	const (
		size = 4
	)
	s := sim.New(sim.WithLayerSize(size))
	s.Setup()

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	tortoise := tortoiseFromSimState(t, s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)))

	var last types.LayerID
	for _, lid := range sim.GenLayers(s,
		sim.WithSequence(1, sim.WithNumBlocks(1)),
		sim.WithSequence(1, sim.WithNumBlocks(1), sim.WithoutHareOutput()),
		sim.WithSequence(1, sim.WithNumBlocks(1)),
	) {
		last = lid
		tortoise.TallyVotes(lid)
	}
	require.Equal(t, last.Sub(3), tortoise.LatestComplete())

	tortoise.TallyVotes(last.Add(1))
	votes, err := tortoise.EncodeVotes(context.Background(), EncodeVotesWithCurrent(last.Add(1)))
	require.NoError(t, err)
	blocks, err := blocks.Layer(s.GetState(0).DB, last)
	require.NoError(t, err)
	var supported []types.Vote
	for _, block := range blocks {
		supported = append(supported, types.Vote{
			ID: block.ID(), LayerID: block.LayerIndex, Height: block.TickHeight,
		})
	}
	require.Equal(t, supported, votes.Support)
	require.Equal(t, []types.LayerID{types.LayerID(9)}, votes.Abstain)
}

func defaultTestConfig() Config {
	return Config{
		LayerSize:                defaultTestLayerSize,
		Hdist:                    defaultTestHdist,
		Zdist:                    defaultTestZdist,
		WindowSize:               defaultTestWindowSize,
		BadBeaconVoteDelayLayers: defaultVoteDelays,
		MaxExceptions:            int(defaultTestHdist) * defaultTestLayerSize * 100,
	}
}

func tortoiseFromSimState(tb testing.TB, state sim.State, opts ...Opt) *recoveryAdapter {
	trtl, err := New(state.Atxdata, opts...)
	require.NoError(tb, err)
	return &recoveryAdapter{
		TB:       tb,
		Tortoise: trtl,
		db:       state.DB.Database,
		atxdata:  state.Atxdata,
	}
}

func defaultAlgorithm(tb testing.TB) *Tortoise {
	tb.Helper()
	trtl, err := New(
		atxsdata.New(),
		WithConfig(defaultTestConfig()),
		WithLogger(zaptest.NewLogger(tb)),
	)
	require.NoError(tb, err)
	return trtl
}

func TestCalculateOpinionWithThreshold(t *testing.T) {
	for _, tc := range []struct {
		desc      string
		expect    sign
		vote      weight
		threshold weight
	}{
		{
			desc:      "Support",
			expect:    support,
			vote:      fixed.From(6),
			threshold: fixed.From(5),
		},
		{
			desc:      "Abstain",
			expect:    abstain,
			vote:      fixed.From(3),
			threshold: fixed.From(5),
		},
		{
			desc:      "AbstainZero",
			expect:    abstain,
			vote:      fixed.From(0),
			threshold: fixed.From(5),
		},
		{
			desc:      "Against",
			expect:    against,
			vote:      fixed.From(-6),
			threshold: fixed.From(5),
		},
		{
			desc:      "Support",
			expect:    support,
			vote:      fixed.From(121),
			threshold: fixed.From(120),
		},
		{
			desc:      "Abstain",
			expect:    abstain,
			vote:      fixed.From(120),
			threshold: fixed.From(120),
		},
		{
			desc:      "Abstain",
			expect:    abstain,
			vote:      fixed.From(-120),
			threshold: fixed.From(120),
		},
		{
			desc:      "Against",
			expect:    against,
			vote:      fixed.From(-121),
			threshold: fixed.From(120),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.EqualValues(t, tc.expect,
				crossesThreshold(tc.vote, tc.threshold))
		})
	}
}

func TestComputeExpectedWeight(t *testing.T) {
	genesis := types.GetEffectiveGenesis()
	require.EqualValues(
		t,
		4,
		types.GetLayersPerEpoch(),
		"expecting layers per epoch to be 4. adjust test if it will change",
	)
	for _, tc := range []struct {
		desc         string
		target, last types.LayerID
		totals       []uint64 // total weights starting from (target, last]
		expect       float64
	}{
		{
			desc:   "SingleIncompleteEpoch",
			target: genesis,
			last:   genesis.Add(2),
			totals: []uint64{10},
			expect: 5,
		},
		{
			desc:   "SingleCompleteEpoch",
			target: genesis,
			last:   genesis.Add(4),
			totals: []uint64{10},
			expect: 10,
		},
		{
			desc:   "MultipleIncompleteEpochs",
			target: genesis.Add(2),
			last:   genesis.Add(7),
			totals: []uint64{8, 12},
			expect: 13,
		},
		{
			desc:   "IncompleteEdges",
			target: genesis.Add(2),
			last:   genesis.Add(13),
			totals: []uint64{4, 12, 12, 4},
			expect: 2 + 12 + 12 + 1,
		},
		{
			desc:   "MultipleCompleteEpochs",
			target: genesis,
			last:   genesis.Add(8),
			totals: []uint64{8, 12},
			expect: 20,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			var (
				db     = statesql.InMemory()
				epochs = map[types.EpochID]*epochInfo{}
				first  = tc.target.Add(1).GetEpoch()
			)
			for i, weight := range tc.totals {
				eid := first + types.EpochID(i)
				atx := &types.ActivationTx{
					PublishEpoch: eid - 1,
					Weight:       weight,
				}
				atx.SetID(types.RandomATXID())
				atx.SetReceived(time.Now())
				require.NoError(t, atxs.Add(db, atx, types.AtxBlob{}))
			}
			for lid := tc.target.Add(1); !lid.After(tc.last); lid = lid.Add(1) {
				weight, _, err := extractAtxsData(db, lid.GetEpoch())
				require.NoError(t, err)
				epochs[lid.GetEpoch()] = &epochInfo{weight: fixed.New64(int64(weight))}
			}

			weight := computeExpectedWeight(epochs, tc.target, tc.last)
			require.InEpsilon(t, tc.expect, weight.Float(), 0.0001)
		})
	}
}

func extractAtxsData(db sql.Executor, target types.EpochID) (uint64, uint64, error) {
	var (
		weight  uint64
		heights []uint64
	)
	if err := atxs.IterateAtxsOps(db, builder.FilterEpochOnly(target-1), func(atx *types.ActivationTx) bool {
		weight += atx.Weight
		heights = append(heights, atx.TickHeight())
		return true
	}); err != nil {
		return 0, 0, fmt.Errorf("computing epoch data for %d: %w", target, err)
	}
	return weight, getMedian(heights), nil
}

func TestOutOfOrderLayersAreVerified(t *testing.T) {
	// increase layer size reduce test flakiness
	const size = 10
	s := sim.New(sim.WithLayerSize(size))
	s.Setup()

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	tortoise := tortoiseFromSimState(t, s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)))

	var (
		last     types.LayerID
		verified types.LayerID
	)
	for _, lid := range sim.GenLayers(s,
		sim.WithSequence(1),
		sim.WithSequence(1, sim.WithNextReorder(1)),
		sim.WithSequence(3),
	) {
		last = lid
		tortoise.TallyVotes(lid)
		verified = tortoise.LatestComplete()
	}
	require.Equal(t, last.Sub(1), verified)
}

type updater interface {
	Updates() []result.Layer
}

func processBlockUpdates(tb testing.TB, tt updater, db sql.Executor) {
	for _, layer := range tt.Updates() {
		for _, block := range layer.Blocks {
			if block.Valid {
				require.NoError(tb, blocks.SetValid(db, block.Header.ID))
			} else if block.Invalid {
				require.NoError(tb, blocks.SetInvalid(db, block.Header.ID))
			}
		}
	}
}

func TestLongTermination(t *testing.T) {
	t.Run("hare output exists", func(t *testing.T) {
		// note that test should pass without switching into full mode
		// therefore limit is lower than hdist
		const (
			size  = 10
			zdist = 2
			hdist = zdist + 3
			skip  = 1 // skipping layer generated at this position
			limit = hdist
		)
		s := sim.New(sim.WithLayerSize(size))
		s.Setup()

		cfg := defaultTestConfig()
		cfg.LayerSize = size
		cfg.Zdist = zdist
		cfg.Hdist = hdist
		tortoise := tortoiseFromSimState(
			t,
			s.GetState(0),
			WithConfig(cfg),
			WithLogger(zaptest.NewLogger(t)),
		)

		var (
			last     types.LayerID
			verified types.LayerID
		)
		for i := 0; i < limit; i++ {
			last = s.Next(sim.WithNumBlocks(1))
			if i == skip {
				continue
			}
			tortoise.TallyVotes(last)
			verified = tortoise.LatestComplete()
		}
		require.Equal(t, last.Sub(1), verified)
		processBlockUpdates(t, tortoise, s.GetState(0).DB)
		for lid := types.GetEffectiveGenesis().Add(1); lid.Before(last); lid = lid.Add(1) {
			validities, err := blocks.ContextualValidity(s.GetState(0).DB, lid)
			require.NoError(t, err)
			require.Len(t, validities, 1, "layer=%s", lid)
			for _, validity := range validities {
				require.True(t, validity.Validity, "layer=%s block=%s", lid, validity.ID)
			}
		}
	})
	t.Run("no hare output for this node", func(t *testing.T) {
		// layers won't be verified within hdist, since everyones opinion
		// will be different from this node opinion
		const (
			size  = 4
			zdist = 2
			hdist = zdist + 3
			skip  = 1 // skipping layer generated at this position
			limit = hdist - 1
		)
		s := sim.New(sim.WithLayerSize(size))
		s.Setup(sim.WithSetupMinerRange(4, 4))

		cfg := defaultTestConfig()
		cfg.LayerSize = size
		cfg.Zdist = zdist
		cfg.Hdist = hdist
		tortoise := tortoiseFromSimState(
			t,
			s.GetState(0),
			WithConfig(cfg),
			WithLogger(zaptest.NewLogger(t)),
		)

		var (
			last     types.LayerID
			verified types.LayerID
		)
		for i := 0; i < limit; i++ {
			opts := []sim.NextOpt{sim.WithNumBlocks(1)}
			if i == skip {
				opts = append(opts, sim.WithoutHareOutput())
			}
			last = s.Next(opts...)
			if i == skip {
				continue
			}
			tortoise.TallyVotes(last)
			verified = tortoise.LatestComplete()
		}
		require.Equal(t, types.GetEffectiveGenesis().Add(skip).Sub(1), verified)
		// switch to full mode happens here
		last = s.Next(sim.WithNumBlocks(1))
		tortoise.TallyVotes(last)
		verified = tortoise.LatestComplete()
		require.Equal(t, last.Sub(1), verified)
		processBlockUpdates(t, tortoise, s.GetState(0).DB)
		for lid := types.GetEffectiveGenesis().Add(1); lid.Before(last); lid = lid.Add(1) {
			validities, err := blocks.ContextualValidity(s.GetState(0).DB, lid)
			require.NoError(t, err)
			require.Len(t, validities, 1, "layer=%s", lid)
			for _, validity := range validities {
				require.True(t, validity.Validity, "layer=%s block=%s", lid, validity.ID)
			}
		}
	})
	t.Run("empty layer", func(t *testing.T) {
		// layer will be verified within hdist, as everyones opinion will
		// be consistent with empty layer
		const (
			size  = 10
			zdist = 2
			hdist = zdist + 3
			skip  = 1 // skipping layer generated at this position
			limit = hdist
		)
		s := sim.New(sim.WithLayerSize(size))
		s.Setup()

		cfg := defaultTestConfig()
		cfg.LayerSize = size
		cfg.Zdist = zdist
		cfg.Hdist = hdist
		tortoise := tortoiseFromSimState(
			t,
			s.GetState(0),
			WithConfig(cfg),
			WithLogger(zaptest.NewLogger(t)),
		)

		var (
			last     types.LayerID
			verified types.LayerID
		)
		for i := 0; i < limit; i++ {
			opts := []sim.NextOpt{sim.WithNumBlocks(1)}
			if i == skip {
				opts = []sim.NextOpt{sim.WithNumBlocks(0), sim.WithEmptyHareOutput()}
			}
			last = s.Next(opts...)
			if i == skip {
				continue
			}
			tortoise.TallyVotes(last)
			verified = tortoise.LatestComplete()
		}
		require.Equal(t, last.Sub(1), verified)
		processBlockUpdates(t, tortoise, s.GetState(0).DB)
		for lid := types.GetEffectiveGenesis().Add(1); lid.Before(last); lid = lid.Add(1) {
			validities, err := blocks.ContextualValidity(s.GetState(0).DB, lid)
			require.NoError(t, err)
			if lid == types.GetEffectiveGenesis().Add(1).Add(skip) {
				require.Empty(t, validities)
			} else {
				require.Len(t, validities, 1, "layer=%s", lid)
				for _, validity := range validities {
					require.True(t, validity.Validity, "layer=%s block=%s", lid, validity.ID)
				}
			}
		}
	})
}

func benchmarkLayersHandling(b *testing.B, opts ...sim.NextOpt) {
	const size = 30
	s := sim.New(
		sim.WithLayerSize(size),
		sim.WithPath(b.TempDir()),
	)
	s.Setup()

	cfg := defaultTestConfig()
	cfg.LayerSize = size

	var lids []types.LayerID
	for i := 0; i < 200; i++ {
		lids = append(lids, s.Next(opts...))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tortoise := tortoiseFromSimState(b, s.GetState(0), WithConfig(cfg))
		for _, lid := range lids {
			tortoise.TallyVotes(lid)
		}
	}
}

func BenchmarkTortoiseLayerHandling(b *testing.B) {
	b.Run("Verifying", func(b *testing.B) {
		benchmarkLayersHandling(b)
	})
	b.Run("Full", func(b *testing.B) {
		benchmarkLayersHandling(b, sim.WithEmptyHareOutput())
	})
}

func benchmarkBaseBallot(b *testing.B, opts ...sim.NextOpt) {
	const size = 30
	s := sim.New(
		sim.WithLayerSize(size),
		sim.WithPath(b.TempDir()),
	)
	s.Setup()

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.WindowSize = 100
	tortoise := tortoiseFromSimState(b, s.GetState(0), WithConfig(cfg))

	var last, verified types.LayerID
	for i := 0; i < 400; i++ {
		last = s.Next(opts...)
		tortoise.TallyVotes(last)
		verified = tortoise.LatestComplete()
	}
	require.Equal(b, last.Sub(1), verified)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tortoise.EncodeVotes(context.Background())
	}
}

func BenchmarkTortoiseBaseBallot(b *testing.B) {
	benchmarkBaseBallot(b)
}

func randomRefBallot(tb testing.TB, lyrID types.LayerID, beacon types.Beacon) *types.Ballot {
	tb.Helper()

	signer, err := signing.NewEdSigner()
	require.NoError(tb, err)

	ballot := types.RandomBallot()
	ballot.Layer = lyrID
	ballot.EpochData = &types.EpochData{
		Beacon: beacon,
	}
	ballot.Signature = signer.Sign(signing.BALLOT, ballot.SignedBytes())
	require.NoError(tb, ballot.Initialize())
	ballot.SmesherID = signer.NodeID()
	return ballot
}

func TestBallotHasGoodBeacon(t *testing.T) {
	layerID := types.GetEffectiveGenesis().Add(1)
	epochBeacon := types.RandomBeacon()
	ballot := randomRefBallot(t, layerID, epochBeacon)

	trtl := defaultAlgorithm(t)

	trtl.OnBeacon(layerID.GetEpoch(), epochBeacon)
	badBeacon, err := trtl.trtl.compareBeacons(ballot.ID(), ballot.Layer, epochBeacon)
	require.NoError(t, err)
	require.False(t, badBeacon)

	// bad beacon
	beacon := types.RandomBeacon()
	require.NotEqual(t, epochBeacon, beacon)
	badBeacon, err = trtl.trtl.compareBeacons(ballot.ID(), ballot.Layer, beacon)
	require.NoError(t, err)
	require.True(t, badBeacon)
}

func TestBallotsNotProcessedWithoutBeacon(t *testing.T) {
	s := sim.New()
	s.Setup()
	simState := s.GetState(0)
	cfg := defaultTestConfig()
	tortoise := tortoiseFromSimState(t, simState, WithConfig(cfg), WithLogger(zaptest.NewLogger(t)))
	last := s.Next()

	beacon, err := beacons.Get(simState.DB, last.GetEpoch())
	require.NoError(t, err)

	require.NoError(t, beacons.Set(simState.DB, last.GetEpoch(), types.EmptyBeacon))
	tortoise.TallyVotes(last)
	_, err = tortoise.EncodeVotes(context.Background())
	require.Error(t, err)

	require.NoError(t, beacons.Set(simState.DB, last.GetEpoch(), beacon))
	// Recover layer so it picks up the beacon and retry tallying votes in the last layer
	require.NoError(t, RecoverLayer(tortoise.Tortoise, tortoise.db, simState.Atxdata, last, tortoise.OnBallot))
	tortoise.Tortoise.TallyVotes(last)
	_, err = tortoise.EncodeVotes(context.Background())
	require.NoError(t, err)
}

func TestVotesDecodingWithoutBaseBallot(t *testing.T) {
	t.Run("AllNotDecoded", func(t *testing.T) {
		s := sim.New()
		s.Setup()
		cfg := defaultTestConfig()
		tortoise := tortoiseFromSimState(
			t,
			s.GetState(0),
			WithConfig(cfg),
			WithLogger(zaptest.NewLogger(t)),
		)

		var verified types.LayerID
		for _, last := range sim.GenLayers(s, sim.WithSequence(2,
			sim.WithVoteGenerator(voteWithBaseBallot(types.BallotID{1, 1, 1})))) {
			tortoise.TallyVotes(last)
			verified = tortoise.LatestComplete()
		}
		require.Equal(t, types.GetEffectiveGenesis(), verified)
	})
	t.Run("PartiallyNotDecodedHaveNoImpact", func(t *testing.T) {
		const (
			size       = 20
			breakpoint = 18
		)
		s := sim.New(sim.WithLayerSize(size))
		s.Setup()
		cfg := defaultTestConfig()
		cfg.LayerSize = size
		tortoise := tortoiseFromSimState(
			t,
			s.GetState(0),
			WithConfig(cfg),
			WithLogger(zaptest.NewLogger(t)),
		)

		var last, verified types.LayerID
		generator := sim.WithVoteGenerator(func(rng *rand.Rand, layers []*types.Layer, i int) sim.Voting {
			if i >= breakpoint {
				return voteWithBaseBallot(types.BallotID{1, 1, 1})(rng, layers, i)
			}
			return sim.ConsistentVoting(rng, layers, i)
		})
		for _, last = range sim.GenLayers(s, sim.WithSequence(2, generator)) {
			tortoise.TallyVotes(last)
			verified = tortoise.LatestComplete()
		}
		require.Equal(t, last.Sub(1), verified)
	})
}

func TestDecodeVotes(t *testing.T) {
	t.Run("without block in state", func(t *testing.T) {
		s := sim.New()
		s.Setup()
		cfg := defaultTestConfig()
		tortoise := tortoiseFromSimState(
			t,
			s.GetState(0),
			WithConfig(cfg),
			WithLogger(zaptest.NewLogger(t)),
		)
		last := s.Next()
		tortoise.TallyVotes(last)
		ballots, err := ballots.Layer(s.GetState(0).DB, last)
		require.NoError(t, err)
		ballot := types.NewExistingBallot(
			types.BallotID{3, 3, 3},
			types.EmptyEdSignature,
			types.EmptyNodeID,
			ballots[0].Layer,
		)
		ballot.InnerBallot = ballots[0].InnerBallot
		hasher := opinionhash.New()
		supported := types.BlockID{2, 2, 2}
		hasher.WriteSupport(supported, 0)
		ballot.OpinionHash = hasher.Hash()
		ballot.Votes.Support = []types.Vote{{ID: supported, LayerID: ballot.Layer - 1}}
		_, err = tortoise.decodeBallot(ballot.ToTortoiseData())
		require.NoError(t, err)
	})
}

// gapVote will skip one layer in voting.
func gapVote(rng *rand.Rand, layers []*types.Layer, i int) sim.Voting {
	return skipLayers(1)(rng, layers, i)
}

func skipLayers(n int) sim.VotesGenerator {
	return func(rng *rand.Rand, layers []*types.Layer, _ int) sim.Voting {
		position := n + 1
		if len(layers) < position {
			panic(fmt.Sprintf("need at least %d layers", position))
		}
		baseLayer := layers[len(layers)-position]
		support := layers[len(layers)-position].Blocks()
		blts := baseLayer.Ballots()
		base := blts[rng.Intn(len(blts))]
		votes := sim.Voting{}
		votes.Base = base.ID()
		for _, block := range support {
			votes.Support = append(votes.Support, types.Vote{
				ID:      block.ID(),
				LayerID: block.LayerIndex,
				Height:  block.TickHeight,
			})
		}
		return votes
	}
}

func addSupport(support types.Vote) sim.VotesGenerator {
	return func(rng *rand.Rand, layers []*types.Layer, i int) sim.Voting {
		votes := sim.PerfectVoting(rng, layers, i)
		votes.Support = append(votes.Support, support)
		return votes
	}
}

// olderExceptions will vote for block older then base ballot.
func olderExceptions(rng *rand.Rand, layers []*types.Layer, _ int) sim.Voting {
	if len(layers) < 2 {
		panic("need at least 2 layers")
	}
	baseLayer := layers[len(layers)-1]
	blts := baseLayer.Ballots()
	base := blts[rng.Intn(len(blts))]
	voting := sim.Voting{Base: base.ID()}
	for _, layer := range layers[len(layers)-2:] {
		supported := layer.Blocks()
		for _, support := range supported {
			voting.Support = append(voting.Support, types.Vote{
				ID:      support.ID(),
				LayerID: support.LayerIndex,
				Height:  support.TickHeight,
			})
		}
	}
	return voting
}

// OutOfWindowBaseBallot creates VotesGenerator with a specific window.
// Vote generator will produce one block that uses base ballot outside the sliding window.
// NOTE that it will produce blocks as if it didn't know about blocks from higher layers.
func outOfWindowBaseBallot(n, window int) sim.VotesGenerator {
	return func(rng *rand.Rand, layers []*types.Layer, i int) sim.Voting {
		if i >= n {
			return sim.PerfectVoting(rng, layers, i)
		}
		li := len(layers) - window
		blts := layers[li].Ballots()
		base := blts[rng.Intn(len(blts))]
		supported := layers[li].Blocks()
		opinion := sim.Voting{Base: base.ID()}
		for _, support := range supported {
			opinion.Support = append(opinion.Support, types.Vote{
				ID:      support.ID(),
				LayerID: support.LayerIndex,
				Height:  support.TickHeight,
			})
		}
		return opinion
	}
}

type voter interface {
	EncodeVotes(ctx context.Context, opts ...EncodeVotesOpts) (*types.Opinion, error)
}

// tortoiseVoting is for testing that protocol makes progress using heuristic that we are
// using for the network.
func tortoiseVoting(tortoise voter) sim.VotesGenerator {
	return func(rng *rand.Rand, layers []*types.Layer, _ int) sim.Voting {
		votes, err := tortoise.EncodeVotes(context.Background())
		if err != nil {
			panic(err)
		}
		return votes.Votes
	}
}

func tortoiseVotingWithCurrent(tortoise voter) sim.VotesGenerator {
	return func(rng *rand.Rand, layers []*types.Layer, _ int) sim.Voting {
		current := types.GetEffectiveGenesis().Add(1)
		if len(layers) > 0 {
			current = layers[len(layers)-1].Index().Add(1)
		}
		votes, err := tortoise.EncodeVotes(context.Background(), EncodeVotesWithCurrent(current))
		if err != nil {
			panic(err)
		}
		return votes.Votes
	}
}

func TestOnBeacon(t *testing.T) {
	cfg := defaultTestConfig()
	tortoise, err := New(atxsdata.New(), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)))
	require.NoError(t, err)

	genesis := types.GetEffectiveGenesis()
	tortoise.OnBeacon(genesis.GetEpoch()-1, types.Beacon{1})
	require.Nil(t, tortoise.trtl.epoch(genesis.GetEpoch()-1).beacon)
	tortoise.OnBeacon(genesis.GetEpoch(), types.Beacon{1})
	require.Equal(t, types.Beacon{1}, *tortoise.trtl.epoch(genesis.GetEpoch()).beacon)

	newGenesis := genesis.Add(types.GetLayersPerEpoch()*10 + types.GetLayersPerEpoch()/2)
	types.SetEffectiveGenesis(newGenesis.Uint32())
	defer func() {
		types.SetEffectiveGenesis(genesis.Uint32())
	}()
	tortoise, err = New(atxsdata.New(), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)))
	require.NoError(t, err)
	tortoise.OnBeacon(newGenesis.GetEpoch()-1, types.Beacon{2})
	require.Nil(t, tortoise.trtl.epoch(newGenesis.GetEpoch()-1).beacon)
	tortoise.OnBeacon(newGenesis.GetEpoch(), types.Beacon{2})
	require.Equal(t, types.Beacon{2}, *tortoise.trtl.epoch(newGenesis.GetEpoch()).beacon)
}

func TestBaseBallotGenesis(t *testing.T) {
	s := sim.New()
	cfg := defaultTestConfig()
	tortoise := tortoiseFromSimState(t, s.GetState(0), WithConfig(cfg),
		WithLogger(zaptest.NewLogger(t)))

	votes, err := tortoise.EncodeVotes(context.Background())
	require.NoError(t, err)
	require.Empty(t, votes.Support)
	require.Empty(t, votes.Against)
	require.Empty(t, votes.Abstain)
	require.Empty(t, votes.Base)
}

func ensureBaseAndExceptionsFromLayer(
	tb testing.TB,
	lid types.LayerID,
	votes *types.Opinion,
	db sql.Executor,
) {
	tb.Helper()

	blts, err := ballots.Get(db, votes.Base)
	require.NoError(tb, err)
	require.Equal(tb, lid, blts.Layer)

	for _, vote := range votes.Support {
		block, err := blocks.Get(db, vote.ID)
		require.NoError(tb, err)
		require.Equal(
			tb,
			lid,
			block.LayerIndex,
			"block=%s block layer=%s last=%s",
			block.ID(),
			block.LayerIndex,
			lid,
		)
	}
}

func TestBaseBallotEvictedBlock(t *testing.T) {
	const size = 12
	s := sim.New(
		sim.WithLayerSize(size),
	)
	s.Setup()

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.WindowSize = 10
	tortoise := tortoiseFromSimState(t, s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)))

	var last, verified types.LayerID

	// turn GenLayers into on-demand generator, so that later case can be placed as a step
	for _, lid := range sim.GenLayers(s,
		sim.WithSequence(30),
		sim.WithSequence(1, sim.WithVoteGenerator(
			outOfWindowBaseBallot(1, int(cfg.WindowSize)*2),
		)),
		sim.WithSequence(2),
	) {
		last = lid
		updates := tortoise.Updates() // drain pending
		tortoise.OnApplied(updates[0].Layer, updates[0].Opinion)
		tortoise.TallyVotes(lid)
		verified = tortoise.LatestComplete()
	}
	require.Equal(t, last.Sub(1), verified)
	for i := 0; i < 10; i++ {
		votes, err := tortoise.EncodeVotes(context.Background())
		require.NoError(t, err)
		ensureBaseAndExceptionsFromLayer(t, last, votes, s.GetState(0).DB)

		last = s.Next(sim.WithVoteGenerator(tortoiseVoting(tortoise)))
		tortoise.TallyVotes(last)
		verified = tortoise.LatestComplete()
		require.Equal(t, last.Sub(1), verified)
	}
}

func TestBaseBallotPrioritization(t *testing.T) {
	genesis := types.GetEffectiveGenesis()
	for _, tc := range []struct {
		desc     string
		seqs     []sim.Sequence
		expected types.LayerID
		window   uint32
	}{
		{
			desc: "GoodBlocksOrderByLayer",
			seqs: []sim.Sequence{
				sim.WithSequence(5),
			},
			expected: genesis.Add(5),
			window:   1,
		},
		{
			desc: "BadBlocksIgnored",
			seqs: []sim.Sequence{
				sim.WithSequence(5),
				sim.WithSequence(5, sim.WithVoteGenerator(olderExceptions)),
			},
			expected: genesis.Add(10),
			window:   5,
		},
		{
			desc: "BadBlocksOverflowAfterEviction",
			seqs: []sim.Sequence{
				sim.WithSequence(5),
				sim.WithSequence(20, sim.WithVoteGenerator(olderExceptions)),
			},
			expected: genesis.Add(25),
			window:   5,
		},
		{
			desc: "ConflictingVotesIgnored",
			seqs: []sim.Sequence{
				sim.WithSequence(5),
				sim.WithSequence(1, sim.WithVoteGenerator(gapVote)),
			},
			expected: genesis.Add(6),
			window:   10,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			const size = 10
			s := sim.New(
				sim.WithLayerSize(size),
			)
			s.Setup()

			cfg := defaultTestConfig()
			cfg.LayerSize = size
			cfg.WindowSize = tc.window
			tortoise := tortoiseFromSimState(
				t,
				s.GetState(0),
				WithConfig(cfg),
				WithLogger(zaptest.NewLogger(t)),
			)

			for _, lid := range sim.GenLayers(s, tc.seqs...) {
				tortoise.TallyVotes(lid)
			}

			votes, err := tortoise.EncodeVotes(context.Background())
			require.NoError(t, err)
			ballot, err := ballots.Get(s.GetState(0).DB, votes.Base)
			require.NoError(t, err)
			require.Equal(t, tc.expected, ballot.Layer)
		})
	}
}

// splitVoting partitions votes into two halves.
func splitVoting(n int) sim.VotesGenerator {
	return func(_ *rand.Rand, layers []*types.Layer, i int) sim.Voting {
		var (
			supported []*types.Block
			last      = layers[len(layers)-1]
			blocks    = last.Blocks()
			half      = len(blocks) / 2
			ballots   = last.BallotIDs()
			base      types.BallotID
		)
		if len(blocks) < 2 {
			panic("make sure that the previous layer has atleast 2 blocks in it")
		}
		if i < n/2 {
			base = ballots[0]
			supported = blocks[:half]
		} else {
			base = ballots[len(ballots)-1]
			supported = blocks[half:]
		}
		voting := sim.Voting{Base: base}
		for _, support := range supported {
			voting.Support = append(voting.Support, types.Vote{
				ID:      support.ID(),
				LayerID: support.LayerIndex,
				Height:  support.TickHeight,
			})
		}
		return voting
	}
}

func ensureBallotLayerWithin(
	tb testing.TB,
	db sql.Executor,
	ballotID types.BallotID,
	from, to types.LayerID,
) {
	tb.Helper()

	ballot, err := ballots.Get(db, ballotID)
	require.NoError(tb, err)
	require.True(tb, !ballot.Layer.Before(from) && !ballot.Layer.After(to),
		"%s not in [%s,%s]", ballot.Layer, from, to,
	)
}

func ensureBlockLayerWithin(
	tb testing.TB,
	db sql.Executor,
	bid types.BlockID,
	from, to types.LayerID,
) {
	tb.Helper()

	block, err := blocks.Get(db, bid)
	require.NoError(tb, err)
	require.True(tb, !block.LayerIndex.Before(from) && !block.LayerIndex.After(to),
		"%s not in [%s,%s]", block.LayerIndex, from, to,
	)
}

func TestWeakCoinVoting(t *testing.T) {
	const (
		size  = 4
		hdist = 2
	)
	s := sim.New(
		sim.WithLayerSize(size),
	)
	s.Setup(
		sim.WithSetupUnitsRange(1, 1),
		sim.WithSetupMinerRange(size, size),
	)

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Hdist = hdist
	cfg.Zdist = hdist

	var (
		tortoise = tortoiseFromSimState(
			t,
			s.GetState(0),
			WithConfig(cfg),
			WithLogger(zaptest.NewLogger(t)),
		)
		last    types.LayerID
		genesis = types.GetEffectiveGenesis()
	)

	for _, lid := range sim.GenLayers(s,
		sim.WithSequence(1, sim.WithNumBlocks(2), sim.WithEmptyHareOutput()),
		sim.WithSequence(hdist,
			sim.WithNumBlocks(2),
			sim.WithEmptyHareOutput(),
			sim.WithVoteGenerator(splitVoting(size)),
		),
	) {
		last = lid
		tortoise.TallyVotes(lid)
	}
	require.Equal(t, genesis, tortoise.LatestComplete())

	require.NoError(t, layers.SetWeakCoin(s.GetState(0).DB, last.Add(1), true))
	votes, err := tortoise.EncodeVotes(context.Background(), EncodeVotesWithCurrent(last.Add(1)))
	require.NoError(t, err)

	require.Len(t, votes.Support, 2)
	block, err := blocks.Get(s.GetState(0).DB, votes.Support[0].ID)
	require.NoError(t, err)
	require.Equal(t, block.LayerIndex, genesis.Add(2))

	for i := 0; i < 10; i++ {
		last = s.Next(sim.WithVoteGenerator(tortoiseVoting(tortoise)))
		tortoise.TallyVotes(last)
	}
	require.Equal(t, last.Sub(1), tortoise.LatestComplete())
}

func TestVoteAgainstSupportedByBaseBallot(t *testing.T) {
	const size = 10
	s := sim.New(
		sim.WithLayerSize(size),
	)
	s.Setup()

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Hdist = 1
	cfg.Zdist = 1

	var (
		tortoise = tortoiseFromSimState(
			t,
			s.GetState(0),
			WithConfig(cfg),
			WithLogger(zaptest.NewLogger(t)),
		)
		last, verified types.LayerID
		genesis        = types.GetEffectiveGenesis()
	)
	for _, last = range sim.GenLayers(s,
		sim.WithSequence(3, sim.WithNumBlocks(1)),
	) {
		tortoise.TallyVotes(last)
		verified = tortoise.LatestComplete()
	}
	require.Equal(t, last.Sub(1), verified)

	unsupported := map[types.BlockID]struct{}{}
	for lid := genesis.Add(1); lid.Before(last); lid = lid.Add(1) {
		hareOutput, err := certificates.GetHareOutput(s.GetState(0).DB, lid)
		require.NoError(t, err)
		if hareOutput != types.EmptyBlockID {
			layer := tortoise.trtl.layer(lid)
			for _, block := range layer.blocks {
				block.validity = against
				block.hare = against
			}
			unsupported[hareOutput] = struct{}{}
		}
	}

	// remove good ballots and genesis to make tortoise select one of the later ballots.
	for _, ballot := range tortoise.trtl.ballotRefs {
		ballot.conditions.badBeacon = true
	}

	votes, err := tortoise.EncodeVotes(context.Background())
	require.NoError(t, err)
	ensureBallotLayerWithin(t, s.GetState(0).DB, votes.Base, last, last)

	require.Len(t, votes.Against, len(unsupported))
	for _, vote := range votes.Against {
		ensureBlockLayerWithin(t, s.GetState(0).DB, vote.ID, genesis, last.Sub(1))
		require.Contains(t, unsupported, vote.ID)
	}
	require.Len(t, votes.Support, numValidBlock)
}

func TestComputeLocalOpinion(t *testing.T) {
	const (
		size  = 10
		hdist = 3
	)
	genesis := types.GetEffectiveGenesis()
	for _, tc := range []struct {
		desc     string
		seqs     []sim.Sequence
		lid      types.LayerID
		expected sign
	}{
		{
			desc: "ContextuallyValid",
			seqs: []sim.Sequence{
				sim.WithSequence(4),
			},
			lid:      genesis.Add(1),
			expected: support,
		},
		{
			desc: "ContextuallyInvalid",
			seqs: []sim.Sequence{
				sim.WithSequence(1, sim.WithEmptyHareOutput()),
				sim.WithSequence(1,
					sim.WithVoteGenerator(gapVote),
				),
				sim.WithSequence(hdist),
			},
			lid:      genesis.Add(1),
			expected: against,
		},
		{
			desc: "SupportedByHare",
			seqs: []sim.Sequence{
				sim.WithSequence(4),
			},
			lid:      genesis.Add(4),
			expected: support,
		},
		{
			desc: "NotSupportedByHare",
			seqs: []sim.Sequence{
				sim.WithSequence(3),
				sim.WithSequence(1, sim.WithEmptyHareOutput()),
			},
			lid:      genesis.Add(4),
			expected: against,
		},
		{
			desc: "WithUnfinishedHare",
			seqs: []sim.Sequence{
				sim.WithSequence(3),
				sim.WithSequence(1, sim.WithoutHareOutput()),
			},
			lid:      genesis.Add(4),
			expected: abstain,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			s := sim.New(
				sim.WithLayerSize(size),
			)
			s.Setup(sim.WithSetupUnitsRange(1, 1))

			cfg := defaultTestConfig()
			cfg.LayerSize = size
			cfg.Hdist = hdist
			cfg.Zdist = hdist
			tortoise := tortoiseFromSimState(t, s.GetState(0), WithConfig(cfg))
			for _, lid := range sim.GenLayers(s, tc.seqs...) {
				tortoise.TallyVotes(lid)
			}

			blks, err := blocks.Layer(s.GetState(0).DB, tc.lid)
			require.NoError(t, err)
			for _, block := range blks {
				tortoise.OnBlock(block.ToVote())
			}
			for _, block := range blks {
				header := block.ToVote()
				vote, _ := getLocalVote(
					cfg,
					tortoise.trtl.state.verified,
					tortoise.trtl.state.last,
					tortoise.trtl.getBlock(header))
				if tc.expected == support {
					hareOutput, err := certificates.GetHareOutput(s.GetState(0).DB, tc.lid)
					require.NoError(t, err)
					// only one block is supported
					if header.ID == hareOutput {
						require.Equal(t, tc.expected, vote, "block id %s", header.ID)
					} else {
						require.Equal(t, against, vote, "block id %s", header.ID)
					}
				} else {
					require.Equal(t, tc.expected, vote, "block id %s", header.ID)
				}
			}
		})
	}
}

func TestComputeBallotWeight(t *testing.T) {
	type testBallot struct {
		TotalEligibilities int
		RefBallot          int // optional index to the ballot, use it in test if active set is nil
		ATX                int // non optional index to this ballot atx
		ExpectedWeight     float64
		Eligibilities      int
	}

	for _, tc := range []struct {
		desc                      string
		atxs                      []uint
		ballots                   []testBallot
		layerSize, layersPerEpoch uint32
	}{
		{
			desc:           "total eligibilities",
			atxs:           []uint{50, 50, 50},
			layerSize:      5,
			layersPerEpoch: 3,
			ballots: []testBallot{
				{TotalEligibilities: 5, ATX: 0, ExpectedWeight: 10, Eligibilities: 1},
				{TotalEligibilities: 5, ATX: 1, ExpectedWeight: 10, Eligibilities: 1},
			},
		},
		{
			desc:           "ref ballot",
			atxs:           []uint{50, 50, 50},
			layerSize:      5,
			layersPerEpoch: 3,
			ballots: []testBallot{
				{TotalEligibilities: 5, ATX: 0, ExpectedWeight: 10, Eligibilities: 1},
				{RefBallot: 0, ATX: 0, ExpectedWeight: 10, Eligibilities: 1},
			},
		},
		{
			desc:           "ref ballot multiple",
			atxs:           []uint{50, 50, 50},
			layerSize:      5,
			layersPerEpoch: 3,
			ballots: []testBallot{
				{TotalEligibilities: 5, ATX: 0, ExpectedWeight: 10, Eligibilities: 1},
				{RefBallot: 0, ATX: 0, ExpectedWeight: 20, Eligibilities: 2},
			},
		},
		{
			desc:           "ref ballot multiple",
			atxs:           []uint{50, 50, 50},
			layerSize:      5,
			layersPerEpoch: 3,
			ballots: []testBallot{
				{TotalEligibilities: 5, ATX: 0, ExpectedWeight: 20, Eligibilities: 2},
				{RefBallot: 0, ATX: 0, ExpectedWeight: 10, Eligibilities: 1},
			},
		},
		{
			desc:           "ref ballot multiple",
			atxs:           []uint{50, 50, 50},
			layerSize:      5,
			layersPerEpoch: 3,
			ballots: []testBallot{
				{TotalEligibilities: 5, ATX: 0, ExpectedWeight: 20, Eligibilities: 2},
				{RefBallot: 0, ATX: 0, ExpectedWeight: 30, Eligibilities: 3},
			},
		},
		{
			desc:           "different weight",
			atxs:           []uint{50, 50, 100, 100},
			layerSize:      5,
			layersPerEpoch: 2,
			ballots: []testBallot{
				{TotalEligibilities: 5, ATX: 0, ExpectedWeight: 10, Eligibilities: 1},
				{TotalEligibilities: 5, ATX: 2, ExpectedWeight: 20, Eligibilities: 1},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			current := types.GetLayersPerEpoch()
			t.Cleanup(func() {
				types.SetLayersPerEpoch(current)
			})
			types.SetLayersPerEpoch(tc.layersPerEpoch)

			var (
				blts   []*types.Ballot
				atxids []types.ATXID
			)

			cfg := DefaultConfig()
			cfg.LayerSize = tc.layerSize
			trtl, err := New(atxsdata.New(), WithLogger(zaptest.NewLogger(t)), WithConfig(cfg))
			require.NoError(t, err)
			lid := types.LayerID(111)
			for _, weight := range tc.atxs {
				atxID := types.RandomATXID()
				atx := atxsdata.ATX{
					Weight: uint64(weight),
					Height: 1,
				}
				trtl.trtl.atxsdata.AddAtx(lid.GetEpoch(), atxID, &atx)
				trtl.OnAtx(lid.GetEpoch(), atxID, &atx)
				atxids = append(atxids, atxID)
			}

			var currentJ int
			for _, b := range tc.ballots {
				ballot := &types.Ballot{
					InnerBallot: types.InnerBallot{
						Layer: lid,
						AtxID: atxids[b.ATX],
					},
				}
				for j := 0; j < b.Eligibilities; j++ {
					ballot.EligibilityProofs = append(ballot.EligibilityProofs,
						types.VotingEligibility{J: uint32(currentJ)})
					currentJ++
				}
				if b.TotalEligibilities != 0 {
					ballot.EpochData = &types.EpochData{
						ActiveSetHash:    types.Hash32{1, 2, 3},
						EligibilityCount: uint32(b.TotalEligibilities),
					}
				} else {
					ballot.RefBallot = blts[b.RefBallot].ID()
				}

				sig, err := signing.NewEdSigner()
				require.NoError(t, err)

				ballot.Signature = sig.Sign(signing.BALLOT, ballot.SignedBytes())
				require.NoError(t, ballot.Initialize())
				ballot.SmesherID = sig.NodeID()
				blts = append(blts, ballot)

				trtl.OnBallot(ballot.ToTortoiseData())
				ref := trtl.trtl.ballotRefs[ballot.ID()]
				require.InEpsilon(t, b.ExpectedWeight, ref.weight.Float(), 0.0001)
			}
		})
	}
}

func TestNetworkRecoversFromFullPartition(t *testing.T) {
	const size = 8
	s1 := sim.New(
		sim.WithLayerSize(size),
		sim.WithStates(2),
		sim.WithLogger(zaptest.NewLogger(t)),
	)
	s1.Setup(
		sim.WithSetupMinerRange(8, 8),
	)

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Hdist = 3
	cfg.Zdist = 3
	cfg.BadBeaconVoteDelayLayers = types.GetLayersPerEpoch()

	var (
		tortoise1 = tortoiseFromSimState(t, s1.GetState(0), WithConfig(cfg),
			WithLogger(zaptest.NewLogger(t).Named("first")))
		tortoise2 = tortoiseFromSimState(t, s1.GetState(1), WithConfig(cfg),
			WithLogger(zaptest.NewLogger(t).Named("second")))
		last types.LayerID
	)

	for i := 0; i < int(types.GetLayersPerEpoch()); i++ {
		last = s1.Next(sim.WithNumBlocks(1))
		tortoise1.TallyVotes(last)
		tortoise2.TallyVotes(last)
		processBlockUpdates(t, tortoise1, s1.GetState(0).DB)
		processBlockUpdates(t, tortoise2, s1.GetState(1).DB)
	}
	require.Equal(t, last.Sub(1), tortoise1.LatestComplete())
	require.Equal(t, last.Sub(1), tortoise2.LatestComplete())

	gens := s1.Split()
	require.Len(t, gens, 2)
	s1, s2 := gens[0], gens[1]

	partitionStart := last
	for i := 0; i < int(types.GetLayersPerEpoch()); i++ {
		last = s1.Next(sim.WithNumBlocks(1))
		tortoise1.TallyVotes(last)
		tortoise2.TallyVotes(s2.Next(sim.WithNumBlocks(1)))
		processBlockUpdates(t, tortoise1, s1.GetState(0).DB)
		processBlockUpdates(t, tortoise2, s2.GetState(0).DB)
	}

	// sync missing state and rerun immediately, both instances won't make progress
	// because weight increases, and each side doesn't have enough weight in votes
	// and then do rerun
	partitionEnd := last
	s1.Merge(s2)
	s1.GetState(0).Atxdata.IterateInEpoch(
		partitionEnd.GetEpoch(), func(id types.ATXID, atx *atxsdata.ATX) {
			tortoise1.OnAtx(partitionEnd.GetEpoch(), id, atx)
			tortoise2.OnAtx(partitionEnd.GetEpoch(), id, atx)
		})
	for lid := partitionStart; !lid.After(partitionEnd); lid = lid.Add(1) {
		mergedBlocks, err := blocks.Layer(s1.GetState(0).DB, lid)
		require.NoError(t, err)
		for _, block := range mergedBlocks {
			tortoise1.OnBlock(block.ToVote())
			tortoise2.OnBlock(block.ToVote())
		}
		mergedBallots, err := ballots.Layer(s1.GetState(0).DB, lid)
		require.NoError(t, err)
		for _, ballot := range mergedBallots {
			tortoise1.OnBallot(ballot.ToTortoiseData())
			tortoise2.OnBallot(ballot.ToTortoiseData())
		}
	}

	tortoise1.TallyVotes(last)
	tortoise2.TallyVotes(last)
	processBlockUpdates(t, tortoise1, s1.GetState(0).DB)
	processBlockUpdates(t, tortoise2, s1.GetState(0).DB)

	// make enough progress to cross global threshold with new votes
	for i := 0; i < int(types.GetLayersPerEpoch())*4; i++ {
		last = s1.Next(
			sim.WithNumBlocks(1),
			sim.WithVoteGenerator(func(rng *rand.Rand, layers []*types.Layer, i int) sim.Voting {
				if i < size/2 {
					return tortoiseVoting(tortoise1)(rng, layers, i)
				}
				return tortoiseVoting(tortoise2)(rng, layers, i)
			}),
		)
		tortoise1.TallyVotes(last)
		tortoise2.TallyVotes(last)
		processBlockUpdates(t, tortoise1, s1.GetState(0).DB)
		processBlockUpdates(t, tortoise2, s1.GetState(0).DB)
	}

	require.Equal(t, last.Sub(1), tortoise1.LatestComplete())
	require.Equal(t, last.Sub(1), tortoise2.LatestComplete())

	// each partition has one valid block
	for lid := partitionStart.Add(1); !lid.After(partitionEnd); lid = lid.Add(1) {
		validities, err := blocks.ContextualValidity(s1.GetState(0).DB, lid)
		var valid []types.BlockID
		for _, validity := range validities {
			if validity.Validity {
				valid = append(valid, validity.ID)
			}
		}
		require.NoError(t, err)
		assert.Len(t, valid, numValidBlock*2, "layer=%s", lid)
	}
}

func TestVerifyLayerByWeightNotSize(t *testing.T) {
	const size = 8
	s := sim.New(
		sim.WithLayerSize(size),
	)
	// change weight to be atleast the same as size
	s.Setup(
		sim.WithSetupMinerRange(size, size),
	)

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	tortoise := tortoiseFromSimState(t, s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)))

	var last types.LayerID
	for _, last = range sim.GenLayers(s,
		sim.WithSequence(2, sim.WithNumBlocks(1)),
		sim.WithSequence(1, sim.WithNumBlocks(1), sim.WithLayerSizeOverwrite(size/3)),
	) {
		tortoise.TallyVotes(last)
	}
	require.Equal(t, last.Sub(3), tortoise.LatestComplete())
}

func perfectVotingFirstBaseBallot(_ *rand.Rand, layers []*types.Layer, _ int) sim.Voting {
	baseLayer := layers[len(layers)-1]
	supported := layers[len(layers)-1].Blocks()[0:1]
	blts := baseLayer.Ballots()
	base := blts[0]
	voting := sim.Voting{Base: base.ID()}
	for _, support := range supported {
		voting.Support = append(voting.Support, types.Vote{
			ID:      support.ID(),
			LayerID: support.LayerIndex,
			Height:  support.TickHeight,
		})
	}
	return voting
}

func abstainVoting(_ *rand.Rand, layers []*types.Layer, _ int) sim.Voting {
	baseLayer := layers[len(layers)-1]
	blts := baseLayer.Ballots()
	return sim.Voting{Base: blts[0].ID(), Abstain: []types.LayerID{baseLayer.Index()}}
}

func TestAbstainVotingVerifyingMode(t *testing.T) {
	const size = 10
	s := sim.New(sim.WithLayerSize(size))
	s.Setup()

	cfg := defaultTestConfig()
	cfg.LayerSize = size

	tortoise := tortoiseFromSimState(t, s.GetState(0), WithLogger(zaptest.NewLogger(t)), WithConfig(cfg))
	var last, verified types.LayerID
	for _, last = range sim.GenLayers(s,
		sim.WithSequence(1),
		sim.WithSequence(10, sim.WithVoteGenerator(
			sim.VaryingVoting(1, perfectVotingFirstBaseBallot, abstainVoting),
		)),
	) {
		tortoise.TallyVotes(last)
		verified = tortoise.LatestComplete()
	}
	require.Equal(t, last.Sub(2), verified)
	for _, last = range sim.GenLayers(s,
		sim.WithSequence(1, sim.WithVoteGenerator(perfectVotingFirstBaseBallot)),
	) {
		tortoise.TallyVotes(last)
		verified = tortoise.LatestComplete()
	}
	require.Equal(t, last.Sub(1), verified)
}

func voteWithBaseBallot(base types.BallotID) sim.VotesGenerator {
	return func(rng *rand.Rand, layers []*types.Layer, i int) sim.Voting {
		voting := sim.PerfectVoting(rng, layers, i)
		voting.Base = base
		return voting
	}
}

func voteForBlock(block *types.Block) sim.VotesGenerator {
	return func(rng *rand.Rand, layers []*types.Layer, i int) sim.Voting {
		voting := sim.PerfectVoting(rng, layers, i)
		voting.Support = append(voting.Support, types.Vote{
			ID:      block.ID(),
			LayerID: block.LayerIndex,
			Height:  block.TickHeight,
		})
		return voting
	}
}

func addAgainst(vote types.Vote) sim.VotesGenerator {
	return func(rng *rand.Rand, layers []*types.Layer, i int) sim.Voting {
		voting := sim.PerfectVoting(rng, layers, i)
		voting.Against = append(voting.Against, vote)
		return voting
	}
}

func TestLateBaseBallot(t *testing.T) {
	const size = 10
	s := sim.New(sim.WithLayerSize(size))
	s.Setup(sim.WithSetupUnitsRange(2, 2))

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Hdist = 1
	cfg.Zdist = cfg.Hdist

	tortoise := tortoiseFromSimState(t, s.GetState(0), WithLogger(zaptest.NewLogger(t)), WithConfig(cfg))
	var last, verified types.LayerID
	for _, last = range sim.GenLayers(s,
		sim.WithSequence(2, sim.WithEmptyHareOutput()),
	) {
		tortoise.TallyVotes(last)
		verified = tortoise.LatestComplete()
	}

	blts, err := ballots.Layer(s.GetState(0).DB, last)
	require.NoError(t, err)
	require.NotEmpty(t, blts)

	buf, err := codec.Encode(blts[0])
	require.NoError(t, err)
	var base types.Ballot
	require.NoError(t, codec.Decode(buf, &base))
	base.EligibilityProofs[0].J++
	require.NoError(t, base.Initialize())
	base.SmesherID = blts[0].SmesherID
	tortoise.OnBallot(base.ToTortoiseData())

	for _, last = range sim.GenLayers(s,
		sim.WithSequence(1, sim.WithVoteGenerator(voteWithBaseBallot(base.ID()))),
		sim.WithSequence(1),
	) {
		tortoise.TallyVotes(last)
		verified = tortoise.LatestComplete()
	}

	require.Equal(t, last.Sub(1), verified)
}

func TestLateBlock(t *testing.T) {
	const size = 10
	s := sim.New(sim.WithLayerSize(size))
	s.Setup(sim.WithSetupUnitsRange(2, 2))

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Hdist = 1
	cfg.Zdist = cfg.Hdist

	tortoise := tortoiseFromSimState(t, s.GetState(0), WithLogger(zaptest.NewLogger(t)), WithConfig(cfg))
	last := s.Next()
	tortoise.TallyVotes(last)

	blks, err := blocks.Layer(s.GetState(0).DB, last)
	require.NoError(t, err)
	require.NotEmpty(t, blks)

	buf, err := codec.Encode(blks[0])
	require.NoError(t, err)
	var block types.Block
	require.NoError(t, codec.Decode(buf, &block))
	require.Greater(t, len(block.TxIDs), 2)
	block.TxIDs = block.TxIDs[:2]
	block.Initialize()
	tortoise.OnBlock(block.ToVote())
	require.NoError(t, blocks.Add(s.GetState(0).DB, &block))

	for _, last = range sim.GenLayers(s,
		sim.WithSequence(1, sim.WithVoteGenerator(voteForBlock(&block))),
		sim.WithSequence(1),
	) {
		tortoise.TallyVotes(last)
	}

	require.Equal(t, last.Sub(1), tortoise.LatestComplete())

	processBlockUpdates(t, tortoise, s.GetState(0).DB)
	valid, err := blocks.IsValid(s.GetState(0).DB, block.ID())
	require.NoError(t, err)
	require.True(t, valid)
}

func TestMaliciousBallotsAreIgnored(t *testing.T) {
	const size = 10
	s := sim.New(sim.WithLayerSize(size))
	s.Setup()

	cfg := defaultTestConfig()
	cfg.LayerSize = size

	tortoise := tortoiseFromSimState(t, s.GetState(0), WithLogger(zaptest.NewLogger(t)), WithConfig(cfg))
	var last types.LayerID
	for _, last = range sim.GenLayers(s, sim.WithSequence(int(types.GetLayersPerEpoch()))) {
	}

	blts, err := ballots.Layer(s.GetState(0).DB, last)
	require.NoError(t, err)
	for _, ballot := range blts {
		tortoise.OnMalfeasance(ballot.SmesherID)
	}

	tortoise.TallyVotes(s.Next())
	require.Equal(t, tortoise.LatestComplete(), types.GetEffectiveGenesis())

	votes, err := tortoise.EncodeVotes(context.Background())
	require.NoError(t, err)
	require.Empty(t, votes.Base)
}

func TestStateManagement(t *testing.T) {
	const (
		size   = 10
		hdist  = 4
		window = 2
	)
	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Hdist = hdist
	cfg.Zdist = hdist
	cfg.WindowSize = window

	s := sim.New(
		sim.WithLayerSize(size),
	)
	s.Setup()

	tortoise := tortoiseFromSimState(t, s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)))

	var last, verified types.LayerID
	for _, last = range sim.GenLayers(s,
		sim.WithSequence(20),
	) {
		tortoise.TallyVotes(last)
		verified = tortoise.LatestComplete()
	}
	require.Equal(t, last.Sub(1), verified)
	require.Equal(t, types.GetEffectiveGenesis()-1, tortoise.trtl.evicted,
		"should not be evicted unless pending is drained",
	)

	updates := tortoise.Updates()
	tortoise.OnApplied(updates[len(updates)-1].Layer, updates[len(updates)-1].Opinion)

	evicted := tortoise.trtl.evicted
	require.Equal(t, verified.Sub(window).Sub(1), evicted)
	require.EqualValues(t, evicted+1, tortoise.trtl.layers.data[0].lid)

	for lid := evicted.Add(1); !lid.After(last); lid = lid.Add(1) {
		for _, ballot := range tortoise.trtl.ballots[lid] {
			require.Contains(t, tortoise.trtl.ballotRefs, ballot.id, "layer %s", lid)
			for current := ballot.votes.tail; current != nil; current = current.prev {
				require.False(
					t,
					current.lid.Before(evicted),
					"no votes for layers before evicted (evicted %s, in state %s, ballot %s)",
					evicted,
					current.lid,
					ballot.layer,
				)
				if current.prev == nil {
					require.Equal(t, current.lid, evicted, "last vote is exactly evicted")
				}
			}
		}
	}
}

func TestFutureHeight(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Hdist = 3
	cfg.Zdist = cfg.Hdist
	cfg.LayerSize = 10
	t.Run("hare from future", func(t *testing.T) {
		s := sim.New(
			sim.WithLayerSize(cfg.LayerSize),
		)
		s.Setup()

		tortoise := tortoiseFromSimState(t,
			s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)),
		)
		tortoise.TallyVotes(s.Next(sim.WithNumBlocks(1), sim.WithBlockTickHeights(100_000)))
		for i := 0; i < int(cfg.Hdist)-1; i++ {
			tortoise.TallyVotes(s.Next())
		}
		require.Equal(t, types.GetEffectiveGenesis(), tortoise.LatestComplete())
		last := s.Next()
		tortoise.TallyVotes(last)
		// verifies layer by counting all votes
		require.Equal(t, last.Sub(1), tortoise.LatestComplete())
	})
	t.Run("find refheight from the last non-empty layer", func(t *testing.T) {
		cfg := defaultTestConfig()
		cfg.Hdist = 10
		cfg.LayerSize = 10
		const (
			median = 20
			slow   = 10

			smeshers = 7
		)
		s := sim.New(
			sim.WithLayerSize(smeshers),
		)
		s.Setup(
			sim.WithSetupMinerRange(smeshers, smeshers),
			sim.WithSetupTicks(
				median, median, median,
				median,
				slow, slow, slow,
			))

		tortoise := tortoiseFromSimState(t,
			s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)),
		)
		tortoise.TallyVotes(s.Next(sim.WithNumBlocks(1), sim.WithBlockTickHeights(slow+1)))
		tortoise.TallyVotes(s.Next(sim.WithoutHareOutput(), sim.WithNumBlocks(0)))
		// 3 is handpicked so that threshold will be crossed if bug wasn't fixed
		for i := 0; i < 3; i++ {
			tortoise.TallyVotes(s.Next(sim.WithNumBlocks(1)))
		}

		require.Equal(t, types.GetEffectiveGenesis().String(), tortoise.LatestComplete().String())
	})
	t.Run("median above slow smeshers", func(t *testing.T) {
		s := sim.New(
			sim.WithLayerSize(cfg.LayerSize),
		)
		const (
			slow   = 10
			normal = 20
		)
		s.Setup(
			sim.WithSetupMinerRange(10, 10),
			sim.WithSetupTicks(
				normal, normal, normal,
				normal, normal, normal, normal,
				slow, slow, slow,
			),
		)
		tortoise := tortoiseFromSimState(t,
			s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)),
		)
		var last types.LayerID
		for i := 0; i < int(cfg.Hdist); i++ {
			last = s.Next(
				sim.WithNumBlocks(1),
				sim.WithBlockTickHeights(slow+1),
				sim.WithVoteGenerator(sim.ConsistentVoting),
			)
			tortoise.TallyVotes(last)
		}
		require.Equal(t, last.Sub(2), tortoise.LatestComplete())
	})
	t.Run("empty layers with slow smeshers", func(t *testing.T) {
		s := sim.New(
			sim.WithLayerSize(cfg.LayerSize),
		)
		const (
			slow   = 10
			normal = 20
		)
		s.Setup(
			sim.WithSetupMinerRange(7, 7),
			sim.WithSetupTicks(normal, normal, normal, normal, normal, slow, slow),
		)
		tortoise := tortoiseFromSimState(t,
			s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)),
		)
		var last types.LayerID
		for i := 0; i < int(cfg.Hdist); i++ {
			last = s.Next(sim.WithNumBlocks(0))
			tortoise.TallyVotes(last)
		}
		require.Equal(t, last.Sub(1), tortoise.LatestComplete())
	})
}

func testEmptyLayers(t *testing.T, hdist int) {
	const size = 4

	cfg := defaultTestConfig()
	cfg.Hdist = uint32(hdist)
	cfg.Zdist = 3
	cfg.LayerSize = size

	// TODO(dshulyak) parametrize test with varying skipFrom, skipTo
	// skipping layers 9, 10
	skipFrom, skipTo := 1, 3

	s := sim.New(
		sim.WithLayerSize(cfg.LayerSize),
	)
	s.Setup(
		sim.WithSetupMinerRange(size, size),
	)
	tortoise := tortoiseFromSimState(t,
		s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)),
	)
	last := types.GetEffectiveGenesis()
	for i := 0; i < int(skipTo); i++ {
		opts := []sim.NextOpt{
			sim.WithVoteGenerator(tortoiseVotingWithCurrent(tortoise)),
		}
		skipped := i >= skipFrom
		if skipped {
			opts = append(opts, sim.WithoutHareOutput(), sim.WithNumBlocks(0))
		} else {
			opts = append(opts, sim.WithNumBlocks(1))
		}
		last = s.Next(opts...)
		tortoise.TallyVotes(last)
	}
	require.Equal(t, types.GetEffectiveGenesis().Add(uint32(skipFrom)), tortoise.LatestComplete())
	for i := 0; i <= int(cfg.Hdist); i++ {
		last = s.Next(
			sim.WithNumBlocks(1),
			sim.WithVoteGenerator(tortoiseVoting(tortoise)),
		)
		tortoise.TallyVotes(last)
	}
	require.Equal(t, last.Sub(1), tortoise.LatestComplete())
}

func TestEmptyLayers(t *testing.T) {
	t.Run("verifying", func(t *testing.T) {
		testEmptyLayers(t, 13)
	})
	t.Run("full", func(t *testing.T) {
		testEmptyLayers(t, 5)
	})
}

func TestSwitchMode(t *testing.T) {
	t.Run("temporary inconsistent", func(t *testing.T) {
		const size = 4

		cfg := defaultTestConfig()
		cfg.LayerSize = size
		cfg.Zdist = 2
		cfg.Hdist = 2

		s := sim.New(
			sim.WithLayerSize(cfg.LayerSize),
		)
		s.Setup(
			sim.WithSetupMinerRange(size, size),
		)
		tortoise := tortoiseFromSimState(t,
			s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)),
		)
		var last types.LayerID
		for i := 0; i <= int(cfg.Hdist); i++ {
			last = s.Next(sim.WithNumBlocks(1), sim.WithEmptyHareOutput())
		}
		tortoise.TallyVotes(last)
		require.True(t, tortoise.trtl.isFull)
		for i := 0; i <= int(cfg.Hdist); i++ {
			last = s.Next(sim.WithNumBlocks(1))
			tortoise.TallyVotes(last)
		}
		tortoise.TallyVotes(last)
		require.False(t, tortoise.trtl.isFull)
	})
	t.Run("loaded validity", func(t *testing.T) {
		const size = 4

		cfg := defaultTestConfig()
		cfg.LayerSize = size
		cfg.Zdist = 2
		cfg.Hdist = 2

		s := sim.New(
			sim.WithLayerSize(cfg.LayerSize),
		)
		s.Setup(
			sim.WithSetupMinerRange(size, size),
		)
		tortoise := tortoiseFromSimState(t,
			s.GetState(0), WithConfig(cfg),
		)
		var last types.LayerID
		for i := 0; i <= int(cfg.Hdist); i++ {
			last = s.Next(sim.WithNumBlocks(1), sim.WithEmptyHareOutput())
		}
		tortoise.TallyVotes(last)
		require.True(t, tortoise.trtl.isFull)

		tortoise1 := tortoiseFromSimState(t,
			s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)),
		)
		for i := 0; i <= int(cfg.Hdist); i++ {
			last = s.Next(sim.WithNumBlocks(1))
			tortoise1.TallyVotes(last)
		}
		tortoise1.TallyVotes(last)
		require.False(t, tortoise1.trtl.isFull)
	})
	t.Run("changed to hare", func(t *testing.T) {
		const size = 4

		cfg := defaultTestConfig()
		cfg.LayerSize = size
		cfg.Zdist = 2
		cfg.Hdist = 2

		s := sim.New(
			sim.WithLayerSize(cfg.LayerSize),
		)
		s.Setup(
			sim.WithSetupMinerRange(size, size),
		)
		tortoise := tortoiseFromSimState(t,
			s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)),
		)
		var last types.LayerID
		s.Next(sim.WithNumBlocks(1))
		s.Next(sim.WithNumBlocks(1), sim.WithVoteGenerator(gapVote))
		for i := 0; i < int(cfg.Hdist)-1; i++ {
			last = s.Next(sim.WithNumBlocks(1))
		}
		tortoise.TallyVotes(last)
		layer := tortoise.trtl.layer(types.GetEffectiveGenesis().Add(1))
		require.Len(t, layer.blocks, 1)
		require.Equal(t, against, layer.blocks[0].validity)

		block := layer.blocks[0]
		last = s.Next(sim.WithNumBlocks(1), sim.WithVoteGenerator(addSupport(types.Vote{
			ID:      block.id,
			LayerID: block.layer,
			Height:  block.height,
		})))
		tortoise.TallyVotes(last)
		for i := 0; i < 10; i++ {
			last = s.Next(sim.WithNumBlocks(1))
			tortoise.TallyVotes(last)
		}
		tortoise.TallyVotes(last)
		require.False(t, tortoise.trtl.isFull)
	})
	t.Run("count after switch back", func(t *testing.T) {
		const size = 4

		cfg := defaultTestConfig()
		cfg.LayerSize = size
		cfg.Zdist = 2
		cfg.Hdist = 2

		s := sim.New(
			sim.WithLayerSize(cfg.LayerSize),
		)
		s.Setup(
			sim.WithSetupMinerRange(size, size),
		)
		tortoise := tortoiseFromSimState(t,
			s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)),
		)
		nohare := s.Next(sim.WithEmptyHareOutput(), sim.WithNumBlocks(1))
		last := nohare
		for i := 0; i < int(cfg.Hdist); i++ {
			last = s.Next(sim.WithNumBlocks(1))
			tortoise.TallyVotes(last)
		}
		events := tortoise.Updates()
		require.Len(t, events, int(cfg.Hdist)+2) // hdist, genesis and last processed
		require.Empty(t, events[0].Blocks)
		for i := 1; i <= int(cfg.Hdist); i++ {
			layer := events[i]
			require.Equal(t, nohare.Add(uint32(i-1)), layer.Layer)
			for _, v := range layer.Blocks {
				require.True(t, v.Valid)
			}
		}
		for _, v := range events[len(events)-1].Blocks {
			require.False(t, v.Valid)
			require.True(t, v.Hare)
		}
		tortoise.OnApplied(events[len(events)-1].Layer, events[len(events)-1].Opinion)

		templates, err := ballots.Layer(s.GetState(0).DB, nohare.Add(1))
		require.NoError(t, err)
		require.NotEmpty(t, templates)
		template := templates[0]
		template.Votes.Support = nil

		// add an atx to increase optimistic threshold in verifying tortoise to trigger a switch
		atx := atxsdata.ATX{
			Height: 200,
			Weight: 200,
		}
		tortoise.trtl.atxsdata.AddAtx(types.EpochID(2), types.ATXID{1}, &atx)
		tortoise.OnAtx(types.EpochID(2), types.ATXID{1}, &atx)
		// feed ballots that vote against previously validated layer
		// without the fix they would be ignored
		for i := 1; i <= 16; i++ {
			ballot := types.NewExistingBallot(
				types.BallotID{byte(i)},
				types.EmptyEdSignature,
				types.EmptyNodeID,
				template.Layer,
			)
			ballot.InnerBallot = template.InnerBallot
			ballot.EligibilityProofs = template.EligibilityProofs
			tortoise.OnBallot(ballot.ToTortoiseData())
		}
		tortoise.Tortoise.TallyVotes(last)
		events = tortoise.Updates()
		require.Len(t, events, 3)
		require.Equal(t, events[0].Layer, nohare)
		for i := 0; i < int(cfg.Hdist); i++ {
			for _, v := range events[0].Blocks {
				require.True(t, v.Invalid)
			}
		}
	})
}

func TestOnBallotComputeOpinion(t *testing.T) {
	const size = 4

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.WindowSize = 10

	t.Run("empty layers after genesis", func(t *testing.T) {
		const distance = 3
		s := sim.New(
			sim.WithLayerSize(cfg.LayerSize),
		)
		s.Setup(
			sim.WithSetupMinerRange(size, size),
		)
		tortoise := tortoiseFromSimState(t,
			s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)),
		)
		var last types.LayerID
		for i := 0; i < distance; i++ {
			last = s.Next(sim.WithNumBlocks(1))
			tortoise.TallyVotes(last)
		}

		rst, err := ballots.Layer(s.GetState(0).DB, last)
		require.NoError(t, err)
		require.NotEmpty(t, rst)

		id := types.BallotID{1}
		ballot := types.NewExistingBallot(
			id,
			types.EmptyEdSignature,
			types.EmptyNodeID,
			rst[0].Layer,
		)
		ballot.InnerBallot = rst[0].InnerBallot
		ballot.EligibilityProofs = rst[0].EligibilityProofs
		ballot.Votes.Base = types.EmptyBallotID
		ballot.Votes.Support = nil
		ballot.Votes.Against = nil

		tortoise.OnBallot(ballot.ToTortoiseData())

		info := tortoise.trtl.ballotRefs[id]
		hasher := opinionhash.New()
		h32 := types.Hash32{}
		for i := 0; i < distance-1; i++ {
			hasher.Sum(h32[:0])
			hasher.Reset()
			hasher.WritePrevious(h32)
		}
		require.Equal(t, hasher.Sum(nil), info.opinion().Bytes())
	})
	t.Run("against abstain support", func(t *testing.T) {
		const distance = 3
		s := sim.New(
			sim.WithLayerSize(cfg.LayerSize),
		)
		s.Setup(
			sim.WithSetupMinerRange(size, size),
		)
		tortoise := tortoiseFromSimState(t,
			s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)),
		)
		var last types.LayerID
		for i := 0; i < distance; i++ {
			last = s.Next(sim.WithNumBlocks(1))
			tortoise.TallyVotes(last)
		}

		rst, err := ballots.Layer(s.GetState(0).DB, last)
		require.NoError(t, err)
		require.NotEmpty(t, rst)

		id := types.BallotID{1}
		ballot := rst[0]
		ballot.SetID(id)
		ballot.Votes.Abstain = []types.LayerID{types.GetEffectiveGenesis().Add(1)}

		tortoise.OnBallot(ballot.ToTortoiseData())

		info := tortoise.trtl.ballotRefs[id]
		hasher := opinionhash.New()
		h32 := types.Hash32{}
		hasher.Sum(h32[:0])
		hasher.Reset()
		hasher.WritePrevious(h32)
		hasher.WriteAbstain()
		hasher.Sum(h32[:0])
		hasher.Reset()

		hasher.WritePrevious(h32)
		hasher.WriteSupport(ballot.Votes.Support[0].ID, ballot.Votes.Support[0].Height)
		require.Equal(t, hasher.Sum(nil), info.opinion().Bytes())
	})
}

func TestOnHareOutput(t *testing.T) {
	const size = 4

	cfg := defaultTestConfig()
	cfg.Zdist = 3
	cfg.Hdist = cfg.Zdist + 3
	cfg.LayerSize = size

	for _, tc := range []struct {
		desc          string
		failedOptions []sim.NextOpt // options for the failed layer
		genDistance   int
	}{
		{
			desc:          "empty after abstain",
			failedOptions: []sim.NextOpt{sim.WithoutHareOutput()},
			genDistance:   int(cfg.Zdist) + 1,
		},
		{
			desc:          "recompute on hare output",
			failedOptions: []sim.NextOpt{sim.WithoutHareOutput()},
		},
		{
			desc:          "empty",
			failedOptions: []sim.NextOpt{sim.WithEmptyHareOutput()},
			genDistance:   int(cfg.Zdist),
		},
		{
			desc:          "different hare output",
			failedOptions: []sim.NextOpt{sim.WithHareOutputIndex(1)},
			genDistance:   int(cfg.Zdist),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			s := sim.New(
				sim.WithLayerSize(cfg.LayerSize),
			)
			s.Setup(
				sim.WithSetupMinerRange(size, size),
			)
			tortoise := tortoiseFromSimState(t,
				s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)),
			)
			tortoise.TallyVotes(s.Next(tc.failedOptions...))
			for i := 0; i < tc.genDistance; i++ {
				tortoise.TallyVotes(s.Next())
			}
			require.Equal(t, types.GetEffectiveGenesis(), tortoise.LatestComplete())
			empty := s.Layer(0)
			tortoise.OnHareOutput(empty.Index(), empty.Blocks()[0].ID())
			last := s.Next(sim.WithNumBlocks(1))
			tortoise.TallyVotes(last)
			require.Equal(t, last.Sub(1), tortoise.LatestComplete())
		})
	}
}

func TestDecodeExceptions(t *testing.T) {
	const size = 1

	cfg := defaultTestConfig()
	cfg.LayerSize = size

	s := sim.New(
		sim.WithLayerSize(cfg.LayerSize),
	)
	s.Setup(
		sim.WithSetupMinerRange(size, size),
	)

	tortoise := tortoiseFromSimState(t,
		s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)),
	)
	last := s.Next(sim.WithNumBlocks(2))
	tortoise.TallyVotes(last)

	layer := tortoise.trtl.layer(last)
	require.Equal(t, against, layer.blocks[0].hare)
	block := layer.blocks[0]

	last = s.Next(
		sim.WithNumBlocks(1),
	)
	tortoise.TallyVotes(last)
	ballots1 := tortoise.trtl.ballots[last]

	last = s.Next(
		sim.WithNumBlocks(1),
		sim.WithVoteGenerator(addSupport(types.Vote{
			ID:      block.id,
			LayerID: block.layer,
			Height:  block.height,
		})),
	)
	tortoise.TallyVotes(last)
	ballots2 := tortoise.trtl.ballots[last]

	last = s.Next(
		sim.WithNumBlocks(1),
		sim.WithVoteGenerator(addAgainst(types.Vote{
			ID:      block.id,
			LayerID: block.layer,
			Height:  block.height,
		})),
	)
	tortoise.TallyVotes(last)
	ballots3 := tortoise.trtl.ballots[last]

	for _, ballot := range ballots1 {
		require.Equal(
			t,
			against,
			findVote(ballot.votes, layer.lid, block.id),
			"base ballot votes against",
		)
	}
	for _, ballot := range ballots2 {
		require.Equal(
			t,
			support,
			findVote(ballot.votes, layer.lid, block.id),
			"new ballot overwrites vote",
		)
	}
	for _, ballot := range ballots3 {
		require.Equal(
			t,
			against,
			findVote(ballot.votes, layer.lid, block.id),
			"latest ballot overwrites back to against",
		)
	}
}

func findVote(v votes, lid types.LayerID, bid types.BlockID) sign {
	for current := v.tail; current != nil; current = current.prev {
		if current.lid == lid {
			for _, block := range current.supported {
				if block.id == bid {
					return support
				}
			}
			return against
		}
	}
	return abstain
}

func TestCountOnBallot(t *testing.T) {
	const size = 10
	cfg := defaultTestConfig()
	cfg.LayerSize = size

	s := sim.New(
		sim.WithLayerSize(cfg.LayerSize),
	)
	s.Setup(
		sim.WithSetupMinerRange(size, size),
	)

	tortoise := tortoiseFromSimState(t,
		s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)),
	)
	s.Next(sim.WithNumBlocks(1), sim.WithEmptyHareOutput())
	last := s.Next(sim.WithNumBlocks(1))
	tortoise.TallyVotes(last)
	require.Equal(t, types.GetEffectiveGenesis(), tortoise.LatestComplete(),
		"does't cross threshold as generated ballots vote inconsistently with hare",
	)
	blts, err := ballots.Layer(s.GetState(0).DB, last)
	require.NoError(t, err)
	require.NotEmpty(t, blts)
	for i := 1; i <= size*2; i++ {
		id := types.BallotID{}
		binary.BigEndian.PutUint64(id[:], uint64(i))
		ballot := types.NewExistingBallot(
			id,
			types.EmptyEdSignature,
			types.EmptyNodeID,
			blts[0].Layer,
		)
		ballot.InnerBallot = blts[0].InnerBallot
		ballot.EligibilityProofs = blts[0].EligibilityProofs
		// unset support to be consistent with local opinion
		ballot.Votes.Support = nil
		tortoise.OnBallot(ballot.ToTortoiseData())
	}
	tortoise.TallyVotes(last)
}

func TestOnBallotBeforeTallyVotes(t *testing.T) {
	const (
		size         = 4
		testDistance = 4
	)
	cfg := defaultTestConfig()
	cfg.Hdist = testDistance + 1
	cfg.Zdist = cfg.Hdist
	cfg.LayerSize = size

	s := sim.New(
		sim.WithLayerSize(cfg.LayerSize),
	)
	s.Setup(
		sim.WithSetupMinerRange(size, size),
	)
	tortoise := tortoiseFromSimState(t,
		s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)),
	)
	var last types.LayerID
	for i := 0; i < testDistance; i++ {
		last = s.Next(sim.WithNumBlocks(1))
		blts, err := ballots.Layer(s.GetState(0).DB, last)
		require.NoError(t, err)
		for _, ballot := range blts {
			tortoise.OnBallot(ballot.ToTortoiseData())
		}
		tortoise.TallyVotes(last)
	}
	require.Equal(t, last.Sub(1), tortoise.LatestComplete())
}

func TestNonTerminatedLayers(t *testing.T) {
	const size = 10
	cfg := defaultTestConfig()
	cfg.Hdist = 10
	cfg.Zdist = 3
	cfg.LayerSize = size

	s := sim.New(
		sim.WithLayerSize(cfg.LayerSize),
	)
	s.Setup(
		sim.WithSetupMinerRange(size, size),
	)

	tortoise := tortoiseFromSimState(t,
		s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(t)),
	)
	for i := 0; i < int(cfg.Zdist); i++ {
		tortoise.TallyVotes(s.Next(
			sim.WithNumBlocks(0), sim.WithoutHareOutput()))
	}
	require.Equal(t, types.GetEffectiveGenesis(), tortoise.LatestComplete())
	var last types.LayerID
	for i := 0; i <= int(cfg.Zdist); i++ {
		last = s.Next(sim.WithNumBlocks(1))
		tortoise.TallyVotes(last)
	}
	require.Equal(t, last.Sub(1), tortoise.LatestComplete())
}

func TestEncodeVotes(t *testing.T) {
	t.Run("support", func(t *testing.T) {
		tortoise := defaultAlgorithm(t)

		block := types.Block{}
		block.LayerIndex = types.GetEffectiveGenesis().Add(1)
		block.Initialize()

		tortoise.OnBlock(block.ToVote())
		tortoise.OnHareOutput(block.LayerIndex, block.ID())

		tortoise.TallyVotes(block.LayerIndex.Add(1))
		opinion, err := tortoise.EncodeVotes(context.Background(), EncodeVotesWithCurrent(block.LayerIndex.Add(1)))
		require.NoError(t, err)
		require.Len(t, opinion.Support, 1)

		hasher := opinionhash.New()
		rst := types.Hash32{}
		hasher.Sum(rst[:0])
		hasher.WritePrevious(rst)
		hasher.WriteSupport(block.ID(), block.TickHeight)
		require.Equal(t, hasher.Sum(nil), opinion.Hash[:])
	})
	t.Run("against", func(t *testing.T) {
		tortoise := defaultAlgorithm(t)

		tortoise.OnHareOutput(types.GetEffectiveGenesis().Add(1), types.EmptyBlockID)
		current := types.GetEffectiveGenesis().Add(2)
		tortoise.TallyVotes(current)
		opinion, err := tortoise.EncodeVotes(context.Background(), EncodeVotesWithCurrent(current))
		require.NoError(t, err)
		require.Empty(t, opinion.Support)

		hasher := opinionhash.New()
		rst := types.Hash32{}
		hasher.Sum(rst[:0])
		hasher.WritePrevious(rst)
		require.Equal(t, hasher.Sum(nil), opinion.Hash[:])
	})
	t.Run("abstain", func(t *testing.T) {
		tortoise := defaultAlgorithm(t)

		current := types.GetEffectiveGenesis().Add(2)
		tortoise.TallyVotes(current)
		opinion, err := tortoise.EncodeVotes(context.Background(), EncodeVotesWithCurrent(current))
		require.NoError(t, err)
		require.Empty(t, opinion.Support)

		hasher := opinionhash.New()
		rst := types.Hash32{}
		hasher.Sum(rst[:0])

		hasher.WritePrevious(rst)
		hasher.WriteAbstain()
		require.Equal(t, hasher.Sum(nil), opinion.Hash[:])
	})
	t.Run("support multiple", func(t *testing.T) {
		cfg := defaultTestConfig()
		cfg.Hdist = 1
		cfg.Zdist = 1
		tortoise, err := New(
			atxsdata.New(),
			WithConfig(cfg),
			WithLogger(zaptest.NewLogger(t)),
		)
		require.NoError(t, err)

		lid := types.GetEffectiveGenesis().Add(1)
		blks := []*types.Block{
			{InnerBlock: types.InnerBlock{LayerIndex: lid, TickHeight: 100}},
			{InnerBlock: types.InnerBlock{LayerIndex: lid, TickHeight: 10}},
		}
		for _, block := range blks {
			block.Initialize()
			tortoise.OnBlock(block.ToVote())
		}

		current := lid.Add(2)
		tortoise.OnWeakCoin(current.Sub(1), true)
		tortoise.TallyVotes(current)

		opinion, err := tortoise.EncodeVotes(context.Background(), EncodeVotesWithCurrent(current))
		require.NoError(t, err)
		require.Len(t, opinion.Support, 2)

		hasher := opinionhash.New()
		rst := types.Hash32{}
		hasher.Sum(rst[:0])

		hasher.WritePrevious(rst)
		hasher.WriteSupport(blks[1].ID(), blks[1].TickHeight) // note the order due to the height
		hasher.WriteSupport(blks[0].ID(), blks[0].TickHeight)
		hasher.Sum(rst[:0])
		hasher.Reset()

		hasher.WritePrevious(rst)
		require.Equal(t, hasher.Sum(nil), opinion.Hash[:])
	})
	t.Run("rewrite before base", func(t *testing.T) {
		atxdata := atxsdata.New()
		tortoise, err := New(
			atxdata,
			WithConfig(defaultTestConfig()),
			WithLogger(zaptest.NewLogger(t)),
		)
		require.NoError(t, err)

		hare := types.GetEffectiveGenesis().Add(1)
		block := types.Block{InnerBlock: types.InnerBlock{LayerIndex: hare}}
		block.Initialize()
		tortoise.OnBlock(block.ToVote())
		tortoise.OnHareOutput(hare, block.ID())

		lid := hare.Add(1)
		ballot := types.Ballot{}

		atxid := types.ATXID{1}
		atx := atxsdata.ATX{
			Weight:     10,
			BaseHeight: 1,
			Height:     2,
		}
		atxdata.AddAtx(lid.GetEpoch(), atxid, &atx)
		tortoise.OnAtx(lid.GetEpoch(), atxid, &atx)
		tortoise.OnBeacon(lid.GetEpoch(), types.EmptyBeacon)

		ballot.EpochData = &types.EpochData{
			ActiveSetHash:    types.Hash32{1, 2, 3},
			EligibilityCount: 1,
		}
		ballot.AtxID = atxid
		ballot.Layer = lid
		ballot.Votes.Support = []types.Vote{
			{ID: block.ID(), LayerID: block.LayerIndex, Height: block.TickHeight},
		}
		ballot.SetID(types.BallotID{1})

		hasher := opinionhash.New()
		rst := types.Hash32{}
		hasher.Sum(rst[:0])

		hasher.WritePrevious(rst)
		hasher.WriteSupport(block.ID(), block.TickHeight)
		hasher.Sum(ballot.OpinionHash[:0])

		decoded, err := tortoise.decodeBallot(ballot.ToTortoiseData())
		require.NoError(t, err)
		require.NoError(t, tortoise.StoreBallot(decoded))

		current := lid.Add(1)
		tortoise.TallyVotes(current)

		opinion, err := tortoise.EncodeVotes(context.Background(), EncodeVotesWithCurrent(current))
		require.NoError(t, err)
		require.Len(t, opinion.Abstain, 1)
		require.Empty(t, opinion.Support)
		require.Empty(t, opinion.Against)

		tortoise.OnHareOutput(hare, types.EmptyBlockID)

		rewritten, err := tortoise.EncodeVotes(context.Background(), EncodeVotesWithCurrent(current))
		require.NoError(t, err)
		require.Len(t, rewritten.Abstain, 1)
		require.Empty(t, rewritten.Support)
		require.Len(t, rewritten.Against, 1)
		require.Equal(t, block.ToVote(), rewritten.Against[0])

		hasher.Reset()
		hasher.Sum(rst[:0])
		hasher.Reset()

		hasher.WritePrevious(rst)
		hasher.Sum(rst[:0])
		hasher.Reset()

		hasher.WritePrevious(rst)
		hasher.WriteAbstain()
		require.Equal(t, hasher.Sum(nil), rewritten.Hash[:])
	})
}

func TestBaseBallotBeforeCurrentLayer(t *testing.T) {
	t.Run("encode", func(t *testing.T) {
		cfg := defaultTestConfig()
		s := sim.New(sim.WithLayerSize(cfg.LayerSize))
		s.Setup()
		tortoise := tortoiseFromSimState(t,
			s.GetState(0),
			WithConfig(cfg),
			WithLogger(zaptest.NewLogger(t)),
		)
		var last types.LayerID
		for i := 0; i < 4; i++ {
			last = s.Next()
		}
		tortoise.TallyVotes(last)
		encoded, err := tortoise.EncodeVotes(context.Background(), EncodeVotesWithCurrent(last))
		require.NoError(t, err)
		ballot, err := ballots.Get(s.GetState(0).DB, encoded.Base)
		require.NoError(t, err)
		require.NotEqual(t, last, ballot.Layer)
	})
	t.Run("decode", func(t *testing.T) {
		cfg := defaultTestConfig()
		s := sim.New(sim.WithLayerSize(cfg.LayerSize))
		s.Setup()
		tortoise := tortoiseFromSimState(t,
			s.GetState(0),
			WithConfig(cfg),
			WithLogger(zaptest.NewLogger(t)),
		)
		var last types.LayerID
		for i := 0; i < 4; i++ {
			last = s.Next()
		}
		tortoise.TallyVotes(last)
		ballots, err := ballots.Layer(s.GetState(0).DB, last)
		require.NoError(t, err)
		ballot := types.NewExistingBallot(
			types.BallotID{1},
			types.EmptyEdSignature,
			types.EmptyNodeID,
			ballots[0].Layer,
		)
		ballot.InnerBallot = ballots[0].InnerBallot
		ballot.EligibilityProofs = ballots[0].EligibilityProofs
		ballot.Votes.Base = ballots[1].ID()
		_, err = tortoise.decodeBallot(ballot.ToTortoiseData())
		require.ErrorContains(t, err, "votes for ballot")
	})
}

func TestMissingActiveSet(t *testing.T) {
	tortoise := defaultAlgorithm(t)
	target := types.EpochID(3)
	aset := []types.ATXID{
		types.ATXID(types.BytesToHash([]byte("first"))),
		types.ATXID(types.BytesToHash([]byte("second"))),
		types.ATXID(types.BytesToHash([]byte("third"))),
	}
	for _, atxid := range aset[:2] {
		atx := &atxsdata.ATX{}
		tortoise.trtl.atxsdata.AddAtx(target, atxid, atx)
		tortoise.OnAtx(target, atxid, atx)
	}
	t.Run("empty", func(t *testing.T) {
		require.Equal(t, aset, tortoise.GetMissingActiveSet(target+1, aset))
	})
	t.Run("all available", func(t *testing.T) {
		require.Empty(t, tortoise.GetMissingActiveSet(target, aset[:2]))
	})
	t.Run("some available", func(t *testing.T) {
		require.Equal(t, []types.ATXID{aset[2]}, tortoise.GetMissingActiveSet(target, aset))
	})
}

func BenchmarkOnBallot(b *testing.B) {
	const (
		layerSize = 50
		window    = 2000
	)
	s := sim.New(
		sim.WithLayerSize(layerSize),
		sim.WithPath(b.TempDir()),
	)
	s.Setup()

	cfg := defaultTestConfig()
	cfg.LayerSize = layerSize
	cfg.WindowSize = window

	tortoise := tortoiseFromSimState(b, s.GetState(0), WithConfig(cfg), WithLogger(zaptest.NewLogger(b)))
	for i := 0; i < window; i++ {
		tortoise.TallyVotes(s.Next())
	}
	last := s.Next()
	tortoise.TallyVotes(last)
	ballots, err := ballots.Layer(s.GetState(0).DB, last)
	require.NoError(b, err)
	hare, err := certificates.GetHareOutput(s.GetState(0).DB, last.Sub(window/2))
	require.NoError(b, err)
	block, err := blocks.Get(s.GetState(0).DB, hare)
	require.NoError(b, err)
	modified := *ballots[0]
	modified.Votes.Against = append(modified.Votes.Against, types.Vote{
		ID:      block.ID(),
		LayerID: block.LayerIndex,
		Height:  block.TickHeight,
	})

	bench := func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			id := types.BallotID{}
			binary.BigEndian.PutUint64(id[:], uint64(i)+1)
			ballot := types.NewExistingBallot(
				id,
				types.EmptyEdSignature,
				types.EmptyNodeID,
				modified.Layer,
			)
			ballot.InnerBallot = modified.InnerBallot
			ballot.EligibilityProofs = modified.EligibilityProofs
			tortoise.OnBallot(ballot.ToTortoiseData())

			b.StopTimer()
			delete(tortoise.trtl.ballotRefs, ballot.ID())
			tortoise.trtl.ballots[ballot.Layer] = nil
			b.StartTimer()
		}
	}

	b.Run("verifying", func(b *testing.B) {
		bench(b)
	})
	b.Run("full", func(b *testing.B) {
		tortoise.trtl.isFull = true
		bench(b)
	})
}

func TestMultipleTargets(t *testing.T) {
	cfg := defaultTestConfig()
	const size = 4
	cfg.LayerSize = size
	cfg.Hdist = 2
	cfg.Zdist = 1
	s := sim.New(sim.WithLayerSize(cfg.LayerSize))
	s.Setup(sim.WithSetupMinerRange(size, size))
	tortoise := tortoiseFromSimState(t,
		s.GetState(0),
		WithConfig(cfg),
		WithLogger(zaptest.NewLogger(t)),
	)
	heights := []uint64{1, 2}
	id := types.BlockID{'t'}
	multi := func(_ *rand.Rand, layers []*types.Layer, i int) sim.Voting {
		prev := layers[len(layers)-1]
		require.NotEmpty(t, prev.BallotIDs())
		return sim.Voting{
			Base: prev.BallotIDs()[0],
			Support: []types.Vote{
				{
					ID:      id,
					Height:  heights[i%len(heights)],
					LayerID: prev.Index(),
				},
			},
		}
	}
	upvote := func(rng *rand.Rand, layers []*types.Layer, i int) sim.Voting {
		prev := layers[len(layers)-1]
		require.NotEmpty(t, prev.BallotIDs())
		return sim.Voting{
			Base: prev.BallotIDs()[0],
		}
	}
	s.Next(sim.WithNumBlocks(0))
	s.Next(sim.WithNumBlocks(0), sim.WithVoteGenerator(multi))
	last := s.Next(sim.WithNumBlocks(0), sim.WithVoteGenerator(upvote))
	tortoise.TallyVotes(last)

	rst := tortoise.Updates()
	require.Len(t, rst, 4)
	block := rst[1].Blocks[0]
	require.Equal(t, block.Header.Height, heights[0])
	require.True(t, block.Valid)
	require.False(t, block.Data)
	votes, err := tortoise.EncodeVotes(context.Background())
	require.NoError(t, err)
	require.Len(t, votes.Against, 1)
	require.Equal(t, votes.Against[0], block.Header)
	tortoise.OnBlock(block.Header)
	votes, err = tortoise.EncodeVotes(context.Background())
	require.NoError(t, err)
	require.Empty(t, votes.Against)
}

func TestUpdates(t *testing.T) {
	genesis := types.GetEffectiveGenesis()
	t.Run("hare output included", func(t *testing.T) {
		trt, err := New(atxsdata.New())
		require.NoError(t, err)
		id := types.BlockID{1}
		lid := genesis + 1

		trt.OnBlock(types.BlockHeader{
			ID:      id,
			LayerID: lid,
		})
		trt.OnHareOutput(lid, id)
		trt.TallyVotes(lid)
		updates := trt.Updates()
		require.Len(t, updates, 2)
		require.Len(t, updates[1].Blocks, 1)
		require.True(t, updates[1].Blocks[0].Hare)
		require.False(t, updates[1].Blocks[0].Valid)
		require.Equal(t, id, updates[1].Blocks[0].Header.ID)
	})
	t.Run("tally first", func(t *testing.T) {
		trt, err := New(atxsdata.New())
		require.NoError(t, err)
		id := types.BlockID{1}
		lid := genesis + 1

		trt.TallyVotes(lid)
		updates := trt.Updates()
		require.Len(t, updates, 2)
		require.Empty(t, updates[0].Blocks)
		require.True(t, updates[0].Verified)
		trt.OnBlock(types.BlockHeader{
			ID:      id,
			LayerID: lid,
		})
		require.True(t, trt.OnApplied(updates[0].Layer, updates[0].Opinion))
		trt.OnHareOutput(lid, id)
		updates = trt.Updates()
		require.Len(t, updates, 1)
		require.Len(t, updates[0].Blocks, 1)
		require.True(t, updates[0].Blocks[0].Hare)
		require.False(t, updates[0].Blocks[0].Valid)
		require.Equal(t, id, updates[0].Blocks[0].Header.ID)
	})
}

func TestDuplicateBallot(t *testing.T) {
	s := newSession(t)
	s.smesher(0).atx(1, new(aopt).height(10).weight(2))
	id := types.BallotID{1}
	s.smesher(0).atx(1).rawballot(id, 1, new(bopt).
		totalEligibilities(s.epochEligibilities()).
		eligibilities(1),
	)
	s.smesher(0).atx(1).rawballot(id, 1, new(bopt).
		totalEligibilities(s.epochEligibilities()).
		eligibilities(1).assert(
		func(db *DecodedBallot, err error) {
			require.NotEmpty(t, db)
			require.NoError(t, err)
		},
		func(err error) {
			require.ErrorContains(t, err, "tortoise: ballot exists")
		},
	),
	)
	s.runInorder()
}

func TestSwitch(t *testing.T) {
	s := newSession(t).
		withHdist(4).
		withZdist(2)
	const smeshers = 4
	elig := s.layerSize / smeshers
	for i := 0; i < smeshers; i++ {
		s.smesher(i).atx(1, new(aopt).height(10).weight(100))
	}
	s.beacon(1, "a")
	for i := 0; i < smeshers; i++ {
		s.smesher(i).atx(1).ballot(1, new(bopt).
			beacon("a").
			totalEligibilities(s.epochEligibilities()).
			eligibilities(elig))
	}

	for lid := 2; lid <= s.hdist; lid++ {
		for i := 0; i < smeshers; i++ {
			votes := new(evotes).
				base(s.smesher(i).atx(1).ballot(lid-1)).
				support(lid-1, strconv.Itoa(lid-1), 0)
			s.smesher(i).atx(1).ballot(lid,
				new(bopt).eligibilities(elig).votes(votes),
			)
		}
	}
	nonverified := new(results).verified(0)
	for lid := 2; lid <= s.hdist; lid++ {
		nonverified = nonverified.next(lid-1).block(strconv.Itoa(lid-1), 0, 0)
	}
	verified := new(results)
	for lid := 2; lid <= s.hdist; lid++ {
		verified = verified.verified(lid-1).block(strconv.Itoa(lid-1), 0, valid|local)
	}
	verified.next(s.hdist)
	s.tallyWait(s.hdist - 1)
	s.mode(t, Verifying)
	s.updates(t, nonverified)
	s.tallyWait(s.hdist)
	s.mode(t, Full)
	s.updates(t, verified)
	t.Run("inorder", func(t *testing.T) { s.runInorder() })
	t.Run("random", func(t *testing.T) { s.runRandomTopoN(100) })
}

func TestOnMalfeasance(t *testing.T) {
	t.Run("atxs", func(t *testing.T) {
		s := newSession(t)
		const smeshers = 3
		elig := s.layerSize / smeshers
		for i := 0; i < smeshers; i++ {
			s.smesher(i).atx(1, new(aopt).height(10).weight(100))
		}
		s.beacon(1, "a")
		for i := 0; i < smeshers; i++ {
			s.smesher(i).atx(1).ballot(1, new(bopt).
				beacon("a").
				totalEligibilities(s.epochEligibilities()).
				eligibilities(elig))
		}
		s.smesher(0).
			malfeasant()
			// without this call threshold will be very large, and s.updates fail
		for i := 0; i < 10; i++ {
			s.smesher(0).rawatx(types.ATXID{byte(i)}, 1, new(aopt).weight(100).height(10))
		}
		s.tally(1)
		s.updates(t, new(results).verified(0).next(1))
		s.runInorder()
	})
	t.Run("ballots", func(t *testing.T) {
		s := newSession(t).
			withHdist(1).
			withZdist(1)
		const smeshers = 3
		elig := s.layerSize / smeshers
		for i := 0; i < smeshers; i++ {
			s.smesher(i).atx(1, new(aopt).height(10).weight(100))
		}
		s.beacon(1, "a")
		for i := 0; i < smeshers; i++ {
			s.smesher(i).atx(1).ballot(1, new(bopt).
				beacon("a").
				totalEligibilities(s.epochEligibilities()).
				eligibilities(elig))
		}
		for i := 0; i < smeshers; i++ {
			s.smesher(i).atx(1).ballot(2, new(bopt).
				votes(new(evotes).
					base(s.smesher(i).atx(1).ballot(1)).
					support(1, "a", 0)).
				eligibilities(elig))
		}
		s.smesher(0).malfeasant() // without this call tally will be skewed by following ballots
		for i := 0; i < 10; i++ {
			s.smesher(0).atx(1).
				rawballot(types.BallotID{'e', byte(i)}, 2,
					new(bopt).eligibilities(elig))
		}
		s.tally(2)
		s.updates(t, new(results).verified(0).verified(1).block("a", 0, valid|local).next(2))
		s.runInorder()
	})
}

func TestBaseAbstain(t *testing.T) {
	s := newSession(t)

	s.smesher(0).atx(1, new(aopt).height(10).weight(100))

	s.beacon(1, "a")
	s.smesher(0).atx(1).ballot(1, new(bopt).
		beacon("a").
		totalEligibilities(s.epochEligibilities()).
		eligibilities(s.layerSize))
	s.smesher(0).atx(1).ballot(2, new(bopt).
		eligibilities(s.layerSize).
		votes(new(evotes).
			base(s.smesher(0).atx(1).ballot(1)).
			abstain(1),
		))
	s.block(1, "aa", 0)
	s.tally(2)

	trt := s.tortoise()
	s.runOn(trt)
	op, err := trt.EncodeVotes(context.Background())
	require.NoError(t, err)
	base := s.smesher(0).atx(1).ballot(2)
	require.Equal(t, op.Base, base.ID)
	require.Equal(t, []types.LayerID{base.Layer}, op.Abstain)
	require.Empty(t, op.Support)
	require.Empty(t, op.Against)
}
