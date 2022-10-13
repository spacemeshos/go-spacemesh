package tortoise

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/big"
	mrand "math/rand"
	"os"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/system"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
	"github.com/spacemeshos/go-spacemesh/tortoise/sim"
)

var sig = signing.NewEdSigner()

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(4)

	res := m.Run()
	os.Exit(res)
}

func newCachedDB(t *testing.T, logger log.Log) *datastore.CachedDB {
	cdb := datastore.NewCachedDB(sql.InMemory(), logger)
	glayer := types.GenesisLayer()
	for _, b := range glayer.Ballots() {
		require.NoError(t, ballots.Add(cdb, b))
	}
	for _, b := range glayer.Blocks() {
		require.NoError(t, blocks.Add(cdb, b))
	}
	return cdb
}

func addLayerToMesh(cdb *datastore.CachedDB, layer *types.Layer) error {
	// add blocks to mDB
	for _, bl := range layer.Ballots() {
		if err := ballots.Add(cdb, bl); err != nil {
			return fmt.Errorf("add ballot: %w", err)
		}
	}
	for _, bl := range layer.Blocks() {
		if err := blocks.Add(cdb, bl); err != nil {
			return fmt.Errorf("add block: %w", err)
		}
	}
	if err := layers.SetHareOutput(cdb, layer.Index(), layer.BlocksIDs()[0]); err != nil {
		panic("database error")
	}

	return nil
}

const (
	defaultTestLayerSize  = 3
	defaultTestWindowSize = 30
	defaultVoteDelays     = 6
	numValidBlock         = 1
)

var (
	defaultTestHdist           = DefaultConfig().Hdist
	defaultTestZdist           = DefaultConfig().Zdist
	defaultTestGlobalThreshold = big.NewRat(6, 10)
	defaultTestLocalThreshold  = big.NewRat(2, 10)
	defaultTestRerunInterval   = time.Hour
)

func TestLayerPatterns(t *testing.T) {
	const size = 10 // more blocks means a longer test
	t.Run("Good", func(t *testing.T) {
		s := sim.New(sim.WithLayerSize(size))
		s.Setup()

		ctx := context.Background()
		cfg := defaultTestConfig()
		cfg.LayerSize = size
		tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))

		var (
			last     types.LayerID
			verified types.LayerID
		)
		for _, lid := range sim.GenLayers(s,
			sim.WithSequence(5),
		) {
			last = lid
			verified = tortoise.HandleIncomingLayer(ctx, lid)
		}
		require.Equal(t, last.Sub(1), verified)
	})

	t.Run("HealAfterBad", func(t *testing.T) {
		s := sim.New(sim.WithLayerSize(size))
		s.Setup()

		ctx := context.Background()
		cfg := defaultTestConfig()
		cfg.LayerSize = size
		tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))

		var (
			last     types.LayerID
			genesis  = types.GetEffectiveGenesis()
			verified types.LayerID
		)
		for _, lid := range sim.GenLayers(s,
			sim.WithSequence(5),
			sim.WithSequence(2, sim.WithEmptyHareOutput()),
		) {
			last = lid
			verified = tortoise.HandleIncomingLayer(ctx, lid)
		}
		require.Equal(t, genesis.Add(5), verified)

		for _, lid := range sim.GenLayers(s,
			sim.WithSequence(21),
		) {
			last = lid
			verified = tortoise.HandleIncomingLayer(ctx, lid)
		}
		require.Equal(t, last.Sub(1), verified)
	})

	t.Run("HealAfterBadGoodBadGoodBad", func(t *testing.T) {
		s := sim.New(sim.WithLayerSize(size))
		s.Setup()

		ctx := context.Background()
		cfg := defaultTestConfig()
		cfg.LayerSize = size
		tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))

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
			verified = tortoise.HandleIncomingLayer(ctx, lid)
		}
		require.Equal(t, last.Sub(1), verified)
	})
}

func TestAbstainsInMiddle(t *testing.T) {
	const size = 10
	s := sim.New(
		sim.WithLayerSize(size),
	)
	s.Setup()

	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Zdist = 3
	tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))

	var last, verified types.LayerID
	for i := 0; i < 5; i++ {
		last = s.Next(sim.WithVoteGenerator(tortoiseVoting(tortoise)))
		verified = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(1), verified)
	expected := last

	for i := 0; i < 2; i++ {
		last = s.Next(
			sim.WithVoteGenerator(tortoiseVoting(tortoise)),
			sim.WithoutHareOutput(),
		)
		verified = tortoise.HandleIncomingLayer(ctx, last)
	}
	for i := 0; i < 4; i++ {
		last = s.Next(
			sim.WithVoteGenerator(tortoiseVoting(tortoise)),
		)
		verified = tortoise.HandleIncomingLayer(ctx, last)
	}

	// verification will get stuck as of the first layer with conflicting local and global opinions.
	// block votes aren't counted because blocks aren't marked good, because they contain exceptions older
	// than their base block.
	// self-healing will not run because the layers aren't old enough.
	require.Equal(t, expected, verified)
}

type (
	baseBallotProvider func(context.Context, ...EncodeVotesOpts) (*types.Votes, error)
)

func genATXs(lid types.LayerID, numATXs, weight int) []*types.VerifiedActivationTx {
	atxList := make([]*types.VerifiedActivationTx, 0, numATXs)
	for i := 0; i < numATXs; i++ {
		atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
			NumUnits: uint32(weight),
		}}
		atx.PubLayerID = lid
		nodeID := types.NodeID{byte(i)}
		atx.SetNodeID(&nodeID)
		atxID := types.RandomATXID()
		atx.SetID(&atxID)
		vAtx, err := atx.Verify(0, 1)
		if err != nil {
			panic(err)
		}
		atxList = append(atxList, vAtx)
	}
	return atxList
}

func generateBallots(t *testing.T, lid types.LayerID, nballots int, activeSet []types.ATXID, bbp baseBallotProvider) []*types.Ballot {
	votes, err := bbp(context.TODO())
	require.NoError(t, err)

	blts := make([]*types.Ballot, 0, nballots)
	for i := 0; i < nballots; i++ {
		b := &types.Ballot{
			InnerBallot: types.InnerBallot{
				AtxID:             activeSet[i%len(activeSet)],
				Votes:             *votes,
				EligibilityProofs: []types.VotingEligibilityProof{{J: uint32(i)}},
				LayerIndex:        lid,
				EpochData: &types.EpochData{
					ActiveSet: activeSet,
				},
			},
		}
		signer := signing.NewEdSigner()
		b.Signature = signer.Sign(b.Bytes())
		b.Initialize()
		blts = append(blts, b)
	}
	return blts
}

func createTurtleLayer(t *testing.T, lid types.LayerID, cdb *datastore.CachedDB, bbp baseBallotProvider, atxids []types.ATXID, layerSize int) *types.Layer {
	if len(atxids) == 0 {
		atxList := genATXs(lid.GetEpoch().FirstLayer().Sub(1), layerSize, 1)
		atxids = make([]types.ATXID, 0, len(atxList))
		for _, atx := range atxList {
			require.NoError(t, atxs.Add(cdb, atx, time.Now()))
			atxids = append(atxids, atx.ID())
		}
	}
	blts := generateBallots(t, lid, layerSize, atxids, bbp)
	blks := []*types.Block{
		types.GenLayerBlock(lid, types.RandomTXSet(rand.Intn(100))),
		types.GenLayerBlock(lid, types.RandomTXSet(rand.Intn(100))),
	}
	return types.NewExistingLayer(lid, types.Hash32{}, blts, blks)
}

func TestLayerCutoff(t *testing.T) {
	r := require.New(t)
	alg := defaultAlgorithm(t, nil)

	// cutoff should be zero if we haven't seen at least Hdist layers yet
	alg.trtl.last = types.NewLayerID(alg.trtl.Hdist - 1)
	r.Equal(0, int(alg.trtl.layerCutoff().Uint32()))
	alg.trtl.last = types.NewLayerID(alg.trtl.Hdist)
	r.Equal(0, int(alg.trtl.layerCutoff().Uint32()))

	// otherwise, cutoff should be Hdist layers before Last
	alg.trtl.last = types.NewLayerID(alg.trtl.Hdist + 1)
	r.Equal(1, int(alg.trtl.layerCutoff().Uint32()))
	alg.trtl.last = types.NewLayerID(alg.trtl.Hdist + 100)
	r.Equal(100, int(alg.trtl.layerCutoff().Uint32()))
}

func TestAddToMesh(t *testing.T) {
	logger := logtest.New(t)
	cdb := newCachedDB(t, logger)
	alg := defaultAlgorithm(t, cdb)

	l := types.GenesisLayer()
	atxList := genATXs(l.Index(), defaultTestLayerSize, 1)
	atxids := make([]types.ATXID, 0, len(atxList))
	for _, atx := range atxList {
		require.NoError(t, atxs.Add(cdb, atx, time.Now()))
		atxids = append(atxids, atx.ID())
	}

	logger.With().Info("genesis is", l.Index(), types.BlockIdsField(types.ToBlockIDs(l.Blocks())))
	logger.With().Info("genesis is", log.Object("block", l.Blocks()[0]))

	l1 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(1), cdb, alg.EncodeVotes, atxids, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(cdb, l1))

	alg.HandleIncomingLayer(context.TODO(), l1.Index())

	l2 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(2), cdb, alg.EncodeVotes, atxids, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(cdb, l2))
	alg.HandleIncomingLayer(context.TODO(), l2.Index())

	require.Equal(t, int(types.GetEffectiveGenesis().Add(1).Uint32()), int(alg.LatestComplete().Uint32()))

	l3a := createTurtleLayer(t, types.GetEffectiveGenesis().Add(3), cdb, alg.EncodeVotes, atxids, defaultTestLayerSize+1)

	// this should fail as the blocks for this layer have not been added to the mesh yet
	alg.HandleIncomingLayer(context.TODO(), l3a.Index())
	require.Equal(t, types.GetEffectiveGenesis().Add(1), alg.LatestComplete())

	require.NoError(t, addLayerToMesh(cdb, l3a))
	require.NoError(t, alg.rerun(context.TODO()))
	alg.updateFromRerun(context.TODO())

	l4 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(4), cdb, alg.EncodeVotes, atxids, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(cdb, l4))
	alg.HandleIncomingLayer(context.TODO(), l4.Index())
	require.Equal(t, int(types.GetEffectiveGenesis().Add(3).Uint32()), int(alg.LatestComplete().Uint32()), "wrong latest complete layer")
}

func TestEncodeVotes(t *testing.T) {
	r := require.New(t)
	cdb := newCachedDB(t, logtest.New(t))
	alg := defaultAlgorithm(t, cdb)

	l0 := types.GenesisLayer()
	atxList := genATXs(l0.Index(), defaultTestLayerSize, 1)
	atxids := make([]types.ATXID, 0, len(atxList))
	for _, atx := range atxList {
		require.NoError(t, atxs.Add(cdb, atx, time.Now()))
		atxids = append(atxids, atx.ID())
	}

	expectBaseBallotLayer := func(layerID types.LayerID, numAgainst, numSupport, numNeutral int) {
		votes, err := alg.EncodeVotes(context.TODO())
		r.NoError(err)
		// expect no exceptions
		r.Len(votes.Support, numSupport)
		r.Len(votes.Against, numAgainst)
		r.Len(votes.Abstain, numNeutral)
		// expect a valid genesis base ballot
		baseBallot, err := ballots.Get(cdb, votes.Base)
		r.NoError(err)
		r.Equal(layerID, baseBallot.LayerIndex, "base ballot is from wrong layer")
	}

	// it should support all genesis blocks
	expectBaseBallotLayer(l0.Index(), 0, len(types.GenesisLayer().Blocks()), 0)

	// add a couple of incoming layers and make sure the base ballot layer advances as well
	l1 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(1), cdb, alg.EncodeVotes, atxids, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(cdb, l1))
	alg.HandleIncomingLayer(context.TODO(), l1.Index())
	expectBaseBallotLayer(l1.Index(), 0, numValidBlock, 0)

	l2 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(2), cdb, alg.EncodeVotes, atxids, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(cdb, l2))
	alg.HandleIncomingLayer(context.TODO(), l2.Index())
	require.Equal(t, int(types.GetEffectiveGenesis().Add(1).Uint32()), int(alg.LatestComplete().Uint32()))
	expectBaseBallotLayer(l2.Index(), 0, numValidBlock, 0)

	// add a layer that's not in the mesh and make sure it does not advance
	l3 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(3), cdb, alg.EncodeVotes, atxids, defaultTestLayerSize)
	alg.HandleIncomingLayer(context.TODO(), l3.Index())
	require.Equal(t, int(types.GetEffectiveGenesis().Add(1).Uint32()), int(alg.LatestComplete().Uint32()))
	expectBaseBallotLayer(l2.Index(), 0, numValidBlock, 1)
}

func TestEncodeAbstainVotesForZdist(t *testing.T) {
	const (
		size  = 4
		zdist = 3
	)
	s := sim.New(sim.WithLayerSize(size))
	s.Setup()

	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Zdist = zdist
	tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))

	var (
		last     types.LayerID
		verified types.LayerID
	)
	for _, lid := range sim.GenLayers(s,
		sim.WithSequence(1),
	) {
		last = lid
		verified = tortoise.HandleIncomingLayer(ctx, lid)
	}
	require.Equal(t, last.Sub(1), verified)

	var start types.LayerID
	for i := 1; i <= zdist; i++ {
		current := last.Add(uint32(i))
		votes, err := tortoise.EncodeVotes(context.Background(), EncodeVotesWithCurrent(current))
		require.NoError(t, err)
		require.Len(t, votes.Support, 1)
		require.Len(t, votes.Against, 0)

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

	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.LayerSize = size
	tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))

	var (
		last     types.LayerID
		verified types.LayerID
	)
	for _, lid := range sim.GenLayers(s,
		sim.WithSequence(1, sim.WithNumBlocks(1)),
		sim.WithSequence(1, sim.WithNumBlocks(1), sim.WithoutHareOutput()),
		sim.WithSequence(1, sim.WithNumBlocks(1)),
	) {
		last = lid
		verified = tortoise.HandleIncomingLayer(ctx, lid)
	}
	require.Equal(t, last.Sub(3), verified)

	votes, err := tortoise.EncodeVotes(context.Background(), EncodeVotesWithCurrent(last.Add(1)))
	require.NoError(t, err)
	require.Len(t, votes.Support, 2)
	require.Equal(t, votes.Abstain, []types.LayerID{types.NewLayerID(9)})
}

func mockedBeacons(tb testing.TB) system.BeaconGetter {
	tb.Helper()

	ctrl := gomock.NewController(tb)
	mockBeacons := smocks.NewMockBeaconGetter(ctrl)
	mockBeacons.EXPECT().GetBeacon(gomock.Any()).Return(types.EmptyBeacon, nil).AnyTimes()
	return mockBeacons
}

type updater struct {
	cdb *datastore.CachedDB
}

func (u *updater) UpdateBlockValidity(bid types.BlockID, _ types.LayerID, valid bool) error {
	if valid {
		return blocks.SetValid(u.cdb, bid)
	}
	return blocks.SetInvalid(u.cdb, bid)
}

func defaultTurtle(tb testing.TB) *turtle {
	lg := logtest.New(tb)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	return newTurtle(
		lg,
		cdb,
		mockedBeacons(tb),
		&updater{cdb},
		defaultTestConfig(),
	)
}

func TestCloneTurtle(t *testing.T) {
	r := require.New(t)
	trtl := defaultTurtle(t)
	trtl.LayerSize++                 // make sure defaults aren't being read
	trtl.last = types.NewLayerID(10) // state should not be cloned
	trtl2 := trtl.cloneTurtleParams()
	r.Equal(trtl.cdb, trtl2.cdb)
	r.Equal(trtl.Hdist, trtl2.Hdist)
	r.Equal(trtl.Zdist, trtl2.Zdist)
	r.Equal(trtl.WindowSize, trtl2.WindowSize)
	r.Equal(trtl.LayerSize, trtl2.LayerSize)
	r.Equal(trtl.BadBeaconVoteDelayLayers, trtl2.BadBeaconVoteDelayLayers)
	r.Equal(trtl.GlobalThreshold, trtl2.GlobalThreshold)
	r.Equal(trtl.LocalThreshold, trtl2.LocalThreshold)
	r.Equal(trtl.RerunInterval, trtl2.RerunInterval)
	r.NotEqual(trtl.last, trtl2.last)
}

func defaultTestConfig() Config {
	return Config{
		LayerSize:                       defaultTestLayerSize,
		Hdist:                           defaultTestHdist,
		Zdist:                           defaultTestZdist,
		WindowSize:                      defaultTestWindowSize,
		BadBeaconVoteDelayLayers:        defaultVoteDelays,
		GlobalThreshold:                 defaultTestGlobalThreshold,
		LocalThreshold:                  defaultTestLocalThreshold,
		RerunInterval:                   defaultTestRerunInterval,
		MaxExceptions:                   int(defaultTestHdist) * defaultTestLayerSize * 100,
		VerifyingModeVerificationWindow: 10_000,
		FullModeVerificationWindow:      100,
	}
}

func tortoiseFromSimState(state sim.State, opts ...Opt) *Tortoise {
	return New(state.DB, state.Beacons, &updater{state.DB}, opts...)
}

func defaultAlgorithm(tb testing.TB, cdb *datastore.CachedDB) *Tortoise {
	tb.Helper()
	return New(cdb, mockedBeacons(tb), &updater{cdb},
		WithConfig(defaultTestConfig()),
		WithLogger(logtest.New(tb)),
	)
}

func checkVerifiedLayer(t *testing.T, trtl *turtle, layerID types.LayerID) {
	t.Helper()
	require.Equal(t, int(layerID.Uint32()), int(trtl.verified.Uint32()), "got unexpected value for last verified layer")
}

func addEpochActiveSet(t *testing.T, activeSets map[types.EpochID][]types.ATXID, lid types.LayerID, layerSize int, dbs []*datastore.CachedDB) {
	epoch := lid.GetEpoch()
	if _, ok := activeSets[epoch]; !ok {
		atxList := genATXs(lid, layerSize, 1)
		atxids := make([]types.ATXID, 0, len(atxList))
		for _, atx := range atxList {
			for _, db := range dbs {
				require.NoError(t, atxs.Add(db, atx, time.Now()))
			}
			atxids = append(atxids, atx.ID())
		}
		activeSets[epoch] = atxids
	}
}

func TestCalculateOpinionWithThreshold(t *testing.T) {
	for _, tc := range []struct {
		desc      string
		expect    sign
		vote      util.Weight
		threshold *big.Rat
		weight    util.Weight
	}{
		{
			desc:      "Support",
			expect:    support,
			vote:      util.WeightFromInt64(6),
			threshold: big.NewRat(1, 2),
			weight:    util.WeightFromInt64(10),
		},
		{
			desc:      "Abstain",
			expect:    abstain,
			vote:      util.WeightFromInt64(3),
			threshold: big.NewRat(1, 2),
			weight:    util.WeightFromInt64(10),
		},
		{
			desc:      "AbstainZero",
			expect:    abstain,
			vote:      util.WeightFromInt64(0),
			threshold: big.NewRat(1, 2),
			weight:    util.WeightFromInt64(10),
		},
		{
			desc:      "Against",
			expect:    against,
			vote:      util.WeightFromInt64(-6),
			threshold: big.NewRat(1, 2),
			weight:    util.WeightFromInt64(10),
		},
		{
			desc:      "ComplexSupport",
			expect:    support,
			vote:      util.WeightFromInt64(121),
			threshold: big.NewRat(60, 100),
			weight:    util.WeightFromInt64(200),
		},
		{
			desc:      "ComplexAbstain",
			expect:    abstain,
			vote:      util.WeightFromInt64(120),
			threshold: big.NewRat(60, 100),
			weight:    util.WeightFromInt64(200),
		},
		{
			desc:      "ComplexAbstain2",
			expect:    abstain,
			vote:      util.WeightFromInt64(-120),
			threshold: big.NewRat(60, 100),
			weight:    util.WeightFromInt64(200),
		},
		{
			desc:      "ComplexAgainst",
			expect:    against,
			vote:      util.WeightFromInt64(-121),
			threshold: big.NewRat(60, 100),
			weight:    util.WeightFromInt64(200),
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			require.EqualValues(t, tc.expect,
				tc.vote.Cmp(tc.weight.Fraction(tc.threshold)))
		})
	}
}

func TestMultiTortoise(t *testing.T) {
	r := require.New(t)

	t.Run("happy path", func(t *testing.T) {
		const layerSize = defaultTestLayerSize * 2

		cdb1 := newCachedDB(t, logtest.New(t).Named("trtl1db"))
		alg1 := defaultAlgorithm(t, cdb1)
		alg1.trtl.LayerSize = layerSize
		alg1.logger = alg1.logger.Named("trtl1")
		alg1.trtl.logger = alg1.logger

		cdb2 := newCachedDB(t, logtest.New(t).Named("trtl2db"))
		alg2 := defaultAlgorithm(t, cdb2)
		alg2.trtl.LayerSize = layerSize
		alg2.logger = alg2.logger.Named("trtl2")
		alg2.trtl.logger = alg2.logger

		activeSets := map[types.EpochID][]types.ATXID{}
		makeAndProcessLayerMultiTortoise := func(layerID types.LayerID) {
			addEpochActiveSet(t, activeSets, layerID, layerSize, []*datastore.CachedDB{cdb1, cdb2})
			epoch := layerID.GetEpoch()
			// simulate producing blocks in parallel
			ballotsA := generateBallots(t, layerID, layerSize, activeSets[epoch], alg1.EncodeVotes)
			ballotsB := generateBallots(t, layerID, layerSize, activeSets[epoch], alg2.EncodeVotes)

			// these will produce identical sets of blocks, so throw away half of each
			// (we could probably get away with just using, say, A's blocks, but to be more thorough we also want
			// to test the BaseBallot provider of each tortoise)
			ballotsA = ballotsA[:layerSize/2]
			ballotsB = ballotsB[len(ballotsA):]
			blts := append(ballotsA, ballotsB...)

			// add all ballots/blocks to both tortoises
			for _, ballot := range blts {
				r.NoError(ballots.Add(cdb1, ballot))
				r.NoError(ballots.Add(cdb2, ballot))
			}
			block := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(blocks.Add(cdb1, block))
			r.NoError(blocks.Add(cdb2, block))
			r.NoError(layers.SetHareOutput(cdb1, layerID, block.ID()))
			r.NoError(layers.SetHareOutput(cdb2, layerID, block.ID()))
			alg1.HandleIncomingLayer(context.TODO(), layerID)
			alg2.HandleIncomingLayer(context.TODO(), layerID)
		}

		// make and process a bunch of layers and make sure both tortoises can verify them
		for i := 1; i < 5; i++ {
			layerID := types.GetEffectiveGenesis().Add(uint32(i))

			makeAndProcessLayerMultiTortoise(layerID)

			checkVerifiedLayer(t, alg1.trtl, layerID.Sub(1))
			checkVerifiedLayer(t, alg2.trtl, layerID.Sub(1))
		}
	})

	t.Run("unequal partition and rejoin", func(t *testing.T) {
		const layerSize = 11

		cdb1 := newCachedDB(t, logtest.New(t))
		alg1 := defaultAlgorithm(t, cdb1)
		alg1.trtl.LayerSize = layerSize
		alg1.logger = alg1.logger.Named("trtl1")
		alg1.trtl.logger = alg1.logger

		cdb2 := newCachedDB(t, logtest.New(t))
		alg2 := defaultAlgorithm(t, cdb2)
		alg2.trtl.LayerSize = layerSize
		alg2.logger = alg2.logger.Named("trtl2")
		alg2.trtl.logger = alg2.logger

		activeSets := map[types.EpochID][]types.ATXID{}
		makeBallots := func(layerID types.LayerID) (ballotsA, ballotsB []*types.Ballot) {
			addEpochActiveSet(t, activeSets, layerID, layerSize, []*datastore.CachedDB{cdb1, cdb2})
			epoch := layerID.GetEpoch()
			// simulate producing blocks in parallel
			ballotsA = generateBallots(t, layerID, layerSize, activeSets[epoch], alg1.EncodeVotes)
			ballotsB = generateBallots(t, layerID, layerSize, activeSets[epoch], alg2.EncodeVotes)

			// majority > 90
			ballotsA = ballotsA[:layerSize-1]
			// minority < 10
			ballotsB = ballotsB[layerSize-1:]
			return
		}

		// a bunch of good layers
		lastVerified := types.GetEffectiveGenesis()
		layerID := types.GetEffectiveGenesis()
		for i := 0; i < 10; i++ {
			layerID = layerID.Add(1)
			ballotsA, ballotsB := makeBallots(layerID)
			blts := append(ballotsA, ballotsB...)

			// add all ballots/blocks to both tortoises
			for _, ballot := range blts {
				r.NoError(ballots.Add(cdb1, ballot))
				r.NoError(ballots.Add(cdb2, ballot))
			}
			block := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(blocks.Add(cdb1, block))
			r.NoError(blocks.Add(cdb2, block))
			r.NoError(layers.SetHareOutput(cdb1, layerID, block.ID()))
			r.NoError(layers.SetHareOutput(cdb2, layerID, block.ID()))
			alg1.HandleIncomingLayer(context.TODO(), layerID)
			alg2.HandleIncomingLayer(context.TODO(), layerID)

			// both should make progress
			checkVerifiedLayer(t, alg1.trtl, layerID.Sub(1))
			checkVerifiedLayer(t, alg2.trtl, layerID.Sub(1))
			lastVerified = layerID.Sub(1)
		}

		// keep track of all blocks on each side of the partition
		var forkballotsA, forkballotsB []*types.Ballot
		var forkblocksA, forkblocksB []*types.Block

		// simulate a partition
		for i := 0; i < 10; i++ {
			layerID = layerID.Add(1)
			ballotsA, ballotsB := makeBallots(layerID)
			forkballotsA = append(forkballotsA, ballotsA...)
			forkballotsB = append(forkballotsB, ballotsB...)

			// add A's blocks to A only, B's to B
			for _, ballot := range ballotsA {
				r.NoError(ballots.Add(cdb1, ballot))
			}
			blockA := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(blocks.Add(cdb1, blockA))
			r.NoError(layers.SetHareOutput(cdb1, layerID, blockA.ID()))
			forkblocksA = append(forkblocksA, blockA)
			alg1.HandleIncomingLayer(context.TODO(), layerID)
			for _, ballot := range ballotsB {
				r.NoError(ballots.Add(cdb2, ballot))
			}
			blockB := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(blocks.Add(cdb2, blockB))
			r.NoError(layers.SetHareOutput(cdb2, layerID, blockB.ID()))
			forkblocksB = append(forkblocksB, blockB)
			alg2.HandleIncomingLayer(context.TODO(), layerID)

			// majority tortoise is unaffected, minority tortoise gets stuck
			checkVerifiedLayer(t, alg1.trtl, layerID.Sub(1))
			checkVerifiedLayer(t, alg2.trtl, lastVerified)
		}

		// these extra layers account for the time needed to generate enough votes to "catch up" and pass
		// the threshold.
		healingDistance := 13

		// after a while (we simulate the distance here), minority tortoise eventually begins producing more blocks
		for i := 0; i < healingDistance; i++ {
			layerID = layerID.Add(1)
			addEpochActiveSet(t, activeSets, layerID, layerSize, []*datastore.CachedDB{cdb1, cdb2})
			epoch := layerID.GetEpoch()

			// these blocks will be nearly identical but they will have different base ballots, since the set of blocks
			// for recent layers has been bifurcated, so we have to generate and store blocks separately to simulate
			// an ongoing partition.
			ballotsA := generateBallots(t, layerID, layerSize, activeSets[epoch], alg1.EncodeVotes)
			forkballotsA = append(forkballotsA, ballotsA...)
			for _, ballot := range ballotsA {
				r.NoError(ballots.Add(cdb1, ballot))
			}
			blockA := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(blocks.Add(cdb1, blockA))
			r.NoError(layers.SetHareOutput(cdb1, layerID, blockA.ID()))
			forkblocksA = append(forkblocksA, blockA)
			alg1.HandleIncomingLayer(context.TODO(), layerID)

			ballotsB := generateBallots(t, layerID, layerSize, activeSets[epoch], alg2.EncodeVotes)
			forkballotsB = append(forkballotsB, ballotsB...)
			for _, ballot := range ballotsB {
				r.NoError(ballots.Add(cdb2, ballot))
			}
			blockB := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(blocks.Add(cdb2, blockB))
			r.NoError(layers.SetHareOutput(cdb2, layerID, blockB.ID()))
			forkblocksB = append(forkblocksB, blockB)
			alg2.HandleIncomingLayer(context.TODO(), layerID)

			// majority tortoise is unaffected, minority tortoise is still stuck
			checkVerifiedLayer(t, alg1.trtl, layerID.Sub(1))
			checkVerifiedLayer(t, alg2.trtl, lastVerified)
		}

		// finally, the minority tortoise heals and regains parity with the majority tortoise
		layerID = layerID.Add(1)
		addEpochActiveSet(t, activeSets, layerID, layerSize, []*datastore.CachedDB{cdb1, cdb2})
		epoch := layerID.GetEpoch()
		ballotsA := generateBallots(t, layerID, layerSize, activeSets[epoch], alg1.EncodeVotes)
		forkballotsA = append(forkballotsA, ballotsA...)
		for _, ballot := range ballotsA {
			r.NoError(ballots.Add(cdb1, ballot))
		}
		blockA := types.GenLayerBlock(layerID, types.RandomTXSet(5))
		r.NoError(blocks.Add(cdb1, blockA))
		r.NoError(layers.SetHareOutput(cdb1, layerID, blockA.ID()))
		forkblocksA = append(forkblocksA, blockA)
		alg1.HandleIncomingLayer(context.TODO(), layerID)

		ballotsB := generateBallots(t, layerID, layerSize, activeSets[epoch], alg2.EncodeVotes)
		forkballotsB = append(forkballotsB, ballotsB...)
		for _, ballot := range ballotsB {
			r.NoError(ballots.Add(cdb2, ballot))
		}
		blockB := types.GenLayerBlock(layerID, types.RandomTXSet(5))
		r.NoError(blocks.Add(cdb2, blockB))
		r.NoError(layers.SetHareOutput(cdb2, layerID, blockB.ID()))
		forkblocksB = append(forkblocksB, blockB)
		alg2.HandleIncomingLayer(context.TODO(), layerID)

		// minority node is healed
		checkVerifiedLayer(t, alg1.trtl, layerID.Sub(1))
		checkVerifiedLayer(t, alg2.trtl, layerID.Sub(1))

		// now simulate a rejoin
		// send each tortoise's blocks to the other (simulated resync)
		// (of layers 18-40)
		for _, ballot := range forkballotsA {
			r.NoError(ballots.Add(cdb2, ballot))
		}
		for _, block := range forkblocksA {
			r.NoError(blocks.Add(cdb2, block))
		}

		for _, ballot := range forkballotsB {
			r.NoError(ballots.Add(cdb1, ballot))
		}
		for _, block := range forkblocksB {
			r.NoError(blocks.Add(cdb1, block))
		}

		require.NoError(t, alg1.rerun(context.TODO()))
		require.NoError(t, alg2.rerun(context.TODO()))

		// now continue for a few layers after rejoining, during which the minority tortoise will be stuck
		// because its opinions about which blocks are valid/invalid are wrong and disagree with the majority
		// opinion. these layers represent its healing distance. after it heals, it will converge to the
		// majority opinion.
		for i := 0; i < 40; i++ {
			layerID = layerID.Add(1)
			ballotsA, ballotsB := makeBallots(layerID)
			blts := append(ballotsA, ballotsB...)

			// add all ballots/blocks to both tortoises
			for _, ballot := range blts {
				r.NoError(ballots.Add(cdb1, ballot))
				r.NoError(ballots.Add(cdb2, ballot))
			}
			block := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(blocks.Add(cdb1, block))
			r.NoError(blocks.Add(cdb2, block))
			r.NoError(layers.SetHareOutput(cdb1, layerID, block.ID()))
			r.NoError(layers.SetHareOutput(cdb2, layerID, block.ID()))

			alg1.HandleIncomingLayer(context.TODO(), layerID)
			alg2.HandleIncomingLayer(context.TODO(), layerID)
		}

		// majority tortoise is unaffected, minority tortoise begins to heal but its verifying tortoise
		// is still stuck
		checkVerifiedLayer(t, alg1.trtl, layerID.Sub(1))
		checkVerifiedLayer(t, alg2.trtl, layerID.Sub(1))
	})

	t.Run("equal partition", func(t *testing.T) {
		const layerSize = 10

		cdb1 := newCachedDB(t, logtest.New(t).Named("trtl1db"))
		alg1 := defaultAlgorithm(t, cdb1)
		alg1.trtl.LayerSize = layerSize
		alg1.logger = alg1.logger.Named("trtl1")
		alg1.trtl.logger = alg1.logger

		cdb2 := newCachedDB(t, logtest.New(t).Named("trtl2db"))
		alg2 := defaultAlgorithm(t, cdb2)
		alg2.trtl.LayerSize = layerSize
		alg2.logger = alg2.logger.Named("trtl2")
		alg2.trtl.logger = alg2.logger

		activeSets := map[types.EpochID][]types.ATXID{}
		makeBallots := func(layerID types.LayerID) (ballotsA, ballotsB []*types.Ballot) {
			addEpochActiveSet(t, activeSets, layerID, layerSize, []*datastore.CachedDB{cdb1, cdb2})
			epoch := layerID.GetEpoch()
			// simulate producing blocks in parallel
			ballotsA = generateBallots(t, layerID, layerSize, activeSets[epoch], alg1.EncodeVotes)
			ballotsB = generateBallots(t, layerID, layerSize, activeSets[epoch], alg2.EncodeVotes)

			// 50/50 split
			ballotsA = ballotsA[:layerSize/2]
			ballotsB = ballotsB[layerSize/2:]
			return
		}

		// a bunch of good layers
		lastVerified := types.GetEffectiveGenesis()
		layerID := types.GetEffectiveGenesis()
		for i := 0; i < 10; i++ {
			layerID = layerID.Add(1)
			ballotsA, ballotsB := makeBallots(layerID)
			blts := append(ballotsA, ballotsB...)

			// add all ballots/blocks to both tortoises
			for _, ballot := range blts {
				r.NoError(ballots.Add(cdb1, ballot))
				r.NoError(ballots.Add(cdb2, ballot))
			}
			block := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(blocks.Add(cdb1, block))
			r.NoError(blocks.Add(cdb2, block))
			r.NoError(layers.SetHareOutput(cdb1, layerID, block.ID()))
			r.NoError(layers.SetHareOutput(cdb2, layerID, block.ID()))
			alg1.HandleIncomingLayer(context.TODO(), layerID)
			alg2.HandleIncomingLayer(context.TODO(), layerID)

			// both should make progress
			checkVerifiedLayer(t, alg1.trtl, layerID.Sub(1))
			checkVerifiedLayer(t, alg2.trtl, layerID.Sub(1))
			lastVerified = layerID.Sub(1)
		}

		// simulate a partition
		for i := 0; i < 10; i++ {
			layerID = layerID.Add(1)
			ballotsA, ballotsB := makeBallots(layerID)

			// add A's blocks to A only, B's to B
			for _, ballot := range ballotsA {
				r.NoError(ballots.Add(cdb1, ballot))
			}
			blockA := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(blocks.Add(cdb1, blockA))
			r.NoError(layers.SetHareOutput(cdb1, layerID, blockA.ID()))
			alg1.HandleIncomingLayer(context.TODO(), layerID)
			for _, ballot := range ballotsB {
				r.NoError(ballots.Add(cdb2, ballot))
			}
			blockB := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(blocks.Add(cdb2, blockB))
			r.NoError(layers.SetHareOutput(cdb2, layerID, blockB.ID()))
			alg2.HandleIncomingLayer(context.TODO(), layerID)

			// both nodes get stuck
			checkVerifiedLayer(t, alg1.trtl, lastVerified)
			checkVerifiedLayer(t, alg2.trtl, lastVerified)
		}

		// after a while (we simulate the distance here), both nodes eventually begin producing more blocks
		// in the case of a 50/50 split, this happens quickly
		for i := uint32(0); i < 3; i++ {
			layerID = layerID.Add(1)
			addEpochActiveSet(t, activeSets, layerID, layerSize, []*datastore.CachedDB{cdb1, cdb2})

			// these blocks will be nearly identical but they will have different base ballots, since the set of blocks
			// for recent layers has been bifurcated, so we have to generate and store blocks separately to simulate
			// an ongoing partition.
			epoch := layerID.GetEpoch()
			ballotsA := generateBallots(t, layerID, layerSize, activeSets[epoch], alg1.EncodeVotes)
			for _, ballot := range ballotsA {
				r.NoError(ballots.Add(cdb1, ballot))
			}
			blockA := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(blocks.Add(cdb1, blockA))
			r.NoError(layers.SetHareOutput(cdb1, layerID, blockA.ID()))
			alg1.HandleIncomingLayer(context.TODO(), layerID)

			ballotsB := generateBallots(t, layerID, layerSize, activeSets[epoch], alg2.EncodeVotes)
			for _, ballot := range ballotsB {
				r.NoError(ballots.Add(cdb2, ballot))
			}
			blockB := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(blocks.Add(cdb2, blockB))
			r.NoError(layers.SetHareOutput(cdb2, layerID, blockB.ID()))
			alg2.HandleIncomingLayer(context.TODO(), layerID)

			// both nodes still stuck
			checkVerifiedLayer(t, alg1.trtl, lastVerified)
			checkVerifiedLayer(t, alg2.trtl, lastVerified)
		}

		// finally, both nodes heal and get unstuck
		layerID = layerID.Add(1)
		addEpochActiveSet(t, activeSets, layerID, layerSize, []*datastore.CachedDB{cdb1, cdb2})

		epoch := layerID.GetEpoch()
		ballotsA := generateBallots(t, layerID, layerSize, activeSets[epoch], alg1.EncodeVotes)
		for _, ballot := range ballotsA {
			r.NoError(ballots.Add(cdb1, ballot))
		}
		blockA := types.GenLayerBlock(layerID, types.RandomTXSet(5))
		r.NoError(blocks.Add(cdb1, blockA))
		r.NoError(layers.SetHareOutput(cdb1, layerID, blockA.ID()))
		alg1.HandleIncomingLayer(context.TODO(), layerID)

		ballotsB := generateBallots(t, layerID, layerSize, activeSets[epoch], alg2.EncodeVotes)
		for _, ballot := range ballotsB {
			r.NoError(ballots.Add(cdb2, ballot))
		}
		blockB := types.GenLayerBlock(layerID, types.RandomTXSet(5))
		r.NoError(blocks.Add(cdb2, blockB))
		r.NoError(layers.SetHareOutput(cdb2, layerID, blockB.ID()))
		alg2.HandleIncomingLayer(context.TODO(), layerID)

		// both nodes can't switch from full tortoise
		checkVerifiedLayer(t, alg1.trtl, layerID.Sub(1))
		checkVerifiedLayer(t, alg2.trtl, layerID.Sub(1))
	})

	t.Run("three-way partition", func(t *testing.T) {
		const layerSize = 12

		cdb1 := newCachedDB(t, logtest.New(t).Named("trtl1db"))
		alg1 := defaultAlgorithm(t, cdb1)
		alg1.trtl.LayerSize = layerSize
		alg1.logger = alg1.logger.Named("trtl1")
		alg1.trtl.logger = alg1.logger

		cdb2 := newCachedDB(t, logtest.New(t).Named("trtl2db"))
		alg2 := defaultAlgorithm(t, cdb2)
		alg2.trtl.LayerSize = layerSize
		alg2.logger = alg2.logger.Named("trtl2")
		alg2.trtl.logger = alg2.logger

		cdb3 := newCachedDB(t, logtest.New(t).Named("trtl3db"))
		alg3 := defaultAlgorithm(t, cdb3)
		alg3.trtl.LayerSize = layerSize
		alg3.logger = alg3.logger.Named("trtl3")
		alg3.trtl.logger = alg3.logger

		activeSets := map[types.EpochID][]types.ATXID{}
		makeBallots := func(layerID types.LayerID) (ballotsA, ballotsB, ballotsC []*types.Ballot) {
			addEpochActiveSet(t, activeSets, layerID, layerSize, []*datastore.CachedDB{cdb1, cdb2, cdb3})
			// simulate producing blocks in parallel
			epoch := layerID.GetEpoch()
			ballotsA = generateBallots(t, layerID, layerSize, activeSets[epoch], alg1.EncodeVotes)
			ballotsB = generateBallots(t, layerID, layerSize, activeSets[epoch], alg2.EncodeVotes)
			ballotsC = generateBallots(t, layerID, layerSize, activeSets[epoch], alg3.EncodeVotes)

			// three-way split
			ballotsA = ballotsA[:layerSize/3]
			ballotsB = ballotsB[layerSize/3 : layerSize*2/3]
			ballotsC = ballotsC[layerSize*2/3:]
			return
		}

		// a bunch of good layers
		lastVerified := types.GetEffectiveGenesis()
		layerID := types.GetEffectiveGenesis()
		for i := 0; i < 10; i++ {
			layerID = layerID.Add(1)
			ballotsA, ballotsB, ballotsC := makeBallots(layerID)
			var blts []*types.Ballot
			blts = append(ballotsA, ballotsB...)
			blts = append(blts, ballotsC...)

			// add all ballots/blocks to all tortoises
			for _, ballot := range blts {
				r.NoError(ballots.Add(cdb1, ballot))
				r.NoError(ballots.Add(cdb2, ballot))
				r.NoError(ballots.Add(cdb3, ballot))
			}
			block := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(blocks.Add(cdb1, block))
			r.NoError(blocks.Add(cdb2, block))
			r.NoError(blocks.Add(cdb3, block))
			r.NoError(layers.SetHareOutput(cdb1, layerID, block.ID()))
			r.NoError(layers.SetHareOutput(cdb2, layerID, block.ID()))
			r.NoError(layers.SetHareOutput(cdb3, layerID, block.ID()))
			alg1.HandleIncomingLayer(context.TODO(), layerID)
			alg2.HandleIncomingLayer(context.TODO(), layerID)
			alg3.HandleIncomingLayer(context.TODO(), layerID)

			// all should make progress
			checkVerifiedLayer(t, alg1.trtl, layerID.Sub(1))
			checkVerifiedLayer(t, alg2.trtl, layerID.Sub(1))
			checkVerifiedLayer(t, alg3.trtl, layerID.Sub(1))
			lastVerified = layerID.Sub(1)
		}

		// simulate a partition
		for i := 0; i < 10; i++ {
			layerID = layerID.Add(1)
			ballotsA, ballotsB, ballotsC := makeBallots(layerID)

			// add each blocks to their own
			for _, ballot := range ballotsA {
				r.NoError(ballots.Add(cdb1, ballot))
			}
			blockA := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(blocks.Add(cdb1, blockA))
			r.NoError(layers.SetHareOutput(cdb1, layerID, blockA.ID()))
			alg1.HandleIncomingLayer(context.TODO(), layerID)

			for _, ballot := range ballotsB {
				r.NoError(ballots.Add(cdb2, ballot))
			}
			blockB := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(blocks.Add(cdb2, blockB))
			r.NoError(layers.SetHareOutput(cdb2, layerID, blockB.ID()))
			alg2.HandleIncomingLayer(context.TODO(), layerID)

			for _, ballot := range ballotsC {
				r.NoError(ballots.Add(cdb3, ballot))
			}
			blockC := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(blocks.Add(cdb3, blockC))
			r.NoError(layers.SetHareOutput(cdb3, layerID, blockC.ID()))
			alg3.HandleIncomingLayer(context.TODO(), layerID)

			// all nodes get stuck
			checkVerifiedLayer(t, alg1.trtl, lastVerified)
			checkVerifiedLayer(t, alg2.trtl, lastVerified)
			checkVerifiedLayer(t, alg3.trtl, lastVerified)
		}

		// after a while (we simulate the distance here), all nodes eventually begin producing more blocks
		for i := uint32(0); i < 7; i++ {
			layerID = layerID.Add(1)
			addEpochActiveSet(t, activeSets, layerID, layerSize, []*datastore.CachedDB{cdb1, cdb2, cdb3})

			// these blocks will be nearly identical but they will have different base ballots, since the set of blocks
			// for recent layers has been bifurcated, so we have to generate and store blocks separately to simulate
			// an ongoing partition.
			epoch := layerID.GetEpoch()
			ballotsA := generateBallots(t, layerID, layerSize, activeSets[epoch], alg1.EncodeVotes)
			for _, ballot := range ballotsA {
				r.NoError(ballots.Add(cdb1, ballot))
			}
			blockA := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(blocks.Add(cdb1, blockA))
			r.NoError(layers.SetHareOutput(cdb1, layerID, blockA.ID()))
			alg1.HandleIncomingLayer(context.TODO(), layerID)

			ballotsB := generateBallots(t, layerID, layerSize, activeSets[epoch], alg2.EncodeVotes)
			for _, ballot := range ballotsB {
				r.NoError(ballots.Add(cdb2, ballot))
			}
			blockB := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(blocks.Add(cdb2, blockB))
			r.NoError(layers.SetHareOutput(cdb2, layerID, blockB.ID()))
			alg2.HandleIncomingLayer(context.TODO(), layerID)

			ballotsC := generateBallots(t, layerID, layerSize, activeSets[epoch], alg3.EncodeVotes)
			for _, ballot := range ballotsC {
				r.NoError(ballots.Add(cdb3, ballot))
			}
			blockC := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(blocks.Add(cdb3, blockC))
			r.NoError(layers.SetHareOutput(cdb3, layerID, blockC.ID()))
			alg3.HandleIncomingLayer(context.TODO(), layerID)

			// nodes still stuck
			checkVerifiedLayer(t, alg1.trtl, lastVerified)
			checkVerifiedLayer(t, alg2.trtl, lastVerified)
			checkVerifiedLayer(t, alg3.trtl, lastVerified)
		}

		// finally, all nodes heal and get unstuck
		layerID = layerID.Add(1)
		addEpochActiveSet(t, activeSets, layerID, layerSize, []*datastore.CachedDB{cdb1, cdb2, cdb3})
		epoch := layerID.GetEpoch()
		ballotsA := generateBallots(t, layerID, layerSize, activeSets[epoch], alg1.EncodeVotes)
		for _, ballot := range ballotsA {
			r.NoError(ballots.Add(cdb1, ballot))
		}
		blockA := types.GenLayerBlock(layerID, types.RandomTXSet(5))
		r.NoError(blocks.Add(cdb1, blockA))
		r.NoError(layers.SetHareOutput(cdb1, layerID, blockA.ID()))
		alg1.HandleIncomingLayer(context.TODO(), layerID)

		ballotsB := generateBallots(t, layerID, layerSize, activeSets[epoch], alg2.EncodeVotes)
		for _, ballot := range ballotsB {
			r.NoError(ballots.Add(cdb2, ballot))
		}
		blockB := types.GenLayerBlock(layerID, types.RandomTXSet(5))
		r.NoError(blocks.Add(cdb2, blockB))
		r.NoError(layers.SetHareOutput(cdb2, layerID, blockB.ID()))
		alg2.HandleIncomingLayer(context.TODO(), layerID)

		ballotsC := generateBallots(t, layerID, layerSize, activeSets[epoch], alg3.EncodeVotes)
		for _, ballot := range ballotsC {
			r.NoError(ballots.Add(cdb3, ballot))
		}
		blockC := types.GenLayerBlock(layerID, types.RandomTXSet(5))
		r.NoError(blocks.Add(cdb3, blockC))
		r.NoError(layers.SetHareOutput(cdb3, layerID, blockC.ID()))
		alg3.HandleIncomingLayer(context.TODO(), layerID)

		// all nodes are healed
		checkVerifiedLayer(t, alg1.trtl, layerID.Sub(1))
		checkVerifiedLayer(t, alg2.trtl, layerID.Sub(1))
		checkVerifiedLayer(t, alg3.trtl, layerID.Sub(1))
	})
}

func TestComputeExpectedWeight(t *testing.T) {
	genesis := types.GetEffectiveGenesis()
	require.EqualValues(t, 4, types.GetLayersPerEpoch(), "expecting layers per epoch to be 4. adjust test if it will change")
	for _, tc := range []struct {
		desc         string
		target, last types.LayerID
		totals       []uint64 // total weights starting from (target, last]
		expect       *big.Float
	}{
		{
			desc:   "SingleIncompleteEpoch",
			target: genesis,
			last:   genesis.Add(2),
			totals: []uint64{10},
			expect: big.NewFloat(5),
		},
		{
			desc:   "SingleCompleteEpoch",
			target: genesis,
			last:   genesis.Add(4),
			totals: []uint64{10},
			expect: big.NewFloat(10),
		},
		{
			desc:   "ExpectZeroEpoch",
			target: genesis,
			last:   genesis.Add(8),
			totals: []uint64{10, 0},
			expect: big.NewFloat(10),
		},
		{
			desc:   "MultipleIncompleteEpochs",
			target: genesis.Add(2),
			last:   genesis.Add(7),
			totals: []uint64{8, 12},
			expect: big.NewFloat(13),
		},
		{
			desc:   "IncompleteEdges",
			target: genesis.Add(2),
			last:   genesis.Add(13),
			totals: []uint64{4, 12, 12, 4},
			expect: big.NewFloat(2 + 12 + 12 + 1),
		},
		{
			desc:   "MultipleCompleteEpochs",
			target: genesis,
			last:   genesis.Add(8),
			totals: []uint64{8, 12},
			expect: big.NewFloat(20),
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			var (
				cdb          = datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
				epochWeights = map[types.EpochID]util.Weight{}
				first        = tc.target.Add(1).GetEpoch()
			)
			for i, weight := range tc.totals {
				eid := first + types.EpochID(i)
				atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
					NIPostChallenge: types.NIPostChallenge{
						PubLayerID: (eid - 1).FirstLayer(),
					},
					NumUnits: uint32(weight),
				}}
				id := types.RandomATXID()
				atx.SetID(&id)
				atx.SetNodeID(&types.NodeID{})
				vAtx, err := atx.Verify(0, 1)
				require.NoError(t, err)
				require.NoError(t, atxs.Add(cdb, vAtx, time.Now()))
			}
			for lid := tc.target.Add(1); !lid.After(tc.last); lid = lid.Add(1) {
				weight, _, err := extractAtxsData(cdb, lid.GetEpoch())
				require.NoError(t, err)
				epochWeights[lid.GetEpoch()] = weight
			}

			weight := computeExpectedWeight(epochWeights, tc.target, tc.last)
			require.Equal(t, tc.expect.String(), weight.String())
		})
	}
}

func TestOutOfOrderLayersAreVerified(t *testing.T) {
	// increase layer size reduce test flakiness
	const size = 10
	s := sim.New(sim.WithLayerSize(size))
	s.Setup()

	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.LayerSize = size
	tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))

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
		verified = tortoise.HandleIncomingLayer(ctx, lid)
	}
	require.Equal(t, last.Sub(1), verified)
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

		ctx := context.Background()
		cfg := defaultTestConfig()
		cfg.LayerSize = size
		cfg.Zdist = zdist
		cfg.Hdist = hdist
		tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))

		var (
			last     types.LayerID
			verified types.LayerID
		)
		for i := 0; i < limit; i++ {
			last = s.Next(sim.WithNumBlocks(1))
			if i == skip {
				continue
			}
			verified = tortoise.HandleIncomingLayer(ctx, last)
		}
		require.Equal(t, last.Sub(1), verified)
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
			size  = 10
			zdist = 2
			hdist = zdist + 3
			skip  = 1 // skipping layer generated at this position
			limit = hdist + 1
		)
		s := sim.New(sim.WithLayerSize(size))
		s.Setup()

		ctx := context.Background()
		cfg := defaultTestConfig()
		cfg.LayerSize = size
		cfg.Zdist = zdist
		cfg.Hdist = hdist
		tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))

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
			verified = tortoise.HandleIncomingLayer(ctx, last)
		}
		require.Equal(t, types.GetEffectiveGenesis().Add(skip).Sub(1), verified)
		// switch to full mode happens here
		last = s.Next(sim.WithNumBlocks(1))
		verified = tortoise.HandleIncomingLayer(ctx, last)
		require.Equal(t, last.Sub(1), verified)
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

		ctx := context.Background()
		cfg := defaultTestConfig()
		cfg.LayerSize = size
		cfg.Zdist = zdist
		cfg.Hdist = hdist
		tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))

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
			verified = tortoise.HandleIncomingLayer(ctx, last)
		}
		require.Equal(t, last.Sub(1), verified)
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

	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.LayerSize = size

	var lids []types.LayerID
	for i := 0; i < 200; i++ {
		lids = append(lids, s.Next(opts...))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg))
		for _, lid := range lids {
			tortoise.HandleIncomingLayer(ctx, lid)
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

	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.WindowSize = 100
	tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg))

	var last, verified types.LayerID
	for i := 0; i < 400; i++ {
		last = s.Next(opts...)
		verified = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(b, last.Sub(1), verified)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tortoise.EncodeVotes(ctx)
	}
}

func BenchmarkTortoiseBaseBallot(b *testing.B) {
	b.Run("Verifying", func(b *testing.B) {
		benchmarkBaseBallot(b)
	})
	b.Run("Full", func(b *testing.B) {
		// hare output will have only 10/30 ballots
		benchmarkBaseBallot(b, sim.WithEmptyHareOutput())
	})
}

func randomRefBallot(tb testing.TB, lyrID types.LayerID, beacon types.Beacon) *types.Ballot {
	tb.Helper()
	ballot := types.RandomBallot()
	ballot.LayerIndex = lyrID
	ballot.EpochData = &types.EpochData{
		Beacon: beacon,
	}
	ballot.Signature = signing.NewEdSigner().Sign(ballot.Bytes())
	require.NoError(tb, ballot.Initialize())
	return ballot
}

func TestBallotHasGoodBeacon(t *testing.T) {
	layerID := types.GetEffectiveGenesis().Add(1)
	epochBeacon := types.RandomBeacon()
	ballot := randomRefBallot(t, layerID, epochBeacon)

	mockBeacons := smocks.NewMockBeaconGetter(gomock.NewController(t))
	trtl := defaultTurtle(t)
	trtl.beacons = mockBeacons

	logger := logtest.New(t)
	// good beacon
	mockBeacons.EXPECT().GetBeacon(layerID.GetEpoch()).Return(epochBeacon, nil).Times(1)
	badBeacon, err := trtl.compareBeacons(logger, ballot.ID(), ballot.LayerIndex, epochBeacon)
	assert.NoError(t, err)
	assert.False(t, badBeacon)

	// bad beacon
	beacon := types.RandomBeacon()
	require.NotEqual(t, epochBeacon, beacon)
	mockBeacons.EXPECT().GetBeacon(layerID.GetEpoch()).Return(epochBeacon, nil).Times(1)
	badBeacon, err = trtl.compareBeacons(logger, ballot.ID(), ballot.LayerIndex, beacon)
	assert.NoError(t, err)
	assert.True(t, badBeacon)
}

func TestBallotsNotProcessedWithoutBeacon(t *testing.T) {
	ctx := context.Background()

	s := sim.New()
	s.Setup()
	cfg := defaultTestConfig()
	tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))
	last := s.Next()

	beacon, err := s.GetState(0).Beacons.GetBeacon(last.GetEpoch())
	require.NoError(t, err)
	s.GetState(0).Beacons.Delete(last.GetEpoch() - 1)

	blts, err := ballots.Layer(s.GetState(0).DB, last)
	require.NoError(t, err)
	for _, ballot := range blts {
		tortoise.OnBallot(ballot)
	}
	s.GetState(0).Beacons.StoreBeacon(last.GetEpoch()-1, beacon)

	_ = tortoise.HandleIncomingLayer(ctx, last)
	last = s.Next()
	verified := tortoise.HandleIncomingLayer(ctx, last)
	require.Equal(t, last.Sub(1), verified)
}

func TestObjectsNotProcessedBeforeRefencedHeight(t *testing.T) {
	ctx := context.Background()

	s := sim.New()
	s.Setup()
	cfg := defaultTestConfig()
	tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))
	last := s.Next()

	blks, err := blocks.Layer(s.GetState(0).DB, last)
	require.NoError(t, err)
	for _, block := range blks {
		tortoise.OnBlock(block)
	}

	blts, err := ballots.Layer(s.GetState(0).DB, last)
	require.NoError(t, err)
	for _, ballot := range blts {
		tortoise.OnBallot(ballot)
	}

	require.Empty(t, tortoise.trtl.layers[last])

	_ = tortoise.HandleIncomingLayer(ctx, last)
	require.NotEmpty(t, tortoise.trtl.layers[last])
	verified := tortoise.HandleIncomingLayer(ctx, s.Next())
	require.Equal(t, last, verified)
}

func TestVotesDecodingWithoutBaseBallot(t *testing.T) {
	ctx := context.Background()

	t.Run("AllNotDecoded", func(t *testing.T) {
		s := sim.New()
		s.Setup()
		cfg := defaultTestConfig()
		tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))

		var verified types.LayerID
		for _, last := range sim.GenLayers(s, sim.WithSequence(2,
			sim.WithVoteGenerator(voteWithBaseBallot(types.BallotID{1, 1, 1})))) {
			verified = tortoise.HandleIncomingLayer(ctx, last)
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
		tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))

		var last, verified types.LayerID
		for _, last = range sim.GenLayers(s, sim.WithSequence(2, sim.WithVoteGenerator(func(rng *mrand.Rand, layers []*types.Layer, i int) sim.Voting {
			if i >= breakpoint {
				return voteWithBaseBallot(types.BallotID{1, 1, 1})(rng, layers, i)
			}
			return sim.ConsistentVoting(rng, layers, i)
		}))) {
			verified = tortoise.HandleIncomingLayer(ctx, last)
		}
		require.Equal(t, last.Sub(1), verified)
	})
}

// gapVote will skip one layer in voting.
func gapVote(rng *mrand.Rand, layers []*types.Layer, i int) sim.Voting {
	return skipLayers(1)(rng, layers, i)
}

func skipLayers(n int) sim.VotesGenerator {
	return func(rng *mrand.Rand, layers []*types.Layer, _ int) sim.Voting {
		position := n + 1
		if len(layers) < position {
			panic(fmt.Sprintf("need at least %d layers", position))
		}
		baseLayer := layers[len(layers)-position]
		support := layers[len(layers)-position].BlocksIDs()
		blts := baseLayer.Ballots()
		base := blts[rng.Intn(len(blts))]
		return sim.Voting{Base: base.ID(), Support: support}
	}
}

// olderExceptions will vote for block older then base ballot.
func olderExceptions(rng *mrand.Rand, layers []*types.Layer, _ int) sim.Voting {
	if len(layers) < 2 {
		panic("need at least 2 layers")
	}
	baseLayer := layers[len(layers)-1]
	blts := baseLayer.Ballots()
	base := blts[rng.Intn(len(blts))]
	voting := sim.Voting{Base: base.ID()}
	for _, layer := range layers[len(layers)-2:] {
		voting.Support = append(voting.Support, layer.BlocksIDs()...)
	}
	return voting
}

// outOfWindowBaseBallot creates VotesGenerator with a specific window.
// vote generator will produce one block that uses base ballot outside the sliding window.
// NOTE that it will produce blocks as if it didn't know about blocks from higher layers.
func outOfWindowBaseBallot(n, window int) sim.VotesGenerator {
	return func(rng *mrand.Rand, layers []*types.Layer, i int) sim.Voting {
		if i >= n {
			return sim.PerfectVoting(rng, layers, i)
		}
		li := len(layers) - window
		blts := layers[li].Ballots()
		base := blts[rng.Intn(len(blts))]
		return sim.Voting{Base: base.ID(), Support: layers[li].BlocksIDs()}
	}
}

// tortoiseVoting is for testing that protocol makes progress using heuristic that we are
// using for the network.
func tortoiseVoting(tortoise *Tortoise) sim.VotesGenerator {
	return func(rng *mrand.Rand, layers []*types.Layer, i int) sim.Voting {
		votes, err := tortoise.EncodeVotes(context.Background())
		if err != nil {
			panic(err)
		}
		return *votes
	}
}

func tortoiseVotingWithCurrent(tortoise *Tortoise) sim.VotesGenerator {
	return func(rng *mrand.Rand, layers []*types.Layer, i int) sim.Voting {
		current := types.GetEffectiveGenesis().Add(1)
		if len(layers) > 0 {
			current = layers[len(layers)-1].Index().Add(1)
		}
		votes, err := tortoise.EncodeVotes(context.Background(), EncodeVotesWithCurrent(current))
		if err != nil {
			panic(err)
		}
		return *votes
	}
}

func TestBaseBallotGenesis(t *testing.T) {
	ctx := context.Background()

	s := sim.New()
	cfg := defaultTestConfig()
	tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg))

	votes, err := tortoise.EncodeVotes(ctx)
	require.NoError(t, err)
	require.Equal(t, votes.Support, []types.BlockID{types.GenesisBlockID})
	require.Equal(t, types.GenesisBallotID, votes.Base)
}

func ensureBaseAndExceptionsFromLayer(tb testing.TB, lid types.LayerID, votes *types.Votes, cdb *datastore.CachedDB) {
	tb.Helper()

	blts, err := ballots.Get(cdb, votes.Base)
	require.NoError(tb, err)
	require.Equal(tb, lid, blts.LayerIndex)

	for _, bid := range votes.Support {
		block, err := blocks.Get(cdb, bid)
		require.NoError(tb, err)
		require.Equal(tb, lid, block.LayerIndex, "block=%s block layer=%s last=%s", block.ID(), block.LayerIndex, lid)
	}
}

func TestBaseBallotEvictedBlock(t *testing.T) {
	const size = 12
	s := sim.New(
		sim.WithLayerSize(size),
	)
	s.Setup()

	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.WindowSize = 10
	tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))

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
		verified = tortoise.HandleIncomingLayer(ctx, lid)
	}
	require.Equal(t, last.Sub(1), verified)
	for i := 0; i < 10; i++ {
		votes, err := tortoise.EncodeVotes(ctx)
		require.NoError(t, err)
		ensureBaseAndExceptionsFromLayer(t, last, votes, s.GetState(0).DB)

		last = s.Next(sim.WithVoteGenerator(tortoiseVoting(tortoise)))
		verified = tortoise.HandleIncomingLayer(ctx, last)
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
		},
		{
			desc: "BadBlocksIgnored",
			seqs: []sim.Sequence{
				sim.WithSequence(5),
				sim.WithSequence(5, sim.WithVoteGenerator(olderExceptions)),
			},
			expected: genesis.Add(5),
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
			expected: genesis.Add(5),
			window:   10,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			const size = 10
			s := sim.New(
				sim.WithLayerSize(size),
			)
			s.Setup()

			ctx := context.Background()
			cfg := defaultTestConfig()
			cfg.LayerSize = size
			cfg.WindowSize = tc.window
			tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))

			for _, lid := range sim.GenLayers(s, tc.seqs...) {
				tortoise.HandleIncomingLayer(ctx, lid)
			}

			votes, err := tortoise.EncodeVotes(ctx)
			require.NoError(t, err)
			ballot, err := ballots.Get(s.GetState(0).DB, votes.Base)
			require.NoError(t, err)
			require.Equal(t, tc.expected, ballot.LayerIndex)
		})
	}
}

// splitVoting partitions votes into two halves.
func splitVoting(n int) sim.VotesGenerator {
	return func(_ *mrand.Rand, layers []*types.Layer, i int) sim.Voting {
		var (
			support []types.BlockID
			last    = layers[len(layers)-1]
			bids    = last.BlocksIDs()
			half    = len(bids) / 2
		)
		if len(bids) < 2 {
			panic("make sure that the previous layer has atleast 2 blocks in it")
		}
		if i < n/2 {
			support = bids[:half]
		} else {
			support = bids[half:]
		}
		return sim.Voting{Base: types.BallotID(support[0]), Support: support}
	}
}

func ensureBallotLayerWithin(tb testing.TB, cdb *datastore.CachedDB, ballotID types.BallotID, from, to types.LayerID) {
	tb.Helper()

	ballot, err := ballots.Get(cdb, ballotID)
	require.NoError(tb, err)
	require.True(tb, !ballot.LayerIndex.Before(from) && !ballot.LayerIndex.After(to),
		"%s not in [%s,%s]", ballot.LayerIndex, from, to,
	)
}

func ensureBlockLayerWithin(tb testing.TB, cdb *datastore.CachedDB, bid types.BlockID, from, to types.LayerID) {
	tb.Helper()

	block, err := blocks.Get(cdb, bid)
	require.NoError(tb, err)
	require.True(tb, !block.LayerIndex.Before(from) && !block.LayerIndex.After(to),
		"%s not in [%s,%s]", block.LayerIndex, from, to,
	)
}

func TestWeakCoinVoting(t *testing.T) {
	const (
		size  = 6
		hdist = 2
	)
	s := sim.New(
		sim.WithLayerSize(size),
	)
	s.Setup(
		sim.WithSetupUnitsRange(1, 1),
		sim.WithSetupMinerRange(size, size),
	)

	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Hdist = hdist
	cfg.Zdist = hdist

	var (
		tortoise       = tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))
		verified, last types.LayerID
		genesis        = types.GetEffectiveGenesis()
	)

	for _, lid := range sim.GenLayers(s,
		sim.WithSequence(5),
		sim.WithSequence(hdist+1,
			sim.WithCoin(true), // declare support
			sim.WithEmptyHareOutput(),
			sim.WithVoteGenerator(splitVoting(size)),
		),
	) {
		last = lid
		verified = tortoise.HandleIncomingLayer(ctx, lid)
	}
	// 5th layer after genesis verifies 4th
	// and later layers can't verify previous as they are split
	require.Equal(t, genesis.Add(4), verified)
	votes, err := tortoise.EncodeVotes(ctx)
	require.NoError(t, err)
	// last ballot that is consistent with local opinion
	ensureBallotLayerWithin(t, s.GetState(0).DB, votes.Base, genesis.Add(5), genesis.Add(5))

	// support for all layers in range of (verified, last - hdist)
	for _, bid := range votes.Support {
		ensureBlockLayerWithin(t, s.GetState(0).DB, bid, genesis.Add(5), last.Sub(hdist).Sub(1))
	}

	for i := 0; i < 10; i++ {
		last = s.Next(sim.WithVoteGenerator(tortoiseVoting(tortoise)))
		verified = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(1), verified)
}

func TestVoteAgainstSupportedByBaseBallot(t *testing.T) {
	const size = 10
	s := sim.New(
		sim.WithLayerSize(size),
	)
	s.Setup()

	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Hdist = 1
	cfg.Zdist = 1

	var (
		tortoise       = tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))
		last, verified types.LayerID
		genesis        = types.GetEffectiveGenesis()
	)
	for _, last = range sim.GenLayers(s,
		sim.WithSequence(3, sim.WithNumBlocks(1)),
	) {
		verified = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(1), verified)

	unsupported := map[types.BlockID]struct{}{}
	for lid := genesis.Add(1); lid.Before(last); lid = lid.Add(1) {
		hareOutput, err := layers.GetHareOutput(s.GetState(0).DB, lid)
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

	votes, err := tortoise.EncodeVotes(ctx)
	require.NoError(t, err)
	ensureBallotLayerWithin(t, s.GetState(0).DB, votes.Base, last, last)

	require.Len(t, votes.Against, len(unsupported))
	for _, bid := range votes.Against {
		ensureBlockLayerWithin(t, s.GetState(0).DB, bid, genesis, last.Sub(1))
		require.Contains(t, unsupported, bid)
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
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			s := sim.New(
				sim.WithLayerSize(size),
			)
			s.Setup(sim.WithSetupUnitsRange(1, 1))

			ctx := context.Background()
			cfg := defaultTestConfig()
			cfg.LayerSize = size
			cfg.Hdist = hdist
			cfg.Zdist = hdist
			tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg))
			for _, lid := range sim.GenLayers(s, tc.seqs...) {
				tortoise.HandleIncomingLayer(ctx, lid)
			}

			err := tortoise.trtl.loadBlocksData(tc.lid)
			require.NoError(t, err)

			blks, err := blocks.IDsInLayer(s.GetState(0).DB, tc.lid)
			require.NoError(t, err)
			for _, bid := range blks {
				vote, _ := getLocalVote(&tortoise.trtl.state, cfg, tortoise.trtl.blockRefs[bid])
				if tc.expected == support {
					hareOutput, err := layers.GetHareOutput(s.GetState(0).DB, tc.lid)
					require.NoError(t, err)
					// only one block is supported
					if bid == hareOutput {
						require.Equal(t, tc.expected, vote, "block id %s", bid)
					} else {
						require.Equal(t, against, vote, "block id %s", bid)
					}
				} else {
					require.Equal(t, tc.expected, vote, "block id %s", bid)
				}
			}
		})
	}
}

func TestComputeBallotWeight(t *testing.T) {
	type testBallot struct {
		ActiveSet      []int // optional index to atx's to form an active set
		RefBallot      int   // optional index to the ballot, use it in test if active set is nil
		ATX            int   // non optional index to this ballot atx
		ExpectedWeight *big.Float
		Eligibilities  int
	}
	createActiveSet := func(pos []int, atxdis []types.ATXID) []types.ATXID {
		var rst []types.ATXID
		for _, i := range pos {
			rst = append(rst, atxdis[i])
		}
		return rst
	}

	for _, tc := range []struct {
		desc                      string
		atxs                      []uint
		ballots                   []testBallot
		layerSize, layersPerEpoch uint32
	}{
		{
			desc:           "FromActiveSet",
			atxs:           []uint{50, 50, 50},
			layerSize:      5,
			layersPerEpoch: 3,
			ballots: []testBallot{
				{ActiveSet: []int{0, 1, 2}, ATX: 0, ExpectedWeight: big.NewFloat(10), Eligibilities: 1},
				{ActiveSet: []int{0, 1, 2}, ATX: 1, ExpectedWeight: big.NewFloat(10), Eligibilities: 1},
			},
		},
		{
			desc:           "FromRefBallot",
			atxs:           []uint{50, 50, 50},
			layerSize:      5,
			layersPerEpoch: 3,
			ballots: []testBallot{
				{ActiveSet: []int{0, 1, 2}, ATX: 0, ExpectedWeight: big.NewFloat(10), Eligibilities: 1},
				{RefBallot: 0, ATX: 0, ExpectedWeight: big.NewFloat(10), Eligibilities: 1},
			},
		},
		{
			desc:           "FromRefBallotMultipleEligibilities",
			atxs:           []uint{50, 50, 50},
			layerSize:      5,
			layersPerEpoch: 3,
			ballots: []testBallot{
				{ActiveSet: []int{0, 1, 2}, ATX: 0, ExpectedWeight: big.NewFloat(10), Eligibilities: 1},
				{RefBallot: 0, ATX: 0, ExpectedWeight: big.NewFloat(20), Eligibilities: 2},
			},
		},
		{
			desc:           "FromRefBallotMultipleEligibilities",
			atxs:           []uint{50, 50, 50},
			layerSize:      5,
			layersPerEpoch: 3,
			ballots: []testBallot{
				{ActiveSet: []int{0, 1, 2}, ATX: 0, ExpectedWeight: big.NewFloat(20), Eligibilities: 2},
				{RefBallot: 0, ATX: 0, ExpectedWeight: big.NewFloat(10), Eligibilities: 1},
			},
		},
		{
			desc:           "FromRefBallotMultipleEligibilities",
			atxs:           []uint{50, 50, 50},
			layerSize:      5,
			layersPerEpoch: 3,
			ballots: []testBallot{
				{ActiveSet: []int{0, 1, 2}, ATX: 0, ExpectedWeight: big.NewFloat(20), Eligibilities: 2},
				{RefBallot: 0, ATX: 0, ExpectedWeight: big.NewFloat(30), Eligibilities: 3},
			},
		},
		{
			desc:           "DifferentActiveSets",
			atxs:           []uint{50, 50, 100, 100},
			layerSize:      5,
			layersPerEpoch: 2,
			ballots: []testBallot{
				{ActiveSet: []int{0, 1}, ATX: 0, ExpectedWeight: big.NewFloat(10), Eligibilities: 1},
				{ActiveSet: []int{2, 3}, ATX: 2, ExpectedWeight: big.NewFloat(20), Eligibilities: 1},
			},
		},
		{
			desc:           "AtxNotInActiveSet",
			atxs:           []uint{50, 50, 50},
			layerSize:      5,
			layersPerEpoch: 2,
			ballots: []testBallot{
				{ActiveSet: []int{0, 2}, ATX: 1, ExpectedWeight: big.NewFloat(0), Eligibilities: 1},
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			var (
				blts   []*types.Ballot
				atxids []types.ATXID

				refWeights = map[types.BallotID]util.Weight{}
			)

			cdb := newCachedDB(t, logtest.New(t))
			lid := types.NewLayerID(111)
			atxLid := lid.GetEpoch().FirstLayer().Sub(1)
			for i, weight := range tc.atxs {
				atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
					NumUnits: uint32(weight),
				}}
				atx.PubLayerID = atxLid
				nodeID := types.NodeID{byte(i)}
				atx.SetNodeID(&nodeID)
				atxID := types.RandomATXID()
				atx.SetID(&atxID)
				vAtx, err := atx.Verify(0, 1)
				require.NoError(t, err)
				require.NoError(t, atxs.Add(cdb, vAtx, time.Now()))
				atxids = append(atxids, atxID)
			}

			var currentJ int
			for _, b := range tc.ballots {
				ballot := &types.Ballot{
					InnerBallot: types.InnerBallot{
						AtxID: atxids[b.ATX],
					},
				}
				for j := 0; j < b.Eligibilities; j++ {
					ballot.EligibilityProofs = append(ballot.EligibilityProofs,
						types.VotingEligibilityProof{J: uint32(currentJ)})
					currentJ++
				}
				if b.ActiveSet != nil {
					ballot.EpochData = &types.EpochData{
						ActiveSet: createActiveSet(b.ActiveSet, atxids),
					}
				} else {
					ballot.RefBallot = blts[b.RefBallot].ID()
				}

				ballot.Signature = sig.Sign(ballot.Bytes())
				require.NoError(t, ballot.Initialize())
				blts = append(blts, ballot)

				weight, err := computeBallotWeight(cdb, refWeights, ballot, tc.layerSize, tc.layersPerEpoch)
				require.NoError(t, err)
				require.Equal(t, b.ExpectedWeight.String(), weight.String())
			}
		})
	}
}

func TestNetworkRecoversFromFullPartition(t *testing.T) {
	const size = 10
	s1 := sim.New(
		sim.WithLayerSize(size),
		sim.WithStates(2),
		sim.WithLogger(logtest.New(t)),
	)
	s1.Setup(
		sim.WithSetupMinerRange(15, 15),
	)

	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Hdist = 3
	cfg.Zdist = 3

	var (
		tortoise1 = tortoiseFromSimState(s1.GetState(0), WithConfig(cfg),
			WithLogger(logtest.New(t).Named("first")))
		tortoise2 = tortoiseFromSimState(s1.GetState(1), WithConfig(cfg),
			WithLogger(logtest.New(t).Named("second")))
		last, verified1, verified2 types.LayerID
	)

	for i := 0; i < int(types.GetLayersPerEpoch()); i++ {
		last = s1.Next()
		verified1 = tortoise1.HandleIncomingLayer(ctx, last)
		verified2 = tortoise2.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(1), verified1)
	require.Equal(t, last.Sub(1), verified2)

	gens := s1.Split()
	require.Len(t, gens, 2)
	s1, s2 := gens[0], gens[1]

	partitionStart := last
	for i := 0; i < int(types.GetLayersPerEpoch()); i++ {
		last = s1.Next()
		verified1 = tortoise1.HandleIncomingLayer(ctx, last)
		verified2 = tortoise2.HandleIncomingLayer(ctx, s2.Next())
	}
	require.Equal(t, last.Sub(1), verified1)
	require.Equal(t, last.Sub(1), verified2)

	// sync missing state and rerun immediately, both instances won't make progress
	// because weight increases, and each side doesn't have enough weight in votes
	// and then do rerun
	partitionEnd := last
	s1.Merge(s2)

	require.NoError(t, tortoise1.rerun(ctx))
	require.NoError(t, tortoise2.rerun(ctx))

	// make enough progress to cross global threshold with new votes
	for i := 0; i < int(types.GetLayersPerEpoch())*4; i++ {
		last = s1.Next(sim.WithVoteGenerator(func(rng *mrand.Rand, layers []*types.Layer, i int) sim.Voting {
			if i < size/2 {
				return tortoiseVoting(tortoise1)(rng, layers, i)
			}
			return tortoiseVoting(tortoise2)(rng, layers, i)
		}))
		verified1 = tortoise1.HandleIncomingLayer(ctx, last)
		verified2 = tortoise2.HandleIncomingLayer(ctx, last)
	}

	require.Equal(t, last.Sub(1), verified1)
	require.Equal(t, last.Sub(1), verified2)

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
	const size = 10
	s := sim.New(
		sim.WithLayerSize(size),
	)
	// change weight to be atleast the same as size
	s.Setup(sim.WithSetupUnitsRange(size, size))

	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.LayerSize = size
	tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))

	var last, verified types.LayerID
	for _, last = range sim.GenLayers(s,
		sim.WithSequence(2),
		sim.WithSequence(1, sim.WithLayerSizeOverwrite(1)),
	) {
		verified = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(2), verified)
}

func TestSwitchVerifyingByChangingGoodness(t *testing.T) {
	ctx := context.Background()
	const size = 10
	s := sim.New(sim.WithLayerSize(size))
	s.Setup()

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Hdist = 2
	cfg.Zdist = 2
	cfg.WindowSize = 10

	tortoise := tortoiseFromSimState(s.GetState(0), WithLogger(logtest.New(t)), WithConfig(cfg))
	var last, verified types.LayerID
	// in the test hare is not working from the start, voters
	for _, last = range sim.GenLayers(s,
		sim.WithSequence(20, sim.WithEmptyHareOutput()),
		sim.WithSequence(10),
	) {
		verified = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(1), verified)
	require.True(t, tortoise.trtl.mode.isVerifying(), "verifying mode")
}

func perfectVotingFirstBaseBallot(_ *mrand.Rand, layers []*types.Layer, _ int) sim.Voting {
	baseLayer := layers[len(layers)-1]
	support := layers[len(layers)-1].BlocksIDs()[0:1]
	against := layers[len(layers)-1].BlocksIDs()[1:]
	blts := baseLayer.Ballots()
	base := blts[0]
	return sim.Voting{Base: base.ID(), Against: against, Support: support}
}

func abstainVoting(_ *mrand.Rand, layers []*types.Layer, _ int) sim.Voting {
	baseLayer := layers[len(layers)-1]
	blts := baseLayer.Ballots()
	return sim.Voting{Base: blts[0].ID(), Abstain: []types.LayerID{baseLayer.Index()}}
}

func TestAbstainVotingVerifyingMode(t *testing.T) {
	ctx := context.Background()
	const size = 10
	s := sim.New(sim.WithLayerSize(size))
	s.Setup()

	cfg := defaultTestConfig()
	cfg.LayerSize = size

	tortoise := tortoiseFromSimState(s.GetState(0), WithLogger(logtest.New(t)), WithConfig(cfg))
	var last, verified types.LayerID
	for _, last = range sim.GenLayers(s,
		sim.WithSequence(1),
		sim.WithSequence(10, sim.WithVoteGenerator(
			sim.VaryingVoting(1, perfectVotingFirstBaseBallot, abstainVoting),
		)),
	) {
		verified = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(2), verified)
	for _, last = range sim.GenLayers(s,
		sim.WithSequence(1, sim.WithVoteGenerator(perfectVotingFirstBaseBallot)),
	) {
		verified = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(1), verified)
}

func voteWithBaseBallot(base types.BallotID) sim.VotesGenerator {
	return func(rng *mrand.Rand, layers []*types.Layer, i int) sim.Voting {
		voting := sim.PerfectVoting(rng, layers, i)
		voting.Base = base
		return voting
	}
}

func voteForBlock(block types.BlockID) sim.VotesGenerator {
	return func(rng *mrand.Rand, layers []*types.Layer, i int) sim.Voting {
		voting := sim.PerfectVoting(rng, layers, i)
		voting.Support = append(voting.Support, block)
		return voting
	}
}

func voteAgainst(block types.BlockID) sim.VotesGenerator {
	return func(rng *mrand.Rand, layers []*types.Layer, i int) sim.Voting {
		voting := sim.PerfectVoting(rng, layers, i)
		voting.Against = append(voting.Against, block)
		return voting
	}
}

func TestLateBaseBallot(t *testing.T) {
	ctx := context.Background()
	const size = 10
	s := sim.New(sim.WithLayerSize(size))
	s.Setup(sim.WithSetupUnitsRange(2, 2))

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Hdist = 1
	cfg.Zdist = cfg.Hdist

	tortoise := tortoiseFromSimState(s.GetState(0), WithLogger(logtest.New(t)), WithConfig(cfg))
	var last, verified types.LayerID
	for _, last = range sim.GenLayers(s,
		sim.WithSequence(2, sim.WithEmptyHareOutput()),
	) {
		verified = tortoise.HandleIncomingLayer(ctx, last)
	}

	blts, err := ballots.Layer(s.GetState(0).DB, last)
	require.NoError(t, err)
	require.NotEmpty(t, blts)

	buf, err := codec.Encode(blts[0])
	require.NoError(t, err)
	var base types.Ballot
	require.NoError(t, codec.Decode(buf, &base))
	base.EligibilityProofs[0].J++
	base.Initialize()
	tortoise.OnBallot(&base)

	for _, last = range sim.GenLayers(s,
		sim.WithSequence(1, sim.WithVoteGenerator(voteWithBaseBallot(base.ID()))),
		sim.WithSequence(1),
	) {
		verified = tortoise.HandleIncomingLayer(ctx, last)
	}

	require.Equal(t, last.Sub(1), verified)
}

func TestLateBlock(t *testing.T) {
	ctx := context.Background()
	const size = 10
	s := sim.New(sim.WithLayerSize(size))
	s.Setup(sim.WithSetupUnitsRange(2, 2))

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Hdist = 1
	cfg.Zdist = cfg.Hdist

	tortoise := tortoiseFromSimState(s.GetState(0), WithLogger(logtest.New(t)), WithConfig(cfg))
	last := s.Next()
	verified := tortoise.HandleIncomingLayer(ctx, last)

	blks, err := blocks.Layer(s.GetState(0).DB, last)
	require.NoError(t, err)
	require.NotEmpty(t, blks)

	buf, err := codec.Encode(blks[0])
	require.NoError(t, err)
	var block types.Block
	require.NoError(t, codec.Decode(buf, &block))
	require.True(t, len(block.TxIDs) > 2)
	block.TxIDs = block.TxIDs[:2]
	block.Initialize()
	tortoise.OnBlock(&block)
	require.NoError(t, blocks.Add(s.GetState(0).DB, &block))

	for _, last = range sim.GenLayers(s,
		sim.WithSequence(1, sim.WithVoteGenerator(voteForBlock(block.ID()))),
		sim.WithSequence(1),
	) {
		verified = tortoise.HandleIncomingLayer(ctx, last)
	}

	require.Equal(t, last.Sub(1), verified)

	valid, err := blocks.IsValid(s.GetState(0).DB, block.ID())
	require.NoError(t, err)
	require.True(t, valid)
}

func TestMaliciousBallotsAreIgnored(t *testing.T) {
	ctx := context.Background()
	const size = 10
	s := sim.New(sim.WithLayerSize(size))
	s.Setup()

	cfg := defaultTestConfig()
	cfg.LayerSize = size

	tortoise := tortoiseFromSimState(s.GetState(0), WithLogger(logtest.New(t)), WithConfig(cfg))
	var last, verified types.LayerID
	for _, last = range sim.GenLayers(s, sim.WithSequence(int(types.GetLayersPerEpoch()))) {
		verified = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(1), verified)

	blts, err := ballots.Layer(s.GetState(0).DB, last)
	require.NoError(t, err)
	for _, ballot := range blts {
		require.NoError(t, identities.SetMalicious(s.GetState(0).DB, ballot.SmesherID().Bytes()))
	}

	require.NoError(t, tortoise.rerun(ctx))
	verified = tortoise.HandleIncomingLayer(ctx, s.Next())
	require.Equal(t, verified, types.GetEffectiveGenesis())

	votes, err := tortoise.EncodeVotes(ctx)
	require.NoError(t, err)
	require.Equal(t, types.GenesisBallotID, votes.Base)
}

func TestStateManagement(t *testing.T) {
	const (
		size   = 10
		hdist  = 4
		window = 2
	)
	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Hdist = hdist
	cfg.Zdist = hdist
	cfg.WindowSize = window

	s := sim.New(
		sim.WithLayerSize(size),
	)
	s.Setup()

	tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))

	var last, verified types.LayerID
	for _, last = range sim.GenLayers(s,
		sim.WithSequence(20),
	) {
		verified = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(1), verified)

	evicted := tortoise.trtl.evicted
	require.Equal(t, verified.Sub(window).Sub(1), evicted)
	for lid := types.GetEffectiveGenesis(); !lid.After(evicted); lid = lid.Add(1) {
		require.Empty(t, tortoise.trtl.layers[lid])
	}

	for lid := evicted.Add(1); !lid.After(last); lid = lid.Add(1) {
		for _, block := range tortoise.trtl.layers[lid].blocks {
			require.Contains(t, tortoise.trtl.blockRefs, block.id, "layer %s", lid)
		}
		for _, ballot := range tortoise.trtl.layer(lid).ballots {
			require.Contains(t, tortoise.trtl.ballotRefs, ballot.id, "layer %s", lid)
			for current := ballot.votes.tail; current != nil; current = current.prev {
				require.True(t, !current.lid.Before(evicted), "no votes for layers before evicted (evicted %s, in state %s, ballot %s)", evicted, current.lid, ballot.layer)
				if current.prev == nil {
					require.Equal(t, current.lid, evicted, "last vote is exactly evicted")
				}
			}
			break
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

		tortoise := tortoiseFromSimState(
			s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)),
		)
		verified := tortoise.HandleIncomingLayer(context.Background(),
			s.Next(sim.WithNumBlocks(1), sim.WithBlockTickHeights(100_000)))
		for i := 0; i < int(cfg.Hdist); i++ {
			verified = tortoise.HandleIncomingLayer(context.Background(), s.Next())
		}
		require.Equal(t, types.GetEffectiveGenesis(), verified)
		last := s.Next()
		verified = tortoise.HandleIncomingLayer(context.Background(), last)
		// verifies layer by counting all votes
		require.Equal(t, last.Sub(1), verified)
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

		tortoise := tortoiseFromSimState(
			s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)),
		)
		tortoise.HandleIncomingLayer(context.Background(), s.Next(sim.WithNumBlocks(1), sim.WithBlockTickHeights(slow+1)))
		tortoise.HandleIncomingLayer(context.Background(),
			s.Next(sim.WithEmptyHareOutput(), sim.WithNumBlocks(0)))
		// 3 is handpicked so that threshold will be crossed if bug wasn't fixed
		for i := 0; i < 3; i++ {
			tortoise.HandleIncomingLayer(context.Background(), s.Next(sim.WithNumBlocks(1)))
		}

		require.Equal(t, types.GetEffectiveGenesis(), tortoise.LatestComplete())
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
			sim.WithSetupMinerRange(8, 8),
			sim.WithSetupTicks(
				normal, normal, normal,
				normal, normal,
				slow, slow, slow,
			),
		)
		tortoise := tortoiseFromSimState(
			s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)),
		)
		var last, verified types.LayerID
		for i := 0; i < int(cfg.Hdist); i++ {
			last = s.Next(sim.WithNumBlocks(1), sim.WithBlockTickHeights(slow+1), sim.WithVoteGenerator(sim.ConsistentVoting))
			verified = tortoise.HandleIncomingLayer(context.Background(), last)
		}
		require.Equal(t, last.Sub(2), verified)
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
		tortoise := tortoiseFromSimState(
			s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)),
		)
		var last, verified types.LayerID
		for i := 0; i < int(cfg.Hdist); i++ {
			last = s.Next(sim.WithNumBlocks(0))
			verified = tortoise.HandleIncomingLayer(context.Background(), last)
		}
		require.Equal(t, last.Sub(1), verified)
	})
}

func testEmptyLayers(t *testing.T, hdist int) {
	const size = 4

	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.Hdist = uint32(hdist)
	cfg.Zdist = 3
	cfg.LayerSize = size

	// TODO(dshulyak) parametrize test with varying skipFrom, skipTo
	// skipping layers 9, 10, 11, 12, 13
	skipFrom, skipTo := 1, 6

	s := sim.New(
		sim.WithLayerSize(cfg.LayerSize),
	)
	s.Setup(
		sim.WithSetupMinerRange(size, size),
	)
	tortoise := tortoiseFromSimState(
		s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)),
	)
	var (
		last     = types.GetEffectiveGenesis()
		verified types.LayerID
	)
	for i := 0; i < int(skipTo); i++ {
		opts := []sim.NextOpt{
			sim.WithVoteGenerator(tortoiseVotingWithCurrent(tortoise)),
		}
		skipped := i >= skipFrom && i < skipTo
		if skipped {
			opts = append(opts, sim.WithoutHareOutput(), sim.WithNumBlocks(0))
		} else {
			opts = append(opts, sim.WithNumBlocks(1))
		}
		last = s.Next(opts...)
		if skipped {
			continue
		}
		verified = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, types.GetEffectiveGenesis(), verified)
	for i := 0; i <= int(cfg.Zdist); i++ {
		last = s.Next(
			sim.WithNumBlocks(1),
			sim.WithVoteGenerator(tortoiseVotingWithCurrent(tortoise)),
		)
		verified = tortoise.HandleIncomingLayer(ctx, last)
	}
	expected := types.GetEffectiveGenesis().Add(uint32(skipFrom))
	require.Equal(t, expected, verified)
	for i := 0; i < skipTo-skipFrom-1; i++ {
		last = s.Next(
			sim.WithNumBlocks(1),
			sim.WithVoteGenerator(tortoiseVotingWithCurrent(tortoise)),
		)
		verified = tortoise.HandleIncomingLayer(ctx, last)
		require.Equal(t, expected.Add(uint32(i)+1), verified)
	}
	last = s.Next(
		sim.WithNumBlocks(1),
		sim.WithVoteGenerator(tortoiseVotingWithCurrent(tortoise)),
	)
	verified = tortoise.HandleIncomingLayer(ctx, last)
	require.Equal(t, last.Sub(1), verified)
}

func TestEmptyLayers(t *testing.T) {
	t.Run("recovered within hdist", func(t *testing.T) {
		testEmptyLayers(t, 10)
	})
	t.Run("not recovered within hdist", func(t *testing.T) {
		testEmptyLayers(t, 5)
	})
}

func TestOnBallotComputeOpinion(t *testing.T) {
	const size = 4

	ctx := context.Background()
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
		tortoise := tortoiseFromSimState(
			s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)),
		)
		var last types.LayerID
		for i := 0; i < distance; i++ {
			last = s.Next(sim.WithNumBlocks(1))
			tortoise.HandleIncomingLayer(ctx, last)
		}

		rst, err := ballots.Layer(s.GetState(0).DB, last)
		require.NoError(t, err)
		require.NotEmpty(t, rst)

		id := types.BallotID{1}
		ballot := types.NewExistingBallot(id, nil, nil, rst[0].InnerBallot)
		ballot.Votes.Base = types.GenesisBallotID
		ballot.Votes.Support = []types.BlockID{types.GenesisBlockID}
		ballot.Votes.Against = nil

		tortoise.OnBallot(&ballot)

		info := tortoise.trtl.ballotRefs[id]
		hasher := hash.New()
		hasher.Write(types.GenesisBlockID[:])
		for i := 0; i < distance-1; i++ {
			buf := hasher.Sum(nil)
			hasher.Reset()
			hasher.Write(buf)
		}
		require.Equal(t, hasher.Sum(nil), info.opinion().Bytes())
	})
	t.Run("support abstain support", func(t *testing.T) {
		const distance = 3
		s := sim.New(
			sim.WithLayerSize(cfg.LayerSize),
		)
		s.Setup(
			sim.WithSetupMinerRange(size, size),
		)
		tortoise := tortoiseFromSimState(
			s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)),
		)
		var last types.LayerID
		for i := 0; i < distance; i++ {
			last = s.Next(sim.WithNumBlocks(1))
			tortoise.HandleIncomingLayer(ctx, last)
		}

		rst, err := ballots.Layer(s.GetState(0).DB, last)
		require.NoError(t, err)
		require.NotEmpty(t, rst)

		id := types.BallotID{1}
		ballot := types.NewExistingBallot(id, nil, nil, rst[0].InnerBallot)
		ballot.Votes.Abstain = []types.LayerID{types.GetEffectiveGenesis().Add(1)}

		tortoise.OnBallot(&ballot)

		info := tortoise.trtl.ballotRefs[id]
		hasher := hash.New()
		hasher.Write(types.GenesisBlockID[:])
		buf := hasher.Sum(nil)
		hasher.Reset()

		hasher.Write(buf)
		hasher.Write(abstainSentinel)
		buf = hasher.Sum(nil)
		hasher.Reset()

		hasher.Write(buf)
		hasher.Write(ballot.Votes.Support[0][:])
		require.Equal(t, hasher.Sum(nil), info.opinion().Bytes())
	})
}

func TestOnHareOutput(t *testing.T) {
	const size = 4

	ctx := context.Background()
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
			genDistance:   int(cfg.Zdist), // after zdist minprocessed is updated
		},
		{
			desc:          "empty",
			failedOptions: []sim.NextOpt{sim.WithEmptyHareOutput()},
			genDistance:   int(cfg.Zdist - 1),
		},
		{
			desc:          "different hare output",
			failedOptions: []sim.NextOpt{sim.WithHareOutputIndex(1)},
			genDistance:   int(cfg.Zdist - 1),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			s := sim.New(
				sim.WithLayerSize(cfg.LayerSize),
			)
			s.Setup(
				sim.WithSetupMinerRange(size, size),
			)
			tortoise := tortoiseFromSimState(
				s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)),
			)
			verified := tortoise.HandleIncomingLayer(ctx, s.Next(tc.failedOptions...))
			for i := 0; i <= tc.genDistance; i++ {
				verified = tortoise.HandleIncomingLayer(ctx, s.Next())
			}
			require.Equal(t, types.GetEffectiveGenesis(), verified)
			empty := s.Layer(0)
			tortoise.OnHareOutput(empty.Index(), empty.Blocks()[0].ID())
			last := s.Next(sim.WithNumBlocks(1))
			verified = tortoise.HandleIncomingLayer(ctx, last)
			require.Equal(t, last.Sub(1), verified)
		})
	}
}

func TestDecodeExceptions(t *testing.T) {
	const size = 1

	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.LayerSize = size

	s := sim.New(
		sim.WithLayerSize(cfg.LayerSize),
	)
	s.Setup(
		sim.WithSetupMinerRange(size, size),
	)

	tortoise := tortoiseFromSimState(
		s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)),
	)
	last := s.Next(sim.WithNumBlocks(2))
	tortoise.HandleIncomingLayer(ctx, last)

	layer := tortoise.trtl.layer(last)
	require.Equal(t, against, layer.blocks[0].hare)
	block := layer.blocks[0].id

	last = s.Next(
		sim.WithNumBlocks(1),
	)
	tortoise.HandleIncomingLayer(ctx, last)
	ballots1 := tortoise.trtl.layer(last).ballots

	last = s.Next(
		sim.WithNumBlocks(1),
		sim.WithVoteGenerator(voteForBlock(block)),
	)
	tortoise.HandleIncomingLayer(ctx, last)
	ballots2 := tortoise.trtl.layer(last).ballots

	last = s.Next(
		sim.WithNumBlocks(1),
		sim.WithVoteGenerator(voteAgainst(block)),
	)
	tortoise.HandleIncomingLayer(ctx, last)
	ballots3 := tortoise.trtl.layer(last).ballots

	for _, ballot := range ballots1 {
		require.Equal(t, against, ballot.votes.find(layer.lid, block), "base ballot votes against")
	}
	for _, ballot := range ballots2 {
		require.Equal(t, support, ballot.votes.find(layer.lid, block), "new ballot overwrites vote")
	}
	for _, ballot := range ballots3 {
		require.Equal(t, against, ballot.votes.find(layer.lid, block), "latest ballot overwrites back to against")
	}
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

	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.LayerSize = layerSize
	cfg.WindowSize = window

	tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(b)))
	for i := 0; i < window; i++ {
		tortoise.HandleIncomingLayer(ctx, s.Next())
	}
	last := s.Next()
	tortoise.HandleIncomingLayer(ctx, last)
	ballots, err := ballots.Layer(s.GetState(0).DB, last)
	require.NoError(b, err)
	hare, err := layers.GetHareOutput(s.GetState(0).DB, last.Sub(window/2))
	require.NoError(b, err)
	modified := *ballots[0]
	modified.Votes.Against = append(modified.Votes.Against, hare)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := types.BallotID{}
		binary.BigEndian.PutUint64(id[:], uint64(i)+1)
		ballot := types.NewExistingBallot(id, nil, nil, modified.InnerBallot)
		tortoise.OnBallot(&ballot)

		b.StopTimer()
		delete(tortoise.trtl.ballotRefs, ballot.ID())
		layer := tortoise.trtl.layer(ballot.LayerIndex)
		layer.ballots = nil
		b.StartTimer()
	}
}
