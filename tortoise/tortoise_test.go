package tortoise

import (
	"context"
	"errors"
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

type blockDataWriter interface {
	blockDataProvider
	AddBlock(*types.Block) error
	SaveLayerInputVectorByID(context.Context, types.LayerID, []types.BlockID) error
	SetInputVectorBackupFunc(mesh.InputVectorBackupFunc)
	GetInputVectorBackupFunc() mesh.InputVectorBackupFunc
}

type meshWrapper struct {
	blockDataWriter
	inputVectorBackupFn      func(types.LayerID) ([]types.BlockID, error)
	saveContextualValidityFn func(types.BlockID, types.LayerID, bool) error
}

func (mw meshWrapper) SaveContextualValidity(bid types.BlockID, lid types.LayerID, valid bool) error {
	if mw.saveContextualValidityFn != nil {
		if err := mw.saveContextualValidityFn(bid, lid, valid); err != nil {
			return fmt.Errorf("save contextual validity fn: %w", err)
		}

		return nil
	}

	if err := mw.blockDataWriter.SaveContextualValidity(bid, lid, valid); err != nil {
		return fmt.Errorf("get layer input vector by ID: %w", err)
	}

	return nil
}

func (mw meshWrapper) GetLayerInputVectorByID(lid types.LayerID) ([]types.BlockID, error) {
	if mw.inputVectorBackupFn != nil {
		blocks, err := mw.inputVectorBackupFn(lid)
		if err != nil {
			return blocks, fmt.Errorf("input vector backup fn: %w", err)
		}

		return blocks, nil
	}

	blocks, err := mw.blockDataWriter.GetLayerInputVectorByID(lid)
	if err != nil {
		return blocks, fmt.Errorf("get layer input vector by ID: %w", err)
	}

	return blocks, nil
}

func (mw meshWrapper) GetCoinflip(context.Context, types.LayerID) (bool, bool) {
	return true, true
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
	for _, bl := range layer.Blocks() {
		if err := m.AddBlock(bl); err != nil {
			return fmt.Errorf("add block: %w", err)
		}
	}
	return nil
}

func randomBallotID() types.BallotID {
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, 8)
	_, err := rand.Read(b)
	// Note that err == nil only if we read len(b) bytes.
	if err != nil {
		return types.EmptyBallotID
	}
	return types.BallotID(types.CalcHash32(b).ToHash20())
}

const (
	defaultTestLayerSize  = 3
	defaultTestWindowSize = 30
	defaultVoteDelays     = 6
)

var (
	defaultTestHdist           = DefaultConfig().Hdist
	defaultTestZdist           = DefaultConfig().Zdist
	defaultTestGlobalThreshold = big.NewRat(6, 10)
	defaultTestLocalThreshold  = big.NewRat(2, 10)
	defaultTestRerunInterval   = time.Hour
	defaultTestConfidenceParam = DefaultConfig().ConfidenceParam
)

func makeAndProcessLayer(t *testing.T, l types.LayerID, trtl *turtle, natxs, blocksPerLayer int, atxdb atxDataWriter, msh blockDataWriter, inputVectorFn func(id types.LayerID) ([]types.BlockID, error)) {
	lyr := makeLayer(t, l, trtl, natxs, blocksPerLayer, atxdb, msh, inputVectorFn)
	logger := logtest.New(t)

	// write blocks to database first; the verifying tortoise will subsequently read them
	if inputVectorFn == nil {
		// just save the layer contents as the input layer vector (the default behavior)
		require.NoError(t, msh.SaveLayerInputVectorByID(context.TODO(), lyr.Index(), lyr.BlocksIDs()))
	} else if blocks, err := inputVectorFn(l); err != nil {
		logger.With().Warning("error from input vector fn", log.Err(err))
	} else {
		// save blocks to db for this layer
		require.NoError(t, msh.SaveLayerInputVectorByID(context.TODO(), l, blocks))
	}

	require.NoError(t, trtl.HandleIncomingLayer(context.TODO(), l))
}

func makeLayer(t *testing.T, layerID types.LayerID, trtl *turtle, natxs, blocksPerLayer int, atxdb atxDataWriter, msh blockDataWriter, inputVectorFn func(id types.LayerID) ([]types.BlockID, error)) *types.Layer {
	return makeLayerWithBeacon(t, layerID, trtl, types.EmptyBeacon, natxs, blocksPerLayer, atxdb, msh, inputVectorFn)
}

func makeLayerWithBeacon(t *testing.T, layerID types.LayerID, trtl *turtle, beacon types.Beacon, natxs, blocksPerLayer int, atxdb atxDataWriter, msh blockDataWriter, inputVectorFn func(id types.LayerID) ([]types.BlockID, error)) *types.Layer {
	if inputVectorFn != nil {
		oldInputVectorFn := msh.GetInputVectorBackupFunc()
		defer func() {
			msh.SetInputVectorBackupFunc(oldInputVectorFn)
		}()
		msh.SetInputVectorBackupFunc(inputVectorFn)
	}
	baseBallotID, lists, err := trtl.BaseBallot(context.TODO())
	require.NoError(t, err)
	lyr := types.NewLayer(layerID)

	atxs := []types.ATXID{}
	for i := 0; i < natxs; i++ {
		atxHeader := makeAtxHeaderWithWeight(1)
		atx := &types.ActivationTx{InnerActivationTx: &types.InnerActivationTx{ActivationTxHeader: atxHeader}}
		atx.PubLayerID = layerID
		atx.NodeID.Key = fmt.Sprintf("%d", i)
		atx.CalcAndSetID()
		require.NoError(t, atxdb.StoreAtx(layerID.GetEpoch(), atx))
		atxs = append(atxs, atx.ID())
	}

	for i := 0; i < blocksPerLayer; i++ {
		ballot := types.Ballot{
			InnerBallot: types.InnerBallot{
				AtxID:            atxs[i%len(atxs)],
				BaseBallot:       baseBallotID,
				EligibilityProof: types.VotingEligibilityProof{J: uint32(i)},
				AgainstDiff:      lists[0],
				ForDiff:          lists[1],
				NeutralDiff:      lists[2],
				LayerIndex:       layerID,
				EpochData: &types.EpochData{
					ActiveSet: atxs,
					Beacon:    beacon,
				},
			},
		}
		p := &types.Proposal{
			InnerProposal: types.InnerProposal{
				Ballot: ballot,
				TxIDs:  nil,
			},
		}
		blk := p.ToBlock()
		blk.Signature = signing.NewEdSigner().Sign(blk.Bytes())
		blk.Initialize()
		lyr.AddBlock(blk)
		require.NoError(t, msh.AddBlock(blk))
	}

	return lyr
}

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
			sim.WithSequence(2, sim.WithoutInputVector()),
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
			sim.WithSequence(1, sim.WithoutInputVector()),
			sim.WithSequence(2),
			sim.WithSequence(2, sim.WithoutInputVector()),
			sim.WithSequence(30),
		) {
			last = lid
			_, verified, _ = tortoise.HandleIncomingLayer(ctx, lid)
		}
		require.Equal(t, last.Sub(1), verified)
	})
}

func TestAbstainsInMiddle(t *testing.T) {
	logger := logtest.New(t)
	layers := types.NewLayerID(15)
	initialNumGood := 5
	blocksPerLayer := 10

	msh := getInMemMesh(t)
	layerfuncs := make([]func(types.LayerID) ([]types.BlockID, error), 0, layers.Uint32())

	// first 5 layers incl genesis just work
	for i := 0; i <= initialNumGood; i++ {
		layerfuncs = append(layerfuncs, func(id types.LayerID) (ids []types.BlockID, err error) {
			logger.Debug("giving good results for layer", id)
			ids, err = msh.LayerBlockIds(id)
			if err != nil {
				return ids, fmt.Errorf("layer block IDs: %w", err)
			}

			return ids, nil
		})
	}

	// next up two layers that didn't finish
	newlastlyr := types.NewLayerID(uint32(len(layerfuncs)))
	for i := newlastlyr; i.Before(newlastlyr.Add(2)); i = i.Add(1) {
		layerfuncs = append(layerfuncs, func(id types.LayerID) (ids []types.BlockID, err error) {
			logger.Debug("giving bad result for layer", id)
			return nil, errors.New("simulated hare failure")
		})
	}

	// more good layers
	newlastlyr = types.NewLayerID(uint32(len(layerfuncs)))
	for i := newlastlyr; i.Before(newlastlyr.Add(layers.Difference(newlastlyr))); i = i.Add(1) {
		layerfuncs = append(layerfuncs, func(id types.LayerID) (ids []types.BlockID, err error) {
			ids, err = msh.LayerBlockIds(id)
			if err != nil {
				return ids, fmt.Errorf("layer block IDs: %w", err)
			}

			return ids, nil
		})
	}

	trtl := defaultTurtle(t)
	trtl.LayerSize = uint32(blocksPerLayer)
	trtl.bdp = msh
	gen := mesh.GenesisLayer()
	trtl.init(context.TODO(), gen)

	var l types.LayerID
	atxdb := getAtxDB()
	trtl.atxdb = atxdb
	for l = types.GetEffectiveGenesis().Add(1); l.Before(types.GetEffectiveGenesis().Add(layers.Uint32())); l = l.Add(1) {
		makeAndProcessLayer(t, l, trtl, blocksPerLayer, blocksPerLayer, atxdb, msh, layerfuncs[l.Difference(types.GetEffectiveGenesis())-1])
	}

	// verification will get stuck as of the first layer with conflicting local and global opinions.
	// block votes aren't counted because blocks aren't marked good, because they contain exceptions older
	// than their base block.
	// self-healing will not run because the layers aren't old enough.
	require.Equal(t, int(types.GetEffectiveGenesis().Add(uint32(initialNumGood)).Uint32()), int(trtl.verified.Uint32()),
		"verification should advance after hare finishes")
	// todo: also check votes with requireVote
}

type (
	baseBallotProvider  func(context.Context) (types.BallotID, [][]types.BlockID, error)
	inputVectorProvider func(types.LayerID) ([]types.BlockID, error)
)

func generateBlocks(t *testing.T, l types.LayerID, natxs, nblocks int, bbp baseBallotProvider, atxdb atxDataWriter, weight uint) (blocks []*types.Block) {
	b, lists, err := bbp(context.TODO())
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
	for i := 0; i < nblocks; i++ {
		ballot := types.Ballot{
			InnerBallot: types.InnerBallot{
				AtxID:            atxs[i%len(atxs)],
				BaseBallot:       b,
				EligibilityProof: types.VotingEligibilityProof{J: uint32(i)},
				AgainstDiff:      lists[0],
				ForDiff:          lists[1],
				NeutralDiff:      lists[2],
				LayerIndex:       l,
				EpochData: &types.EpochData{
					ActiveSet: atxs,
				},
			},
		}
		p := &types.Proposal{
			InnerProposal: types.InnerProposal{
				Ballot: ballot,
				TxIDs:  nil,
			},
		}
		blk := p.ToBlock()
		blk.Signature = signing.NewEdSigner().Sign(b.Bytes())
		blk.Initialize()
		blocks = append(blocks, blk)
	}
	return
}

func createTurtleLayer(t *testing.T, l types.LayerID, msh *mesh.DB, atxdb atxDataWriter, bbp baseBallotProvider, ivp inputVectorProvider, blocksPerLayer int) *types.Layer {
	oldInputVectorFn := msh.InputVectorBackupFunc
	defer func() {
		msh.InputVectorBackupFunc = oldInputVectorFn
	}()
	msh.InputVectorBackupFunc = ivp
	blocks, err := ivp(l.Sub(1))
	if err != nil {
		blocks = nil
	}
	if err := msh.SaveLayerInputVectorByID(context.TODO(), l.Sub(1), blocks); err != nil {
		panic("database error")
	}
	lyr := types.NewLayer(l)
	for _, block := range generateBlocks(t, l, blocksPerLayer, blocksPerLayer, bbp, atxdb, 1) {
		lyr.AddBlock(block)
	}

	return lyr
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

	getHareResults := mdb.LayerBlockIds

	mdb.InputVectorBackupFunc = getHareResults
	atxdb := getAtxDB()
	alg := defaultAlgorithm(t, mdb)
	alg.trtl.atxdb = atxdb
	l := mesh.GenesisLayer()

	logger.With().Info("genesis is", l.Index(), types.BlockIdsField(types.BlockIDs(l.Blocks())))
	logger.With().Info("genesis is", l.Blocks()[0].Fields()...)

	l1 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(1), mdb, atxdb, alg.BaseBallot, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l1))

	logger.With().Info("first is", l1.Index(), types.BlockIdsField(types.BlockIDs(l1.Blocks())))
	logger.With().Info("first bb is", l1.Index(), l1.Blocks()[0].BaseBlock, types.BlockIdsField(l1.Blocks()[0].ForDiff))

	alg.HandleIncomingLayer(context.TODO(), l1.Index())

	l2 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(2), mdb, atxdb, alg.BaseBallot, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l2))
	alg.HandleIncomingLayer(context.TODO(), l2.Index())

	require.Equal(t, int(types.GetEffectiveGenesis().Add(1).Uint32()), int(alg.LatestComplete().Uint32()))

	l3a := createTurtleLayer(t, types.GetEffectiveGenesis().Add(3), mdb, atxdb, alg.BaseBallot, getHareResults, defaultTestLayerSize+1)

	// this should fail as the blocks for this layer have not been added to the mesh yet
	alg.HandleIncomingLayer(context.TODO(), l3a.Index())
	require.Equal(t, int(types.GetEffectiveGenesis().Add(1).Uint32()), int(alg.LatestComplete().Uint32()))

	l3 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(3), mdb, atxdb, alg.BaseBallot, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l3))
	require.NoError(t, alg.rerun(context.TODO()))

	l4 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(4), mdb, atxdb, alg.BaseBallot, getHareResults, defaultTestLayerSize)
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

	getHareResults := mdb.LayerBlockIds
	mdb.InputVectorBackupFunc = getHareResults

	l0 := mesh.GenesisLayer()
	expectBaseBallotLayer := func(layerID types.LayerID, numAgainst, numSupport, numNeutral int) {
		baseBallotID, exceptions, err := alg.BaseBallot(context.TODO())
		r.NoError(err)
		// expect no exceptions
		r.Len(exceptions, 3, "expected three vote exception arrays")
		r.Len(exceptions[0], numAgainst, "wrong number of against exception votes")
		r.Len(exceptions[1], numSupport, "wrong number of support exception votes")
		r.Len(exceptions[2], numNeutral, "wrong number of neutral exception votes")
		// expect a valid genesis base ballot
		baseBallot, err := alg.trtl.bdp.GetBallot(baseBallotID)
		r.NoError(err)
		r.Equal(layerID, baseBallot.LayerIndex, "base ballot is from wrong layer")
	}
	// it should support all genesis blocks
	expectBaseBallotLayer(l0.Index(), 0, len(mesh.GenesisLayer().Blocks()), 0)

	// add a couple of incoming layers and make sure the base ballot layer advances as well
	l1 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(1), mdb, atxdb, alg.BaseBallot, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l1))
	alg.HandleIncomingLayer(context.TODO(), l1.Index())
	expectBaseBallotLayer(l1.Index(), 0, defaultTestLayerSize, 0)

	l2 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(2), mdb, atxdb, alg.BaseBallot, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l2))
	alg.HandleIncomingLayer(context.TODO(), l2.Index())
	require.Equal(t, int(types.GetEffectiveGenesis().Add(1).Uint32()), int(alg.LatestComplete().Uint32()))
	expectBaseBallotLayer(l2.Index(), 0, defaultTestLayerSize, 0)

	// add a layer that's not in the mesh and make sure it does not advance
	l3 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(3), mdb, atxdb, alg.BaseBallot, getHareResults, defaultTestLayerSize)
	alg.HandleIncomingLayer(context.TODO(), l3.Index())
	require.Equal(t, int(types.GetEffectiveGenesis().Add(1).Uint32()), int(alg.LatestComplete().Uint32()))
	expectBaseBallotLayer(l2.Index(), 0, defaultTestLayerSize, 0)
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
	r.Equal(trtl.ConfidenceParam, trtl2.ConfidenceParam)
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
		LayerSize:                defaultTestLayerSize,
		Hdist:                    defaultTestHdist,
		Zdist:                    defaultTestZdist,
		ConfidenceParam:          defaultTestConfidenceParam,
		WindowSize:               defaultTestWindowSize,
		BadBeaconVoteDelayLayers: defaultVoteDelays,
		GlobalThreshold:          defaultTestGlobalThreshold,
		LocalThreshold:           defaultTestLocalThreshold,
		RerunInterval:            defaultTestRerunInterval,
		MaxExceptions:            int(defaultTestHdist) * defaultTestLayerSize * 100,
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

func TestSumVotes(t *testing.T) {
	type testBallot struct {
		Base                      [2]int   // [layer, ballot] tuple
		Support, Against, Abstain [][2]int // list of [layer, block] tuples
		ATX                       int
	}
	type testBlock struct{}

	rng := mrand.New(mrand.NewSource(0))
	signer := signing.NewEdSignerFromRand(rng)

	getDiff := func(layers [][]*types.Block, choices [][2]int) []types.BlockID {
		var rst []types.BlockID
		for _, choice := range choices {
			rst = append(rst, layers[choice[0]][choice[1]].ID())
		}
		return rst
	}

	ctx := context.TODO()
	genesis := types.GetEffectiveGenesis()

	for _, tc := range []struct {
		desc         string
		activeset    []uint         // list of weights in activeset
		layerBallots [][]testBallot // list of layers with ballots
		layerBlocks  [][]testBlock
		target       [2]int // [layer, block] tuple
		expect       *big.Float
		filter       func(types.BallotID) bool
	}{
		{
			desc:      "TwoLayersSupport",
			activeset: []uint{10, 10, 10},
			layerBlocks: [][]testBlock{
				{{}, {}, {}},
				{{}, {}, {}},
			},
			layerBallots: [][]testBallot{
				{{ATX: 0}, {ATX: 1}, {ATX: 2}},
				{
					{ATX: 0, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 1, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 2, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
				},
				{
					{ATX: 0, Base: [2]int{1, 1}, Support: [][2]int{{1, 1}, {1, 0}, {1, 2}}},
					{ATX: 1, Base: [2]int{1, 1}, Support: [][2]int{{1, 1}, {1, 0}, {1, 2}}},
					{ATX: 2, Base: [2]int{1, 1}, Support: [][2]int{{1, 1}, {1, 0}, {1, 2}}},
				},
			},
			target: [2]int{0, 0},
			expect: big.NewFloat(15),
			filter: func(types.BallotID) bool { return true },
		},
		{
			desc:      "ConflictWithBase",
			activeset: []uint{10, 10, 10},
			layerBlocks: [][]testBlock{
				{{}, {}, {}},
				{{}, {}, {}},
			},
			layerBallots: [][]testBallot{
				{{ATX: 0}, {ATX: 1}, {ATX: 2}},
				{
					{ATX: 0, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 1, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 2, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
				},
				{
					{
						ATX: 0, Base: [2]int{1, 1},
						Support: [][2]int{{1, 1}, {1, 0}, {1, 2}},
						Against: [][2]int{{0, 1}, {0, 0}, {0, 2}},
					},
					{
						ATX: 1, Base: [2]int{1, 1},
						Support: [][2]int{{1, 1}, {1, 0}, {1, 2}},
						Against: [][2]int{{0, 1}, {0, 0}, {0, 2}},
					},
					{
						ATX: 2, Base: [2]int{1, 1},
						Support: [][2]int{{1, 1}, {1, 0}, {1, 2}},
						Against: [][2]int{{0, 1}, {0, 0}, {0, 2}},
					},
				},
			},
			target: [2]int{0, 0},
			expect: big.NewFloat(0),
			filter: func(types.BallotID) bool { return true },
		},
		{
			desc:      "UnequalWeights",
			activeset: []uint{80, 40, 20},
			layerBlocks: [][]testBlock{
				{{}, {}, {}},
				{{}, {}, {}},
				{{}, {}, {}},
			},
			layerBallots: [][]testBallot{
				{{ATX: 0}, {ATX: 1}, {ATX: 2}},
				{
					{ATX: 0, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 1, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 2, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
				},
				{
					{ATX: 0, Base: [2]int{1, 1}, Support: [][2]int{{1, 1}, {1, 0}, {1, 2}}},
					{ATX: 0, Base: [2]int{1, 1}, Support: [][2]int{{1, 1}, {1, 0}, {1, 2}}},
					{ATX: 1, Base: [2]int{1, 1}, Support: [][2]int{{1, 1}, {1, 0}, {1, 2}}},
				},
				{
					{ATX: 0, Base: [2]int{2, 1}, Support: [][2]int{{2, 1}, {2, 0}, {2, 2}}},
					{ATX: 0, Base: [2]int{2, 1}, Support: [][2]int{{2, 1}, {2, 0}, {2, 2}}},
					{ATX: 0, Base: [2]int{2, 1}, Support: [][2]int{{2, 1}, {2, 0}, {2, 2}}},
					{ATX: 1, Base: [2]int{2, 1}, Support: [][2]int{{2, 1}, {2, 0}, {2, 2}}},
				},
			},
			target: [2]int{0, 0},
			expect: big.NewFloat(140),
			filter: func(types.BallotID) bool { return true },
		},
		{
			desc:      "UnequalWeightsVoteFromAtxMissing",
			activeset: []uint{80, 40, 20},
			layerBlocks: [][]testBlock{
				{{}, {}, {}},
				{{}, {}, {}},
				{{}, {}, {}},
			},
			layerBallots: [][]testBallot{
				{{ATX: 0}, {ATX: 1}, {ATX: 2}},
				{
					{ATX: 0, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 2, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
				},
				{
					{ATX: 0, Base: [2]int{1, 1}, Support: [][2]int{{1, 1}, {1, 0}}},
					{ATX: 0, Base: [2]int{1, 1}, Support: [][2]int{{1, 1}, {1, 0}}},
				},
				{
					{ATX: 0, Base: [2]int{2, 1}, Support: [][2]int{{2, 1}, {2, 0}}},
					{ATX: 0, Base: [2]int{2, 1}, Support: [][2]int{{2, 1}, {2, 0}}},
					{ATX: 0, Base: [2]int{2, 1}, Support: [][2]int{{2, 1}, {2, 0}}},
				},
			},
			target: [2]int{0, 0},
			expect: big.NewFloat(100),
			filter: func(types.BallotID) bool { return true },
		},
		{
			desc:      "OneLayerSupport",
			activeset: []uint{10, 10, 10},
			layerBlocks: [][]testBlock{
				{{}, {}, {}},
			}, layerBallots: [][]testBallot{
				{{ATX: 0}, {ATX: 1}, {ATX: 2}},
				{
					{ATX: 0, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 1, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 2, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
				},
			},
			target: [2]int{0, 0},
			expect: big.NewFloat(7.5),
			filter: func(types.BallotID) bool { return true },
		},
		{
			desc:      "OneBlockAbstain",
			activeset: []uint{10, 10, 10},
			layerBlocks: [][]testBlock{
				{{}, {}, {}},
			},
			layerBallots: [][]testBallot{
				{{ATX: 0}, {ATX: 1}, {ATX: 2}},
				{
					{ATX: 0, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 1, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 2, Base: [2]int{0, 1}, Abstain: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
				},
			},
			target: [2]int{0, 0},
			expect: big.NewFloat(5),
			filter: func(types.BallotID) bool { return true },
		},
		{
			desc:      "OneBlockAagaisnt",
			activeset: []uint{10, 10, 10},
			layerBlocks: [][]testBlock{
				{{}, {}, {}},
			},
			layerBallots: [][]testBallot{
				{{ATX: 0}, {ATX: 1}, {ATX: 2}},
				{
					{ATX: 0, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 1, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 2, Base: [2]int{0, 1}, Against: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
				},
			},
			target: [2]int{0, 0},
			expect: big.NewFloat(2.5),
			filter: func(types.BallotID) bool { return true },
		},
		{
			desc:      "MajorityAgainst",
			activeset: []uint{10, 10, 10},
			layerBlocks: [][]testBlock{
				{{}, {}, {}},
			},
			layerBallots: [][]testBallot{
				{{ATX: 0}, {ATX: 1}, {ATX: 2}},
				{
					{ATX: 0, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 1, Base: [2]int{0, 1}, Against: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 2, Base: [2]int{0, 1}, Against: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
				},
			},
			target: [2]int{0, 0},
			expect: big.NewFloat(-2.5),
			filter: func(types.BallotID) bool { return true },
		},
		{
			desc:      "NoVotes",
			activeset: []uint{10, 10, 10},
			layerBlocks: [][]testBlock{
				{{}, {}, {}},
			},
			layerBallots: [][]testBallot{
				{{ATX: 0}, {ATX: 1}, {ATX: 2}},
			},
			target: [2]int{0, 0},
			expect: big.NewFloat(0),
			filter: func(types.BallotID) bool { return true },
		},
		{
			desc:      "IgnoreVotes",
			activeset: []uint{10, 10, 10},
			layerBlocks: [][]testBlock{
				{{}, {}, {}},
			},
			layerBallots: [][]testBallot{
				{{ATX: 0}, {ATX: 1}, {ATX: 2}},
				{
					{ATX: 0, Base: [2]int{0, 1}, Support: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 1, Base: [2]int{0, 1}, Against: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
					{ATX: 2, Base: [2]int{0, 1}, Against: [][2]int{{0, 1}, {0, 0}, {0, 2}}},
				},
			},
			target: [2]int{0, 0},
			expect: big.NewFloat(0),
			filter: func(types.BallotID) bool { return false },
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			atxdb := mocks.NewMockatxDataProvider(ctrl)
			activeset := []types.ATXID{}
			for i, weight := range tc.activeset {
				header := makeAtxHeaderWithWeight(weight)
				atxid := types.ATXID{byte(i)}
				header.SetID(&atxid)
				atxdb.EXPECT().GetAtxHeader(atxid).Return(header, nil).AnyTimes()
				activeset = append(activeset, atxid)
			}

			tortoise := defaultAlgorithm(t, getInMemMesh(t))
			tortoise.trtl.atxdb = atxdb
			consensus := tortoise.trtl

			blocks := [][]*types.Block{}
			for i, layer := range tc.layerBlocks {
				layerBlocks := []*types.Block{}
				lid := genesis.Add(uint32(i) + 1)
				for j := range layer {
					block := &types.Block{}
					block.EligibilityProof = types.BlockEligibilityProof{J: uint32(j)}
					block.LayerIndex = lid
					block.Signature = signer.Sign(block.Bytes())
					block.Initialize()
					layerBlocks = append(layerBlocks, block)
				}

				consensus.processBlocks(lid, layerBlocks)
				blocks = append(blocks, layerBlocks)
			}

			ballots := [][]*types.Ballot{}
			for i, layer := range tc.layerBallots {
				layerBallots := []*types.Ballot{}
				lid := genesis.Add(uint32(i) + 1)
				for j, b := range layer {
					ballot := &types.Ballot{}
					ballot.EligibilityProof = types.VotingEligibilityProof{J: uint32(j)}
					ballot.AtxID = activeset[b.ATX]
					ballot.EpochData = &types.EpochData{ActiveSet: activeset}
					ballot.LayerIndex = lid
					// don't vote on genesis for simplicity,
					// since we don't care about block goodness in this test
					if i > 0 {
						ballot.ForDiff = getDiff(blocks, b.Support)
						ballot.AgainstDiff = getDiff(blocks, b.Against)
						ballot.NeutralDiff = getDiff(blocks, b.Abstain)
						ballot.BaseBallot = ballots[b.Base[0]][b.Base[1]].ID()
					}
					ballot.Signature = signer.Sign(ballot.Bytes())
					ballot.Initialize()
					layerBallots = append(layerBallots, ballot)
				}
				ballots = append(ballots, layerBallots)

				consensus.processed = lid
				consensus.last = lid
				tballots, err := consensus.processBallots(wrapContext(ctx), lid, layerBallots)
				consensus.keepVotes(tballots)
				require.NoError(t, err)
			}
			bid := types.BlockID(blocks[tc.target[0]][tc.target[1]].ID())
			lid := genesis.Add(uint32(tc.target[0] + 2)) // +2 so that we count votes after target
			rst, err := consensus.sumVotesForBlock(ctx, bid, lid, tc.filter)
			require.NoError(t, err)
			require.Equal(t, tc.expect.String(), rst.String())
		})
	}
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
			blocksA := generateBlocks(t, layerID, layerSize, layerSize, alg1.BaseBallot, atxdb1, 1)
			blocksB := generateBlocks(t, layerID, layerSize, layerSize, alg2.BaseBallot, atxdb2, 1)

			// these will produce identical sets of blocks, so throw away half of each
			// (we could probably get away with just using, say, A's blocks, but to be more thorough we also want
			// to test the BaseBallot provider of each tortoise)
			blocksA = blocksA[:layerSize/2]
			blocksB = blocksB[len(blocksA):]
			blocks := append(blocksA, blocksB...)

			// add all blocks to both tortoises
			var blockIDs []types.BlockID
			for _, block := range blocks {
				blockIDs = append(blockIDs, block.ID())
				r.NoError(mdb1.AddBlock(block))
				r.NoError(mdb2.AddBlock(block))
			}
			r.NoError(mdb1.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDs))
			r.NoError(mdb2.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDs))
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

		makeBlocks := func(layerID types.LayerID) (blocksA, blocksB []*types.Block) {
			// simulate producing blocks in parallel
			blocksA = generateBlocks(t, layerID, layerSize, layerSize, alg1.BaseBallot, atxdb1, 1)
			blocksB = generateBlocks(t, layerID, layerSize, layerSize, alg2.BaseBallot, atxdb2, 1)

			// 90/10 split
			blocksA = blocksA[:layerSize-1]
			blocksB = blocksB[layerSize-1:]
			return
		}

		// a bunch of good layers
		lastVerified := types.GetEffectiveGenesis()
		layerID := types.GetEffectiveGenesis()
		for i := 0; i < 10; i++ {
			layerID = layerID.Add(1)
			blocksA, blocksB := makeBlocks(layerID)
			var blocks []*types.Block
			blocks = append(blocksA, blocksB...)

			// add all blocks to both tortoises
			var blockIDs []types.BlockID
			for _, block := range blocks {
				blockIDs = append(blockIDs, block.ID())
				r.NoError(mdb1.AddBlock(block))
				r.NoError(mdb2.AddBlock(block))
			}
			r.NoError(mdb1.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDs))
			r.NoError(mdb2.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDs))
			alg1.HandleIncomingLayer(context.TODO(), layerID)
			alg2.HandleIncomingLayer(context.TODO(), layerID)

			// both should make progress
			checkVerifiedLayer(t, alg1.trtl, layerID.Sub(1))
			checkVerifiedLayer(t, alg2.trtl, layerID.Sub(1))
			lastVerified = layerID.Sub(1)
		}

		// keep track of all blocks on each side of the partition
		var forkBlocksA, forkBlocksB []*types.Block
		beforePartition := layerID
		// simulate a partition
		for i := 0; i < 10; i++ {
			layerID = layerID.Add(1)
			blocksA, blocksB := makeBlocks(layerID)
			forkBlocksA = append(forkBlocksA, blocksA...)
			forkBlocksB = append(forkBlocksB, blocksB...)

			// add A's blocks to A only, B's to B
			var blockIDsA, blockIDsB []types.BlockID
			for _, block := range blocksA {
				blockIDsA = append(blockIDsA, block.ID())
				r.NoError(mdb1.AddBlock(block))
			}
			r.NoError(mdb1.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsA))
			alg1.HandleIncomingLayer(context.TODO(), layerID)
			for _, block := range blocksB {
				blockIDsB = append(blockIDsB, block.ID())
				r.NoError(mdb2.AddBlock(block))
			}
			r.NoError(mdb2.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsB))
			alg2.HandleIncomingLayer(context.TODO(), layerID)

			// majority tortoise is unaffected, minority tortoise gets stuck
			checkVerifiedLayer(t, alg1.trtl, layerID.Sub(1))
			checkVerifiedLayer(t, alg2.trtl, lastVerified)
		}

		// these extra layers account for the time needed to generate enough votes to "catch up" and pass
		// the threshold.
		healingDistance := 12

		// after a while (we simulate the distance here), minority tortoise eventually begins producing more blocks
		for i := 0; i < healingDistance; i++ {
			layerID = layerID.Add(1)

			// these blocks will be nearly identical but they will have different base ballots, since the set of blocks
			// for recent layers has been bifurcated, so we have to generate and store blocks separately to simulate
			// an ongoing partition.
			blocksA := generateBlocks(t, layerID, layerSize, layerSize, alg1.BaseBallot, atxdb1, 1)
			forkBlocksA = append(forkBlocksA, blocksA...)
			var blockIDsA, blockIDsB []types.BlockID
			for _, block := range blocksA {
				blockIDsA = append(blockIDsA, block.ID())
				r.NoError(mdb1.AddBlock(block))
			}
			r.NoError(mdb1.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsA))
			alg1.HandleIncomingLayer(context.TODO(), layerID)

			blocksB := generateBlocks(t, layerID, layerSize, layerSize, alg2.BaseBallot, atxdb2, 1)
			forkBlocksB = append(forkBlocksB, blocksB...)
			for _, block := range blocksB {
				blockIDsB = append(blockIDsB, block.ID())
				r.NoError(mdb2.AddBlock(block))
			}
			r.NoError(mdb2.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsB))
			alg2.HandleIncomingLayer(context.TODO(), layerID)

			// majority tortoise is unaffected, minority tortoise is still stuck
			checkVerifiedLayer(t, alg1.trtl, layerID.Sub(1))
			checkVerifiedLayer(t, alg2.trtl, lastVerified)
		}

		// finally, the minority tortoise heals and regains parity with the majority tortoise
		layerID = layerID.Add(1)
		blocksA := generateBlocks(t, layerID, layerSize, layerSize, alg1.BaseBallot, atxdb1, 1)
		forkBlocksA = append(forkBlocksA, blocksA...)
		var blockIDsA, blockIDsB []types.BlockID
		for _, block := range blocksA {
			blockIDsA = append(blockIDsA, block.ID())
			r.NoError(mdb1.AddBlock(block))
		}
		r.NoError(mdb1.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsA))
		alg1.HandleIncomingLayer(context.TODO(), layerID)

		blocksB := generateBlocks(t, layerID, layerSize, layerSize, alg2.BaseBallot, atxdb2, 1)
		forkBlocksB = append(forkBlocksB, blocksB...)
		for _, block := range blocksB {
			blockIDsB = append(blockIDsB, block.ID())
			r.NoError(mdb2.AddBlock(block))
		}
		r.NoError(mdb2.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsB))
		alg2.HandleIncomingLayer(context.TODO(), layerID)

		// minority node is healed
		lastVerified = layerID.Sub(1).Sub(alg1.cfg.Hdist)
		checkVerifiedLayer(t, alg1.trtl, layerID.Sub(1))
		checkVerifiedLayer(t, alg2.trtl, layerID.Sub(1))

		// now simulate a rejoin
		// send each tortoise's blocks to the other (simulated resync)
		// (of layers 18-40)
		// TODO(nkryuchkov): remove or uncomment when it's used
		// var forkBlockIDsA, forkBlockIDsB []types.BlockID
		for _, block := range forkBlocksA {
			// forkBlockIDsA = append(forkBlockIDsA, block.ID())
			r.NoError(mdb2.AddBlock(block))
		}

		for _, block := range forkBlocksB {
			// forkBlockIDsB = append(forkBlockIDsB, block.ID())
			r.NoError(mdb1.AddBlock(block))
		}

		for lid := beforePartition.Add(1); !lid.After(layerID); lid = lid.Add(1) {
			r.NoError(alg1.trtl.HandleIncomingLayer(context.TODO(), lid))
			r.NoError(alg2.trtl.HandleIncomingLayer(context.TODO(), lid))
		}

		// now continue for a few layers after rejoining, during which the minority tortoise will be stuck
		// because its opinions about which blocks are valid/invalid are wrong and disagree with the majority
		// opinion. these ten layers represent its healing distance. after it heals, it will converge to the
		// majority opinion.
		for i := 0; i < 10; i++ {
			layerID = layerID.Add(1)
			blocksA, blocksB := makeBlocks(layerID)
			var blocks []*types.Block
			blocks = append(blocksA, blocksB...)

			// add all blocks to both tortoises
			var blockIDs []types.BlockID
			for _, block := range blocks {
				blockIDs = append(blockIDs, block.ID())
				r.NoError(mdb1.AddBlock(block))
				r.NoError(mdb2.AddBlock(block))
			}
			r.NoError(mdb1.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDs))
			r.NoError(mdb2.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDs))
			alg1.HandleIncomingLayer(context.TODO(), layerID)
			alg2.HandleIncomingLayer(context.TODO(), layerID)

			// majority tortoise is unaffected, minority tortoise remains stuck
			checkVerifiedLayer(t, alg1.trtl, layerID.Sub(1))
			checkVerifiedLayer(t, alg2.trtl, layerID.Sub(1))
		}

		// minority tortoise begins healing
		for i := 0; i < 21; i++ {
			layerID = layerID.Add(1)
			blocksA, blocksB := makeBlocks(layerID)
			var blocks []*types.Block
			blocks = append(blocksA, blocksB...)

			// add all blocks to both tortoises
			var blockIDs []types.BlockID
			for _, block := range blocks {
				blockIDs = append(blockIDs, block.ID())
				r.NoError(mdb1.AddBlock(block))
				r.NoError(mdb2.AddBlock(block))
			}
			r.NoError(mdb1.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDs))
			r.NoError(mdb2.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDs))
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

		makeBlocks := func(layerID types.LayerID) (blocksA, blocksB []*types.Block) {
			// simulate producing blocks in parallel
			blocksA = generateBlocks(t, layerID, layerSize, layerSize, alg1.BaseBallot, atxdb1, 1)
			blocksB = generateBlocks(t, layerID, layerSize, layerSize, alg2.BaseBallot, atxdb2, 1)

			// 50/50 split
			blocksA = blocksA[:layerSize/2]
			blocksB = blocksB[layerSize/2:]
			return
		}

		// a bunch of good layers
		lastVerified := types.GetEffectiveGenesis()
		layerID := types.GetEffectiveGenesis()
		for i := 0; i < 10; i++ {
			layerID = layerID.Add(1)
			blocksA, blocksB := makeBlocks(layerID)
			var blocks []*types.Block
			blocks = append(blocksA, blocksB...)

			// add all blocks to both tortoises
			var blockIDs []types.BlockID
			for _, block := range blocks {
				blockIDs = append(blockIDs, block.ID())
				r.NoError(mdb1.AddBlock(block))
				r.NoError(mdb2.AddBlock(block))
			}
			r.NoError(mdb1.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDs))
			r.NoError(mdb2.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDs))
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
			blocksA, blocksB := makeBlocks(layerID)

			// add A's blocks to A only, B's to B
			var blockIDsA, blockIDsB []types.BlockID
			for _, block := range blocksA {
				blockIDsA = append(blockIDsA, block.ID())
				r.NoError(mdb1.AddBlock(block))
			}
			r.NoError(mdb1.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsA))
			alg1.HandleIncomingLayer(context.TODO(), layerID)
			for _, block := range blocksB {
				blockIDsB = append(blockIDsB, block.ID())
				r.NoError(mdb2.AddBlock(block))
			}
			r.NoError(mdb2.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsB))
			alg2.HandleIncomingLayer(context.TODO(), layerID)

			// both nodes get stuck
			checkVerifiedLayer(t, alg1.trtl, lastVerified)
			checkVerifiedLayer(t, alg2.trtl, lastVerified)
		}

		// after a while (we simulate the distance here), both nodes eventually begin producing more blocks
		// in the case of a 50/50 split, this happens quickly
		for i := uint32(0); i < 2; i++ {
			layerID = layerID.Add(1)

			// these blocks will be nearly identical but they will have different base ballots, since the set of blocks
			// for recent layers has been bifurcated, so we have to generate and store blocks separately to simulate
			// an ongoing partition.
			blocksA := generateBlocks(t, layerID, layerSize, layerSize, alg1.BaseBallot, atxdb1, 1)
			var blockIDsA, blockIDsB []types.BlockID
			for _, block := range blocksA {
				blockIDsA = append(blockIDsA, block.ID())
				r.NoError(mdb1.AddBlock(block))
			}
			r.NoError(mdb1.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsA))
			alg1.HandleIncomingLayer(context.TODO(), layerID)

			blocksB := generateBlocks(t, layerID, layerSize, layerSize, alg2.BaseBallot, atxdb2, 1)
			for _, block := range blocksB {
				blockIDsB = append(blockIDsB, block.ID())
				r.NoError(mdb2.AddBlock(block))
			}
			r.NoError(mdb2.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsB))
			alg2.HandleIncomingLayer(context.TODO(), layerID)

			// both nodes still stuck
			checkVerifiedLayer(t, alg1.trtl, lastVerified)
			checkVerifiedLayer(t, alg2.trtl, lastVerified)
		}

		// finally, both nodes heal and get unstuck
		layerID = layerID.Add(1)
		blocksA := generateBlocks(t, layerID, layerSize, layerSize, alg1.BaseBallot, atxdb1, 1)
		var blockIDsA, blockIDsB []types.BlockID
		for _, block := range blocksA {
			blockIDsA = append(blockIDsA, block.ID())
			r.NoError(mdb1.AddBlock(block))
		}
		r.NoError(mdb1.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsA))
		alg1.HandleIncomingLayer(context.TODO(), layerID)

		blocksB := generateBlocks(t, layerID, layerSize, layerSize, alg2.BaseBallot, atxdb2, 1)
		for _, block := range blocksB {
			blockIDsB = append(blockIDsB, block.ID())
			r.NoError(mdb2.AddBlock(block))
		}
		r.NoError(mdb2.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsB))
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

		makeBlocks := func(layerID types.LayerID) (blocksA, blocksB, blocksC []*types.Block) {
			// simulate producing blocks in parallel
			blocksA = generateBlocks(t, layerID, layerSize, layerSize, alg1.BaseBallot, atxdb1, 1)
			blocksB = generateBlocks(t, layerID, layerSize, layerSize, alg2.BaseBallot, atxdb2, 1)
			blocksC = generateBlocks(t, layerID, layerSize, layerSize, alg3.BaseBallot, atxdb3, 1)

			// three-way split
			blocksA = blocksA[:layerSize/3]
			blocksB = blocksB[layerSize/3 : layerSize*2/3]
			blocksC = blocksC[layerSize*2/3:]
			return
		}

		// a bunch of good layers
		lastVerified := types.GetEffectiveGenesis()
		layerID := types.GetEffectiveGenesis()
		for i := 0; i < 10; i++ {
			layerID = layerID.Add(1)
			blocksA, blocksB, blocksC := makeBlocks(layerID)
			var blocks []*types.Block
			blocks = append(blocksA, blocksB...)
			blocks = append(blocks, blocksC...)

			// add all blocks to all tortoises
			var blockIDs []types.BlockID
			for _, block := range blocks {
				blockIDs = append(blockIDs, block.ID())
				r.NoError(mdb1.AddBlock(block))
				r.NoError(mdb2.AddBlock(block))
				r.NoError(mdb3.AddBlock(block))
			}
			r.NoError(mdb1.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDs))
			r.NoError(mdb2.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDs))
			r.NoError(mdb3.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDs))
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
			blocksA, blocksB, blocksC := makeBlocks(layerID)

			// add each blocks to their own
			var blockIDsA, blockIDsB, blockIDsC []types.BlockID
			for _, block := range blocksA {
				blockIDsA = append(blockIDsA, block.ID())
				r.NoError(mdb1.AddBlock(block))
			}
			r.NoError(mdb1.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsA))
			alg1.HandleIncomingLayer(context.TODO(), layerID)

			for _, block := range blocksB {
				blockIDsB = append(blockIDsB, block.ID())
				r.NoError(mdb2.AddBlock(block))
			}
			r.NoError(mdb2.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsB))
			alg2.HandleIncomingLayer(context.TODO(), layerID)

			for _, block := range blocksC {
				blockIDsC = append(blockIDsC, block.ID())
				r.NoError(mdb3.AddBlock(block))
			}
			r.NoError(mdb3.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsC))
			alg3.HandleIncomingLayer(context.TODO(), layerID)

			// all nodes get stuck
			checkVerifiedLayer(t, alg1.trtl, lastVerified)
			checkVerifiedLayer(t, alg2.trtl, lastVerified)
			checkVerifiedLayer(t, alg3.trtl, lastVerified)
		}

		// after a while (we simulate the distance here), all nodes eventually begin producing more blocks
		for i := uint32(0); i < 6; i++ {
			layerID = layerID.Add(1)

			// these blocks will be nearly identical but they will have different base ballots, since the set of blocks
			// for recent layers has been bifurcated, so we have to generate and store blocks separately to simulate
			// an ongoing partition.
			blocksA := generateBlocks(t, layerID, layerSize, layerSize, alg1.BaseBallot, atxdb1, 1)
			var blockIDsA, blockIDsB, blockIDsC []types.BlockID
			for _, block := range blocksA {
				blockIDsA = append(blockIDsA, block.ID())
				r.NoError(mdb1.AddBlock(block))
			}
			r.NoError(mdb1.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsA))
			alg1.HandleIncomingLayer(context.TODO(), layerID)

			blocksB := generateBlocks(t, layerID, layerSize, layerSize, alg2.BaseBallot, atxdb2, 1)
			for _, block := range blocksB {
				blockIDsB = append(blockIDsB, block.ID())
				r.NoError(mdb2.AddBlock(block))
			}
			r.NoError(mdb2.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsB))
			alg2.HandleIncomingLayer(context.TODO(), layerID)

			blocksC := generateBlocks(t, layerID, layerSize, layerSize, alg3.BaseBallot, atxdb3, 1)
			for _, block := range blocksC {
				blockIDsC = append(blockIDsC, block.ID())
				r.NoError(mdb3.AddBlock(block))
			}
			r.NoError(mdb3.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsC))
			alg3.HandleIncomingLayer(context.TODO(), layerID)

			// nodes still stuck
			checkVerifiedLayer(t, alg1.trtl, lastVerified)
			checkVerifiedLayer(t, alg2.trtl, lastVerified)
			checkVerifiedLayer(t, alg3.trtl, lastVerified)
		}

		// finally, all nodes heal and get unstuck
		layerID = layerID.Add(1)
		blocksA := generateBlocks(t, layerID, layerSize, layerSize, alg1.BaseBallot, atxdb1, 1)
		var blockIDsA, blockIDsB, blockIDsC []types.BlockID
		for _, block := range blocksA {
			blockIDsA = append(blockIDsA, block.ID())
			r.NoError(mdb1.AddBlock(block))
		}
		r.NoError(mdb1.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsA))
		alg1.HandleIncomingLayer(context.TODO(), layerID)

		blocksB := generateBlocks(t, layerID, layerSize, layerSize, alg2.BaseBallot, atxdb2, 1)
		for _, block := range blocksB {
			blockIDsB = append(blockIDsB, block.ID())
			r.NoError(mdb2.AddBlock(block))
		}
		r.NoError(mdb2.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsB))
		alg2.HandleIncomingLayer(context.TODO(), layerID)

		blocksC := generateBlocks(t, layerID, layerSize, layerSize, alg3.BaseBallot, atxdb3, 1)
		for _, block := range blocksC {
			blockIDsC = append(blockIDsC, block.ID())
			r.NoError(mdb3.AddBlock(block))
		}
		r.NoError(mdb3.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsC))
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

func BenchmarkTortoiseLayerHandling(b *testing.B) {
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
		layers = append(layers, s.Next())
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg))
		for _, lid := range layers {
			tortoise.HandleIncomingLayer(ctx, lid)
		}
	}
}

func BenchmarkTortoiseBaseBallotSelection(b *testing.B) {
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
		last = s.Next()
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(b, last.Sub(1), verified)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tortoise.BaseBallot(ctx)
	}
}

func randomBlock(tb testing.TB, lyrID types.LayerID, beacon types.Beacon, refBlockID *types.BlockID) *types.Block {
	tb.Helper()
	block := types.NewExistingBlock(lyrID, types.RandomBytes(4), nil)
	block.TortoiseBeacon = beacon
	block.RefBlock = refBlockID
	return block
}

func TestBallotHasGoodBeacon(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	layerID := types.GetEffectiveGenesis().Add(1)
	epochBeacon := types.RandomBeacon()
	ballot := randomBlock(t, layerID, epochBeacon, nil).ToBallot()

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
	refBlock := randomBlock(t, layerID, beacon, nil)
	refBlockID := refBlock.ID()
	ballot := randomBlock(t, layerID, types.EmptyBeacon, &refBlockID).ToBallot()

	mockBdp := mocks.NewMockblockDataProvider(ctrl)
	trtl := defaultTurtle(t)
	trtl.bdp = mockBdp

	logger := logtest.New(t)
	mockBdp.EXPECT().GetBlock(refBlockID).Return(refBlock, nil).Times(1)
	got, err := trtl.getBallotBeacon(ballot, logger)
	assert.NoError(t, err)
	assert.Equal(t, beacon, got)

	// get the block beacon again and the data is cached
	got, err = trtl.getBallotBeacon(ballot, logger)
	assert.NoError(t, err)
	assert.Equal(t, beacon, got)
}

func TestBallotFilterForHealing(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	layerID := types.GetEffectiveGenesis().Add(100)
	epochBeacon := types.RandomBeacon()
	goodBallot := randomBlock(t, layerID, epochBeacon, nil).ToBallot()
	badBeacon := types.RandomBeacon()
	require.NotEqual(t, epochBeacon, badBeacon)
	badBallot := randomBlock(t, layerID, badBeacon, nil).ToBallot()

	mockBeacons := smocks.NewMockBeaconGetter(ctrl)
	trtl := defaultTurtle(t)
	trtl.beacons = mockBeacons

	trtl.ballotLayer[goodBallot.ID()] = layerID
	trtl.ballotLayer[badBallot.ID()] = layerID

	logger := logtest.New(t)
	// cause the bad beacon block to be cached
	mockBeacons.EXPECT().GetBeacon(layerID.GetEpoch()).Return(epochBeacon, nil).Times(2)
	assert.True(t, trtl.markBeaconWithBadBallot(logger, goodBallot))
	assert.False(t, trtl.markBeaconWithBadBallot(logger, badBallot))

	// we don't count votes in bad beacon block for badBeaconVoteDelays layers
	trtl.last = layerID
	i := uint32(1)
	for ; i <= defaultVoteDelays; i++ {
		filter := trtl.ballotFilterForHealing(logger)
		assert.True(t, filter(goodBallot.ID()))
		assert.False(t, filter(badBallot.ID()))
	}
	// now we count the bad beacon block's votes
	trtl.last = layerID.Add(10)
	filter := trtl.ballotFilterForHealing(logger)
	assert.True(t, filter(goodBallot.ID()))
	assert.True(t, filter(badBallot.ID()))
}

// gapVote will skip one layer in voting.
func gapVote(rng *mrand.Rand, layers []*types.Layer, _ int) sim.Voting {
	if len(layers) < 2 {
		panic("need atleast 2 layers")
	}
	baseLayer := layers[len(layers)-2]
	support := layers[len(layers)-2].BlocksIDs()
	ballots := baseLayer.Ballots()
	base := ballots[rng.Intn(len(ballots))]
	return sim.Voting{Base: base.ID(), Support: support}
}

// olderExceptions will vote for block older then base ballot.
func olderExceptions(rng *mrand.Rand, layers []*types.Layer, _ int) sim.Voting {
	if len(layers) < 2 {
		panic("need atleast 2 layers")
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
		base, exceptions, err := tortoise.BaseBallot(context.Background())
		if err != nil {
			panic(err)
		}
		return sim.Voting{
			Base:    base,
			Against: exceptions[0],
			Support: exceptions[1],
			Abstain: exceptions[2],
		}
	}
}

func TestBaseBallotGenesis(t *testing.T) {
	ctx := context.Background()

	s := sim.New()
	cfg := defaultTestConfig()
	tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg))

	base, exceptions, err := tortoise.BaseBallot(ctx)
	genesisBlk := mesh.GenesisBlock()
	require.NoError(t, err)
	require.Equal(t, exceptions, [][]types.BlockID{{}, {genesisBlk.ID()}, {}})
	require.Equal(t, mesh.GenesisBlock().ID(), types.BlockID(base))
}

func ensureBaseAndExceptionsFromLayer(tb testing.TB, lid types.LayerID, base types.BallotID, exceptions [][]types.BlockID, mdb blockDataProvider) {
	tb.Helper()

	ballot, err := mdb.GetBallot(base)
	require.NoError(tb, err)
	require.Equal(tb, lid, ballot.LayerIndex)
	require.Empty(tb, exceptions[0])
	require.Empty(tb, exceptions[2])
	for _, bid := range exceptions[1] {
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
		base, exceptions, err := tortoise.BaseBallot(ctx)
		require.NoError(t, err)
		ensureBaseAndExceptionsFromLayer(t, last, base, exceptions, s.GetState(0).MeshDB)

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

			base, _, err := tortoise.BaseBallot(ctx)
			require.NoError(t, err)
			ballot, err := s.GetState(0).MeshDB.GetBallot(base)
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
	cfg.ConfidenceParam = 0

	var (
		tortoise       = tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))
		verified, last types.LayerID
		genesis        = types.GetEffectiveGenesis()
	)

	for _, lid := range sim.GenLayers(s,
		sim.WithSequence(5),
		sim.WithSequence(hdist+1,
			sim.WithCoin(true), // declare support
			sim.WithEmptyInputVector(),
			sim.WithVoteGenerator(splitVoting(size)),
		),
	) {
		last = lid
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, lid)
	}
	// 5th layer after genesis verifies 4th
	// and later layers can't verify previous as they are split
	require.Equal(t, genesis.Add(4), verified)
	base, exceptions, err := tortoise.BaseBallot(ctx)
	require.NoError(t, err)
	// last ballot that is consistent with local opinion
	ensureBallotLayerWithin(t, s.GetState(0).MeshDB, base, genesis.Add(5), genesis.Add(5))

	against, support, neutral := exceptions[0], exceptions[1], exceptions[2]

	require.Empty(t, against, "implicit voting")
	require.Empty(t, neutral, "empty input vector")

	// support for all layers in range of (verified, last - hdist)
	for _, bid := range support {
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
	// cfg.WindowSize = 1
	cfg.Hdist = 1
	cfg.Zdist = 1
	cfg.ConfidenceParam = 0

	var (
		tortoise       = tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))
		last, verified types.LayerID
		genesis        = types.GetEffectiveGenesis()
	)
	for _, last = range sim.GenLayers(s,
		sim.WithSequence(1, sim.WithEmptyInputVector()),
		sim.WithSequence(2),
	) {
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(1), verified)

	unsupported := map[types.BlockID]struct{}{}
	bids, err := s.GetState(0).MeshDB.LayerBlockIds(genesis.Add(1))
	require.NoError(t, err)
	require.NoError(t, s.GetState(0).MeshDB.SaveContextualValidity(bids[0], genesis.Add(1), false))
	unsupported[bids[0]] = struct{}{}

	// remove good ballots and genesis to make tortoise select one of the later blocks.
	delete(tortoise.trtl.ballots, genesis)
	tortoise.trtl.verifying.goodBallots = map[types.BallotID]struct{}{}

	base, exceptions, err := tortoise.BaseBallot(ctx)
	require.NoError(t, err)
	ensureBallotLayerWithin(t, s.GetState(0).MeshDB, base, last, last)
	require.Empty(t, exceptions[2])
	require.Len(t, exceptions[0], 1) // against one block in genesis.Add(1)
	for _, bid := range exceptions[0] {
		ensureBlockLayerWithin(t, s.GetState(0).MeshDB, bid, genesis.Add(1), last.Sub(1))
		require.Contains(t, unsupported, bid)
	}
	require.Len(t, exceptions[1], size+1)
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
				sim.WithSequence(1, sim.WithEmptyInputVector()),
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
				sim.WithSequence(1, sim.WithEmptyInputVector()),
			},
			lid:      genesis.Add(4),
			expected: against,
		},
		{
			desc: "WithUnfinishedHare",
			seqs: []sim.Sequence{
				sim.WithSequence(3),
				sim.WithSequence(1, sim.WithoutInputVector()),
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

			local := Opinion{}
			err := tortoise.trtl.addLocalOpinion(wrapContext(ctx), tc.lid, local)
			require.NoError(t, err)

			for bid, opinion := range local {
				require.Equal(t, tc.expected, opinion, "block id %s", bid)
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

				weight, err := computeBallotWeight(atxdb, weights, ballot, tc.layerSize, tc.layersPerEpoch)
				require.NoError(t, err)
				require.Equal(t, b.ExpectedWeight.String(), weight.String())
				weights[ballot.ID()] = weight
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
	cfg.ConfidenceParam = 0

	var (
		tortoise1                  = tortoiseFromSimState(s1.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))
		tortoise2                  = tortoiseFromSimState(s1.GetState(1), WithConfig(cfg))
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
	for i := 0; i < int(types.GetLayersPerEpoch())*2; i++ {
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

	for lid := partitionStart.Add(1); !lid.After(partitionEnd); lid = lid.Add(1) {
		bids, err := s1.GetState(1).MeshDB.LayerBlockIds(lid)
		require.NoError(t, err)
		for _, bid := range bids {
			valid, err := s1.GetState(0).MeshDB.ContextualValidity(bid)
			require.NoError(t, err)
			assert.True(t, valid, "block %s at layer %s", bid, lid)
		}
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

		require.Empty(t, tortoise.trtl.full.votes)

		evicted := tortoise.trtl.evicted
		require.Equal(t, verified.Sub(window).Sub(1), evicted)
		for lid := types.GetEffectiveGenesis(); !lid.After(evicted); lid = lid.Add(1) {
			require.Empty(t, tortoise.trtl.blocks[lid])
			require.Empty(t, tortoise.trtl.ballots[lid])
		}

		for lid := evicted.Add(1); !lid.After(last); lid = lid.Add(1) {
			for _, bid := range tortoise.trtl.blocks[lid] {
				require.Contains(t, tortoise.trtl.localOpinion, bid, "layer %s", lid)
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
			sim.WithSequence(20, sim.WithEmptyInputVector()),
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
				require.Contains(t, tortoise.trtl.localOpinion, bid, "layer %s", lid)
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
