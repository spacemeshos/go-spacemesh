package tortoise

import (
	"context"
	"fmt"
	"math/big"
	mrand "math/rand"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/system"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
	"github.com/spacemeshos/go-spacemesh/tortoise/mocks"
	"github.com/spacemeshos/go-spacemesh/tortoise/sim"
)

func init() {
	types.SetLayersPerEpoch(4)
}

type atxDataWriter interface {
	atxDataProvider
	StoreAtx(types.EpochID, *types.ActivationTx) error
}

func getAtxDB() *mockAtxDataProvider {
	return &mockAtxDataProvider{atxDB: make(map[types.ATXID]*types.ActivationTxHeader)}
}

type mockAtxDataProvider struct {
	mockAtxHeader *types.ActivationTxHeader
	atxDB         map[types.ATXID]*types.ActivationTxHeader
	epochWeight   uint64
	firstTime     time.Time
}

func (madp *mockAtxDataProvider) GetAtxHeader(atxID types.ATXID) (*types.ActivationTxHeader, error) {
	if madp.mockAtxHeader != nil {
		return madp.mockAtxHeader, nil
	}
	if atxHeader, ok := madp.atxDB[atxID]; ok {
		return atxHeader, nil
	}

	// return a mocked value
	return &types.ActivationTxHeader{NIPostChallenge: types.NIPostChallenge{NodeID: types.NodeID{Key: "fakekey"}}}, nil
}

func (madp *mockAtxDataProvider) StoreAtx(_ types.EpochID, atx *types.ActivationTx) error {
	// store only the header
	madp.atxDB[atx.ID()] = atx.ActivationTxHeader
	if madp.firstTime.IsZero() {
		madp.firstTime = time.Now()
	}
	return nil
}

func (madp *mockAtxDataProvider) storeEpochWeight(weight uint64) {
	madp.epochWeight = weight
}

func (madp *mockAtxDataProvider) GetEpochWeight(_ types.EpochID) (uint64, []types.ATXID, error) {
	return madp.epochWeight, nil, nil
}

func getInMemMesh(tb testing.TB) *mesh.DB {
	return mesh.NewMemMeshDB(logtest.New(tb))
}

func addLayerToMesh(m *mesh.DB, layer *types.Layer) error {
	// add blocks to mDB
	for _, bl := range layer.Ballots() {
		if err := m.AddBallot(bl); err != nil {
			return fmt.Errorf("add ballot: %w", err)
		}
	}
	for _, bl := range layer.Blocks() {
		if err := m.AddBlock(bl); err != nil {
			return fmt.Errorf("add block: %w", err)
		}
	}
	if err := m.SaveHareConsensusOutput(context.TODO(), layer.Index(), layer.BlocksIDs()[0]); err != nil {
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
			_, verified, _ = tortoise.HandleIncomingLayer(ctx, lid)
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
			sim.WithSequence(2, sim.WithoutHareOutput()),
		) {
			last = lid
			_, verified, _ = tortoise.HandleIncomingLayer(ctx, lid)
		}
		require.Equal(t, genesis.Add(5), verified)

		for _, lid := range sim.GenLayers(s,
			sim.WithSequence(21),
		) {
			last = lid
			_, verified, _ = tortoise.HandleIncomingLayer(ctx, lid)
		}
		require.Equal(t, last.Sub(1), verified)
	})

	t.Run("HealAfterBadGoodBadGoodBad", func(t *testing.T) {
		s := sim.New(sim.WithLayerSize(size))
		s.Setup()

		ctx := context.Background()
		cfg := defaultTestConfig()
		cfg.LayerSize = size
		tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg))

		var (
			last     types.LayerID
			verified types.LayerID
		)
		for _, lid := range sim.GenLayers(s,
			sim.WithSequence(5),
			sim.WithSequence(1, sim.WithoutHareOutput()),
			sim.WithSequence(2),
			sim.WithSequence(2, sim.WithoutHareOutput()),
			sim.WithSequence(30),
		) {
			last = lid
			_, verified, _ = tortoise.HandleIncomingLayer(ctx, lid)
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
	tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))

	var last, verified types.LayerID
	for i := 0; i < 5; i++ {
		last = s.Next(sim.WithVoteGenerator(tortoiseVoting(tortoise)))
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(1), verified)
	expected := last

	for i := 0; i < 2; i++ {
		last = s.Next(
			sim.WithVoteGenerator(tortoiseVoting(tortoise)),
			sim.WithoutHareOutput(),
		)
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
	}
	for i := 0; i < 4; i++ {
		last = s.Next(
			sim.WithVoteGenerator(tortoiseVoting(tortoise)),
		)
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
	}

	// verification will get stuck as of the first layer with conflicting local and global opinions.
	// block votes aren't counted because blocks aren't marked good, because they contain exceptions older
	// than their base block.
	// self-healing will not run because the layers aren't old enough.
	require.Equal(t, expected, verified)
}

type (
	baseBallotProvider func(context.Context) (*types.Votes, error)
)

func generateBallots(t *testing.T, l types.LayerID, natxs, nballots int, bbp baseBallotProvider, atxdb atxDataWriter, weight uint) []*types.Ballot {
	votes, err := bbp(context.TODO())
	require.NoError(t, err)

	atxs := []types.ATXID{}
	for i := 0; i < natxs; i++ {
		atxHeader := makeAtxHeaderWithWeight(weight)
		atx := &types.ActivationTx{InnerActivationTx: &types.InnerActivationTx{ActivationTxHeader: atxHeader}}
		atx.PubLayerID = l
		atx.NodeID.Key = fmt.Sprintf("%d", i)
		atx.CalcAndSetID()
		require.NoError(t, atxdb.StoreAtx(l.GetEpoch(), atx))
		atxs = append(atxs, atx.ID())
	}
	ballots := make([]*types.Ballot, 0, nballots)
	for i := 0; i < nballots; i++ {
		b := &types.Ballot{
			InnerBallot: types.InnerBallot{
				AtxID:            atxs[i%len(atxs)],
				Votes:            *votes,
				EligibilityProof: types.VotingEligibilityProof{J: uint32(i)},
				LayerIndex:       l,
				EpochData: &types.EpochData{
					ActiveSet: atxs,
				},
			},
		}
		signer := signing.NewEdSigner()
		b.Signature = signer.Sign(b.Bytes())
		b.Initialize()
		ballots = append(ballots, b)
	}
	return ballots
}

func createTurtleLayer(t *testing.T, l types.LayerID, msh *mesh.DB, atxdb atxDataWriter, bbp baseBallotProvider, blocksPerLayer int) *types.Layer {
	ballots := generateBallots(t, l, blocksPerLayer, blocksPerLayer, bbp, atxdb, 1)
	blocks := []*types.Block{
		types.GenLayerBlock(l, types.RandomTXSet(rand.Intn(100))),
		types.GenLayerBlock(l, types.RandomTXSet(rand.Intn(100))),
	}
	return types.NewExistingLayer(l, ballots, blocks)
}

func TestLayerCutoff(t *testing.T) {
	r := require.New(t)
	mdb := getInMemMesh(t)
	alg := defaultAlgorithm(t, mdb)

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
	mdb := getInMemMesh(t)

	atxdb := getAtxDB()
	alg := defaultAlgorithm(t, mdb)
	alg.trtl.atxdb = atxdb
	l := types.GenesisLayer()

	logger.With().Info("genesis is", l.Index(), types.BlockIdsField(types.ToBlockIDs(l.Blocks())))
	logger.With().Info("genesis is", log.Object("block", l.Blocks()[0]))

	l1 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(1), mdb, atxdb, alg.BaseBallot, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l1))

	alg.HandleIncomingLayer(context.TODO(), l1.Index())

	l2 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(2), mdb, atxdb, alg.BaseBallot, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l2))
	alg.HandleIncomingLayer(context.TODO(), l2.Index())

	require.Equal(t, int(types.GetEffectiveGenesis().Add(1).Uint32()), int(alg.LatestComplete().Uint32()))

	l3a := createTurtleLayer(t, types.GetEffectiveGenesis().Add(3), mdb, atxdb, alg.BaseBallot, defaultTestLayerSize+1)

	// this should fail as the blocks for this layer have not been added to the mesh yet
	alg.HandleIncomingLayer(context.TODO(), l3a.Index())
	require.Equal(t, types.GetEffectiveGenesis().Add(1), alg.LatestComplete())

	l3 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(3), mdb, atxdb, alg.BaseBallot, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l3))
	require.NoError(t, alg.rerun(context.TODO()))
	alg.updateFromRerun(context.TODO())

	l4 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(4), mdb, atxdb, alg.BaseBallot, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l4))
	alg.HandleIncomingLayer(context.TODO(), l4.Index())
	require.Equal(t, int(types.GetEffectiveGenesis().Add(3).Uint32()), int(alg.LatestComplete().Uint32()), "wrong latest complete layer")
}

func TestBaseBallot(t *testing.T) {
	r := require.New(t)
	mdb := getInMemMesh(t)
	atxdb := getAtxDB()
	alg := defaultAlgorithm(t, mdb)
	alg.trtl.atxdb = atxdb

	l0 := types.GenesisLayer()
	expectBaseBallotLayer := func(layerID types.LayerID, numAgainst, numSupport, numNeutral int) {
		votes, err := alg.BaseBallot(context.TODO())
		r.NoError(err)
		// expect no exceptions
		r.Len(votes.Support, numSupport)
		r.Len(votes.Against, numAgainst)
		r.Len(votes.Abstain, numNeutral)
		// expect a valid genesis base ballot
		baseBallot, err := alg.trtl.bdp.GetBallot(votes.Base)
		r.NoError(err)
		r.Equal(layerID, baseBallot.LayerIndex, "base ballot is from wrong layer")
	}

	// it should support all genesis blocks
	expectBaseBallotLayer(l0.Index(), 0, len(types.GenesisLayer().Blocks()), 0)

	// add a couple of incoming layers and make sure the base ballot layer advances as well
	l1 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(1), mdb, atxdb, alg.BaseBallot, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l1))
	alg.HandleIncomingLayer(context.TODO(), l1.Index())
	expectBaseBallotLayer(l1.Index(), 0, numValidBlock, 0)

	l2 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(2), mdb, atxdb, alg.BaseBallot, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l2))
	alg.HandleIncomingLayer(context.TODO(), l2.Index())
	require.Equal(t, int(types.GetEffectiveGenesis().Add(1).Uint32()), int(alg.LatestComplete().Uint32()))
	expectBaseBallotLayer(l2.Index(), 0, numValidBlock, 0)

	// add a layer that's not in the mesh and make sure it does not advance
	l3 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(3), mdb, atxdb, alg.BaseBallot, defaultTestLayerSize)
	alg.HandleIncomingLayer(context.TODO(), l3.Index())
	require.Equal(t, int(types.GetEffectiveGenesis().Add(1).Uint32()), int(alg.LatestComplete().Uint32()))
	expectBaseBallotLayer(l2.Index(), 0, numValidBlock, 0)
}

func mockedBeacons(tb testing.TB) system.BeaconGetter {
	tb.Helper()

	ctrl := gomock.NewController(tb)
	mockBeacons := smocks.NewMockBeaconGetter(ctrl)
	mockBeacons.EXPECT().GetBeacon(gomock.Any()).Return(types.EmptyBeacon, nil).AnyTimes()
	return mockBeacons
}

func defaultTurtle(tb testing.TB) *turtle {
	return newTurtle(
		logtest.New(tb),
		getInMemMesh(tb),
		getAtxDB(),
		mockedBeacons(tb),
		defaultTestConfig(),
	)
}

func TestCloneTurtle(t *testing.T) {
	r := require.New(t)
	trtl := defaultTurtle(t)
	trtl.LayerSize++                 // make sure defaults aren't being read
	trtl.last = types.NewLayerID(10) // state should not be cloned
	trtl2 := trtl.cloneTurtleParams()
	r.Equal(trtl.bdp, trtl2.bdp)
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
	return New(state.MeshDB, state.AtxDB, state.Beacons, opts...)
}

func defaultAlgorithm(tb testing.TB, mdb *mesh.DB) *Tortoise {
	tb.Helper()
	return New(mdb, getAtxDB(), mockedBeacons(tb),
		WithConfig(defaultTestConfig()),
		WithLogger(logtest.New(tb)),
	)
}

func makeAtxHeaderWithWeight(weight uint) *types.ActivationTxHeader {
	header := &types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{NodeID: types.NodeID{Key: "key"}},
	}
	header.StartTick = 0
	header.EndTick = 1
	header.NumUnits = weight
	return header
}

func checkVerifiedLayer(t *testing.T, trtl *turtle, layerID types.LayerID) {
	require.Equal(t, int(layerID.Uint32()), int(trtl.verified.Uint32()), "got unexpected value for last verified layer")
}

func TestCalculateOpinionWithThreshold(t *testing.T) {
	for _, tc := range []struct {
		desc      string
		expect    sign
		vote      weight
		threshold *big.Rat
		weight    weight
	}{
		{
			desc:      "Support",
			expect:    support,
			vote:      weightFromInt64(6),
			threshold: big.NewRat(1, 2),
			weight:    weightFromInt64(10),
		},
		{
			desc:      "Abstain",
			expect:    abstain,
			vote:      weightFromInt64(3),
			threshold: big.NewRat(1, 2),
			weight:    weightFromInt64(10),
		},
		{
			desc:      "AbstainZero",
			expect:    abstain,
			vote:      weightFromInt64(0),
			threshold: big.NewRat(1, 2),
			weight:    weightFromInt64(10),
		},
		{
			desc:      "Against",
			expect:    against,
			vote:      weightFromInt64(-6),
			threshold: big.NewRat(1, 2),
			weight:    weightFromInt64(10),
		},
		{
			desc:      "ComplexSupport",
			expect:    support,
			vote:      weightFromInt64(121),
			threshold: big.NewRat(60, 100),
			weight:    weightFromInt64(200),
		},
		{
			desc:      "ComplexAbstain",
			expect:    abstain,
			vote:      weightFromInt64(120),
			threshold: big.NewRat(60, 100),
			weight:    weightFromInt64(200),
		},
		{
			desc:      "ComplexAbstain2",
			expect:    abstain,
			vote:      weightFromInt64(-120),
			threshold: big.NewRat(60, 100),
			weight:    weightFromInt64(200),
		},
		{
			desc:      "ComplexAgainst",
			expect:    against,
			vote:      weightFromInt64(-121),
			threshold: big.NewRat(60, 100),
			weight:    weightFromInt64(200),
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.expect,
				tc.vote.cmp(tc.weight.fraction(tc.threshold)))
		})
	}
}

func TestMultiTortoise(t *testing.T) {
	r := require.New(t)

	t.Run("happy path", func(t *testing.T) {
		const layerSize = defaultTestLayerSize * 2

		mdb1 := getInMemMesh(t)
		atxdb1 := getAtxDB()
		atxdb1.storeEpochWeight(uint64(layerSize))
		alg1 := defaultAlgorithm(t, mdb1)
		alg1.trtl.atxdb = atxdb1
		alg1.trtl.LayerSize = layerSize
		alg1.logger = alg1.logger.Named("trtl1")
		alg1.trtl.logger = alg1.logger

		mdb2 := getInMemMesh(t)
		atxdb2 := getAtxDB()
		atxdb2.storeEpochWeight(uint64(layerSize))
		alg2 := defaultAlgorithm(t, mdb2)
		alg2.trtl.atxdb = atxdb2
		alg2.trtl.LayerSize = layerSize
		alg2.logger = alg2.logger.Named("trtl2")
		alg2.trtl.logger = alg2.logger

		makeAndProcessLayerMultiTortoise := func(layerID types.LayerID) {
			// simulate producing blocks in parallel
			ballotsA := generateBallots(t, layerID, layerSize, layerSize, alg1.BaseBallot, atxdb1, 1)
			ballotsB := generateBallots(t, layerID, layerSize, layerSize, alg2.BaseBallot, atxdb2, 1)

			// these will produce identical sets of blocks, so throw away half of each
			// (we could probably get away with just using, say, A's blocks, but to be more thorough we also want
			// to test the BaseBallot provider of each tortoise)
			ballotsA = ballotsA[:layerSize/2]
			ballotsB = ballotsB[len(ballotsA):]
			blocks := append(ballotsA, ballotsB...)

			// add all ballots/blocks to both tortoises
			for _, ballot := range blocks {
				r.NoError(mdb1.AddBallot(ballot))
				r.NoError(mdb2.AddBallot(ballot))
			}
			block := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(mdb1.AddBlock(block))
			r.NoError(mdb2.AddBlock(block))
			r.NoError(mdb1.SaveHareConsensusOutput(context.TODO(), layerID, block.ID()))
			r.NoError(mdb2.SaveHareConsensusOutput(context.TODO(), layerID, block.ID()))
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

		mdb1 := getInMemMesh(t)
		atxdb1 := getAtxDB()
		atxdb1.storeEpochWeight(uint64(layerSize))
		alg1 := defaultAlgorithm(t, mdb1)
		alg1.trtl.atxdb = atxdb1
		alg1.trtl.LayerSize = layerSize
		alg1.logger = alg1.logger.Named("trtl1")
		alg1.trtl.logger = alg1.logger

		mdb2 := getInMemMesh(t)
		atxdb2 := getAtxDB()
		atxdb2.storeEpochWeight(uint64(layerSize))
		alg2 := defaultAlgorithm(t, mdb2)
		alg2.trtl.atxdb = atxdb2
		alg2.trtl.LayerSize = layerSize
		alg2.logger = alg2.logger.Named("trtl2")
		alg2.trtl.logger = alg2.logger

		makeBallots := func(layerID types.LayerID) (ballotsA, ballotsB []*types.Ballot) {
			// simulate producing blocks in parallel
			ballotsA = generateBallots(t, layerID, layerSize, layerSize, alg1.BaseBallot, atxdb1, 1)
			ballotsB = generateBallots(t, layerID, layerSize, layerSize, alg2.BaseBallot, atxdb2, 1)

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
			var ballots []*types.Ballot
			ballots = append(ballotsA, ballotsB...)

			// add all ballots/blocks to both tortoises
			for _, ballot := range ballots {
				r.NoError(mdb1.AddBallot(ballot))
				r.NoError(mdb2.AddBallot(ballot))
			}
			block := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(mdb1.AddBlock(block))
			r.NoError(mdb2.AddBlock(block))
			r.NoError(mdb1.SaveHareConsensusOutput(context.TODO(), layerID, block.ID()))
			r.NoError(mdb2.SaveHareConsensusOutput(context.TODO(), layerID, block.ID()))
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
				r.NoError(mdb1.AddBallot(ballot))
			}
			blockA := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(mdb1.AddBlock(blockA))
			r.NoError(mdb1.SaveHareConsensusOutput(context.TODO(), layerID, blockA.ID()))
			forkblocksA = append(forkblocksA, blockA)
			alg1.HandleIncomingLayer(context.TODO(), layerID)
			for _, ballot := range ballotsB {
				r.NoError(mdb2.AddBallot(ballot))
			}
			blockB := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(mdb2.AddBlock(blockB))
			r.NoError(mdb2.SaveHareConsensusOutput(context.TODO(), layerID, blockB.ID()))
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

			// these blocks will be nearly identical but they will have different base ballots, since the set of blocks
			// for recent layers has been bifurcated, so we have to generate and store blocks separately to simulate
			// an ongoing partition.
			ballotsA := generateBallots(t, layerID, layerSize, layerSize, alg1.BaseBallot, atxdb1, 1)
			forkballotsA = append(forkballotsA, ballotsA...)
			for _, ballot := range ballotsA {
				r.NoError(mdb1.AddBallot(ballot))
			}
			blockA := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(mdb1.AddBlock(blockA))
			r.NoError(mdb1.SaveHareConsensusOutput(context.TODO(), layerID, blockA.ID()))
			forkblocksA = append(forkblocksA, blockA)
			alg1.HandleIncomingLayer(context.TODO(), layerID)

			ballotsB := generateBallots(t, layerID, layerSize, layerSize, alg2.BaseBallot, atxdb2, 1)
			forkballotsB = append(forkballotsB, ballotsB...)
			for _, ballot := range ballotsB {
				r.NoError(mdb2.AddBallot(ballot))
			}
			blockB := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(mdb2.AddBlock(blockB))
			r.NoError(mdb2.SaveHareConsensusOutput(context.TODO(), layerID, blockB.ID()))
			forkblocksB = append(forkblocksB, blockB)
			alg2.HandleIncomingLayer(context.TODO(), layerID)

			// majority tortoise is unaffected, minority tortoise is still stuck
			checkVerifiedLayer(t, alg1.trtl, layerID.Sub(1))
			checkVerifiedLayer(t, alg2.trtl, lastVerified)
		}

		// finally, the minority tortoise heals and regains parity with the majority tortoise
		layerID = layerID.Add(1)
		ballotsA := generateBallots(t, layerID, layerSize, layerSize, alg1.BaseBallot, atxdb1, 1)
		forkballotsA = append(forkballotsA, ballotsA...)
		for _, ballot := range ballotsA {
			r.NoError(mdb1.AddBallot(ballot))
		}
		blockA := types.GenLayerBlock(layerID, types.RandomTXSet(5))
		r.NoError(mdb1.AddBlock(blockA))
		r.NoError(mdb1.SaveHareConsensusOutput(context.TODO(), layerID, blockA.ID()))
		forkblocksA = append(forkblocksA, blockA)
		alg1.HandleIncomingLayer(context.TODO(), layerID)

		ballotsB := generateBallots(t, layerID, layerSize, layerSize, alg2.BaseBallot, atxdb2, 1)
		forkballotsB = append(forkballotsB, ballotsB...)
		for _, ballot := range ballotsB {
			r.NoError(mdb2.AddBallot(ballot))
		}
		blockB := types.GenLayerBlock(layerID, types.RandomTXSet(5))
		r.NoError(mdb2.AddBlock(blockB))
		r.NoError(mdb2.SaveHareConsensusOutput(context.TODO(), layerID, blockB.ID()))
		forkblocksB = append(forkblocksB, blockB)
		alg2.HandleIncomingLayer(context.TODO(), layerID)

		// minority node is healed
		lastVerified = layerID.Sub(1).Sub(alg1.cfg.Hdist)
		checkVerifiedLayer(t, alg1.trtl, layerID.Sub(1))
		checkVerifiedLayer(t, alg2.trtl, layerID.Sub(1))

		// now simulate a rejoin
		// send each tortoise's blocks to the other (simulated resync)
		// (of layers 18-40)
		for _, ballot := range forkballotsA {
			r.NoError(mdb2.AddBallot(ballot))
		}
		for _, block := range forkblocksA {
			r.NoError(mdb2.AddBlock(block))
		}

		for _, ballot := range forkballotsB {
			r.NoError(mdb1.AddBallot(ballot))
		}
		for _, block := range forkblocksB {
			r.NoError(mdb1.AddBlock(block))
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
			var ballots []*types.Ballot
			ballots = append(ballotsA, ballotsB...)

			// add all ballots/blocks to both tortoises
			for _, ballot := range ballots {
				r.NoError(mdb1.AddBallot(ballot))
				r.NoError(mdb2.AddBallot(ballot))
			}
			block := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(mdb1.AddBlock(block))
			r.NoError(mdb2.AddBlock(block))
			r.NoError(mdb1.SaveHareConsensusOutput(context.TODO(), layerID, block.ID()))
			r.NoError(mdb2.SaveHareConsensusOutput(context.TODO(), layerID, block.ID()))

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

		mdb1 := getInMemMesh(t)
		atxdb1 := getAtxDB()
		atxdb1.storeEpochWeight(uint64(layerSize))
		alg1 := defaultAlgorithm(t, mdb1)
		alg1.trtl.atxdb = atxdb1
		alg1.trtl.LayerSize = layerSize
		alg1.logger = alg1.logger.Named("trtl1")
		alg1.trtl.logger = alg1.logger

		mdb2 := getInMemMesh(t)
		atxdb2 := getAtxDB()
		atxdb2.storeEpochWeight(uint64(layerSize))
		alg2 := defaultAlgorithm(t, mdb2)
		alg2.trtl.atxdb = atxdb2
		alg2.trtl.LayerSize = layerSize
		alg2.logger = alg2.logger.Named("trtl2")
		alg2.trtl.logger = alg2.logger

		makeBallots := func(layerID types.LayerID) (ballotsA, ballotsB []*types.Ballot) {
			// simulate producing blocks in parallel
			ballotsA = generateBallots(t, layerID, layerSize, layerSize, alg1.BaseBallot, atxdb1, 1)
			ballotsB = generateBallots(t, layerID, layerSize, layerSize, alg2.BaseBallot, atxdb2, 1)

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
			var ballots []*types.Ballot
			ballots = append(ballotsA, ballotsB...)

			// add all ballots/blocks to both tortoises
			for _, ballot := range ballots {
				r.NoError(mdb1.AddBallot(ballot))
				r.NoError(mdb2.AddBallot(ballot))
			}
			block := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(mdb1.AddBlock(block))
			r.NoError(mdb2.AddBlock(block))
			r.NoError(mdb1.SaveHareConsensusOutput(context.TODO(), layerID, block.ID()))
			r.NoError(mdb2.SaveHareConsensusOutput(context.TODO(), layerID, block.ID()))
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
				r.NoError(mdb1.AddBallot(ballot))
			}
			blockA := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(mdb1.AddBlock(blockA))
			r.NoError(mdb1.SaveHareConsensusOutput(context.TODO(), layerID, blockA.ID()))
			alg1.HandleIncomingLayer(context.TODO(), layerID)
			for _, ballot := range ballotsB {
				r.NoError(mdb2.AddBallot(ballot))
			}
			blockB := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(mdb2.AddBlock(blockB))
			r.NoError(mdb2.SaveHareConsensusOutput(context.TODO(), layerID, blockB.ID()))
			alg2.HandleIncomingLayer(context.TODO(), layerID)

			// both nodes get stuck
			checkVerifiedLayer(t, alg1.trtl, lastVerified)
			checkVerifiedLayer(t, alg2.trtl, lastVerified)
		}

		// after a while (we simulate the distance here), both nodes eventually begin producing more blocks
		// in the case of a 50/50 split, this happens quickly
		for i := uint32(0); i < 3; i++ {
			layerID = layerID.Add(1)

			// these blocks will be nearly identical but they will have different base ballots, since the set of blocks
			// for recent layers has been bifurcated, so we have to generate and store blocks separately to simulate
			// an ongoing partition.
			ballotsA := generateBallots(t, layerID, layerSize, layerSize, alg1.BaseBallot, atxdb1, 1)
			for _, ballot := range ballotsA {
				r.NoError(mdb1.AddBallot(ballot))
			}
			blockA := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(mdb1.AddBlock(blockA))
			r.NoError(mdb1.SaveHareConsensusOutput(context.TODO(), layerID, blockA.ID()))
			alg1.HandleIncomingLayer(context.TODO(), layerID)

			ballotsB := generateBallots(t, layerID, layerSize, layerSize, alg2.BaseBallot, atxdb2, 1)
			for _, ballot := range ballotsB {
				r.NoError(mdb2.AddBallot(ballot))
			}
			blockB := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(mdb2.AddBlock(blockB))
			r.NoError(mdb2.SaveHareConsensusOutput(context.TODO(), layerID, blockB.ID()))
			alg2.HandleIncomingLayer(context.TODO(), layerID)

			// both nodes still stuck
			checkVerifiedLayer(t, alg1.trtl, lastVerified)
			checkVerifiedLayer(t, alg2.trtl, lastVerified)
		}

		// finally, both nodes heal and get unstuck
		layerID = layerID.Add(1)
		ballotsA := generateBallots(t, layerID, layerSize, layerSize, alg1.BaseBallot, atxdb1, 1)
		for _, ballot := range ballotsA {
			r.NoError(mdb1.AddBallot(ballot))
		}
		blockA := types.GenLayerBlock(layerID, types.RandomTXSet(5))
		r.NoError(mdb1.AddBlock(blockA))
		r.NoError(mdb1.SaveHareConsensusOutput(context.TODO(), layerID, blockA.ID()))
		alg1.HandleIncomingLayer(context.TODO(), layerID)

		ballotsB := generateBallots(t, layerID, layerSize, layerSize, alg2.BaseBallot, atxdb2, 1)
		for _, ballot := range ballotsB {
			r.NoError(mdb2.AddBallot(ballot))
		}
		blockB := types.GenLayerBlock(layerID, types.RandomTXSet(5))
		r.NoError(mdb2.AddBlock(blockB))
		r.NoError(mdb2.SaveHareConsensusOutput(context.TODO(), layerID, blockB.ID()))
		alg2.HandleIncomingLayer(context.TODO(), layerID)

		// both nodes can't switch from full tortoise
		checkVerifiedLayer(t, alg1.trtl, layerID.Sub(1))
		checkVerifiedLayer(t, alg2.trtl, layerID.Sub(1))
	})

	t.Run("three-way partition", func(t *testing.T) {
		const layerSize = 12

		mdb1 := getInMemMesh(t)
		atxdb1 := getAtxDB()
		atxdb1.storeEpochWeight(uint64(layerSize))
		alg1 := defaultAlgorithm(t, mdb1)
		alg1.trtl.atxdb = atxdb1
		alg1.trtl.LayerSize = layerSize
		alg1.logger = alg1.logger.Named("trtl1")
		alg1.trtl.logger = alg1.logger

		mdb2 := getInMemMesh(t)
		atxdb2 := getAtxDB()
		atxdb2.storeEpochWeight(uint64(layerSize))
		alg2 := defaultAlgorithm(t, mdb2)
		alg2.trtl.atxdb = atxdb2
		alg2.trtl.LayerSize = layerSize
		alg2.logger = alg2.logger.Named("trtl2")
		alg2.trtl.logger = alg2.logger

		mdb3 := getInMemMesh(t)
		atxdb3 := getAtxDB()
		atxdb3.storeEpochWeight(uint64(layerSize))
		alg3 := defaultAlgorithm(t, mdb3)
		alg3.trtl.atxdb = atxdb3
		alg3.trtl.LayerSize = layerSize
		alg3.logger = alg3.logger.Named("trtl3")
		alg3.trtl.logger = alg3.logger

		makeBallots := func(layerID types.LayerID) (ballotsA, ballotsB, ballotsC []*types.Ballot) {
			// simulate producing blocks in parallel
			ballotsA = generateBallots(t, layerID, layerSize, layerSize, alg1.BaseBallot, atxdb1, 1)
			ballotsB = generateBallots(t, layerID, layerSize, layerSize, alg2.BaseBallot, atxdb2, 1)
			ballotsC = generateBallots(t, layerID, layerSize, layerSize, alg3.BaseBallot, atxdb3, 1)

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
			var ballots []*types.Ballot
			ballots = append(ballotsA, ballotsB...)
			ballots = append(ballots, ballotsC...)

			// add all ballots/blocks to all tortoises
			for _, ballot := range ballots {
				r.NoError(mdb1.AddBallot(ballot))
				r.NoError(mdb2.AddBallot(ballot))
				r.NoError(mdb3.AddBallot(ballot))
			}
			block := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(mdb1.AddBlock(block))
			r.NoError(mdb2.AddBlock(block))
			r.NoError(mdb3.AddBlock(block))
			r.NoError(mdb1.SaveHareConsensusOutput(context.TODO(), layerID, block.ID()))
			r.NoError(mdb2.SaveHareConsensusOutput(context.TODO(), layerID, block.ID()))
			r.NoError(mdb3.SaveHareConsensusOutput(context.TODO(), layerID, block.ID()))
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
			for _, block := range ballotsA {
				r.NoError(mdb1.AddBallot(block))
			}
			blockA := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(mdb1.AddBlock(blockA))
			r.NoError(mdb1.SaveHareConsensusOutput(context.TODO(), layerID, blockA.ID()))
			alg1.HandleIncomingLayer(context.TODO(), layerID)

			for _, block := range ballotsB {
				r.NoError(mdb2.AddBallot(block))
			}
			blockB := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(mdb2.AddBlock(blockB))
			r.NoError(mdb2.SaveHareConsensusOutput(context.TODO(), layerID, blockB.ID()))
			alg2.HandleIncomingLayer(context.TODO(), layerID)

			for _, block := range ballotsC {
				r.NoError(mdb3.AddBallot(block))
			}
			blockC := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(mdb3.AddBlock(blockC))
			r.NoError(mdb3.SaveHareConsensusOutput(context.TODO(), layerID, blockC.ID()))
			alg3.HandleIncomingLayer(context.TODO(), layerID)

			// all nodes get stuck
			checkVerifiedLayer(t, alg1.trtl, lastVerified)
			checkVerifiedLayer(t, alg2.trtl, lastVerified)
			checkVerifiedLayer(t, alg3.trtl, lastVerified)
		}

		// after a while (we simulate the distance here), all nodes eventually begin producing more blocks
		for i := uint32(0); i < 7; i++ {
			layerID = layerID.Add(1)

			// these blocks will be nearly identical but they will have different base ballots, since the set of blocks
			// for recent layers has been bifurcated, so we have to generate and store blocks separately to simulate
			// an ongoing partition.
			ballotsA := generateBallots(t, layerID, layerSize, layerSize, alg1.BaseBallot, atxdb1, 1)
			for _, block := range ballotsA {
				r.NoError(mdb1.AddBallot(block))
			}
			blockA := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(mdb1.AddBlock(blockA))
			r.NoError(mdb1.SaveHareConsensusOutput(context.TODO(), layerID, blockA.ID()))
			alg1.HandleIncomingLayer(context.TODO(), layerID)

			ballotsB := generateBallots(t, layerID, layerSize, layerSize, alg2.BaseBallot, atxdb2, 1)
			for _, block := range ballotsB {
				r.NoError(mdb2.AddBallot(block))
			}
			blockB := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(mdb2.AddBlock(blockB))
			r.NoError(mdb2.SaveHareConsensusOutput(context.TODO(), layerID, blockB.ID()))
			alg2.HandleIncomingLayer(context.TODO(), layerID)

			ballotsC := generateBallots(t, layerID, layerSize, layerSize, alg3.BaseBallot, atxdb3, 1)
			for _, block := range ballotsC {
				r.NoError(mdb3.AddBallot(block))
			}
			blockC := types.GenLayerBlock(layerID, types.RandomTXSet(5))
			r.NoError(mdb3.AddBlock(blockC))
			r.NoError(mdb3.SaveHareConsensusOutput(context.TODO(), layerID, blockC.ID()))
			alg3.HandleIncomingLayer(context.TODO(), layerID)

			// nodes still stuck
			checkVerifiedLayer(t, alg1.trtl, lastVerified)
			checkVerifiedLayer(t, alg2.trtl, lastVerified)
			checkVerifiedLayer(t, alg3.trtl, lastVerified)
		}

		// finally, all nodes heal and get unstuck
		layerID = layerID.Add(1)
		ballotsA := generateBallots(t, layerID, layerSize, layerSize, alg1.BaseBallot, atxdb1, 1)
		for _, block := range ballotsA {
			r.NoError(mdb1.AddBallot(block))
		}
		blockA := types.GenLayerBlock(layerID, types.RandomTXSet(5))
		r.NoError(mdb1.AddBlock(blockA))
		r.NoError(mdb1.SaveHareConsensusOutput(context.TODO(), layerID, blockA.ID()))
		alg1.HandleIncomingLayer(context.TODO(), layerID)

		ballotsB := generateBallots(t, layerID, layerSize, layerSize, alg2.BaseBallot, atxdb2, 1)
		for _, block := range ballotsB {
			r.NoError(mdb2.AddBallot(block))
		}
		blockB := types.GenLayerBlock(layerID, types.RandomTXSet(5))
		r.NoError(mdb2.AddBlock(blockB))
		r.NoError(mdb2.SaveHareConsensusOutput(context.TODO(), layerID, blockB.ID()))
		alg2.HandleIncomingLayer(context.TODO(), layerID)

		ballotsC := generateBallots(t, layerID, layerSize, layerSize, alg3.BaseBallot, atxdb3, 1)
		for _, block := range ballotsC {
			r.NoError(mdb3.AddBallot(block))
		}
		blockC := types.GenLayerBlock(layerID, types.RandomTXSet(5))
		r.NoError(mdb3.AddBlock(blockC))
		r.NoError(mdb3.SaveHareConsensusOutput(context.TODO(), layerID, blockC.ID()))
		alg3.HandleIncomingLayer(context.TODO(), layerID)

		// all nodes are healed
		checkVerifiedLayer(t, alg1.trtl, layerID.Sub(1))
		checkVerifiedLayer(t, alg2.trtl, layerID.Sub(1))
		checkVerifiedLayer(t, alg3.trtl, layerID.Sub(1))
	})
}

func TestComputeExpectedWeight(t *testing.T) {
	genesis := types.GetEffectiveGenesis()
	layers := types.GetLayersPerEpoch()
	require.EqualValues(t, 4, layers, "expecting layers per epoch to be 4. adjust test if it will change")
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
				ctrl         = gomock.NewController(t)
				atxdb        = mocks.NewMockatxDataProvider(ctrl)
				epochWeights = map[types.EpochID]weight{}
				first        = tc.target.Add(1).GetEpoch()
			)
			atxdb.EXPECT().GetEpochWeight(gomock.Any()).DoAndReturn(func(eid types.EpochID) (uint64, []types.ATXID, error) {
				pos := eid - first
				if len(tc.totals) <= int(pos) {
					return 0, nil, fmt.Errorf("invalid test: have only %d weights, want position %d", len(tc.totals), pos)
				}
				return tc.totals[pos], nil, nil
			}).AnyTimes()
			for lid := tc.target.Add(1); !lid.After(tc.last); lid = lid.Add(1) {
				_, err := computeEpochWeight(atxdb, epochWeights, lid.GetEpoch())
				require.NoError(t, err)
			}

			weight := computeExpectedWeight(epochWeights, tc.target, tc.last)
			require.Equal(t, tc.expect.String(), weight.String())
		})
	}
}

func TestOutOfOrderLayersAreVerified(t *testing.T) {
	// increase layer size reduce test flakiness
	s := sim.New(sim.WithLayerSize(defaultTestLayerSize * 10))
	s.Setup()

	ctx := context.Background()
	cfg := defaultTestConfig()
	tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg))

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
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, lid)
	}
	require.Equal(t, last.Sub(1), verified)
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

	var layers []types.LayerID
	for i := 0; i < 200; i++ {
		layers = append(layers, s.Next(opts...))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg))
		for _, lid := range layers {
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
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(b, last.Sub(1), verified)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tortoise.BaseBallot(ctx)
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

func randomBallot(tb testing.TB, lyrID types.LayerID, refBallotID types.BallotID) *types.Ballot {
	tb.Helper()
	ballot := types.RandomBallot()
	ballot.LayerIndex = lyrID
	ballot.RefBallot = refBallotID
	ballot.Initialize()
	return ballot
}

func randomRefBallot(tb testing.TB, lyrID types.LayerID, beacon types.Beacon) *types.Ballot {
	tb.Helper()
	ballot := types.RandomBallot()
	ballot.LayerIndex = lyrID
	ballot.EpochData = &types.EpochData{
		Beacon: beacon,
	}
	ballot.Initialize()
	return ballot
}

func TestBallotHasGoodBeacon(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	layerID := types.GetEffectiveGenesis().Add(1)
	epochBeacon := types.RandomBeacon()
	ballot := randomRefBallot(t, layerID, epochBeacon)

	mockBeacons := smocks.NewMockBeaconGetter(ctrl)
	trtl := defaultTurtle(t)
	trtl.beacons = mockBeacons

	logger := logtest.New(t)
	// good beacon
	mockBeacons.EXPECT().GetBeacon(layerID.GetEpoch()).Return(epochBeacon, nil).Times(1)
	assert.True(t, trtl.markBeaconWithBadBallot(logger, ballot))

	// bad beacon
	beacon := types.RandomBeacon()
	require.NotEqual(t, epochBeacon, beacon)
	mockBeacons.EXPECT().GetBeacon(layerID.GetEpoch()).Return(beacon, nil).Times(1)
	assert.False(t, trtl.markBeaconWithBadBallot(logger, ballot))

	// ask a bad beacon again won't cause a lookup since it's cached
	assert.False(t, trtl.markBeaconWithBadBallot(logger, ballot))
}

func TestGetBallotBeacon(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	layerID := types.GetEffectiveGenesis().Add(1)
	beacon := types.RandomBeacon()
	refBallot := randomRefBallot(t, layerID, beacon)
	refBallotID := refBallot.ID()
	ballot := randomBallot(t, layerID, refBallotID)

	mockBdp := mocks.NewMockblockDataProvider(ctrl)
	trtl := defaultTurtle(t)
	trtl.bdp = mockBdp

	logger := logtest.New(t)
	mockBdp.EXPECT().GetBallot(refBallotID).Return(refBallot, nil).Times(1)
	got, err := trtl.getBallotBeacon(ballot, logger)
	assert.NoError(t, err)
	assert.Equal(t, beacon, got)

	// get the block beacon again and the data is cached
	got, err = trtl.getBallotBeacon(ballot, logger)
	assert.NoError(t, err)
	assert.Equal(t, beacon, got)
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
		ballots := baseLayer.Ballots()
		base := ballots[rng.Intn(len(ballots))]
		return sim.Voting{Base: base.ID(), Support: support}
	}
}

// olderExceptions will vote for block older then base ballot.
func olderExceptions(rng *mrand.Rand, layers []*types.Layer, _ int) sim.Voting {
	if len(layers) < 2 {
		panic("need at least 2 layers")
	}
	baseLayer := layers[len(layers)-1]
	ballots := baseLayer.Ballots()
	base := ballots[rng.Intn(len(ballots))]
	voting := sim.Voting{Base: base.ID()}
	for _, layer := range layers[len(layers)-2:] {
		for _, bid := range layer.BlocksIDs() {
			voting.Support = append(voting.Support, bid)
		}
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
		ballots := layers[li].Ballots()
		base := ballots[rng.Intn(len(ballots))]
		return sim.Voting{Base: base.ID(), Support: layers[li].BlocksIDs()}
	}
}

// tortoiseVoting is for testing that protocol makes progress using heuristic that we are
// using for the network.
func tortoiseVoting(tortoise *Tortoise) sim.VotesGenerator {
	return func(rng *mrand.Rand, layers []*types.Layer, i int) sim.Voting {
		votes, err := tortoise.BaseBallot(context.Background())
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

	votes, err := tortoise.BaseBallot(ctx)
	require.NoError(t, err)
	require.Equal(t, votes.Support, []types.BlockID{types.GenesisBlockID})
	require.Equal(t, types.GenesisBallotID, votes.Base)
}

func ensureBaseAndExceptionsFromLayer(tb testing.TB, lid types.LayerID, votes *types.Votes, mdb blockDataProvider) {
	tb.Helper()

	ballot, err := mdb.GetBallot(votes.Base)
	require.NoError(tb, err)
	require.Equal(tb, lid, ballot.LayerIndex)

	for _, bid := range votes.Support {
		block, err := mdb.GetBlock(bid)
		require.NoError(tb, err)
		require.Equal(tb, lid, block.LayerIndex, "block=%s block layer=%s last=%s", block.ID(), block.LayerIndex, lid)
	}
}

func TestBaseBallotEvictedBlock(t *testing.T) {
	const size = 10
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
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, lid)
	}
	require.Equal(t, last.Sub(1), verified)
	for i := 0; i < 10; i++ {
		votes, err := tortoise.BaseBallot(ctx)
		require.NoError(t, err)
		ensureBaseAndExceptionsFromLayer(t, last, votes, s.GetState(0).MeshDB)

		last = s.Next(sim.WithVoteGenerator(tortoiseVoting(tortoise)))
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
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

			votes, err := tortoise.BaseBallot(ctx)
			require.NoError(t, err)
			ballot, err := s.GetState(0).MeshDB.GetBallot(votes.Base)
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

func ensureBallotLayerWithin(tb testing.TB, bdp blockDataProvider, ballotID types.BallotID, from, to types.LayerID) {
	tb.Helper()

	ballot, err := bdp.GetBallot(ballotID)
	require.NoError(tb, err)
	require.True(tb, !ballot.LayerIndex.Before(from) && !ballot.LayerIndex.After(to),
		"%s not in [%s,%s]", ballot.LayerIndex, from, to,
	)
}

func ensureBlockLayerWithin(tb testing.TB, bdp blockDataProvider, bid types.BlockID, from, to types.LayerID) {
	tb.Helper()

	block, err := bdp.GetBlock(bid)
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
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, lid)
	}
	// 5th layer after genesis verifies 4th
	// and later layers can't verify previous as they are split
	require.Equal(t, genesis.Add(4), verified)
	votes, err := tortoise.BaseBallot(ctx)
	require.NoError(t, err)
	// last ballot that is consistent with local opinion
	ensureBallotLayerWithin(t, s.GetState(0).MeshDB, votes.Base, genesis.Add(5), genesis.Add(5))

	// support for all layers in range of (verified, last - hdist)
	for _, bid := range votes.Support {
		ensureBlockLayerWithin(t, s.GetState(0).MeshDB, bid, genesis.Add(5), last.Sub(hdist).Sub(1))
	}

	for i := 0; i < 10; i++ {
		last = s.Next(sim.WithVoteGenerator(tortoiseVoting(tortoise)))
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
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
		sim.WithSequence(1, sim.WithEmptyHareOutput()),
		sim.WithSequence(2),
	) {
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(1), verified)

	unsupported := map[types.BlockID]struct{}{}
	for lid := genesis; lid.Before(last); lid = lid.Add(1) {
		hareOutput, err := s.GetState(0).MeshDB.GetHareConsensusOutput(lid)
		require.NoError(t, err)
		if hareOutput != types.EmptyBlockID {
			tortoise.trtl.validity[hareOutput] = against
			tortoise.trtl.hareOutput[hareOutput] = against
			unsupported[hareOutput] = struct{}{}
		}
	}

	// remove good ballots and genesis to make tortoise select one of the later blocks.
	delete(tortoise.trtl.ballots, genesis)
	tortoise.trtl.verifying.goodBallots = map[types.BallotID]goodness{}

	votes, err := tortoise.BaseBallot(ctx)
	require.NoError(t, err)
	ensureBallotLayerWithin(t, s.GetState(0).MeshDB, votes.Base, last, last)

	require.Len(t, votes.Against, len(unsupported))
	for _, bid := range votes.Against {
		ensureBlockLayerWithin(t, s.GetState(0).MeshDB, bid, genesis, last.Sub(1))
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

			err := tortoise.trtl.addLocalVotes(ctx, logtest.New(t), tc.lid)
			require.NoError(t, err)

			blocks, err := s.GetState(0).MeshDB.LayerBlockIds(tc.lid)
			require.NoError(t, err)
			for _, bid := range blocks {
				vote, _ := getLocalVote(&tortoise.trtl.commonState, cfg, tc.lid, bid)
				if tc.expected == support {
					hareOutput, err := s.GetState(0).MeshDB.GetHareConsensusOutput(tc.lid)
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
	}
	createActiveSet := func(pos []int, atxs []*types.ActivationTxHeader) []types.ATXID {
		var rst []types.ATXID
		for _, i := range pos {
			rst = append(rst, atxs[i].ID())
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
				{ActiveSet: []int{0, 1, 2}, ATX: 0, ExpectedWeight: big.NewFloat(10)},
				{ActiveSet: []int{0, 1, 2}, ATX: 1, ExpectedWeight: big.NewFloat(10)},
			},
		},
		{
			desc:           "FromRefBallot",
			atxs:           []uint{50, 50, 50},
			layerSize:      5,
			layersPerEpoch: 3,
			ballots: []testBallot{
				{ActiveSet: []int{0, 1, 2}, ATX: 0, ExpectedWeight: big.NewFloat(10)},
				{RefBallot: 0, ATX: 0, ExpectedWeight: big.NewFloat(10)},
			},
		},
		{
			desc:           "DifferentActiveSets",
			atxs:           []uint{50, 50, 100, 100},
			layerSize:      5,
			layersPerEpoch: 2,
			ballots: []testBallot{
				{ActiveSet: []int{0, 1}, ATX: 0, ExpectedWeight: big.NewFloat(10)},
				{ActiveSet: []int{2, 3}, ATX: 2, ExpectedWeight: big.NewFloat(20)},
			},
		},
		{
			desc:           "AtxNotInActiveSet",
			atxs:           []uint{50, 50, 50},
			layerSize:      5,
			layersPerEpoch: 2,
			ballots: []testBallot{
				{ActiveSet: []int{0, 2}, ATX: 1, ExpectedWeight: big.NewFloat(0)},
			},
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			var (
				ballots []*types.Ballot
				atxs    []*types.ActivationTxHeader

				weights = map[types.BallotID]weight{}

				ctrl  = gomock.NewController(t)
				mdb   = mocks.NewMockblockDataProvider(ctrl)
				atxdb = mocks.NewMockatxDataProvider(ctrl)
			)

			for i, weight := range tc.atxs {
				header := makeAtxHeaderWithWeight(weight)
				atxid := types.ATXID{byte(i)}
				header.SetID(&atxid)
				atxdb.EXPECT().GetAtxHeader(atxid).Return(header, nil).AnyTimes()
				atxs = append(atxs, header)
			}

			for i, b := range tc.ballots {
				ballot := &types.Ballot{
					InnerBallot: types.InnerBallot{
						EligibilityProof: types.VotingEligibilityProof{J: uint32(i)},
						AtxID:            atxs[b.ATX].ID(),
					},
				}
				if b.ActiveSet != nil {
					ballot.EpochData = &types.EpochData{
						ActiveSet: createActiveSet(b.ActiveSet, atxs),
					}
				} else {
					ballot.RefBallot = ballots[b.RefBallot].ID()
				}

				ballot.Initialize()
				ballots = append(ballots, ballot)

				weight, err := computeBallotWeight(atxdb, mdb, weights, ballot, tc.layerSize, tc.layersPerEpoch)
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
	)
	s1.Setup(
		sim.WithSetupMinerRange(15, 15),
		sim.WithSetupUnitsRange(1, 1),
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
		_, verified1, _ = tortoise1.HandleIncomingLayer(ctx, last)
		_, verified2, _ = tortoise2.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(1), verified1)
	require.Equal(t, last.Sub(1), verified2)

	gens := s1.Split()
	require.Len(t, gens, 2)
	s1, s2 := gens[0], gens[1]

	partitionStart := last
	for i := 0; i < int(types.GetLayersPerEpoch()); i++ {
		last = s1.Next()
		_, verified1, _ = tortoise1.HandleIncomingLayer(ctx, last)
		_, verified2, _ = tortoise2.HandleIncomingLayer(ctx, s2.Next())
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
		_, verified1, _ = tortoise1.HandleIncomingLayer(ctx, last)
		_, verified2, _ = tortoise2.HandleIncomingLayer(ctx, last)
	}

	require.Equal(t, last.Sub(1), verified1)
	require.Equal(t, last.Sub(1), verified2)

	// each partition has one valid block
	for lid := partitionStart.Add(1); !lid.After(partitionEnd); lid = lid.Add(1) {
		validBlocks, err := s1.GetState(0).MeshDB.LayerContextuallyValidBlocks(context.TODO(), lid)
		require.NoError(t, err)
		assert.Len(t, validBlocks, numValidBlock*2)
		assert.NotContains(t, validBlocks, types.EmptyBlockID)
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
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(2), verified)
}

func TestSwitchVerifyingByUsingFullOutput(t *testing.T) {
	ctx := context.Background()
	const size = 10
	s := sim.New(sim.WithLayerSize(size))
	s.Setup()

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Hdist = 2
	cfg.Zdist = 2

	tortoise := tortoiseFromSimState(s.GetState(0), WithLogger(logtest.New(t)), WithConfig(cfg))
	var last, verified types.LayerID

	for i := 0; i < 2; i++ {
		last = s.Next()
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
	}
	for i := 0; i < int(cfg.Hdist)+1; i++ {
		last = s.Next(
			sim.WithEmptyHareOutput(),
		)
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
	}
	last = s.Next(sim.WithVoteGenerator(tortoiseVoting(tortoise)))
	_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
	require.True(t, tortoise.trtl.mode.isFull(), "full mode")
	for i := 0; i < 10; i++ {
		last = s.Next(sim.WithVoteGenerator(tortoiseVoting(tortoise)))
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(1), verified)
	require.True(t, tortoise.trtl.mode.isVerifying(), "verifying mode")
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
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(1), verified)
	require.True(t, tortoise.trtl.mode.isVerifying(), "verifying mode")
}

func perfectVotingFirstBaseBallot(rng *mrand.Rand, layers []*types.Layer, _ int) sim.Voting {
	baseLayer := layers[len(layers)-1]
	support := layers[len(layers)-1].BlocksIDs()[0:1]
	against := layers[len(layers)-1].BlocksIDs()[1:]
	ballots := baseLayer.Ballots()
	base := ballots[0]
	return sim.Voting{Base: base.ID(), Against: against, Support: support}
}

func abstainVoting(rng *mrand.Rand, layers []*types.Layer, _ int) sim.Voting {
	baseLayer := layers[len(layers)-1]
	ballots := baseLayer.Ballots()
	return sim.Voting{Base: ballots[0].ID(), Abstain: []types.LayerID{baseLayer.Index()}}
}

func TestAbstainVotingVerifyingMode(t *testing.T) {
	ctx := context.Background()
	const size = 10
	s := sim.New(sim.WithLayerSize(size))
	s.Setup(sim.WithSetupUnitsRange(2, 2))

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
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(2), verified)
	for _, last = range sim.GenLayers(s,
		sim.WithSequence(1, sim.WithVoteGenerator(perfectVotingFirstBaseBallot)),
	) {
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(1), verified)
}

func TestSupportingUnknownBlock(t *testing.T) {
	ctx := context.Background()
	const size = 10
	s := sim.New(sim.WithLayerSize(size))
	s.Setup(sim.WithSetupUnitsRange(2, 2))
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

	t.Run("VerifyingState", func(t *testing.T) {
		s := sim.New(
			sim.WithLayerSize(size),
		)
		s.Setup()

		tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))

		var last, verified types.LayerID
		for _, last = range sim.GenLayers(s,
			sim.WithSequence(20),
		) {
			_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
		}
		require.Equal(t, last.Sub(1), verified)

		evicted := tortoise.trtl.evicted
		require.Equal(t, verified.Sub(window).Sub(1), evicted)
		for lid := types.GetEffectiveGenesis(); !lid.After(evicted); lid = lid.Add(1) {
			require.Empty(t, tortoise.trtl.blocks[lid])
			require.Empty(t, tortoise.trtl.ballots[lid])
		}

		for lid := evicted.Add(1); !lid.After(last); lid = lid.Add(1) {
			for _, bid := range tortoise.trtl.blocks[lid] {
				require.Contains(t, tortoise.trtl.blockLayer, bid, "layer %s", lid)
			}
			for _, ballot := range tortoise.trtl.ballots[lid] {
				require.Contains(t, tortoise.trtl.ballotWeight, ballot, "layer %s", lid)
				require.Contains(t, tortoise.trtl.ballotLayer, ballot, "layer %s", lid)
			}
		}
	})
	t.Run("FullState", func(t *testing.T) {
		s := sim.New(
			sim.WithLayerSize(size),
		)
		s.Setup()

		tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))

		var last, verified types.LayerID
		for _, last = range sim.GenLayers(s,
			sim.WithSequence(20, sim.WithEmptyHareOutput()),
		) {
			_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
		}
		require.Equal(t, last.Sub(1), verified)

		evicted := tortoise.trtl.evicted
		require.Equal(t, verified.Sub(window).Sub(1), evicted)
		for lid := types.GetEffectiveGenesis(); !lid.After(evicted); lid = lid.Add(1) {
			require.Empty(t, tortoise.trtl.blocks[lid])
			require.Empty(t, tortoise.trtl.ballots[lid])
		}

		for lid := evicted.Add(1); !lid.After(tortoise.trtl.verified); lid = lid.Add(1) {
			for _, bid := range tortoise.trtl.blocks[lid] {
				require.Contains(t, tortoise.trtl.blockLayer, bid, "layer %s", lid)
			}
			for _, ballot := range tortoise.trtl.ballots[lid] {
				require.Contains(t, tortoise.trtl.full.votes, ballot, "layer %s", lid)
				require.Contains(t, tortoise.trtl.ballotWeight, ballot, "layer %s", lid)
				require.Contains(t, tortoise.trtl.ballotLayer, ballot, "layer %s", lid)
			}
		}
	})
}
