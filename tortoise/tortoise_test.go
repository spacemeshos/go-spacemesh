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
	"github.com/spacemeshos/go-spacemesh/database"
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

// voteNegative - the number of blocks to vote negative per layer
// voteAbstain - the number of layers to vote abstain because we always abstain on a whole layer.
func turtleSanity(t *testing.T, numLayers types.LayerID, blocksPerLayer int, voteNegative, voteAbstain int) (trtl *turtle, negative, abstain []types.BlockID) {
	msh := getInMemMesh(t)
	logger := logtest.New(t)
	newlyrs := make(map[types.LayerID]struct{})

	inputVectorFn := func(l types.LayerID) (ids []types.BlockID, err error) {
		if l.Before(mesh.GenesisLayer().Index()) {
			panic("shouldn't happen")
		}
		if l == mesh.GenesisLayer().Index() {
			return types.BlockIDs(mesh.GenesisLayer().Blocks()), nil
		}

		_, exist := newlyrs[l]

		if !exist && l != numLayers {
			newlyrs[l] = struct{}{}
		}

		blks, err := msh.LayerBlockIds(l)
		if err != nil {
			t.Log(err)
			panic("db err")
		}

		if voteAbstain > 0 {
			if !exist && l != numLayers {
				voteAbstain--
				abstain = append(abstain, blks...)
			}
			return nil, errors.New("hare didn't finish")
		}

		if voteNegative == 0 {
			return blks, nil
		}

		sorted := types.SortBlockIDs(blks)

		if !exist && l != numLayers {
			negative = append(negative, sorted[:voteNegative]...)
		}
		return sorted[voteNegative:], nil
	}

	trtl = defaultTurtle(t)
	trtl.LayerSize = uint32(blocksPerLayer)
	trtl.bdp = msh
	trtl.init(context.TODO(), mesh.GenesisLayer())

	var l types.LayerID
	atxdb := getAtxDB()
	trtl.atxdb = atxdb
	for l = mesh.GenesisLayer().Index().Add(1); !l.After(numLayers); l = l.Add(1) {
		makeAndProcessLayer(t, l, trtl, blocksPerLayer, blocksPerLayer, atxdb, msh, inputVectorFn)
		logger.Debug("======================== handled layer", l)
		lastlyr := trtl.BallotOpinionsByLayer[l]
		for _, v := range lastlyr {
			logger.Debug("block opinion map size", len(v))
			// the max. number of layers we store opinions for is the window size (since last evicted) + 3.
			// eviction happens _after_ blocks for a new layer N have been processed, and that layer hasn't yet
			// been verified. at this point in time,
			// tortoise window := N - 1 (last verified) - windowSize - 1 (first layer to evict) - 1 (layer not yet evicted)
			if (len(v)) > blocksPerLayer*int(trtl.WindowSize+3) {
				t.Errorf("layer opinion table exceeded max size, LEAK! size: %v, maxsize: %v",
					len(v), blocksPerLayer*int(trtl.WindowSize+3))
			}
			break
		}
	}

	return
}

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
	return makeLayerWithBeacon(t, layerID, trtl, nil, natxs, blocksPerLayer, atxdb, msh, inputVectorFn)
}

func makeLayerWithBeacon(t *testing.T, layerID types.LayerID, trtl *turtle, beacon []byte, natxs, blocksPerLayer int, atxdb atxDataWriter, msh blockDataWriter, inputVectorFn func(id types.LayerID) ([]types.BlockID, error)) *types.Layer {
	logger := logtest.New(t)
	logger.Debug("======================== choosing base block for layer", layerID)
	if inputVectorFn != nil {
		oldInputVectorFn := msh.GetInputVectorBackupFunc()
		defer func() {
			msh.SetInputVectorBackupFunc(oldInputVectorFn)
		}()
		msh.SetInputVectorBackupFunc(inputVectorFn)
	}
	baseBallotID, lists, err := trtl.BaseBallot(context.TODO())
	require.NoError(t, err)
	logger.Debug("base ballot for layer", layerID, "is", baseBallotID)
	logger.Debug("exception lists for layer", layerID, "(against, support, neutral):", lists)
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
		logger.Debug("generated block", blk.ID(), "in layer", layerID)
	}

	return lyr
}

func TestLayerPatterns(t *testing.T) {
	const size = 10 // more blocks means a longer test
	t.Run("happy", func(t *testing.T) {
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

	t.Run("heal after bad layers", func(t *testing.T) {
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
			sim.WithSequence(11),
		) {
			last = lid
			_, verified, _ = tortoise.HandleIncomingLayer(ctx, lid)
		}
		require.Equal(t, last.Sub(1), verified)
	})

	t.Run("heal after good bad good bad good pattern", func(t *testing.T) {
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
		logger.Debug("handled layer", l, "verified layer", trtl.Verified,
			"========================================================================")
	}

	// verification will get stuck as of the first layer with conflicting local and global opinions.
	// block votes aren't counted because blocks aren't marked good, because they contain exceptions older
	// than their base block.
	// self-healing will not run because the layers aren't old enough.
	require.Equal(t, int(types.GetEffectiveGenesis().Add(uint32(initialNumGood)).Uint32()), int(trtl.Verified.Uint32()),
		"verification should advance after hare finishes")
	// todo: also check votes with requireVote
}

type (
	baseBallotProvider  func(context.Context) (types.BallotID, [][]types.BlockID, error)
	inputVectorProvider func(types.LayerID) ([]types.BlockID, error)
)

func generateBlocks(t *testing.T, l types.LayerID, natxs, nblocks int, bbp baseBallotProvider, atxdb atxDataWriter, weight uint) (blocks []*types.Block) {
	logger := logtest.New(t)
	logger.Debug("======================== choosing base ballot for layer", l)
	b, lists, err := bbp(context.TODO())
	require.NoError(t, err)
	logger.Debug("the base ballot for layer", l, "is", b, ". exception lists:")
	logger.Debug("\tagainst\t", lists[0])
	logger.Debug("\tfor\t", lists[1])
	logger.Debug("\tneutral\t", lists[2])

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
		logger.Debug("generated block", blk.ID(), "in layer", l)
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

func TestEviction(t *testing.T) {
	const size = 10
	s := sim.New(
		sim.WithLayerSize(size),
	)
	s.Setup()

	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.LayerSize = size
	tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg))
	trtl := tortoise.trtl

	for i := 0; i < 100; i++ {
		tortoise.HandleIncomingLayer(ctx, s.Next())
	}

	// old data were evicted
	require.EqualValues(t, trtl.Verified.Sub(tortoise.trtl.WindowSize+1).Uint32(), trtl.LastEvicted.Uint32())

	checkBlockLayer := func(bid types.BlockID, layerAfter types.LayerID) types.LayerID {
		blk, err := trtl.bdp.GetBlock(bid)
		require.NoError(t, err, "error reading block data")
		require.True(t, !blk.LayerIndex.Before(layerAfter),
			"opinion on ancient block should have been evicted: block %v layer %v maxdepth %v lastevicted %v windowsize %v",
			blk.ID(), blk.LayerIndex, layerAfter, trtl.LastEvicted, trtl.WindowSize)
		return blk.LayerIndex
	}

	count := 0
	for _, blks := range trtl.BallotOpinionsByLayer {
		count += len(blks)
		for ballotID, opinion := range blks {
			lid := checkBlockLayer(types.BlockID(ballotID), trtl.LastEvicted)

			// check deep opinion layers
			for bid := range opinion {
				// check that child (opinion) block layer is within window size from parent block layer
				// we allow a leeway of three layers:
				// 1. eviction evicts one layer prior to window start (Verified - WindowSize)
				// 2. Verified layer lags Last processed layer by one
				// 3. block opinions are added before block layer is finished processing
				checkBlockLayer(bid, lid.Sub(trtl.WindowSize+3))
			}
		}
	}
}

func TestEviction2(t *testing.T) {
	layers := types.NewLayerID(uint32(defaultTestWindowSize) * 3)
	avgPerLayer := 10
	trtl, _, _ := turtleSanity(t, layers, avgPerLayer, 0, 0)
	require.Equal(t, int(defaultTestWindowSize)+2, len(trtl.BallotOpinionsByLayer))
	count := 0
	for _, blks := range trtl.BallotOpinionsByLayer {
		count += len(blks)
	}
	require.Equal(t, (int(defaultTestWindowSize)+2)*avgPerLayer, count)
	require.Equal(t, (int(defaultTestWindowSize)+2)*avgPerLayer, len(trtl.GoodBallotsIndex)) // all blocks should be good
}

func TestLayerCutoff(t *testing.T) {
	r := require.New(t)
	mdb := getInMemMesh(t)
	alg := defaultAlgorithm(t, mdb)

	// cutoff should be zero if we haven't seen at least Hdist layers yet
	alg.trtl.Last = types.NewLayerID(alg.trtl.Hdist - 1)
	r.Equal(0, int(alg.trtl.layerCutoff().Uint32()))
	alg.trtl.Last = types.NewLayerID(alg.trtl.Hdist)
	r.Equal(0, int(alg.trtl.layerCutoff().Uint32()))

	// otherwise, cutoff should be Hdist layers before Last
	alg.trtl.Last = types.NewLayerID(alg.trtl.Hdist + 1)
	r.Equal(1, int(alg.trtl.layerCutoff().Uint32()))
	alg.trtl.Last = types.NewLayerID(alg.trtl.Hdist + 100)
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
	mockBeacons.EXPECT().GetBeacon(gomock.Any()).Return(nil, nil).AnyTimes()
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
	trtl.Last = types.NewLayerID(10) // state should not be cloned
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
	r.NotEqual(trtl.Last, trtl2.Last)
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

func TestGetLocalBlockOpinion(t *testing.T) {
	r := require.New(t)
	mdb := getInMemMesh(t)
	atxdb := getAtxDB()
	alg := defaultAlgorithm(t, mdb)
	alg.trtl.atxdb = atxdb

	l1ID := types.GetEffectiveGenesis().Add(1)
	blocks := generateBlocks(t, l1ID, 2, 2, alg.BaseBallot, atxdb, 1)

	// no input vector for recent layer: expect abstain vote
	for _, block := range blocks {
		require.NoError(t, mdb.AddBlock(block))
	}
	vec, err := alg.trtl.getLocalBlockOpinion(wrapContext(context.TODO()), l1ID, blocks[0].ID())
	r.NoError(err)
	r.Equal(abstain, vec)

	// block included in input vector
	r.NoError(mdb.SaveLayerInputVectorByID(context.TODO(), l1ID, []types.BlockID{blocks[0].ID()}))
	vec, err = alg.trtl.getLocalBlockOpinion(wrapContext(context.TODO()), l1ID, blocks[0].ID())
	r.NoError(err)
	r.Equal(support, vec)

	// block not included in input vector
	vec, err = alg.trtl.getLocalBlockOpinion(wrapContext(context.TODO()), l1ID, blocks[1].ID())
	r.NoError(err)
	r.Equal(against, vec)
}

func TestCheckBlockAndGetInputVector(t *testing.T) {
	r := require.New(t)
	mdb := getInMemMesh(t)
	atxdb := getAtxDB()
	alg := defaultAlgorithm(t, mdb)
	alg.trtl.atxdb = atxdb

	l1ID := types.GetEffectiveGenesis().Add(1)
	blocks := generateBlocks(t, l1ID, 3, 3, alg.BaseBallot, atxdb, 1)
	diffList := []types.BlockID{blocks[0].ID()}
	lg := logtest.New(t)

	// missing block
	r.False(alg.trtl.checkBallotAndGetLocalOpinion(wrapContext(context.TODO()), diffList, "foo", support, l1ID, lg))

	// exception block older than base block
	blocks[0].LayerIndex = mesh.GenesisLayer().Index()
	r.NoError(mdb.AddBlock(blocks[0]))
	r.False(alg.trtl.checkBallotAndGetLocalOpinion(wrapContext(context.TODO()), diffList, "foo", support, l1ID, lg))

	// missing input vector for layer
	r.NoError(mdb.AddBlock(blocks[1]))
	diffList[0] = blocks[1].ID()
	r.False(alg.trtl.checkBallotAndGetLocalOpinion(wrapContext(context.TODO()), diffList, "foo", support, l1ID, lg))

	// good
	r.NoError(mdb.SaveLayerInputVectorByID(context.TODO(), l1ID, diffList))
	r.True(alg.trtl.checkBallotAndGetLocalOpinion(wrapContext(context.TODO()), diffList, "foo", support, l1ID, lg))

	// vote differs from input vector
	diffList[0] = blocks[2].ID()
	r.NoError(mdb.AddBlock(blocks[2]))
	r.False(alg.trtl.checkBallotAndGetLocalOpinion(wrapContext(context.TODO()), diffList, "foo", support, l1ID, lg))
}

func TestCalculateExceptions(t *testing.T) {
	r := require.New(t)
	mdb := getInMemMesh(t)
	atxdb := getAtxDB()
	alg := defaultAlgorithm(t, mdb)
	alg.trtl.atxdb = atxdb

	// helper function for checking votes
	expectVotes := func(votes []map[types.BlockID]struct{}, numAgainst, numFor, numNeutral int) {
		r.Len(votes, 3, "vote vector size is wrong")
		r.Len(votes[0], numAgainst) // against
		r.Len(votes[1], numFor)     // for
		r.Len(votes[2], numNeutral) // neutral
	}

	// genesis layer
	l0ID := types.GetEffectiveGenesis()
	votes, err := alg.trtl.calculateExceptions(wrapContext(context.TODO()), types.LayerID{}, l0ID, Opinion{})
	r.NoError(err)
	// expect votes in support of all genesis blocks
	expectVotes(votes, 0, len(mesh.GenesisLayer().Blocks()), 0)

	// layer greater than last processed: expect support only for genesis
	l1ID := l0ID.Add(1)
	votes, err = alg.trtl.calculateExceptions(wrapContext(context.TODO()), types.LayerID{}, l1ID, Opinion{})
	r.NoError(err)
	expectVotes(votes, 0, 1, 0)

	// now advance the processed layer
	alg.trtl.Last = l1ID

	// missing layer data
	votes, err = alg.trtl.calculateExceptions(wrapContext(context.TODO()), types.LayerID{}, l1ID, Opinion{})
	r.NoError(err)
	expectVotes(votes, 0, 1, 0)

	// layer opinion vector is nil (abstains): recent layer, in mesh, no input vector
	alg.trtl.Last = l0ID
	l1 := createTurtleLayer(t, l1ID, mdb, atxdb, alg.BaseBallot, mdb.LayerBlockIds, defaultTestLayerSize)
	r.NoError(addLayerToMesh(mdb, l1))
	alg.trtl.Last = l1ID
	mdb.InputVectorBackupFunc = nil
	votes, err = alg.trtl.calculateExceptions(wrapContext(context.TODO()), types.LayerID{}, l1ID, Opinion{})
	r.NoError(err)
	// expect no against, FOR only for l0, and NEUTRAL for l1
	expectVotes(votes, 0, len(mesh.GenesisLayer().Blocks()), defaultTestLayerSize)

	// adding diffs for: support all blocks in the layer
	mdb.InputVectorBackupFunc = mdb.LayerBlockIds
	votes, err = alg.trtl.calculateExceptions(wrapContext(context.TODO()), types.LayerID{}, l1ID, Opinion{})
	r.NoError(err)
	expectVotes(votes, 0, len(mesh.GenesisLayer().Blocks())+defaultTestLayerSize, 0)

	// adding diffs against: vote against all blocks in the layer
	mdb.InputVectorBackupFunc = nil
	// we cannot store an empty vector here (it comes back as nil), so just put another block ID in it
	r.NoError(mdb.SaveLayerInputVectorByID(context.TODO(), l1ID, []types.BlockID{mesh.GenesisBlock().ID()}))
	votes, err = alg.trtl.calculateExceptions(wrapContext(context.TODO()), types.LayerID{}, l1ID, Opinion{})
	r.NoError(err)

	// we don't explicitly vote against blocks in the layer, we implicitly vote against them by not voting for them
	expectVotes(votes, 0, len(mesh.GenesisLayer().Blocks()), 0)

	// compare opinions: all agree, no exceptions
	mdb.InputVectorBackupFunc = mdb.LayerBlockIds
	opinion := Opinion{
		mesh.GenesisBlock().ID(): support,
		l1.Blocks()[0].ID():      support,
		l1.Blocks()[1].ID():      support,
		l1.Blocks()[2].ID():      support,
	}
	votes, err = alg.trtl.calculateExceptions(wrapContext(context.TODO()), types.LayerID{}, l1ID, opinion)
	r.NoError(err)
	expectVotes(votes, 0, 0, 0)

	// compare opinions: all disagree, adds exceptions
	opinion = Opinion{
		mesh.GenesisBlock().ID(): against,
		l1.Blocks()[0].ID():      against,
		l1.Blocks()[1].ID():      against,
		l1.Blocks()[2].ID():      against,
	}
	votes, err = alg.trtl.calculateExceptions(wrapContext(context.TODO()), types.LayerID{}, l1ID, opinion)
	r.NoError(err)
	expectVotes(votes, 0, 4, 0)

	l2ID := l1ID.Add(1)
	l3ID := l2ID.Add(1)
	var l3 *types.Layer

	// exceeding max exceptions
	t.Run("exceeding max exceptions", func(t *testing.T) {
		alg.trtl.MaxExceptions = 10
		l2 := createTurtleLayer(t, l2ID, mdb, atxdb, alg.BaseBallot, mdb.LayerBlockIds, alg.trtl.MaxExceptions+1)
		for _, block := range l2.Blocks() {
			r.NoError(mdb.AddBlock(block))
		}
		l3 = createTurtleLayer(t, l3ID, mdb, atxdb, alg.BaseBallot, mdb.LayerBlockIds, defaultTestLayerSize)
		alg.trtl.Last = l2ID
		votes, err = alg.trtl.calculateExceptions(wrapContext(context.TODO()), types.LayerID{}, l2ID, Opinion{})
		r.Error(err)
		r.Contains(err.Error(), errstrTooManyExceptions, "expected too many exceptions error")
		r.Nil(votes)
	})

	// TODO: test adding base block opinion in support of a block that disagrees with the local opinion, e.g., a block
	//   that this node has not seen yet. See https://github.com/spacemeshos/go-spacemesh/issues/2424.

	t.Run("advance sliding window", func(t *testing.T) {
		// advance the evicted layer until the exception layer slides outside the sliding window
		for _, block := range l3.Blocks() {
			r.NoError(mdb.AddBlock(block))
		}
		l4ID := l3ID.Add(1)
		alg.trtl.LastEvicted = l2ID
		r.NoError(alg.trtl.HandleIncomingLayer(context.TODO(), l3ID))
		l4 := createTurtleLayer(t, l4ID, mdb, atxdb, alg.BaseBallot, mdb.LayerBlockIds, defaultTestLayerSize)
		for _, block := range l4.Blocks() {
			r.NoError(mdb.AddBlock(block))
		}
		l5ID := l4ID.Add(1)
		createTurtleLayer(t, l5ID, mdb, atxdb, alg.BaseBallot, mdb.LayerBlockIds, defaultTestLayerSize)
		alg.trtl.Last = l4ID
		votes, err = alg.trtl.calculateExceptions(wrapContext(context.TODO()), types.LayerID{}, l3ID, Opinion{})
		r.NoError(err)
		// expect votes FOR the blocks in the two intervening layers between the base ballot layer and the last layer
		expectVotes(votes, 0, 2*defaultTestLayerSize, 0)
	})
}

func TestDetermineBallotGoodness(t *testing.T) {
	r := require.New(t)
	mdb := getInMemMesh(t)
	atxdb := getAtxDB()
	alg := defaultAlgorithm(t, mdb)
	alg.trtl.atxdb = atxdb

	l1ID := types.GetEffectiveGenesis().Add(1)
	l1Ballots := types.ToBallots(generateBlocks(t, l1ID, 3, 3, alg.BaseBallot, atxdb, 1))

	// block marked good
	r.True(alg.trtl.determineBallotGoodness(wrapContext(context.TODO()), l1Ballots[0]))

	// base ballot not found
	randBallotID := randomBallotID()
	alg.trtl.GoodBallotsIndex[randBallotID] = false
	l1Ballots[1].BaseBallot = randBallotID
	r.False(alg.trtl.determineBallotGoodness(wrapContext(context.TODO()), l1Ballots[1]))

	// base ballot not good
	l1Ballots[1].BaseBallot = l1Ballots[2].ID()
	r.False(alg.trtl.determineBallotGoodness(wrapContext(context.TODO()), l1Ballots[1]))

	// diff inconsistent with local opinion
	l1Ballots[2].AgainstDiff = []types.BlockID{mesh.GenesisBlock().ID()}
	r.False(alg.trtl.determineBallotGoodness(wrapContext(context.TODO()), l1Ballots[2]))

	// can run again on the same block with no change (idempotency)
	r.True(alg.trtl.determineBallotGoodness(wrapContext(context.TODO()), l1Ballots[0]))
	r.False(alg.trtl.determineBallotGoodness(wrapContext(context.TODO()), l1Ballots[2]))
}

func TestScoreBallots(t *testing.T) {
	r := require.New(t)
	mdb := getInMemMesh(t)
	atxdb := getAtxDB()
	alg := defaultAlgorithm(t, mdb)
	alg.trtl.atxdb = atxdb

	l1ID := types.GetEffectiveGenesis().Add(1)
	l1Ballots := types.ToBallots(generateBlocks(t, l1ID, 3, 3, alg.BaseBallot, atxdb, 1))

	// adds a block not already marked good
	r.NotContains(alg.trtl.GoodBallotsIndex, l1Ballots[0].ID())
	alg.trtl.scoreBallots(wrapContext(context.TODO()), []*types.Ballot{l1Ballots[0]})
	r.Contains(alg.trtl.GoodBallotsIndex, l1Ballots[0].ID())

	// no change if already marked good
	alg.trtl.scoreBallots(wrapContext(context.TODO()), []*types.Ballot{l1Ballots[0]})
	r.Contains(alg.trtl.GoodBallotsIndex, l1Ballots[0].ID())

	// removes a block previously marked good
	// diff inconsistent with local opinion
	l1Ballots[0].AgainstDiff = []types.BlockID{mesh.GenesisBlock().ID()}
	alg.trtl.scoreBallots(wrapContext(context.TODO()), []*types.Ballot{l1Ballots[0]})
	r.NotContains(alg.trtl.GoodBallotsIndex, l1Ballots[0].ID())

	// try a few blocks
	r.NotContains(alg.trtl.GoodBallotsIndex, l1Ballots[1].ID())
	r.NotContains(alg.trtl.GoodBallotsIndex, l1Ballots[2].ID())
	alg.trtl.scoreBallots(wrapContext(context.TODO()), l1Ballots)

	// adds new blocks
	r.Contains(alg.trtl.GoodBallotsIndex, l1Ballots[1].ID())
	r.Contains(alg.trtl.GoodBallotsIndex, l1Ballots[2].ID())

	// no change if already not marked good
	r.NotContains(alg.trtl.GoodBallotsIndex, l1Ballots[0].ID())
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

				consensus.processBlocks(ctx, layerBlocks)
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

				require.NoError(t, consensus.processBallots(wrapContext(ctx), layerBallots))
				consensus.Processed = lid
				consensus.Last = lid
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

func TestVerifyLayers(t *testing.T) {
	r := require.New(t)

	mdb := getInMemMesh(t)
	atxdb := getAtxDB()
	atxdb.storeEpochWeight(uint64(defaultTestLayerSize))

	alg := defaultAlgorithm(t, mdb)
	alg.trtl.atxdb = atxdb
	l1ID := types.GetEffectiveGenesis().Add(1)
	l2ID := l1ID.Add(1)
	l2Blocks := generateBlocks(t, l2ID, defaultTestLayerSize, defaultTestLayerSize, alg.BaseBallot, atxdb, 1)
	l3ID := l2ID.Add(1)
	l3Blocks := generateBlocks(t, l3ID, defaultTestLayerSize, defaultTestLayerSize, alg.BaseBallot, atxdb, 1)
	l4ID := l3ID.Add(1)
	l4Blocks := generateBlocks(t, l4ID, defaultTestLayerSize, defaultTestLayerSize, alg.BaseBallot, atxdb, 1)
	l5ID := l4ID.Add(1)
	l5Blocks := generateBlocks(t, l5ID, defaultTestLayerSize, defaultTestLayerSize, alg.BaseBallot, atxdb, 1)
	l6ID := l5ID.Add(1)
	l6Blocks := generateBlocks(t, l6ID, defaultTestLayerSize, defaultTestLayerSize, alg.BaseBallot, atxdb, 1)
	for _, blocks := range [][]*types.Block{l2Blocks, l3Blocks, l4Blocks, l5Blocks, l6Blocks} {
		var ballots []*types.Ballot
		for _, block := range blocks {
			ballots = append(ballots, block.ToBallot())
		}
		require.NoError(t, alg.trtl.processBallots(wrapContext(context.TODO()), ballots))
	}

	// layer missing in database
	alg.trtl.Processed = l2ID
	err := alg.trtl.verifyLayers(wrapContext(context.TODO()))
	r.ErrorIs(err, database.ErrNotFound)

	// empty layer: local opinion vector is nil, abstains on all blocks in layer
	// no contextual validity data recorded
	// layer should be verified
	r.NoError(mdb.AddZeroBlockLayer(l1ID))

	mdbWrapper := meshWrapper{
		blockDataWriter: mdb,
		saveContextualValidityFn: func(types.BlockID, types.LayerID, bool) error {
			r.Fail("should not save contextual validity")
			return nil
		},
	}
	alg.trtl.bdp = mdbWrapper
	err = alg.trtl.verifyLayers(wrapContext(context.TODO()))
	r.NoError(err)
	r.Equal(int(l1ID.Uint32()), int(alg.trtl.Verified.Uint32()))

	for _, block := range l2Blocks {
		r.NoError(mdb.AddBlock(block))
	}
	// L3 blocks support all L2 blocks
	l2SupportVec := Opinion{
		l2Blocks[0].ID(): support,
		l2Blocks[1].ID(): support,
		l2Blocks[2].ID(): support,
	}
	l3Ballots := types.ToBallots(l3Blocks)
	alg.trtl.BallotOpinionsByLayer[l3ID] = map[types.BallotID]Opinion{
		l3Ballots[0].ID(): l2SupportVec,
		l3Ballots[1].ID(): l2SupportVec,
		l3Ballots[2].ID(): l2SupportVec,
	}
	alg.trtl.Processed = l3ID

	// voting blocks not marked good, both global and local opinion is abstain, verified layer does not advance
	r.NoError(alg.trtl.verifyLayers(wrapContext(context.TODO())))
	r.Equal(int(l1ID.Uint32()), int(alg.trtl.Verified.Uint32()))

	// now mark voting blocks good
	alg.trtl.GoodBallotsIndex[l3Ballots[0].ID()] = false
	alg.trtl.GoodBallotsIndex[l3Ballots[1].ID()] = false
	alg.trtl.GoodBallotsIndex[l3Ballots[2].ID()] = false

	// TODO(nkryuchkov): uncomment when it's used
	var /*l2BlockIDs,*/ l3BlockIDs, l4BlockIDs, l5BlockIDs []types.BlockID
	for _, block := range l3Blocks {
		r.NoError(mdb.AddBlock(block))
		l3BlockIDs = append(l3BlockIDs, block.ID())
	}

	// consensus doesn't match: fail to verify candidate layer
	// global opinion: good, local opinion: abstain
	r.NoError(alg.trtl.verifyLayers(wrapContext(context.TODO())))
	r.Equal(int(l1ID.Uint32()), int(alg.trtl.Verified.Uint32()))

	// mark local opinion of L2 good so verified layer advances
	// do the reverse for L3: local opinion is good, global opinion is undecided
	// TODO(nkryuchkov): remove or uncomment when it's used
	//for _, block := range l2Blocks {
	//	l2BlockIDs = append(l2BlockIDs, block.ID())
	//}
	mdbWrapper.inputVectorBackupFn = mdb.LayerBlockIds
	mdbWrapper.saveContextualValidityFn = nil
	alg.trtl.bdp = mdbWrapper
	for _, block := range l4Blocks {
		r.NoError(mdb.AddBlock(block))
		l4BlockIDs = append(l4BlockIDs, block.ID())
	}
	for _, block := range l5Blocks {
		r.NoError(mdb.AddBlock(block))
		l5BlockIDs = append(l5BlockIDs, block.ID())
	}
	for _, block := range l6Blocks {
		r.NoError(mdb.AddBlock(block))
	}
	r.NoError(mdb.SaveLayerInputVectorByID(context.TODO(), l3ID, l3BlockIDs))
	r.NoError(mdb.SaveLayerInputVectorByID(context.TODO(), l4ID, l4BlockIDs))
	r.NoError(mdb.SaveLayerInputVectorByID(context.TODO(), l5ID, l5BlockIDs))
	l4Votes := Opinion{
		// support these so global opinion is support
		l2Blocks[0].ID(): support,
		l2Blocks[1].ID(): support,
		l2Blocks[2].ID(): support,

		// abstain
		l3Blocks[0].ID(): abstain,
		l3Blocks[1].ID(): abstain,
		l3Blocks[2].ID(): abstain,
	}
	l4Ballots := types.ToBallots(l4Blocks)
	alg.trtl.BallotOpinionsByLayer[l4ID] = map[types.BallotID]Opinion{
		l4Ballots[0].ID(): l4Votes,
		l4Ballots[1].ID(): l4Votes,
		l4Ballots[2].ID(): l4Votes,
	}
	alg.trtl.Processed = l4ID
	alg.trtl.GoodBallotsIndex[l4Ballots[0].ID()] = false
	alg.trtl.GoodBallotsIndex[l4Ballots[1].ID()] = false
	alg.trtl.GoodBallotsIndex[l4Ballots[2].ID()] = false
	l5Votes := Opinion{
		l2Blocks[0].ID(): support,
		l2Blocks[1].ID(): support,
		l2Blocks[2].ID(): support,
		l3Blocks[0].ID(): abstain,
		l3Blocks[1].ID(): abstain,
		l3Blocks[2].ID(): abstain,
		l4Blocks[0].ID(): support,
		l4Blocks[1].ID(): support,
		l4Blocks[2].ID(): support,
	}
	l5Ballots := types.ToBallots(l5Blocks)
	alg.trtl.BallotOpinionsByLayer[l5ID] = map[types.BallotID]Opinion{
		l5Ballots[0].ID(): l5Votes,
		l5Ballots[1].ID(): l5Votes,
		l5Ballots[2].ID(): l5Votes,
	}
	alg.trtl.GoodBallotsIndex[l5Ballots[0].ID()] = false
	alg.trtl.GoodBallotsIndex[l5Ballots[1].ID()] = false
	alg.trtl.GoodBallotsIndex[l5Ballots[2].ID()] = false
	l6Votes := Opinion{
		l2Blocks[0].ID(): support,
		l2Blocks[1].ID(): support,
		l2Blocks[2].ID(): support,
		l3Blocks[0].ID(): abstain,
		l3Blocks[1].ID(): abstain,
		l3Blocks[2].ID(): abstain,
		l4Blocks[0].ID(): support,
		l4Blocks[1].ID(): support,
		l4Blocks[2].ID(): support,
		l5Blocks[0].ID(): support,
		l5Blocks[1].ID(): support,
		l5Blocks[2].ID(): support,
	}
	l6Ballots := types.ToBallots(l6Blocks)
	alg.trtl.BallotOpinionsByLayer[l6ID] = map[types.BallotID]Opinion{
		l6Ballots[0].ID(): l6Votes,
		l6Ballots[1].ID(): l6Votes,
		l6Ballots[2].ID(): l6Votes,
	}
	alg.trtl.GoodBallotsIndex[l6Ballots[0].ID()] = false
	alg.trtl.GoodBallotsIndex[l6Ballots[1].ID()] = false
	alg.trtl.GoodBallotsIndex[l6Ballots[2].ID()] = false

	// verified layer advances one step, but L3 is not verified because global opinion is undecided, so verification
	// stops there
	t.Run("global opinion undecided", func(t *testing.T) {
		err = alg.trtl.verifyLayers(wrapContext(context.TODO()))
		r.NoError(err)
		r.Equal(int(l2ID.Uint32()), int(alg.trtl.Verified.Uint32()))
	})

	l4Votes = Opinion{
		l2Blocks[0].ID(): support,
		l2Blocks[1].ID(): support,
		l2Blocks[2].ID(): support,

		// change from abstain to support
		l3Blocks[0].ID(): support,
		l3Blocks[1].ID(): support,
		l3Blocks[2].ID(): support,
	}

	// weight not exceeded
	t.Run("weight not exceeded", func(t *testing.T) {
		// modify vote so one block votes in support of L3 blocks, two blocks continue to abstain, so threshold not met
		alg.trtl.BallotOpinionsByLayer[l4ID][l4Ballots[0].ID()] = l4Votes
		err = alg.trtl.verifyLayers(wrapContext(context.TODO()))
		r.NoError(err)
		r.Equal(int(l2ID.Uint32()), int(alg.trtl.Verified.Uint32()))
	})

	t.Run("healing handoff", func(t *testing.T) {
		// test self-healing: self-healing can verify layers that are stuck for specific reasons, i.e., where local and
		// global opinion differ (or local opinion is missing).
		alg.trtl.Hdist = 1
		alg.trtl.Zdist = 1
		alg.trtl.ConfidenceParam = 1

		// add more votes in favor of l3 blocks
		alg.trtl.BallotOpinionsByLayer[l4ID][l4Ballots[1].ID()] = l4Votes
		alg.trtl.BallotOpinionsByLayer[l4ID][l4Ballots[2].ID()] = l4Votes
		l5Votes := Opinion{
			l2Blocks[0].ID(): support,
			l2Blocks[1].ID(): support,
			l2Blocks[2].ID(): support,
			l3Blocks[0].ID(): support,
			l3Blocks[1].ID(): support,
			l3Blocks[2].ID(): support,
			l4Blocks[0].ID(): support,
			l4Blocks[1].ID(): support,
			l4Blocks[2].ID(): support,
		}
		alg.trtl.BallotOpinionsByLayer[l5ID] = map[types.BallotID]Opinion{
			l5Ballots[0].ID(): l5Votes,
			l5Ballots[1].ID(): l5Votes,
			l5Ballots[2].ID(): l5Votes,
		}
		l6Votes := Opinion{
			l2Blocks[0].ID(): support,
			l2Blocks[1].ID(): support,
			l2Blocks[2].ID(): support,
			l3Blocks[0].ID(): support,
			l3Blocks[1].ID(): support,
			l3Blocks[2].ID(): support,
			l4Blocks[0].ID(): support,
			l4Blocks[1].ID(): support,
			l4Blocks[2].ID(): support,
			l5Blocks[0].ID(): support,
			l5Blocks[1].ID(): support,
			l5Blocks[2].ID(): support,
		}
		alg.trtl.BallotOpinionsByLayer[l6ID] = map[types.BallotID]Opinion{
			l6Ballots[0].ID(): l6Votes,
			l6Ballots[1].ID(): l6Votes,
			l6Ballots[2].ID(): l6Votes,
		}

		// simulate a layer that's older than the LayerCutoff, and older than Zdist+ConfidenceParam, but not verified,
		// that has no contextually valid blocks in the database. this should trigger self-healing.

		// perform some surgery: erase just enough good blocks data so that ordinary tortoise verification fails for
		// one layer, triggering self-healing, but leave enough good blocks data so that ordinary tortoise can
		// subsequently verify a later layer after self-healing has finished. this works because self-healing does not
		// rely on local data, including the set of good blocks.
		delete(alg.trtl.GoodBallotsIndex, l4Ballots[0].ID())
		delete(alg.trtl.GoodBallotsIndex, l4Ballots[1].ID())
		delete(alg.trtl.GoodBallotsIndex, l4Ballots[2].ID())
		delete(alg.trtl.GoodBallotsIndex, l5Ballots[0].ID())

		// self-healing should advance verification two steps, over the
		// previously stuck layer (l3) and the following layer (l4) since it's old enough, then hand control back to the
		// ordinary verifying tortoise which should continue and verify l5.
		alg.trtl.Last = l4ID.Add(alg.trtl.Hdist + 1)
		alg.trtl.Processed = l4ID.Add(alg.trtl.Hdist + 1)
		r.NoError(alg.trtl.verifyLayers(wrapContext(context.TODO())))
		r.Equal(int(l5ID.Uint32()), int(alg.trtl.Verified.Uint32()))
	})
}

func checkVerifiedLayer(t *testing.T, trtl *turtle, layerID types.LayerID) {
	require.Equal(t, int(layerID.Uint32()), int(trtl.Verified.Uint32()), "got unexpected value for last verified layer")
}

func TestHealing(t *testing.T) {
	r := require.New(t)

	mdb := getInMemMesh(t)
	alg := defaultAlgorithm(t, mdb)
	atxdb := getAtxDB()
	atxdb.storeEpochWeight(uint64(defaultTestLayerSize))
	alg.trtl.atxdb = atxdb

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockBeacons := smocks.NewMockBeaconGetter(ctrl)
	alg.trtl.beacons = mockBeacons

	goodBeacon := randomBytes(t, 32)
	mockBeacons.EXPECT().GetBeacon(gomock.Any()).Return(goodBeacon, nil).AnyTimes()

	l0ID := types.GetEffectiveGenesis()
	l1ID := l0ID.Add(1)
	l2ID := l1ID.Add(1)

	// don't attempt to heal recent layers
	t.Run("don't heal recent layers", func(t *testing.T) {
		checkVerifiedLayer(t, alg.trtl, l0ID)

		// while bootstrapping there should be no healing
		alg.trtl.heal(wrapContext(context.TODO()), l2ID)
		checkVerifiedLayer(t, alg.trtl, l0ID)

		// later, healing should not occur on layers not at least Hdist back
		alg.trtl.Last = types.NewLayerID(alg.trtl.Hdist + 1)
		alg.trtl.heal(wrapContext(context.TODO()), l2ID)
		checkVerifiedLayer(t, alg.trtl, l0ID)
	})

	alg.trtl.Last = l0ID
	atxdb = getAtxDB()
	atxdb.storeEpochWeight(uint64(defaultTestLayerSize))
	alg.trtl.atxdb = atxdb
	l1 := makeLayerWithBeacon(t, l1ID, alg.trtl, goodBeacon, defaultTestLayerSize, defaultTestLayerSize, atxdb, mdb, mdb.LayerBlockIds)
	l2 := makeLayerWithBeacon(t, l2ID, alg.trtl, goodBeacon, defaultTestLayerSize, defaultTestLayerSize, atxdb, mdb, mdb.LayerBlockIds)

	// healing should work even when there is no local opinion on a layer (i.e., no output vector, while waiting
	// for hare results)
	t.Run("does not depend on local opinion", func(t *testing.T) {
		checkVerifiedLayer(t, alg.trtl, l0ID)
		r.NoError(alg.trtl.HandleIncomingLayer(context.TODO(), l1ID))
		r.NoError(alg.trtl.HandleIncomingLayer(context.TODO(), l2ID))

		// reducing hdist will allow verification to start happening
		alg.trtl.Hdist = 1
		// alg.trtl.Last = l2ID
		alg.trtl.heal(wrapContext(context.TODO()), l2ID)
		checkVerifiedLayer(t, alg.trtl, l1ID)
	})

	l3ID := l2ID.Add(1)

	// can heal when global and local opinion differ
	t.Run("local and global opinions differ", func(t *testing.T) {
		checkVerifiedLayer(t, alg.trtl, l1ID)

		// store input vector for already-processed layers
		var l1BlockIDs, l2BlockIDs []types.BlockID
		for _, block := range l1.Blocks() {
			l1BlockIDs = append(l1BlockIDs, block.ID())
		}
		for _, block := range l2.Blocks() {
			l2BlockIDs = append(l2BlockIDs, block.ID())
		}
		require.NoError(t, mdb.SaveLayerInputVectorByID(context.TODO(), l1ID, l1BlockIDs))
		require.NoError(t, mdb.SaveLayerInputVectorByID(context.TODO(), l2ID, l2BlockIDs))

		// then create and process one more new layer
		// prevent base ballot from referencing earlier (approved) layers
		alg.trtl.Last = l0ID
		makeLayerWithBeacon(t, l3ID, alg.trtl, goodBeacon, defaultTestLayerSize, defaultTestLayerSize, atxdb, mdb, mdb.LayerBlockIds)
		require.NoError(t, alg.trtl.HandleIncomingLayer(context.TODO(), l3ID))

		// make sure local opinion supports L2
		localOpinionVec, err := alg.trtl.getLocalOpinion(wrapContext(context.TODO()), l2ID)
		require.NoError(t, err)
		for _, bid := range l2BlockIDs {
			r.Contains(localOpinionVec, bid)
			r.Equal(support, localOpinionVec[bid])
		}

		alg.trtl.heal(wrapContext(context.TODO()), l3ID)
		checkVerifiedLayer(t, alg.trtl, l2ID)

		// make sure contextual validity is updated
		for _, bid := range l2BlockIDs {
			valid, err := mdb.ContextualValidity(bid)
			r.NoError(err)
			// global opinion should be against all the blocks in this layer since blocks in subsequent
			// layers don't vote for them
			r.False(valid)
		}
	})

	l4ID := l3ID.Add(1)

	// healing should count votes of non-good blocks
	t.Run("counts votes of non-good blocks", func(t *testing.T) {
		checkVerifiedLayer(t, alg.trtl, l2ID)

		// create and process several more layers
		// but don't save layer input vectors, so local opinion is abstain
		makeLayerWithBeacon(t, l4ID, alg.trtl, goodBeacon, defaultTestLayerSize, defaultTestLayerSize, atxdb, mdb, mdb.LayerBlockIds)
		require.NoError(t, alg.trtl.HandleIncomingLayer(context.TODO(), l4ID))

		// delete good blocks data
		oldGoodBallotsIndex := alg.trtl.GoodBallotsIndex
		alg.trtl.GoodBallotsIndex = make(map[types.BallotID]bool, 0)

		alg.trtl.heal(wrapContext(context.TODO()), l4ID)
		checkVerifiedLayer(t, alg.trtl, l3ID)
		alg.trtl.GoodBallotsIndex = oldGoodBallotsIndex
	})

	l5ID := l4ID.Add(1)
	l6ID := l5ID.Add(1)
	l7ID := l6ID.Add(1)

	// healing should count votes of non-good blocks
	t.Run("counts votes of bad-beacon blocks", func(t *testing.T) {
		checkVerifiedLayer(t, alg.trtl, l3ID)
		badBeacon := randomBytes(t, 32)

		// create and process several more layers with the wrong beacon.
		// but don't save layer input vectors, so local opinion is abstain
		makeLayerWithBeacon(t, l5ID, alg.trtl, badBeacon, defaultTestLayerSize, defaultTestLayerSize, atxdb, mdb, mdb.LayerBlockIds)
		makeLayerWithBeacon(t, l6ID, alg.trtl, badBeacon, defaultTestLayerSize, defaultTestLayerSize, atxdb, mdb, mdb.LayerBlockIds)
		makeLayerWithBeacon(t, l7ID, alg.trtl, badBeacon, defaultTestLayerSize, defaultTestLayerSize, atxdb, mdb, mdb.LayerBlockIds)
		require.NoError(t, alg.trtl.HandleIncomingLayer(context.TODO(), l5ID))
		require.NoError(t, alg.trtl.HandleIncomingLayer(context.TODO(), l6ID))
		require.NoError(t, alg.trtl.HandleIncomingLayer(context.TODO(), l7ID))
		checkVerifiedLayer(t, alg.trtl, l3ID)

		// delete good blocks data
		alg.trtl.GoodBallotsIndex = make(map[types.BallotID]bool, 0)

		alg.trtl.Last = l7ID.Add(types.GetLayersPerEpoch() * 2)
		alg.trtl.Hdist = 0
		alg.trtl.heal(wrapContext(context.TODO()), l7ID)
		checkVerifiedLayer(t, alg.trtl, l6ID)
	})
}

// can heal when half of votes are missing (doesn't meet threshold)
// this requires waiting an epoch or two before the active set size is reduced enough to cross the threshold
// see https://github.com/spacemeshos/go-spacemesh/issues/2497 for an idea about making this faster.
func TestHealingAfterPartition(t *testing.T) {
	mdb := getInMemMesh(t)
	atxdb := getAtxDB()
	alg := defaultAlgorithm(t, mdb)
	alg.trtl.atxdb = atxdb
	l0ID := types.GetEffectiveGenesis()

	// use a larger number of blocks per layer to give us more scope for testing
	const goodLayerSize = defaultTestLayerSize * 10
	alg.trtl.LayerSize = goodLayerSize
	atxdb.storeEpochWeight(uint64(goodLayerSize))

	// create several good layers
	makeAndProcessLayer(t, l0ID.Add(1), alg.trtl, goodLayerSize, goodLayerSize, atxdb, mdb, mdb.LayerBlockIds)
	makeAndProcessLayer(t, l0ID.Add(2), alg.trtl, goodLayerSize, goodLayerSize, atxdb, mdb, mdb.LayerBlockIds)
	makeAndProcessLayer(t, l0ID.Add(3), alg.trtl, goodLayerSize, goodLayerSize, atxdb, mdb, mdb.LayerBlockIds)
	makeAndProcessLayer(t, l0ID.Add(4), alg.trtl, goodLayerSize, goodLayerSize, atxdb, mdb, mdb.LayerBlockIds)
	makeAndProcessLayer(t, l0ID.Add(5), alg.trtl, goodLayerSize, goodLayerSize, atxdb, mdb, mdb.LayerBlockIds)
	checkVerifiedLayer(t, alg.trtl, l0ID.Add(4))

	// create a few layers with half the number of blocks
	makeAndProcessLayer(t, l0ID.Add(6), alg.trtl, goodLayerSize, goodLayerSize/2, atxdb, mdb, mdb.LayerBlockIds)
	makeAndProcessLayer(t, l0ID.Add(7), alg.trtl, goodLayerSize, goodLayerSize/2, atxdb, mdb, mdb.LayerBlockIds)
	makeAndProcessLayer(t, l0ID.Add(8), alg.trtl, goodLayerSize, goodLayerSize/2, atxdb, mdb, mdb.LayerBlockIds)

	// verification should fail, global opinion should be abstain since not enough votes
	checkVerifiedLayer(t, alg.trtl, l0ID.Add(4))

	// once we start receiving full layers again, verification should restart immediately. this scenario doesn't
	// actually require healing, since local and global opinions are the same, and the threshold is just > 1/2.
	makeAndProcessLayer(t, l0ID.Add(9), alg.trtl, goodLayerSize, goodLayerSize, atxdb, mdb, mdb.LayerBlockIds)
	checkVerifiedLayer(t, alg.trtl, l0ID.Add(8))

	// then we start receiving fewer blocks again
	for i := 0; types.NewLayerID(uint32(i)).Before(types.NewLayerID(alg.trtl.Zdist + alg.trtl.ConfidenceParam)); i++ {
		makeAndProcessLayer(t, l0ID.Add(10+uint32(i)), alg.trtl, goodLayerSize, goodLayerSize/2, atxdb, mdb, mdb.LayerBlockIds)
	}
	checkVerifiedLayer(t, alg.trtl, l0ID.Add(8))

	// healing would begin here, but without enough blocks to accumulate votes to cross the global threshold, we're
	// effectively stuck (until, in practice, active set size would be reduced in a following epoch and the remaining
	// miners would produce more blocks--this is tested in the app tests)
	firstHealedLayer := l0ID.Add(10 + uint32(alg.trtl.Zdist+alg.trtl.ConfidenceParam))
	makeAndProcessLayer(t, firstHealedLayer, alg.trtl, goodLayerSize, goodLayerSize/2, atxdb, mdb, mdb.LayerBlockIds)
	checkVerifiedLayer(t, alg.trtl, l0ID.Add(8))
}

func TestHealBalanceAttack(t *testing.T) {
	r := require.New(t)
	mdb := getInMemMesh(t)
	atxdb := getAtxDB()
	alg := defaultAlgorithm(t, mdb)
	alg.trtl.atxdb = atxdb
	l0ID := types.GetEffectiveGenesis()
	layerSize := 4
	l4ID := l0ID.Add(4)
	l5ID := l4ID.Add(1)

	// create several good layers and make sure verified layer advances
	makeAndProcessLayer(t, l0ID.Add(1), alg.trtl, layerSize, layerSize, atxdb, mdb, mdb.LayerBlockIds)
	makeAndProcessLayer(t, l0ID.Add(2), alg.trtl, layerSize, layerSize, atxdb, mdb, mdb.LayerBlockIds)
	makeAndProcessLayer(t, l0ID.Add(3), alg.trtl, layerSize, layerSize, atxdb, mdb, mdb.LayerBlockIds)
	makeAndProcessLayer(t, l4ID, alg.trtl, layerSize, layerSize, atxdb, mdb, mdb.LayerBlockIds)
	makeLayer(t, l5ID, alg.trtl, layerSize, layerSize, atxdb, mdb, mdb.LayerBlockIds)
	checkVerifiedLayer(t, alg.trtl, l0ID.Add(3))

	// everyone will agree on the validity of these blocks
	l5blockIDs, err := mdb.LayerBlockIds(l5ID)
	r.NoError(err)
	l5BaseBlock1 := l5blockIDs[0]
	l5BaseBlock2 := l5blockIDs[1]

	// opinions will differ about the validity of this block
	// note: we are NOT adding it to the layer input vector, so this node thinks the block is invalid (late)
	// this means that later blocks/layers with a base ballot or vote that supports this block will not be marked good
	l4lateblock := generateBlocks(t, l4ID, layerSize, 1, alg.BaseBallot, atxdb, 1)[0]
	r.NoError(mdb.AddBlock(l4lateblock))
	r.NoError(alg.trtl.HandleIncomingLayer(context.TODO(), l4ID))

	// this primes the block opinions for these blocks, without attempting to verify the previous layer
	r.NoError(alg.trtl.handleLayer(wrapContext(context.TODO()), l5ID))

	addOpinion := func(lid types.LayerID, ballot types.BallotID, block types.BlockID, vector sign) {
		alg.trtl.BallotOpinionsByLayer[lid][ballot][block] = vector
		alg.trtl.BallotLayer[ballot] = lid
	}

	// make one of the base ballots support it, and make one vote against it. note: these base ballots have already been
	// marked good. this means that blocks that use one of these as a base ballot will also be marked good (as long as
	// they don't add explicit exception votes for or against the late block).
	addOpinion(l5ID, types.BallotID(l5BaseBlock1), l4lateblock.ID(), support)
	addOpinion(l5ID, types.BallotID(l5BaseBlock2), l4lateblock.ID(), against)
	addOpinion(l5ID, types.BallotID(l5blockIDs[2]), l4lateblock.ID(), support)
	addOpinion(l5ID, types.BallotID(l5blockIDs[3]), l4lateblock.ID(), against)

	// now process l5
	r.NoError(mdb.SaveLayerInputVectorByID(context.TODO(), l5ID, l5blockIDs))
	r.NoError(alg.trtl.verifyLayers(wrapContext(context.TODO())))
	checkVerifiedLayer(t, alg.trtl, l0ID.Add(3))

	// we can trick calculateExceptions into not adding explicit exception votes for or against this block by
	// making it think we've already evicted its layer
	// basically, this is a shortcut: it lets us be lazy and just use our tortoise instance's calculateExceptions
	// method to fill in the exceptions for later layers, but not make every new block vote for or against the
	// late block (based on local opinion). without this, we'd need to override that method, too, and manually
	// generate the exceptions list.
	// TODO: but, eventually, when it's old enough, we do need exceptions to be added so healing can happen
	alg.trtl.LastEvicted = l4ID

	counter := 0
	bbp := func(context.Context) (types.BallotID, [][]types.BlockID, error) {
		defer func() { counter++ }()

		// half the time, return a base ballot that supports the late block
		// half the time, return one that does not support it
		var baseBlockID types.BlockID
		if counter%2 == 0 {
			baseBlockID = l5BaseBlock1
		} else {
			baseBlockID = l5BaseBlock2
		}

		baseBallotID := types.BallotID(baseBlockID)
		evm, err := alg.trtl.calculateExceptions(wrapContext(context.TODO()), alg.trtl.Last, l5ID, alg.trtl.BallotOpinionsByLayer[l5ID][baseBallotID])
		r.NoError(err)
		return baseBallotID, [][]types.BlockID{
			blockMapToArray(evm[0]),
			blockMapToArray(evm[1]),
			blockMapToArray(evm[2]),
		}, nil
	}

	// create a series of layers, each with half of blocks supporting and half against this block
	healingDistance := alg.trtl.Zdist + alg.trtl.ConfidenceParam

	// check our assumptions: local opinion will be disregarded after Hdist layers, which is also when healing
	// should kick in
	r.Equal(int(alg.trtl.Hdist), int(healingDistance))
	lastUnhealedLayer := l0ID.Add(6 + uint32(healingDistance))

	// note: a single coinflip will do it. it's only needed once, for one layer before the candidate layer that
	// first counts votes in order to determine the local opinion on the layer with the late block. once the local
	// opinion has been established, blocks will immediately begin explicitly voting for or against the block, and
	// the local opinion will no longer be abstain.
	mdb.RecordCoinflip(context.TODO(), lastUnhealedLayer.Sub(1), true)

	// after healing begins, we need a few more layers until the global opinion of the block passes the threshold
	finalLayer := lastUnhealedLayer.Add(9)

	for layerID := l0ID.Add(6); !layerID.After(finalLayer); layerID = layerID.Add(1) {
		// allow exceptions to be added again after this distance
		if layerID == lastUnhealedLayer {
			alg.trtl.LastEvicted = l0ID.Add(3)
		}

		// half of blocks use a base ballot that supports the late block
		// half use a base ballot that doesn't support it
		for j := 0; j < 2; j++ {
			blocks := generateBlocks(t, layerID, layerSize, layerSize/2, bbp, atxdb, 1)
			for _, block := range blocks {
				r.NoError(mdb.AddBlock(block))
			}
		}

		blockIDs, err := mdb.LayerBlockIds(layerID)
		r.NoError(err)
		r.NoError(mdb.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDs))
		r.NoError(alg.trtl.HandleIncomingLayer(context.TODO(), layerID))
	}
	checkVerifiedLayer(t, alg.trtl, finalLayer.Sub(1))

	// layer validity should match recent coinflip value
	valid, err := mdb.ContextualValidity(l4lateblock.ID())
	r.NoError(err)
	r.Equal(true, valid)
}

func TestCalculateOpinionWithThreshold(t *testing.T) {
	for _, tc := range []struct {
		desc      string
		expect    sign
		vote      *big.Float
		threshold *big.Rat
		weight    *big.Float
	}{
		{
			desc:      "Support",
			expect:    support,
			vote:      big.NewFloat(6),
			threshold: big.NewRat(1, 2),
			weight:    big.NewFloat(10),
		},
		{
			desc:      "Abstain",
			expect:    abstain,
			vote:      big.NewFloat(3),
			threshold: big.NewRat(1, 2),
			weight:    big.NewFloat(10),
		},
		{
			desc:      "AbstainZero",
			expect:    abstain,
			vote:      big.NewFloat(0),
			threshold: big.NewRat(1, 2),
			weight:    big.NewFloat(10),
		},
		{
			desc:      "Against",
			expect:    against,
			vote:      big.NewFloat(-6),
			threshold: big.NewRat(1, 2),
			weight:    big.NewFloat(10),
		},
		{
			desc:      "ComplexSupport",
			expect:    support,
			vote:      big.NewFloat(121),
			threshold: big.NewRat(60, 100),
			weight:    big.NewFloat(200),
		},
		{
			desc:      "ComplexAbstain",
			expect:    abstain,
			vote:      big.NewFloat(120),
			threshold: big.NewRat(60, 100),
			weight:    big.NewFloat(200),
		},
		{
			desc:      "ComplexAbstain2",
			expect:    abstain,
			vote:      big.NewFloat(-120),
			threshold: big.NewRat(60, 100),
			weight:    big.NewFloat(200),
		},
		{
			desc:      "ComplexAgainst",
			expect:    against,
			vote:      big.NewFloat(-121),
			threshold: big.NewRat(60, 100),
			weight:    big.NewFloat(200),
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.expect,
				calculateOpinionWithThreshold(logtest.New(t), tc.vote, tc.threshold, tc.weight))
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
		lastVerified = layerID.Sub(1)
		checkVerifiedLayer(t, alg1.trtl, lastVerified)
		checkVerifiedLayer(t, alg2.trtl, lastVerified)

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
			checkVerifiedLayer(t, alg2.trtl, lastVerified)
		}

		// minority tortoise begins healing
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

			// majority tortoise is unaffected, minority tortoise begins to heal but its verifying tortoise
			// is still stuck
			checkVerifiedLayer(t, alg1.trtl, layerID.Sub(1))
			checkVerifiedLayer(t, alg2.trtl, lastVerified.Add(uint32(i+1)))
		}

		// TODO: finish adding support for reorgs. minority tortoise should complete healing and successfully
		//   hand off back to verifying tortoise.
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

		// both nodes are healed
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
				epochWeights = map[types.EpochID]uint64{}
				first        = tc.target.Add(1).GetEpoch()
			)
			atxdb.EXPECT().GetEpochWeight(gomock.Any()).DoAndReturn(func(eid types.EpochID) (uint64, []types.ATXID, error) {
				pos := eid - first
				if len(tc.totals) <= int(pos) {
					return 0, nil, fmt.Errorf("invalid test: have only %d weights, want position %d", len(tc.totals), pos)
				}
				return tc.totals[pos], nil, nil
			}).AnyTimes()
			weight, err := computeExpectedVoteWeight(atxdb, epochWeights, tc.target, tc.last)
			require.NoError(t, err)
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

func randomBytes(tb testing.TB, size int) []byte {
	tb.Helper()
	data := make([]byte, size)
	_, err := rand.Read(data)
	require.NoError(tb, err)
	return data
}

func randomBlock(tb testing.TB, lyrID types.LayerID, beacon []byte, refBlockID *types.BlockID) *types.Block {
	tb.Helper()
	block := types.NewExistingBlock(lyrID, randomBytes(tb, 4), nil)
	block.TortoiseBeacon = beacon
	block.RefBlock = refBlockID
	return block
}

func TestBallotHasGoodBeacon(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	layerID := types.GetEffectiveGenesis().Add(1)
	epochBeacon := randomBytes(t, 32)
	ballot := randomBlock(t, layerID, epochBeacon, nil).ToBallot()

	mockBeacons := smocks.NewMockBeaconGetter(ctrl)
	trtl := defaultTurtle(t)
	trtl.beacons = mockBeacons

	logger := logtest.New(t)
	// good beacon
	mockBeacons.EXPECT().GetBeacon(layerID.GetEpoch()).Return(epochBeacon, nil).Times(1)
	assert.True(t, trtl.ballotHasGoodBeacon(ballot, logger))

	// bad beacon
	beacon := randomBytes(t, 32)
	require.NotEqual(t, epochBeacon, beacon)
	mockBeacons.EXPECT().GetBeacon(layerID.GetEpoch()).Return(beacon, nil).Times(1)
	assert.False(t, trtl.ballotHasGoodBeacon(ballot, logger))

	// ask a bad beacon again won't cause a lookup since it's cached
	assert.False(t, trtl.ballotHasGoodBeacon(ballot, logger))
}

func TestGetBallotBeacon(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	layerID := types.GetEffectiveGenesis().Add(1)
	beacon := randomBytes(t, 32)
	refBlock := randomBlock(t, layerID, beacon, nil)
	refBlockID := refBlock.ID()
	ballot := randomBlock(t, layerID, nil, &refBlockID).ToBallot()

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
	epochBeacon := randomBytes(t, 32)
	goodBallot := randomBlock(t, layerID, epochBeacon, nil).ToBallot()
	badBeacon := randomBytes(t, 32)
	require.NotEqual(t, epochBeacon, badBeacon)
	badBallot := randomBlock(t, layerID, badBeacon, nil).ToBallot()

	mockBeacons := smocks.NewMockBeaconGetter(ctrl)
	trtl := defaultTurtle(t)
	trtl.beacons = mockBeacons

	trtl.BallotLayer[goodBallot.ID()] = layerID
	trtl.BallotLayer[badBallot.ID()] = layerID

	logger := logtest.New(t)
	// cause the bad beacon block to be cached
	mockBeacons.EXPECT().GetBeacon(layerID.GetEpoch()).Return(epochBeacon, nil).Times(2)
	assert.True(t, trtl.ballotHasGoodBeacon(goodBallot, logger))
	assert.False(t, trtl.ballotHasGoodBeacon(badBallot, logger))

	// we don't count votes in bad beacon block for badBeaconVoteDelays layers
	trtl.Last = layerID
	i := uint32(1)
	for ; i <= defaultVoteDelays; i++ {
		filter := trtl.ballotFilterForHealing(logger)
		assert.True(t, filter(goodBallot.ID()))
		assert.False(t, filter(badBallot.ID()))
	}
	// now we count the bad beacon block's votes
	trtl.Last = layerID.Add(10)
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
	tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg))

	var last, verified types.LayerID
	// turn GenLayers into on-demand generator, so that later case can be placed as a step
	for _, lid := range sim.GenLayers(s,
		sim.WithSequence(30),
		sim.WithSequence(1, sim.WithVoteGenerator(
			outOfWindowBaseBallot(1, int(cfg.WindowSize)*2),
		)),
		sim.WithSequence(1),
	) {
		last = lid
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, lid)
		require.Equal(t, last.Sub(1), verified)
	}
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
			tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg))

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
		hdist = 3
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

	// test that by voting honestly node can switch to verifying tortoise from this condition
	//
	// NOTE(dshulyak) this is about weight required to recover from split-votes.
	// on 7th layer we have enough weight to heal genesis + 5 and all layers afterwards.
	// consider figuring out some arithmetic for it.
	for i := 0; i < 7; i++ {
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
	cfg.WindowSize = 1
	cfg.Hdist = 1 // for eviction
	cfg.Zdist = 1

	var (
		tortoise       = tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))
		last, verified types.LayerID
		genesis        = types.GetEffectiveGenesis()
	)
	for _, last = range sim.GenLayers(s,
		sim.WithSequence(3),
	) {
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(1), verified)

	// change local opinion for verified last.Sub(1) to be different from blocks opinion
	unsupported := map[types.BlockID]struct{}{}
	for lid := genesis.Add(1); !lid.After(last); lid = lid.Add(1) {
		bids, err := s.GetState(0).MeshDB.LayerBlockIds(lid)
		require.NoError(t, err)
		require.NoError(t, s.GetState(0).MeshDB.SaveContextualValidity(bids[0], lid, false))
		require.NoError(t, s.GetState(0).MeshDB.SaveLayerInputVectorByID(ctx, lid, bids[1:]))
		unsupported[bids[0]] = struct{}{}
	}
	base, exceptions, _ := tortoise.BaseBallot(ctx)
	ensureBallotLayerWithin(t, s.GetState(0).MeshDB, base, genesis.Add(1), genesis.Add(1))
	require.Empty(t, exceptions[0])
	require.Empty(t, exceptions[2])
	require.Len(t, exceptions[1], (size-1)*int(last.Difference(genesis)))
	for _, bid := range exceptions[1] {
		require.NotContains(t, unsupported, bid)
	}

	// because we are comparing with local opinion only within sliding window
	// we will never find inconsistencies for the leftmost block in sliding window
	// and it will not include against votes explicitly, as you can see above
	delete(tortoise.trtl.BallotOpinionsByLayer, genesis.Add(1))
	base, exceptions, _ = tortoise.BaseBallot(ctx)
	ensureBallotLayerWithin(t, s.GetState(0).MeshDB, base, last, last)
	require.Empty(t, exceptions[2])
	require.Len(t, exceptions[0], 2)
	for _, bid := range exceptions[0] {
		ensureBlockLayerWithin(t, s.GetState(0).MeshDB, bid, genesis.Add(1), last.Sub(1))
		require.Contains(t, unsupported, bid)
	}
	require.Len(t, exceptions[1], size-1)
	for _, bid := range exceptions[1] {
		require.NotContains(t, unsupported, bid)
	}
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
		{
			desc: "SupportFromCountedVotes",
			seqs: []sim.Sequence{
				sim.WithSequence(hdist+2, sim.WithoutInputVector()),
			},
			lid:      genesis.Add(1),
			expected: support,
		},
		{
			desc: "AbstainFromCountedVotes",
			seqs: []sim.Sequence{
				sim.WithSequence(1),
				sim.WithSequence(hdist+2,
					sim.WithoutInputVector(),
					sim.WithVoteGenerator(splitVoting(size)),
				),
			},
			lid:      genesis.Add(2),
			expected: abstain,
		},
		// TODO(dshulyak) figure out sequence that will allow to test against from counted votes
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

			local, err := tortoise.trtl.computeLocalOpinion(wrapContext(ctx), tc.lid)
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

				weights = map[types.BallotID]*big.Float{}

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
	cfg.Hdist = 2
	cfg.Zdist = 2
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
