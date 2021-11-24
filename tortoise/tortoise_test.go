package tortoise

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	mrand "math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/blocks"
	bMocks "github.com/spacemeshos/go-spacemesh/blocks/mocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
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

func (madp *mockAtxDataProvider) GetEpochWeight(epochID types.EpochID) (uint64, []types.ATXID, error) {
	var (
		ids    []types.ATXID
		weight uint64
	)
	for atxid, atxheader := range madp.atxDB {
		ids = append(ids, atxid)
		weight += atxheader.GetWeight()
	}
	return weight, ids, nil
}

func getPersistentMesh(tb testing.TB) *mesh.DB {
	db, err := mesh.NewPersistentMeshDB(tb.TempDir(), 10, log.NewNop())
	require.NoError(tb, err)
	return db
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

func requireVote(t *testing.T, trtl *turtle, vote vec, blocks ...types.BlockID) {
	logger := logtest.New(t)
	for _, i := range blocks {
		sum := abstain
		blk, _ := trtl.bdp.GetBlock(i)

		wind := types.NewLayerID(0)
		if blk.LayerIndex.Uint32() > trtl.Hdist {
			wind = trtl.Last.Sub(trtl.Hdist)
		}
		if blk.LayerIndex.Before(wind) {
			continue
		}

		for l := trtl.Last; l.After(blk.LayerIndex); l = l.Sub(1) {
			logger.Info("counting votes of blocks in layer %v on %v (lyr: %v)",
				l,
				i.String(),
				blk.LayerIndex)

			for bid, opinionVote := range trtl.BallotOpinionsByLayer[l] {
				opinionVote, ok := opinionVote[i]
				if !ok {
					continue
				}

				weight, err := trtl.voteWeightByID(bid)
				require.NoError(t, err)
				sum = sum.Add(opinionVote.Multiply(weight))
			}
		}
		globalOpinion := calculateOpinionWithThreshold(trtl.logger, sum, trtl.GlobalThreshold, uint64(trtl.LayerSize))
		require.Equal(t, vote, globalOpinion, "test block %v expected vote %v but got %v", i, vote, sum)
	}
}

func TestHandleIncomingLayer(t *testing.T) {
	t.Run("HappyFlow", func(t *testing.T) {
		topLayer := types.GetEffectiveGenesis().Add(28)
		const avgPerLayer = 10
		// no negative votes, no abstain votes
		trtl, _, _ := turtleSanity(t, topLayer, avgPerLayer, 0, 0)
		require.Equal(t, int(topLayer.Sub(1).Uint32()), int(trtl.Verified.Uint32()))
		blkids := make([]types.BlockID, 0, avgPerLayer*topLayer.Uint32())
		for l := types.NewLayerID(0); l.Before(topLayer); l = l.Add(1) {
			lids, _ := trtl.bdp.LayerBlockIds(l)
			blkids = append(blkids, lids...)
		}
		requireVote(t, trtl, support, blkids...)
	})

	t.Run("VoteNegative", func(t *testing.T) {
		lyrsAfterGenesis := types.NewLayerID(10)
		layers := types.GetEffectiveGenesis().Add(lyrsAfterGenesis.Uint32())
		const avgPerLayer = 10
		voteNegative := 2
		// just a couple of negative votes
		trtl, negs, abs := turtleSanity(t, layers, avgPerLayer, voteNegative, 0)
		require.Equal(t, int(layers.Sub(1).Uint32()), int(trtl.Verified.Uint32()))
		poblkids := make([]types.BlockID, 0, avgPerLayer*int(layers.Uint32()))
		for l := types.NewLayerID(0); l.Before(layers); l = l.Add(1) {
			lids, _ := trtl.bdp.LayerBlockIds(l)
			for _, lid := range lids {
				if !inArr(lid, negs) {
					poblkids = append(poblkids, lid)
				}
			}
		}
		require.Len(t, abs, 0)
		require.Equal(t, len(negs), int(lyrsAfterGenesis.Sub(1).Uint32())*voteNegative) // don't count last layer because no one is voting on it

		// this test is called VoteNegative, but in fact we just abstain on blocks that we disagree with, unless
		// the base block explicitly supports them.
		// TODO: add a test for this, pending https://github.com/spacemeshos/go-spacemesh/issues/2424
		requireVote(t, trtl, abstain, negs...)
		requireVote(t, trtl, support, poblkids...)
	})

	t.Run("VoteAbstain", func(t *testing.T) {
		layers := types.NewLayerID(10)
		const avgPerLayer = 10
		trtl, _, abs := turtleSanity(t, layers, avgPerLayer, 0, 10)
		require.Equal(t, int(types.GetEffectiveGenesis().Uint32()), int(trtl.Verified.Uint32()), "when all votes abstain verification should stay at first layer")
		requireVote(t, trtl, abstain, abs...)
	})
}

func inArr(id types.BlockID, list []types.BlockID) bool {
	for _, l := range list {
		if l == id {
			return true
		}
	}
	return false
}

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
		makeAndProcessLayer(t, l, trtl, blocksPerLayer, atxdb, msh, inputVectorFn)
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

func makeAndProcessLayer(t *testing.T, l types.LayerID, trtl *turtle, blocksPerLayer int, atxdb atxDataWriter, msh blockDataWriter, inputVectorFn func(id types.LayerID) ([]types.BlockID, error)) {
	lyr := makeLayer(t, l, trtl, blocksPerLayer, atxdb, msh, inputVectorFn)
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

func makeLayer(t *testing.T, layerID types.LayerID, trtl *turtle, blocksPerLayer int, atxdb atxDataWriter, msh blockDataWriter, inputVectorFn func(id types.LayerID) ([]types.BlockID, error)) *types.Layer {
	return makeLayerWithBeacon(t, layerID, trtl, nil, blocksPerLayer, atxdb, msh, inputVectorFn)
}

func makeLayerWithBeacon(t *testing.T, layerID types.LayerID, trtl *turtle, beacon []byte, blocksPerLayer int, atxdb atxDataWriter, msh blockDataWriter, inputVectorFn func(id types.LayerID) ([]types.BlockID, error)) *types.Layer {
	logger := logtest.New(t)
	logger.Debug("======================== choosing base block for layer", layerID)
	if inputVectorFn != nil {
		oldInputVectorFn := msh.GetInputVectorBackupFunc()
		defer func() {
			msh.SetInputVectorBackupFunc(oldInputVectorFn)
		}()
		msh.SetInputVectorBackupFunc(inputVectorFn)
	}
	baseBlockID, lists, err := trtl.BaseBlock(context.TODO())
	require.NoError(t, err)
	logger.Debug("base block for layer", layerID, "is", baseBlockID)
	logger.Debug("exception lists for layer", layerID, "(against, support, neutral):", lists)
	lyr := types.NewLayer(layerID)

	for i := 0; i < blocksPerLayer; i++ {
		atxHeader := makeAtxHeaderWithWeight(1)
		atxHeader.NIPostChallenge.NodeID.Key = fmt.Sprintf("key-%d", i)
		atx := &types.ActivationTx{InnerActivationTx: &types.InnerActivationTx{ActivationTxHeader: atxHeader}}
		atx.CalcAndSetID()
		require.NoError(t, atxdb.StoreAtx(layerID.GetEpoch(), atx))

		blk := &types.Block{
			MiniBlock: types.MiniBlock{
				BlockHeader: types.BlockHeader{
					ATXID:      atxHeader.ID(),
					LayerIndex: layerID,
					Data:       []byte(strconv.Itoa(i)),
				},
				TxIDs:          nil,
				TortoiseBeacon: beacon,
			},
		}
		blk.BaseBlock = baseBlockID
		blk.AgainstDiff = lists[0]
		blk.ForDiff = lists[1]
		blk.NeutralDiff = lists[2]
		blk.Signature = signing.NewEdSigner().Sign(baseBlockID.Bytes())
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
		tortoise := tortoiseFromSimState(s.State, WithConfig(cfg), WithLogger(logtest.New(t)))

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
		tortoise := tortoiseFromSimState(s.State, WithConfig(cfg), WithLogger(logtest.New(t)))

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
		tortoise := tortoiseFromSimState(s.State, WithConfig(cfg))

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
		makeAndProcessLayer(t, l, trtl, blocksPerLayer, atxdb, msh, layerfuncs[l.Difference(types.GetEffectiveGenesis())-1])
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
	baseBlockProvider   func(context.Context) (types.BlockID, [][]types.BlockID, error)
	inputVectorProvider func(types.LayerID) ([]types.BlockID, error)
)

func generateBlocks(t *testing.T, l types.LayerID, n int, bbp baseBlockProvider, atxdb atxDataWriter, weight uint) (blocks []*types.Block) {
	logger := logtest.New(t)
	logger.Debug("======================== choosing base block for layer", l)
	b, lists, err := bbp(context.TODO())
	require.NoError(t, err)
	logger.Debug("the base block for layer", l, "is", b, ". exception lists:")
	logger.Debug("\tagainst\t", lists[0])
	logger.Debug("\tfor\t", lists[1])
	logger.Debug("\tneutral\t", lists[2])

	for i := 0; i < n; i++ {
		atxHeader := makeAtxHeaderWithWeight(weight)
		atxHeader.NIPostChallenge.NodeID.Key = fmt.Sprintf("key-%d", i)
		atx := &types.ActivationTx{InnerActivationTx: &types.InnerActivationTx{ActivationTxHeader: atxHeader}}
		atx.CalcAndSetID()
		require.NoError(t, atxdb.StoreAtx(l.GetEpoch(), atx))

		blk := &types.Block{
			MiniBlock: types.MiniBlock{
				BlockHeader: types.BlockHeader{
					ATXID:      atx.ID(),
					LayerIndex: l,
					Data:       []byte(strconv.Itoa(i)),
				},
				TxIDs: nil,
			},
		}
		blk.BaseBlock = b
		blk.AgainstDiff = lists[0]
		blk.ForDiff = lists[1]
		blk.NeutralDiff = lists[2]
		blk.Signature = signing.NewEdSigner().Sign(b.Bytes())
		blk.Initialize()
		blocks = append(blocks, blk)
		logger.Debug("generated block", blk.ID(), "in layer", l)
	}
	return
}

func generateBlock(t *testing.T, l types.LayerID, bbp baseBlockProvider, atxdb atxDataWriter, weight uint) *types.Block {
	b, lists, err := bbp(context.TODO())
	require.NoError(t, err)
	atxHeader := makeAtxHeaderWithWeight(weight)
	atx := &types.ActivationTx{InnerActivationTx: &types.InnerActivationTx{ActivationTxHeader: atxHeader}}
	atx.CalcAndSetID()
	require.NoError(t, atxdb.StoreAtx(l.GetEpoch(), atx))

	blk := &types.Block{
		MiniBlock: types.MiniBlock{
			BlockHeader: types.BlockHeader{
				ATXID:      atx.ID(),
				LayerIndex: l,
				Data:       []byte{0},
			},
			TxIDs: nil,
		},
	}
	blk.BaseBlock = b
	blk.AgainstDiff = lists[0]
	blk.ForDiff = lists[1]
	blk.NeutralDiff = lists[2]
	blk.Signature = signing.NewEdSigner().Sign(b.Bytes())
	blk.Initialize()
	return blk
}

func createTurtleLayer(t *testing.T, l types.LayerID, msh *mesh.DB, atxdb atxDataWriter, bbp baseBlockProvider, ivp inputVectorProvider, blocksPerLayer int) *types.Layer {
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
	for _, block := range generateBlocks(t, l, blocksPerLayer, bbp, atxdb, 1) {
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
	tortoise := tortoiseFromSimState(s.State, WithConfig(cfg))
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

	l1 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(1), mdb, atxdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l1))

	logger.With().Info("first is", l1.Index(), types.BlockIdsField(types.BlockIDs(l1.Blocks())))
	logger.With().Info("first bb is", l1.Index(), l1.Blocks()[0].BaseBlock, types.BlockIdsField(l1.Blocks()[0].ForDiff))

	alg.HandleIncomingLayer(context.TODO(), l1.Index())

	l2 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(2), mdb, atxdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l2))
	alg.HandleIncomingLayer(context.TODO(), l2.Index())

	require.Equal(t, int(types.GetEffectiveGenesis().Add(1).Uint32()), int(alg.LatestComplete().Uint32()))

	l3a := createTurtleLayer(t, types.GetEffectiveGenesis().Add(3), mdb, atxdb, alg.BaseBlock, getHareResults, defaultTestLayerSize+1)

	// this should fail as the blocks for this layer have not been added to the mesh yet
	alg.HandleIncomingLayer(context.TODO(), l3a.Index())
	require.Equal(t, int(types.GetEffectiveGenesis().Add(1).Uint32()), int(alg.LatestComplete().Uint32()))

	l3 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(3), mdb, atxdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l3))
	require.NoError(t, alg.rerun(context.TODO()))

	l4 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(4), mdb, atxdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l4))
	alg.HandleIncomingLayer(context.TODO(), l4.Index())
	require.Equal(t, int(types.GetEffectiveGenesis().Add(3).Uint32()), int(alg.LatestComplete().Uint32()), "wrong latest complete layer")
}

func TestPersistAndRecover(t *testing.T) {
	mdb := getPersistentMesh(t)

	getHareResults := mdb.LayerBlockIds

	mdb.InputVectorBackupFunc = getHareResults
	atxdb := getAtxDB()
	cfg := defaultTestConfig()
	db := database.NewMemDatabase()
	alg := New(db, mdb, atxdb, mockedBeacons(t), WithConfig(cfg))

	l1 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(1), mdb, atxdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l1))
	alg.HandleIncomingLayer(context.TODO(), l1.Index())
	require.NoError(t, alg.Persist(context.TODO()))

	l2 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(2), mdb, atxdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l2))
	alg.HandleIncomingLayer(context.TODO(), l2.Index())
	require.NoError(t, alg.Persist(context.TODO()))
	require.Equal(t, int(types.GetEffectiveGenesis().Add(1).Uint32()), int(alg.LatestComplete().Uint32()))

	// now recover
	alg2 := New(db, mdb, atxdb, mockedBeacons(t), WithConfig(cfg))
	require.Equal(t, alg.LatestComplete(), alg2.LatestComplete())
	require.Equal(t, alg.trtl.bdp, alg2.trtl.bdp)
	require.Equal(t, alg.trtl.LastEvicted, alg2.trtl.LastEvicted)
	require.Equal(t, alg.trtl.Verified, alg2.trtl.Verified)
	require.Equal(t, alg.trtl.WindowSize, alg2.trtl.WindowSize)
	require.Equal(t, alg.trtl.Last, alg2.trtl.Last)
	require.Equal(t, alg.trtl.Hdist, alg2.trtl.Hdist)
	require.Equal(t, alg.trtl.ConfidenceParam, alg2.trtl.ConfidenceParam)
	require.Equal(t, alg.trtl.Zdist, alg2.trtl.Zdist)
	require.Equal(t, alg.trtl.RerunInterval, alg2.trtl.RerunInterval)

	l3 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(3), mdb, atxdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l3))

	alg.HandleIncomingLayer(context.TODO(), l3.Index())
	alg2.HandleIncomingLayer(context.TODO(), l3.Index())

	// expect identical results
	require.Equal(t, int(types.GetEffectiveGenesis().Add(2).Uint32()), int(alg.LatestComplete().Uint32()), "wrong latest complete layer")
	require.Equal(t, int(types.GetEffectiveGenesis().Add(2).Uint32()), int(alg2.LatestComplete().Uint32()), "wrong latest complete layer")
}

func TestBaseBlock(t *testing.T) {
	r := require.New(t)
	mdb := getInMemMesh(t)
	atxdb := getAtxDB()
	alg := defaultAlgorithm(t, mdb)
	alg.trtl.atxdb = atxdb

	getHareResults := mdb.LayerBlockIds
	mdb.InputVectorBackupFunc = getHareResults

	l0 := mesh.GenesisLayer()
	expectBaseBlockLayer := func(layerID types.LayerID, numAgainst, numSupport, numNeutral int) {
		baseBlockID, exceptions, err := alg.BaseBlock(context.TODO())
		r.NoError(err)
		// expect no exceptions
		r.Len(exceptions, 3, "expected three vote exception arrays")
		r.Len(exceptions[0], numAgainst, "wrong number of against exception votes")
		r.Len(exceptions[1], numSupport, "wrong number of support exception votes")
		r.Len(exceptions[2], numNeutral, "wrong number of neutral exception votes")
		// expect a valid genesis base block
		baseBlock, err := alg.trtl.bdp.GetBlock(baseBlockID)
		r.NoError(err)
		r.Equal(layerID, baseBlock.Layer(), "base block is from wrong layer")
	}
	// it should support all genesis blocks
	expectBaseBlockLayer(l0.Index(), 0, len(mesh.GenesisLayer().Blocks()), 0)

	// add a couple of incoming layers and make sure the base block layer advances as well
	l1 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(1), mdb, atxdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l1))
	alg.HandleIncomingLayer(context.TODO(), l1.Index())
	expectBaseBlockLayer(l1.Index(), 0, defaultTestLayerSize, 0)

	l2 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(2), mdb, atxdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l2))
	alg.HandleIncomingLayer(context.TODO(), l2.Index())
	require.Equal(t, int(types.GetEffectiveGenesis().Add(1).Uint32()), int(alg.LatestComplete().Uint32()))
	expectBaseBlockLayer(l2.Index(), 0, defaultTestLayerSize, 0)

	// add a layer that's not in the mesh and make sure it does not advance
	l3 := createTurtleLayer(t, types.GetEffectiveGenesis().Add(3), mdb, atxdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	alg.HandleIncomingLayer(context.TODO(), l3.Index())
	require.Equal(t, int(types.GetEffectiveGenesis().Add(1).Uint32()), int(alg.LatestComplete().Uint32()))
	expectBaseBlockLayer(l2.Index(), 0, defaultTestLayerSize, 0)
}

func mockedBeacons(tb testing.TB) blocks.BeaconGetter {
	tb.Helper()

	ctrl := gomock.NewController(tb)
	mockBeacons := bMocks.NewMockBeaconGetter(ctrl)
	mockBeacons.EXPECT().GetBeacon(gomock.Any()).Return(nil, nil).AnyTimes()
	return mockBeacons
}

func defaultTurtle(tb testing.TB) *turtle {
	return newTurtle(
		logtest.New(tb),
		database.NewMemDatabase(),
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
	return New(database.NewMemDatabase(), state.MeshDB, state.AtxDB, state.Beacons, opts...)
}

func defaultAlgorithm(tb testing.TB, mdb *mesh.DB) *Tortoise {
	tb.Helper()
	return New(database.NewMemDatabase(), mdb, getAtxDB(), mockedBeacons(tb),
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
	blocks := generateBlocks(t, l1ID, 2, alg.BaseBlock, atxdb, 1)

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
	blocks := generateBlocks(t, l1ID, 3, alg.BaseBlock, atxdb, 1)
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
	l1 := createTurtleLayer(t, l1ID, mdb, atxdb, alg.BaseBlock, mdb.LayerBlockIds, defaultTestLayerSize)
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
		l2 := createTurtleLayer(t, l2ID, mdb, atxdb, alg.BaseBlock, mdb.LayerBlockIds, alg.trtl.MaxExceptions+1)
		for _, block := range l2.Blocks() {
			r.NoError(mdb.AddBlock(block))
		}
		l3 = createTurtleLayer(t, l3ID, mdb, atxdb, alg.BaseBlock, mdb.LayerBlockIds, defaultTestLayerSize)
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
		l4 := createTurtleLayer(t, l4ID, mdb, atxdb, alg.BaseBlock, mdb.LayerBlockIds, defaultTestLayerSize)
		for _, block := range l4.Blocks() {
			r.NoError(mdb.AddBlock(block))
		}
		l5ID := l4ID.Add(1)
		createTurtleLayer(t, l5ID, mdb, atxdb, alg.BaseBlock, mdb.LayerBlockIds, defaultTestLayerSize)
		alg.trtl.Last = l4ID
		votes, err = alg.trtl.calculateExceptions(wrapContext(context.TODO()), types.LayerID{}, l3ID, Opinion{})
		r.NoError(err)
		// expect votes FOR the blocks in the two intervening layers between the base block layer and the last layer
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
	l1Ballots := types.ToBallots(generateBlocks(t, l1ID, 3, alg.BaseBlock, atxdb, 1))

	// block marked good
	r.True(alg.trtl.determineBallotGoodness(wrapContext(context.TODO()), l1Ballots[0]))

	// base block not found
	randBallotID := randomBallotID()
	alg.trtl.GoodBallotsIndex[randBallotID] = false
	l1Ballots[1].BaseBallot = randBallotID
	r.False(alg.trtl.determineBallotGoodness(wrapContext(context.TODO()), l1Ballots[1]))

	// base block not good
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
	l1Ballots := types.ToBallots(generateBlocks(t, l1ID, 3, alg.BaseBlock, atxdb, 1))

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

func TestProcessBlock(t *testing.T) {
	r := require.New(t)
	mdb := getInMemMesh(t)
	atxdb := getAtxDB()
	alg := defaultAlgorithm(t, mdb)
	alg.trtl.atxdb = atxdb
	blocksPerLayer := 4
	baseBlockVoteWeight := uint(2)

	// blocks in this layer will use the genesis block as their base block
	l1ID := types.GetEffectiveGenesis().Add(1)
	l1Blocks := generateBlocks(t, l1ID, blocksPerLayer, alg.BaseBlock, atxdb, 1)
	l1Ballots := types.ToBallots(generateBlocks(t, l1ID, blocksPerLayer, alg.BaseBlock, atxdb, 1))
	// add one block from the layer
	r.NoError(mdb.AddBlock(l1Blocks[0]))
	ballotWithMissingBaseBallot := l1Ballots[0]
	ballotWithMissingBaseBallot.BaseBallot = l1Ballots[1].ID()

	// blocks in this layer will use a block from the previous layer as their base block
	baseBlockProviderFn := func(context.Context) (types.BlockID, [][]types.BlockID, error) {
		return l1Blocks[0].ID(), make([][]types.BlockID, blocksPerLayer), nil
	}
	l2ID := l1ID.Add(1)
	l2Blocks := generateBlocks(t, l2ID, blocksPerLayer, baseBlockProviderFn, atxdb, baseBlockVoteWeight)
	l2Ballots := types.ToBallots(l2Blocks)

	alg.trtl.BallotOpinionsByLayer[l2ID] = make(map[types.BallotID]Opinion, defaultTestLayerSize)
	alg.trtl.BallotOpinionsByLayer[l1ID] = make(map[types.BallotID]Opinion, defaultTestLayerSize)

	// add vote diffs: make sure that base block votes flow through, but that block votes override them, and that the
	// data structure is correctly updated, and that weights are calculated correctly

	// add base block to DB
	r.NoError(mdb.AddBlock(l2Blocks[0]))
	baseBlockProviderFn = func(context.Context) (types.BlockID, [][]types.BlockID, error) {
		return l2Blocks[0].ID(), make([][]types.BlockID, blocksPerLayer), nil
	}
	baseBallotOpinionVector := Opinion{
		l1Blocks[0].ID(): against.Multiply(uint64(baseBlockVoteWeight)), // disagrees with block below
		l1Blocks[1].ID(): support.Multiply(uint64(baseBlockVoteWeight)), // disagrees with block below
		l1Blocks[2].ID(): abstain,                                       // disagrees with block below
		l1Blocks[3].ID(): against.Multiply(uint64(baseBlockVoteWeight)), // agrees with block below
	}
	alg.trtl.BallotOpinionsByLayer[l2ID][l2Ballots[0].ID()] = baseBallotOpinionVector
	l3ID := l2ID.Add(1)
	blockVoteWeight := uint(3)
	l3Blocks := generateBlocks(t, l3ID, blocksPerLayer, baseBlockProviderFn, atxdb, blockVoteWeight)
	l3Ballots := types.ToBallots(l3Blocks)
	l3Ballots[0].AgainstDiff = []types.BlockID{
		l1Blocks[1].ID(),
	}
	l3Ballots[0].ForDiff = []types.BlockID{}
	l3Ballots[0].NeutralDiff = []types.BlockID{
		l1Blocks[0].ID(),
	}
	alg.trtl.BallotOpinionsByLayer[l3ID] = make(map[types.BallotID]Opinion, blocksPerLayer)
	// these must be in the mesh or we'll get an error when processing a block (l3Blocks[0])
	// with a base block (l2Blocks[0]) that contains an opinion on them
	for i, block := range l1Blocks[1:] {
		r.NoError(mdb.AddBlock(block))
		alg.trtl.BlockLayer[block.ID()] = block.LayerIndex
		ballot := l1Ballots[i]
		alg.trtl.BallotLayer[ballot.ID()] = ballot.LayerIndex
	}
	alg.trtl.BlockLayer[l2Blocks[0].ID()] = l2Blocks[0].LayerIndex
	alg.trtl.BallotLayer[l2Ballots[0].ID()] = l2Ballots[0].LayerIndex
	r.NoError(alg.trtl.processBallot(context.TODO(), l3Ballots[0]))
	expectedOpinionVector := Opinion{
		l1Blocks[0].ID(): abstain,                                   // from exception
		l1Blocks[1].ID(): against.Multiply(uint64(blockVoteWeight)), // from exception
		l1Blocks[2].ID(): abstain,                                   // from base block
		l1Blocks[3].ID(): against.Multiply(uint64(blockVoteWeight)), // from base block, reweighted
	}
	r.Equal(baseBallotOpinionVector, alg.trtl.BallotOpinionsByLayer[l2ID][l2Ballots[0].ID()])
	r.Equal(expectedOpinionVector, alg.trtl.BallotOpinionsByLayer[l3ID][l3Ballots[0].ID()])
}

func makeAtxHeaderWithWeight(weight uint) (mockAtxHeader *types.ActivationTxHeader) {
	mockAtxHeader = &types.ActivationTxHeader{NIPostChallenge: types.NIPostChallenge{NodeID: types.NodeID{Key: "fakekey"}}}
	mockAtxHeader.StartTick = 0
	mockAtxHeader.EndTick = 1
	mockAtxHeader.NumUnits = weight
	return
}

func TestVoteWeight(t *testing.T) {
	r := require.New(t)
	mdb := getInMemMesh(t)
	atxdb := getAtxDB()
	alg := defaultAlgorithm(t, mdb)
	alg.trtl.atxdb = atxdb
	totalSpace := 100
	atxdb.mockAtxHeader = makeAtxHeaderWithWeight(uint(totalSpace))
	someBlocks := types.ToBallots(generateBlocks(t, types.GetEffectiveGenesis().Add(1), 1, alg.BaseBlock, atxdb, 1))
	weight, err := alg.trtl.voteWeight(someBlocks[0])
	r.NoError(err)
	r.Equal(totalSpace, int(weight))
}

func TestVoteWeightInOpinion(t *testing.T) {
	r := require.New(t)
	mdb := getInMemMesh(t)
	atxdb := getAtxDB()
	alg := defaultAlgorithm(t, mdb)
	alg.trtl.atxdb = atxdb
	weight := uint(2)

	// add one base block that votes for (genesis) base block with weight > 1
	atxdb.mockAtxHeader = makeAtxHeaderWithWeight(weight)
	genesisBlockID := mesh.GenesisBlock().ID()
	l1ID := types.GetEffectiveGenesis().Add(1)
	makeAndProcessLayer(t, l1ID, alg.trtl, 1, atxdb, mdb, mdb.LayerBlockIds)
	layerBlockIDs, err := mdb.LayerBlockIds(l1ID)
	r.NoError(err)
	r.Len(layerBlockIDs, 1)
	blockID := layerBlockIDs[0]
	ballotID := types.BallotID(blockID)

	// make sure opinion is set correctly
	r.Equal(support.Multiply(uint64(weight)), alg.trtl.BallotOpinionsByLayer[l1ID][ballotID][genesisBlockID])

	// make sure the only exception added was for the base block itself
	l2 := makeLayer(t, l1ID, alg.trtl, 1, atxdb, mdb, mdb.LayerBlockIds)
	r.Len(l2.BlocksIDs(), 1)
	l2Ballot := l2.Blocks()[0].ToBallot()
	r.Len(l2Ballot.ForDiff, 1)
	r.Equal(blockID, l2Ballot.ForDiff[0])
}

func TestSumVotesForBlock(t *testing.T) {
	r := require.New(t)
	mdb := getInMemMesh(t)
	atxdb := getAtxDB()
	alg := defaultAlgorithm(t, mdb)
	alg.trtl.atxdb = atxdb

	// store a bunch of votes against a block
	l1ID := types.GetEffectiveGenesis().Add(1)
	l1Blocks := generateBlocks(t, l1ID, 4, alg.BaseBlock, atxdb, 1)
	for _, block := range l1Blocks {
		r.NoError(mdb.AddBlock(block))
	}
	blockWeReallyDislike := l1Blocks[0]
	blockWeReallyLike := l1Blocks[1]
	blockWeReallyDontCare := l1Blocks[2]
	blockWeNeverSaw := l1Blocks[3]
	l2ID := l1ID.Add(1)
	l2Blocks := generateBlocks(t, l2ID, 9, alg.BaseBlock, atxdb, 1)
	for _, block := range l2Blocks {
		r.NoError(mdb.AddBlock(block))
	}
	l2Ballots := types.ToBallots(l2Blocks)
	alg.trtl.BallotOpinionsByLayer[l2ID] = map[types.BallotID]Opinion{
		l2Ballots[0].ID(): {blockWeReallyDislike.ID(): against},
		l2Ballots[1].ID(): {blockWeReallyDislike.ID(): against},
		l2Ballots[2].ID(): {blockWeReallyDislike.ID(): against},
	}

	// test filter
	filterPassAll := func(types.BallotID) bool { return true }
	filterRejectAll := func(types.BallotID) bool { return false }

	// if we reject all blocks, we expect an abstain outcome
	alg.trtl.Last = l2ID
	sum, err := alg.trtl.sumVotesForBlock(context.TODO(), blockWeReallyDislike.ID(), l2ID, filterRejectAll)
	r.NoError(err)
	r.Equal(abstain, sum)

	// if we allow all blocks to vote, we expect an against outcome
	sum, err = alg.trtl.sumVotesForBlock(context.TODO(), blockWeReallyDislike.ID(), l2ID, filterPassAll)
	r.NoError(err)
	r.Equal(against.Multiply(3), sum)

	// add more blocks
	alg.trtl.BallotOpinionsByLayer[l2ID] = map[types.BallotID]Opinion{
		l2Ballots[0].ID(): {blockWeReallyDislike.ID(): against},
		l2Ballots[1].ID(): {blockWeReallyDislike.ID(): against},
		l2Ballots[2].ID(): {blockWeReallyDislike.ID(): against},
		l2Ballots[3].ID(): {blockWeReallyLike.ID(): support},
		l2Ballots[4].ID(): {blockWeReallyLike.ID(): support},
		l2Ballots[5].ID(): {blockWeReallyDontCare.ID(): abstain},
		l2Ballots[6].ID(): {},
		l2Ballots[7].ID(): {},
		l2Ballots[8].ID(): {},
	}
	// some blocks explicitly vote against, others have no opinion
	sum, err = alg.trtl.sumVotesForBlock(context.TODO(), blockWeReallyDislike.ID(), l2ID, filterPassAll)
	r.NoError(err)
	r.Equal(against.Multiply(9), sum)
	// some blocks vote for, others have no opinion
	sum, err = alg.trtl.sumVotesForBlock(context.TODO(), blockWeReallyLike.ID(), l2ID, filterPassAll)
	r.NoError(err)
	r.Equal(support.Multiply(2).Add(against.Multiply(7)), sum)
	// one block votes neutral, others have no opinion
	sum, err = alg.trtl.sumVotesForBlock(context.TODO(), blockWeReallyDontCare.ID(), l2ID, filterPassAll)
	r.NoError(err)
	r.Equal(abstain.Multiply(1).Add(against.Multiply(8)), sum)

	// vote missing: counts against
	sum, err = alg.trtl.sumVotesForBlock(context.TODO(), blockWeNeverSaw.ID(), l2ID, filterPassAll)
	r.NoError(err)
	r.Equal(against.Multiply(9), sum)
}

func TestSumWeightedVotesForBlock(t *testing.T) {
	r := require.New(t)
	mdb := getInMemMesh(t)
	atxdb := getAtxDB()
	alg := defaultAlgorithm(t, mdb)
	alg.trtl.atxdb = atxdb
	numBlocks := 5
	genesisBlockID := mesh.GenesisBlock().ID()
	l1ID := types.GetEffectiveGenesis().Add(1)
	filterPassAll := func(id types.BallotID) bool { return true }

	// use the same base block for all newly-created blocks
	b, lists, err := alg.BaseBlock(context.TODO())
	r.NoError(err)
	bbp := func(context.Context) (types.BlockID, [][]types.BlockID, error) {
		return b, lists, nil
	}

	// create several voting blocks with different weights
	netWeight := uint(0)
	for i := 0; i < numBlocks; i++ {
		thisWeight := uint(1) << i
		netWeight += thisWeight
		block := generateBlock(t, l1ID, bbp, atxdb, thisWeight)
		r.NoError(mdb.AddBlock(block))

		r.NoError(alg.trtl.HandleIncomingLayer(context.TODO(), l1ID))

		// check
		sum, err := alg.trtl.sumVotesForBlock(context.TODO(), genesisBlockID, l1ID, filterPassAll)
		r.NoError(err)
		r.EqualValues(netWeight, sum.Support-sum.Against)
	}
}

func checkVerifiedLayer(t *testing.T, trtl *turtle, layerID types.LayerID) {
	require.Equal(t, int(layerID.Uint32()), int(trtl.Verified.Uint32()), "got unexpected value for last verified layer")
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
	makeAndProcessLayer(t, l0ID.Add(1), alg.trtl, layerSize, atxdb, mdb, mdb.LayerBlockIds)
	makeAndProcessLayer(t, l0ID.Add(2), alg.trtl, layerSize, atxdb, mdb, mdb.LayerBlockIds)
	makeAndProcessLayer(t, l0ID.Add(3), alg.trtl, layerSize, atxdb, mdb, mdb.LayerBlockIds)
	makeAndProcessLayer(t, l4ID, alg.trtl, layerSize, atxdb, mdb, mdb.LayerBlockIds)
	makeLayer(t, l5ID, alg.trtl, layerSize, atxdb, mdb, mdb.LayerBlockIds)
	checkVerifiedLayer(t, alg.trtl, l0ID.Add(3))

	// everyone will agree on the validity of these blocks
	l5blockIDs, err := mdb.LayerBlockIds(l5ID)
	r.NoError(err)
	l5BaseBlock1 := l5blockIDs[0]
	l5BaseBlock2 := l5blockIDs[1]

	// opinions will differ about the validity of this block
	// note: we are NOT adding it to the layer input vector, so this node thinks the block is invalid (late)
	// this means that later blocks/layers with a base block or vote that supports this block will not be marked good
	l4lateblock := generateBlocks(t, l4ID, 1, alg.BaseBlock, atxdb, 1)[0]
	r.NoError(mdb.AddBlock(l4lateblock))
	r.NoError(alg.trtl.HandleIncomingLayer(context.TODO(), l4ID))

	// this primes the block opinions for these blocks, without attempting to verify the previous layer
	r.NoError(alg.trtl.handleLayer(wrapContext(context.TODO()), l5ID))

	addOpinion := func(lid types.LayerID, ballot types.BallotID, block types.BlockID, vector vec) {
		alg.trtl.BallotOpinionsByLayer[lid][ballot][block] = vector
		alg.trtl.BallotLayer[ballot] = lid
	}

	// make one of the base blocks support it, and make one vote against it. note: these base blocks have already been
	// marked good. this means that blocks that use one of these as a base block will also be marked good (as long as
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
	bbp := func(context.Context) (types.BlockID, [][]types.BlockID, error) {
		defer func() { counter++ }()

		// half the time, return a base block that supports the late block
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
		return baseBlockID, [][]types.BlockID{
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

		// half of blocks use a base block that supports the late block
		// half use a base block that doesn't support it
		for j := 0; j < 2; j++ {
			blocks := generateBlocks(t, layerID, layerSize/2, bbp, atxdb, 1)
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

func TestVectorArithmetic(t *testing.T) {
	r := require.New(t)
	r.Equal(abstain, abstain.Add(abstain))
	r.Equal(support, abstain.Add(support))
	r.Equal(support, support.Add(abstain))
	r.Equal(against, abstain.Add(against))
	r.Equal(against, against.Add(abstain))
	r.Equal(vec{Support: 1, Against: 1}, against.Add(support))
	r.Equal(abstain, simplifyVote(against.Add(support)))
	r.Equal(vec{Support: 1, Against: 1}, support.Add(against))
	r.Equal(abstain, simplifyVote(support.Add(against)))
	r.Equal(support, simplifyVote(support.Add(support)))
	r.Equal(against, simplifyVote(against.Add(against)))
	r.Equal(support, simplifyVote(vec{Support: 100, Against: 10}))
	r.Equal(against, simplifyVote(vec{Support: 10, Against: 100}))
	r.Equal(abstain, simplifyVote(abstain))
	r.Equal(abstain, abstain.Multiply(1))
	r.Equal(support, support.Multiply(1))
	r.Equal(against, against.Multiply(1))
	r.Equal(abstain, abstain.Multiply(0))
	r.Equal(abstain, support.Multiply(0))
	r.Equal(abstain, against.Multiply(0))
	r.Equal(support.Add(abstain), abstain.Add(support))
	r.Equal(against.Add(abstain), abstain.Add(against))
	r.Equal(support.Multiply(2), support.Add(support))
	r.Equal(against.Multiply(2), against.Add(against))

	// test wraparound
	bigVec := vec{Support: math.MaxUint64, Against: math.MaxUint64}
	r.NotPanics(func() { bigVec.Add(abstain) })
	r.NotPanics(func() { abstain.Add(bigVec) })
	r.PanicsWithError(errOverflow.Error(), func() { bigVec.Add(support) })
	r.PanicsWithError(errOverflow.Error(), func() { support.Add(bigVec) })
	r.NotPanics(func() { bigVec.Multiply(0) })
	r.NotPanics(func() { bigVec.Multiply(1) })
	r.PanicsWithError(errOverflow.Error(), func() { bigVec.Multiply(2) })
	r.NotPanics(func() { support.Multiply(math.MaxUint64) })
	r.PanicsWithError(errOverflow.Error(), func() { support.Add(support).Multiply(math.MaxUint64) })
}

func TestCalculateOpinionWithThreshold(t *testing.T) {
	for _, tc := range []struct {
		desc      string
		expect    vec
		vote      vec
		threshold *big.Rat
		weight    uint64
	}{
		{
			desc:      "Support",
			expect:    support,
			vote:      vec{Support: 6},
			threshold: big.NewRat(1, 2),
			weight:    10,
		},
		{
			desc:      "SupportDelta",
			expect:    support,
			vote:      vec{Support: 12, Against: 6},
			threshold: big.NewRat(1, 2),
			weight:    10,
		},
		{
			desc:      "Abstain",
			expect:    abstain,
			vote:      vec{Support: 5},
			threshold: big.NewRat(1, 2),
			weight:    10,
		},
		{
			desc:      "AbstainDelta",
			expect:    abstain,
			vote:      vec{Support: 11, Against: 6},
			threshold: big.NewRat(1, 2),
			weight:    10,
		},
		{
			desc:      "Against",
			expect:    against,
			vote:      vec{Against: 6},
			threshold: big.NewRat(1, 2),
			weight:    10,
		},
		{
			desc:      "AgainstDelta",
			expect:    against,
			vote:      vec{Support: 6, Against: 12},
			threshold: big.NewRat(1, 2),
			weight:    10,
		},
		{
			desc:      "ComplexSupport",
			expect:    support,
			vote:      vec{Support: 162, Against: 41},
			threshold: big.NewRat(60, 100),
			weight:    200,
		},
		{
			desc:      "ComplexAbstain",
			expect:    abstain,
			vote:      vec{Support: 162, Against: 42},
			threshold: big.NewRat(60, 100),
			weight:    200,
		},
		{
			desc:      "ComplexAbstain2",
			expect:    abstain,
			vote:      vec{Support: 42, Against: 162},
			threshold: big.NewRat(60, 100),
			weight:    200,
		},
		{
			desc:      "ComplexAgainst",
			expect:    against,
			vote:      vec{Support: 41, Against: 162},
			threshold: big.NewRat(60, 100),
			weight:    200,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.expect,
				calculateOpinionWithThreshold(logtest.New(t), tc.vote, tc.threshold, tc.weight))
		})
	}
}

func TestComputeExpectedWeight(t *testing.T) {
	genesis := types.GetEffectiveGenesis()
	layers := types.GetLayersPerEpoch()
	require.EqualValues(t, 4, layers, "expecting layers per epoch to be 4. adjust test if it will change")
	for _, tc := range []struct {
		desc         string
		target, last types.LayerID
		totals       []uint64 // total weights starting from (target, last]
		expect       uint64
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
			desc:   "ExpectZeroEpoch",
			target: genesis,
			last:   genesis.Add(8),
			totals: []uint64{10, 0},
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
			desc:   "MultipleCompleteEpochs",
			target: genesis,
			last:   genesis.Add(8),
			totals: []uint64{8, 12},
			expect: 20,
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
			require.Equal(t, int(tc.expect), int(weight))
		})
	}
}

func TestOutOfOrderLayersAreVerified(t *testing.T) {
	// increase layer size reduce test flakiness
	s := sim.New(sim.WithLayerSize(defaultTestLayerSize * 10))
	s.Setup()

	ctx := context.Background()
	cfg := defaultTestConfig()
	tortoise := tortoiseFromSimState(s.State, WithConfig(cfg))

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
		tortoise := tortoiseFromSimState(s.State, WithConfig(cfg))
		for _, lid := range layers {
			tortoise.HandleIncomingLayer(ctx, lid)
		}
	}
}

func BenchmarkTortoiseBaseBlockSelection(b *testing.B) {
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
	tortoise := tortoiseFromSimState(s.State, WithConfig(cfg))

	var last, verified types.LayerID
	for i := 0; i < 400; i++ {
		last = s.Next()
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(b, last.Sub(1), verified)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tortoise.BaseBlock(ctx)
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

	mockBeacons := bMocks.NewMockBeaconGetter(ctrl)
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

	mockBeacons := bMocks.NewMockBeaconGetter(ctrl)
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
	i := uint32(1)
	for ; i <= defaultVoteDelays; i++ {
		filter := trtl.ballotFilterForHealing(layerID.Sub(i), logger)
		assert.True(t, filter(goodBallot.ID()))
		assert.False(t, filter(badBallot.ID()))
	}
	// now we count the bad beacon block's votes
	filter := trtl.ballotFilterForHealing(layerID.Sub(i), logger)
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
	base := baseLayer.Blocks()[rng.Intn(len(baseLayer.Blocks()))]
	return sim.Voting{Base: base.ID(), Support: support}
}

// olderExceptions will vote for block older then base block.
func olderExceptions(rng *mrand.Rand, layers []*types.Layer, _ int) sim.Voting {
	if len(layers) < 2 {
		panic("need atleast 2 layers")
	}
	baseLayer := layers[len(layers)-1]
	base := baseLayer.Blocks()[rng.Intn(len(baseLayer.Blocks()))]
	voting := sim.Voting{Base: base.ID()}
	for _, layer := range layers[len(layers)-2:] {
		for _, bid := range layer.BlocksIDs() {
			voting.Support = append(voting.Support, bid)
		}
	}
	return voting
}

// outOfWindowBaseBlock creates VotesGenerator with a specific window.
// vote generator will produce one block that uses base block outside of the sliding window.
// NOTE that it will produce blocks as if it didn't knew about blocks from higher layers.
func outOfWindowBaseBlock(n, window int) sim.VotesGenerator {
	return func(rng *mrand.Rand, layers []*types.Layer, i int) sim.Voting {
		if i >= n {
			return sim.PerfectVoting(rng, layers, i)
		}
		li := len(layers) - window
		baseLayer := layers[li]
		base := baseLayer.Blocks()[rng.Intn(len(baseLayer.Blocks()))]
		return sim.Voting{Base: base.ID(), Support: layers[li].BlocksIDs()}
	}
}

// tortoiseVoting is for testing that protocol makes progress using heuristic that we are
// using for the network.
func tortoiseVoting(tortoise *Tortoise) sim.VotesGenerator {
	return func(rng *mrand.Rand, layers []*types.Layer, i int) sim.Voting {
		base, exceptions, err := tortoise.BaseBlock(context.Background())
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

func TestBaseBlockGenesis(t *testing.T) {
	ctx := context.Background()

	s := sim.New()
	cfg := defaultTestConfig()
	tortoise := tortoiseFromSimState(s.State, WithConfig(cfg))

	base, exceptions, err := tortoise.BaseBlock(ctx)
	require.NoError(t, err)
	require.Equal(t, exceptions, [][]types.BlockID{{}, {base}, {}})
	require.Equal(t, mesh.GenesisBlock().ID(), base)
}

func ensureBaseAndExceptionsFromLayer(tb testing.TB, lid types.LayerID, base types.BlockID, exceptions [][]types.BlockID, mdb blockDataProvider) {
	tb.Helper()

	block, err := mdb.GetBlock(base)
	require.NoError(tb, err)
	require.Equal(tb, lid, block.LayerIndex)
	require.Empty(tb, exceptions[0])
	require.Empty(tb, exceptions[2])
	for _, bid := range exceptions[1] {
		block, err := mdb.GetBlock(bid)
		require.NoError(tb, err)
		require.Equal(tb, lid, block.LayerIndex, "block=%s block layer=%s last=%s", block.ID(), block.LayerIndex, lid)
	}
}

func TestBaseBlockEvictedBlock(t *testing.T) {
	const size = 10
	s := sim.New(
		sim.WithLayerSize(size),
	)
	s.Setup()

	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.WindowSize = 10
	tortoise := tortoiseFromSimState(s.State, WithConfig(cfg))

	var last, verified types.LayerID
	// turn GenLayers into on-demand generator, so that later case can be placed as a step
	for _, lid := range sim.GenLayers(s,
		sim.WithSequence(30),
		sim.WithSequence(1, sim.WithVoteGenerator(
			outOfWindowBaseBlock(1, int(cfg.WindowSize)*2),
		)),
		sim.WithSequence(1),
	) {
		last = lid
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, lid)
		require.Equal(t, last.Sub(1), verified)
	}
	for i := 0; i < 10; i++ {
		base, exceptions, err := tortoise.BaseBlock(ctx)
		require.NoError(t, err)
		ensureBaseAndExceptionsFromLayer(t, last, base, exceptions, s.State.MeshDB)

		last = s.Next(sim.WithVoteGenerator(tortoiseVoting(tortoise)))
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
		require.Equal(t, last.Sub(1), verified)
	}
}

func TestBaseBlockPrioritization(t *testing.T) {
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
			tortoise := tortoiseFromSimState(s.State, WithConfig(cfg))

			for _, lid := range sim.GenLayers(s, tc.seqs...) {
				tortoise.HandleIncomingLayer(ctx, lid)
			}

			base, _, err := tortoise.BaseBlock(ctx)
			require.NoError(t, err)
			block, err := s.State.MeshDB.GetBlock(base)
			require.NoError(t, err)
			require.Equal(t, tc.expected, block.LayerIndex)
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
		return sim.Voting{Base: support[0], Support: support}
	}
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
		tortoise       = tortoiseFromSimState(s.State, WithConfig(cfg), WithLogger(logtest.New(t)))
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
	base, exceptions, err := tortoise.BaseBlock(ctx)
	require.NoError(t, err)
	// last block that is consistent with local opinion
	ensureBlockLayerWithin(t, s.State.MeshDB, base, genesis.Add(5), genesis.Add(5))

	against, support, neutral := exceptions[0], exceptions[1], exceptions[2]

	require.Empty(t, against, "implicit voting")
	require.Empty(t, neutral, "empty input vector")

	// support for all layers in range of (verified, last - hdist)
	for _, bid := range support {
		ensureBlockLayerWithin(t, s.State.MeshDB, bid, genesis.Add(5), last.Sub(hdist).Sub(1))
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

func TestVoteAgainstSupportedByBaseBlock(t *testing.T) {
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
		tortoise       = tortoiseFromSimState(s.State, WithConfig(cfg))
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
		bids, err := s.State.MeshDB.LayerBlockIds(lid)
		require.NoError(t, err)
		require.NoError(t, s.State.MeshDB.SaveContextualValidity(bids[0], lid, false))
		require.NoError(t, s.State.MeshDB.SaveLayerInputVectorByID(ctx, lid, bids[1:]))
		unsupported[bids[0]] = struct{}{}
	}
	base, exceptions, _ := tortoise.BaseBlock(ctx)
	ensureBlockLayerWithin(t, s.State.MeshDB, base, genesis.Add(1), genesis.Add(1))
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
	base, exceptions, _ = tortoise.BaseBlock(ctx)
	ensureBlockLayerWithin(t, s.State.MeshDB, base, last, last)
	require.Empty(t, exceptions[2])
	require.Len(t, exceptions[0], 2)
	for _, bid := range exceptions[0] {
		ensureBlockLayerWithin(t, s.State.MeshDB, bid, genesis.Add(1), last.Sub(1))
		require.Contains(t, unsupported, bid)
	}
	require.Len(t, exceptions[1], (size - 1))
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
		expected vec
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
			tortoise := tortoiseFromSimState(s.State, WithConfig(cfg))
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

func TestNetworkDoesNotRecoverFromFullPartition(t *testing.T) {
	const size = 10
	s1 := sim.New(
		sim.WithLayerSize(size),
	)
	s1.Setup()

	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Hdist = 2
	cfg.Zdist = 2
	cfg.ConfidenceParam = 0

	var (
		tortoise1                  = tortoiseFromSimState(s1.State, WithConfig(cfg))
		tortoise2                  = tortoiseFromSimState(s1.State, WithConfig(cfg))
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

	// FIXME needs improvement
	tortoise2.trtl.bdp = s2.State.MeshDB
	tortoise2.trtl.atxdb = s2.State.AtxDB
	tortoise2.trtl.beacons = s2.State.Beacons

	partitionStart := last
	for i := 0; i < int(types.GetLayersPerEpoch()); i++ {
		last = s1.Next()

		_, verified1, _ = tortoise1.HandleIncomingLayer(ctx, last)
		_, verified2, _ = tortoise2.HandleIncomingLayer(ctx, s2.Next())
	}
	require.Equal(t, last.Sub(1), verified1)
	require.Equal(t, last.Sub(1), verified2)

	// sync missing state
	// make enough progress so that blocks with other beacons are considered
	// and then do rerun
	partitionEnd := last
	s1.Merge(s2)

	for i := 0; i < int(cfg.BadBeaconVoteDelayLayers); i++ {
		last = s1.Next()
		_, verified1, _ = tortoise1.HandleIncomingLayer(ctx, last)
	}
	require.NoError(t, tortoise1.rerun(ctx))
	last = s1.Next()
	_, verified1, _ = tortoise1.HandleIncomingLayer(ctx, last)
	require.Equal(t, last.Sub(1), verified1)

	// succesfull test should verify that all blocks that were created in s2
	// during partition are contextually valid in s1 state.
	for lid := partitionStart.Add(1); lid.Before(partitionEnd); lid = lid.Add(1) {
		bids, err := s2.State.MeshDB.LayerBlockIds(lid)
		require.NoError(t, err)
		for _, bid := range bids {
			valid, err := s1.State.MeshDB.ContextualValidity(bid)
			require.NoError(t, err)
			assert.False(t, valid, "block %s at layer %s", bid, lid)
		}
	}
	// for some reason only blocks from last layer in the partition are valid
	bids, err := s2.State.MeshDB.LayerBlockIds(partitionEnd)
	require.NoError(t, err)
	for _, bid := range bids {
		valid, err := s1.State.MeshDB.ContextualValidity(bid)
		require.NoError(t, err)
		assert.True(t, valid, "block %s at layer %s", bid, partitionEnd)
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
	tortoise := tortoiseFromSimState(s.State, WithConfig(cfg), WithLogger(logtest.New(t)))

	var last, verified types.LayerID
	for _, last = range sim.GenLayers(s,
		sim.WithSequence(2),
		sim.WithSequence(1, sim.WithLayerSizeOverwrite(1)),
	) {
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(2), verified)
}
