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

	bMocks "github.com/spacemeshos/go-spacemesh/blocks/mocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
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
		return *types.EmptyBallotID
	}
	return types.BallotID(types.CalcHash32(b).ToHash20())
}

const (
	defaultTestLayerSize  = 3
	defaultTestWindowSize = 30
)

var (
	defaultTestHdist           = config.DefaultConfig().Hdist
	defaultTestZdist           = config.DefaultConfig().Zdist
	defaultVoteDelays          = config.DefaultConfig().LayersPerEpoch
	defaultTestGlobalThreshold = big.NewRat(6, 10)
	defaultTestLocalThreshold  = big.NewRat(2, 10)
	defaultTestRerunInterval   = time.Hour
	defaultTestConfidenceParam = config.DefaultConfig().ConfidenceParam
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
		globalOpinion := calculateOpinionWithThreshold(trtl.logger, sum, trtl.GlobalThreshold, trtl.AvgLayerSize, 1)
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
	trtl.AvgLayerSize = uint32(blocksPerLayer)
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

var (
	atxHeader = makeAtxHeaderWithWeight(1)
	atx       = &types.ActivationTx{InnerActivationTx: &types.InnerActivationTx{ActivationTxHeader: atxHeader}}
)

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

	// for now just create a single ATX for all of the blocks with a weight of one
	atx.CalcAndSetID()
	require.NoError(t, atxdb.StoreAtx(layerID.GetEpoch(), atx))

	for i := 0; i < blocksPerLayer; i++ {
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
	t.Run("many good layers", func(t *testing.T) {
		s := sim.New(sim.WithLayerSize(size))
		s.Setup()

		ctx := context.Background()
		cfg := defaultConfig(t, s.State.MeshDB, s.State.AtxDB)
		cfg.LayerSize = size
		tortoise := NewVerifyingTortoise(ctx, cfg)

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
		cfg := defaultConfig(t, s.State.MeshDB, s.State.AtxDB)
		cfg.LayerSize = size
		tortoise := NewVerifyingTortoise(ctx, cfg)

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
		vote := abstain
		numLayersToFullyHeal := 0
		for i := 0; vote == abstain; i++ {
			// fast-forward to the point where Zdist is past, we've stopped waiting for hare results for the bad
			// layers and started voting against them, and we've accumulated votes for ConfidenceParam layers
			netVote := vec{Against: uint64(i * size)}
			// Zdist + ConfidenceParam (+ a margin of one due to the math) layers have already passed, so that's our
			// delta
			vote = calculateOpinionWithThreshold(logtest.New(t), netVote, cfg.GlobalThreshold, size, uint32(i+1)+cfg.Zdist+cfg.ConfidenceParam)
			// safety cutoff
			if i > 100 {
				panic("failed to accumulate enough votes")
			}
			numLayersToFullyHeal++
		}
		for _, lid := range sim.GenLayers(s,
			sim.WithSequence(numLayersToFullyHeal),
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
		cfg := defaultConfig(t, s.State.MeshDB, s.State.AtxDB)
		cfg.LayerSize = size
		tortoise := NewVerifyingTortoise(ctx, cfg)

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
	trtl.AvgLayerSize = uint32(blocksPerLayer)
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

	// for now just create a single ATX for all of the blocks with a constant weight
	atxHeader := makeAtxHeaderWithWeight(weight)
	atx := &types.ActivationTx{InnerActivationTx: &types.InnerActivationTx{ActivationTxHeader: atxHeader}}
	atx.CalcAndSetID()
	require.NoError(t, atxdb.StoreAtx(l.GetEpoch(), atx))

	for i := 0; i < n; i++ {
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
	cfg := defaultConfig(t, s.State.MeshDB, s.State.AtxDB)
	cfg.LayerSize = size
	tortoise := NewVerifyingTortoise(ctx, cfg)
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
	cfg := defaultConfig(t, mdb, getAtxDB())
	cfg.ATXDB = atxdb
	alg := NewVerifyingTortoise(context.TODO(), cfg)

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
	alg2 := NewVerifyingTortoise(context.TODO(), cfg)
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

func defaultTurtle(tb testing.TB) *turtle {
	mdb := getInMemMesh(tb)
	ctrl := gomock.NewController(tb)
	mockBeacons := bMocks.NewMockBeaconGetter(ctrl)
	mockBeacons.EXPECT().GetBeacon(gomock.Any()).Return(nil, nil).AnyTimes()
	return newTurtle(
		logtest.New(tb),
		database.NewMemDatabase(),
		mdb,
		getAtxDB(),
		mockBeacons,
		defaultTestHdist,
		defaultTestZdist,
		defaultTestConfidenceParam,
		defaultTestWindowSize,
		defaultTestLayerSize,
		defaultTestGlobalThreshold,
		defaultTestLocalThreshold,
		defaultVoteDelays,
	)
}

func TestCloneTurtle(t *testing.T) {
	r := require.New(t)
	trtl := defaultTurtle(t)
	trtl.AvgLayerSize++              // make sure defaults aren't being read
	trtl.Last = types.NewLayerID(10) // state should not be cloned
	trtl2 := trtl.cloneTurtleParams()
	r.Equal(trtl.bdp, trtl2.bdp)
	r.Equal(trtl.Hdist, trtl2.Hdist)
	r.Equal(trtl.Zdist, trtl2.Zdist)
	r.Equal(trtl.ConfidenceParam, trtl2.ConfidenceParam)
	r.Equal(trtl.WindowSize, trtl2.WindowSize)
	r.Equal(trtl.AvgLayerSize, trtl2.AvgLayerSize)
	r.Equal(trtl.badBeaconVoteDelayLayers, trtl2.badBeaconVoteDelayLayers)
	r.Equal(trtl.GlobalThreshold, trtl2.GlobalThreshold)
	r.Equal(trtl.LocalThreshold, trtl2.LocalThreshold)
	r.Equal(trtl.RerunInterval, trtl2.RerunInterval)
	r.NotEqual(trtl.Last, trtl2.Last)
}

func defaultConfig(tb testing.TB, mdb *mesh.DB, atxdb atxDataProvider) Config {
	tb.Helper()
	ctrl := gomock.NewController(tb)
	mockBeacons := bMocks.NewMockBeaconGetter(ctrl)
	mockBeacons.EXPECT().GetBeacon(gomock.Any()).Return(nil, nil).AnyTimes()
	return Config{
		LayerSize:                defaultTestLayerSize,
		Database:                 database.NewMemDatabase(),
		MeshDatabase:             mdb,
		Beacons:                  mockBeacons,
		ATXDB:                    atxdb,
		Hdist:                    defaultTestHdist,
		Zdist:                    defaultTestZdist,
		ConfidenceParam:          defaultTestConfidenceParam,
		WindowSize:               defaultTestWindowSize,
		BadBeaconVoteDelayLayers: defaultVoteDelays,
		GlobalThreshold:          defaultTestGlobalThreshold,
		LocalThreshold:           defaultTestLocalThreshold,
		RerunInterval:            defaultTestRerunInterval,
		Log:                      logtest.New(tb),
	}
}

func defaultAlgorithm(t *testing.T, mdb *mesh.DB) *ThreadSafeVerifyingTortoise {
	return NewVerifyingTortoise(context.TODO(), defaultConfig(t, mdb, getAtxDB()))
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

	// malicious (conflicting) voting pattern
	l2Ballots[0].BaseBallot = types.BallotID(mesh.GenesisBlock().ID())
	l2Ballots[0].ForDiff = []types.BlockID{l1Blocks[1].ID()}
	l2Ballots[0].AgainstDiff = l2Ballots[0].ForDiff
	err := alg.trtl.processBallot(context.TODO(), l2Ballots[0])
	r.Error(err)
	r.Contains(err.Error(), errstrConflictingVotes)

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
		alg.trtl.BallotLayer[ballot.ID()] = ballot.Layer()
	}
	alg.trtl.BlockLayer[l2Blocks[0].ID()] = l2Blocks[0].LayerIndex
	alg.trtl.BallotLayer[l2Ballots[0].ID()] = l2Ballots[0].Layer()
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

func TestVerifyLayers(t *testing.T) {
	r := require.New(t)

	mdb := getInMemMesh(t)
	atxdb := getAtxDB()
	alg := defaultAlgorithm(t, mdb)
	alg.trtl.atxdb = atxdb
	l1ID := types.GetEffectiveGenesis().Add(1)
	l2ID := l1ID.Add(1)
	l2Blocks := generateBlocks(t, l2ID, defaultTestLayerSize, alg.BaseBlock, atxdb, 1)
	l3ID := l2ID.Add(1)
	l3Blocks := generateBlocks(t, l3ID, defaultTestLayerSize, alg.BaseBlock, atxdb, 1)
	l4ID := l3ID.Add(1)
	l4Blocks := generateBlocks(t, l4ID, defaultTestLayerSize, alg.BaseBlock, atxdb, 1)
	l5ID := l4ID.Add(1)
	l5Blocks := generateBlocks(t, l5ID, defaultTestLayerSize, alg.BaseBlock, atxdb, 1)
	l6ID := l5ID.Add(1)
	l6Blocks := generateBlocks(t, l6ID, defaultTestLayerSize, alg.BaseBlock, atxdb, 1)

	// layer missing in database
	alg.trtl.Last = l2ID
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
	alg.trtl.Last = l3ID

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
	alg.trtl.Last = l4ID
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
		r.NoError(alg.trtl.verifyLayers(wrapContext(context.TODO())))
		r.Equal(int(l5ID.Uint32()), int(alg.trtl.Verified.Uint32()))
	})
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

		// update t.Last and process block votes
		r.NoError(alg.trtl.handleLayer(wrapContext(context.TODO()), l1ID))

		// check
		sum, err := alg.trtl.sumVotesForBlock(context.TODO(), genesisBlockID, l1ID, filterPassAll)
		r.NoError(err)
		r.EqualValues(netWeight, sum.Support-sum.Against)
	}
}

func checkVerifiedLayer(t *testing.T, trtl *turtle, layerID types.LayerID) {
	require.Equal(t, int(layerID.Uint32()), int(trtl.Verified.Uint32()), "got unexpected value for last verified layer")
}

func TestHealing(t *testing.T) {
	r := require.New(t)

	mdb := getInMemMesh(t)
	alg := defaultAlgorithm(t, mdb)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockBeacons := bMocks.NewMockBeaconGetter(ctrl)
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
	atxdb := getAtxDB()
	alg.trtl.atxdb = atxdb
	l1 := makeLayerWithBeacon(t, l1ID, alg.trtl, goodBeacon, defaultTestLayerSize, atxdb, mdb, mdb.LayerBlockIds)
	l2 := makeLayerWithBeacon(t, l2ID, alg.trtl, goodBeacon, defaultTestLayerSize, atxdb, mdb, mdb.LayerBlockIds)

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
		// prevent base block from referencing earlier (approved) layers
		alg.trtl.Last = l0ID
		makeLayerWithBeacon(t, l3ID, alg.trtl, goodBeacon, defaultTestLayerSize, atxdb, mdb, mdb.LayerBlockIds)
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
		makeLayerWithBeacon(t, l4ID, alg.trtl, goodBeacon, defaultTestLayerSize, atxdb, mdb, mdb.LayerBlockIds)
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
		makeLayerWithBeacon(t, l5ID, alg.trtl, badBeacon, defaultTestLayerSize, atxdb, mdb, mdb.LayerBlockIds)
		makeLayerWithBeacon(t, l6ID, alg.trtl, badBeacon, defaultTestLayerSize, atxdb, mdb, mdb.LayerBlockIds)
		makeLayerWithBeacon(t, l7ID, alg.trtl, badBeacon, defaultTestLayerSize, atxdb, mdb, mdb.LayerBlockIds)
		require.NoError(t, alg.trtl.HandleIncomingLayer(context.TODO(), l5ID))
		require.NoError(t, alg.trtl.HandleIncomingLayer(context.TODO(), l6ID))
		require.NoError(t, alg.trtl.HandleIncomingLayer(context.TODO(), l7ID))
		checkVerifiedLayer(t, alg.trtl, l3ID)

		// delete good blocks data
		alg.trtl.GoodBallotsIndex = make(map[types.BallotID]bool, 0)

		alg.trtl.badBeaconVoteDelayLayers = 1
		alg.trtl.Hdist = 0
		alg.trtl.heal(wrapContext(context.TODO()), l7ID)
		checkVerifiedLayer(t, alg.trtl, l4ID)
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
	alg.trtl.AvgLayerSize = goodLayerSize

	// create several good layers
	makeAndProcessLayer(t, l0ID.Add(1), alg.trtl, goodLayerSize, atxdb, mdb, mdb.LayerBlockIds)
	makeAndProcessLayer(t, l0ID.Add(2), alg.trtl, goodLayerSize, atxdb, mdb, mdb.LayerBlockIds)
	makeAndProcessLayer(t, l0ID.Add(3), alg.trtl, goodLayerSize, atxdb, mdb, mdb.LayerBlockIds)
	makeAndProcessLayer(t, l0ID.Add(4), alg.trtl, goodLayerSize, atxdb, mdb, mdb.LayerBlockIds)
	makeAndProcessLayer(t, l0ID.Add(5), alg.trtl, goodLayerSize, atxdb, mdb, mdb.LayerBlockIds)
	checkVerifiedLayer(t, alg.trtl, l0ID.Add(4))

	// create a few layers with half the number of blocks
	makeAndProcessLayer(t, l0ID.Add(6), alg.trtl, goodLayerSize/2, atxdb, mdb, mdb.LayerBlockIds)
	makeAndProcessLayer(t, l0ID.Add(7), alg.trtl, goodLayerSize/2, atxdb, mdb, mdb.LayerBlockIds)
	makeAndProcessLayer(t, l0ID.Add(8), alg.trtl, goodLayerSize/2, atxdb, mdb, mdb.LayerBlockIds)

	// verification should fail, global opinion should be abstain since not enough votes
	checkVerifiedLayer(t, alg.trtl, l0ID.Add(4))

	// once we start receiving full layers again, verification should restart immediately. this scenario doesn't
	// actually require healing, since local and global opinions are the same, and the threshold is just > 1/2.
	makeAndProcessLayer(t, l0ID.Add(9), alg.trtl, goodLayerSize, atxdb, mdb, mdb.LayerBlockIds)
	checkVerifiedLayer(t, alg.trtl, l0ID.Add(8))

	// then we start receiving fewer blocks again
	for i := 0; types.NewLayerID(uint32(i)).Before(types.NewLayerID(alg.trtl.Zdist + alg.trtl.ConfidenceParam)); i++ {
		makeAndProcessLayer(t, l0ID.Add(10+uint32(i)), alg.trtl, goodLayerSize/2, atxdb, mdb, mdb.LayerBlockIds)
	}
	checkVerifiedLayer(t, alg.trtl, l0ID.Add(8))

	// healing would begin here, but without enough blocks to accumulate votes to cross the global threshold, we're
	// effectively stuck (until, in practice, active set size would be reduced in a following epoch and the remaining
	// miners would produce more blocks--this is tested in the app tests)
	firstHealedLayer := l0ID.Add(10 + uint32(alg.trtl.Zdist+alg.trtl.ConfidenceParam))
	makeAndProcessLayer(t, firstHealedLayer, alg.trtl, goodLayerSize/2, atxdb, mdb, mdb.LayerBlockIds)
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
	r := require.New(t)
	r.Equal(abstain, calculateOpinionWithThreshold(logtest.New(t), abstain, big.NewRat(1, 100), 1, 1))
	r.Equal(abstain, calculateOpinionWithThreshold(logtest.New(t), abstain, big.NewRat(1, 100), 10, 1))
	r.Equal(abstain, calculateOpinionWithThreshold(logtest.New(t), abstain, big.NewRat(1, 10), 1, 1))
	r.Equal(abstain, calculateOpinionWithThreshold(logtest.New(t), abstain, big.NewRat(1, 10), 1, 10))
	r.Equal(abstain, calculateOpinionWithThreshold(logtest.New(t), abstain, big.NewRat(1, 10), 1, 10))
	r.Equal(abstain, calculateOpinionWithThreshold(logtest.New(t), abstain, big.NewRat(1, 10), 10, 1))
	r.Equal(abstain, calculateOpinionWithThreshold(logtest.New(t), abstain, big.NewRat(1, 100), 1, 10))
	r.Equal(abstain, calculateOpinionWithThreshold(logtest.New(t), abstain, big.NewRat(1, 10), 10, 10))
	r.Equal(support, calculateOpinionWithThreshold(logtest.New(t), support.Multiply(10), big.NewRat(1, 100), 1, 1))
	r.Equal(support, calculateOpinionWithThreshold(logtest.New(t), support.Multiply(10), big.NewRat(1, 100), 10, 1))
	r.Equal(support, calculateOpinionWithThreshold(logtest.New(t), support.Multiply(10), big.NewRat(1, 10), 1, 1))
	r.Equal(support, calculateOpinionWithThreshold(logtest.New(t), support.Multiply(10), big.NewRat(1, 100), 1, 10))
	r.Equal(support, calculateOpinionWithThreshold(logtest.New(t), support.Multiply(10), big.NewRat(1, 100), 10, 10))
	r.Equal(support, calculateOpinionWithThreshold(logtest.New(t), support.Multiply(10), big.NewRat(1, 10), 10, 1))
	r.Equal(support, calculateOpinionWithThreshold(logtest.New(t), support.Multiply(10), big.NewRat(1, 10), 1, 10))
	r.Equal(abstain, calculateOpinionWithThreshold(logtest.New(t), support.Multiply(10), big.NewRat(1, 10), 10, 10))
	r.Equal(support, calculateOpinionWithThreshold(logtest.New(t), support.Multiply(11), big.NewRat(1, 10), 10, 10))
	r.Equal(against, calculateOpinionWithThreshold(logtest.New(t), against.Multiply(10), big.NewRat(1, 100), 1, 1))
	r.Equal(against, calculateOpinionWithThreshold(logtest.New(t), against.Multiply(10), big.NewRat(1, 100), 10, 1))
	r.Equal(against, calculateOpinionWithThreshold(logtest.New(t), against.Multiply(10), big.NewRat(1, 10), 1, 1))
	r.Equal(against, calculateOpinionWithThreshold(logtest.New(t), against.Multiply(10), big.NewRat(1, 100), 1, 10))
	r.Equal(against, calculateOpinionWithThreshold(logtest.New(t), against.Multiply(10), big.NewRat(1, 100), 10, 10))
	r.Equal(against, calculateOpinionWithThreshold(logtest.New(t), against.Multiply(10), big.NewRat(1, 10), 10, 1))
	r.Equal(against, calculateOpinionWithThreshold(logtest.New(t), against.Multiply(10), big.NewRat(1, 10), 1, 10))
	r.Equal(abstain, calculateOpinionWithThreshold(logtest.New(t), against.Multiply(10), big.NewRat(1, 10), 10, 10))
	r.Equal(against, calculateOpinionWithThreshold(logtest.New(t), against.Multiply(11), big.NewRat(1, 10), 10, 10))

	// a more realistic example
	r.Equal(support, calculateOpinionWithThreshold(logtest.New(t), vec{Support: 72, Against: 9}, defaultTestGlobalThreshold, defaultTestLayerSize, 3))
	r.Equal(abstain, calculateOpinionWithThreshold(logtest.New(t), vec{Support: 12, Against: 9}, defaultTestGlobalThreshold, defaultTestLayerSize, 3))
	r.Equal(against, calculateOpinionWithThreshold(logtest.New(t), vec{Support: 9, Against: 18}, defaultTestGlobalThreshold, defaultTestLayerSize, 3))
}

func TestMultiTortoise(t *testing.T) {
	r := require.New(t)

	t.Run("happy path", func(t *testing.T) {
		const layerSize = defaultTestLayerSize * 2

		mdb1 := getInMemMesh(t)
		atxdb1 := getAtxDB()
		alg1 := defaultAlgorithm(t, mdb1)
		alg1.trtl.atxdb = atxdb1
		alg1.trtl.AvgLayerSize = layerSize
		alg1.logger = alg1.logger.Named("trtl1")
		alg1.trtl.logger = alg1.logger

		mdb2 := getInMemMesh(t)
		atxdb2 := getAtxDB()
		alg2 := defaultAlgorithm(t, mdb2)
		alg2.trtl.atxdb = atxdb2
		alg2.trtl.AvgLayerSize = layerSize
		alg2.logger = alg2.logger.Named("trtl2")
		alg2.trtl.logger = alg2.logger

		makeAndProcessLayerMultiTortoise := func(layerID types.LayerID) {
			// simulate producing blocks in parallel
			blocksA := generateBlocks(t, layerID, layerSize, alg1.BaseBlock, atxdb1, 1)
			blocksB := generateBlocks(t, layerID, layerSize, alg2.BaseBlock, atxdb2, 1)

			// these will produce identical sets of blocks, so throw away half of each
			// (we could probably get away with just using, say, A's blocks, but to be more thorough we also want
			// to test the BaseBlock provider of each tortoise)
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
		alg1 := defaultAlgorithm(t, mdb1)
		alg1.trtl.atxdb = atxdb1
		alg1.trtl.AvgLayerSize = layerSize
		alg1.logger = alg1.logger.Named("trtl1")
		alg1.trtl.logger = alg1.logger

		mdb2 := getInMemMesh(t)
		atxdb2 := getAtxDB()
		alg2 := defaultAlgorithm(t, mdb2)
		alg2.trtl.atxdb = atxdb2
		alg2.trtl.AvgLayerSize = layerSize
		alg2.logger = alg2.logger.Named("trtl2")
		alg2.trtl.logger = alg2.logger

		makeBlocks := func(layerID types.LayerID) (blocksA, blocksB []*types.Block) {
			// simulate producing blocks in parallel
			blocksA = generateBlocks(t, layerID, layerSize, alg1.BaseBlock, atxdb1, 1)
			blocksB = generateBlocks(t, layerID, layerSize, alg2.BaseBlock, atxdb2, 1)

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

			// these blocks will be nearly identical but they will have different base blocks, since the set of blocks
			// for recent layers has been bifurcated, so we have to generate and store blocks separately to simulate
			// an ongoing partition.
			blocksA := generateBlocks(t, layerID, layerSize, alg1.BaseBlock, atxdb1, 1)
			forkBlocksA = append(forkBlocksA, blocksA...)
			var blockIDsA, blockIDsB []types.BlockID
			for _, block := range blocksA {
				blockIDsA = append(blockIDsA, block.ID())
				r.NoError(mdb1.AddBlock(block))
			}
			r.NoError(mdb1.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsA))
			alg1.HandleIncomingLayer(context.TODO(), layerID)

			blocksB := generateBlocks(t, layerID, layerSize, alg2.BaseBlock, atxdb2, 1)
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
		blocksA := generateBlocks(t, layerID, layerSize, alg1.BaseBlock, atxdb1, 1)
		forkBlocksA = append(forkBlocksA, blocksA...)
		var blockIDsA, blockIDsB []types.BlockID
		for _, block := range blocksA {
			blockIDsA = append(blockIDsA, block.ID())
			r.NoError(mdb1.AddBlock(block))
		}
		r.NoError(mdb1.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsA))
		alg1.HandleIncomingLayer(context.TODO(), layerID)

		blocksB := generateBlocks(t, layerID, layerSize, alg2.BaseBlock, atxdb2, 1)
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
		alg1 := defaultAlgorithm(t, mdb1)
		alg1.trtl.atxdb = atxdb1
		alg1.trtl.AvgLayerSize = layerSize
		alg1.logger = alg1.logger.Named("trtl1")
		alg1.trtl.logger = alg1.logger

		mdb2 := getInMemMesh(t)
		atxdb2 := getAtxDB()
		alg2 := defaultAlgorithm(t, mdb2)
		alg2.trtl.atxdb = atxdb2
		alg2.trtl.AvgLayerSize = layerSize
		alg2.logger = alg2.logger.Named("trtl2")
		alg2.trtl.logger = alg2.logger

		makeBlocks := func(layerID types.LayerID) (blocksA, blocksB []*types.Block) {
			// simulate producing blocks in parallel
			blocksA = generateBlocks(t, layerID, layerSize, alg1.BaseBlock, atxdb1, 1)
			blocksB = generateBlocks(t, layerID, layerSize, alg2.BaseBlock, atxdb2, 1)

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

			// these blocks will be nearly identical but they will have different base blocks, since the set of blocks
			// for recent layers has been bifurcated, so we have to generate and store blocks separately to simulate
			// an ongoing partition.
			blocksA := generateBlocks(t, layerID, layerSize, alg1.BaseBlock, atxdb1, 1)
			var blockIDsA, blockIDsB []types.BlockID
			for _, block := range blocksA {
				blockIDsA = append(blockIDsA, block.ID())
				r.NoError(mdb1.AddBlock(block))
			}
			r.NoError(mdb1.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsA))
			alg1.HandleIncomingLayer(context.TODO(), layerID)

			blocksB := generateBlocks(t, layerID, layerSize, alg2.BaseBlock, atxdb2, 1)
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
		blocksA := generateBlocks(t, layerID, layerSize, alg1.BaseBlock, atxdb1, 1)
		var blockIDsA, blockIDsB []types.BlockID
		for _, block := range blocksA {
			blockIDsA = append(blockIDsA, block.ID())
			r.NoError(mdb1.AddBlock(block))
		}
		r.NoError(mdb1.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsA))
		alg1.HandleIncomingLayer(context.TODO(), layerID)

		blocksB := generateBlocks(t, layerID, layerSize, alg2.BaseBlock, atxdb2, 1)
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
		alg1 := defaultAlgorithm(t, mdb1)
		alg1.trtl.atxdb = atxdb1
		alg1.trtl.AvgLayerSize = layerSize
		alg1.logger = alg1.logger.Named("trtl1")
		alg1.trtl.logger = alg1.logger

		mdb2 := getInMemMesh(t)
		atxdb2 := getAtxDB()
		alg2 := defaultAlgorithm(t, mdb2)
		alg2.trtl.atxdb = atxdb2
		alg2.trtl.AvgLayerSize = layerSize
		alg2.logger = alg2.logger.Named("trtl2")
		alg2.trtl.logger = alg2.logger

		mdb3 := getInMemMesh(t)
		atxdb3 := getAtxDB()
		alg3 := defaultAlgorithm(t, mdb3)
		alg3.trtl.atxdb = atxdb3
		alg3.trtl.AvgLayerSize = layerSize
		alg3.logger = alg3.logger.Named("trtl3")
		alg3.trtl.logger = alg3.logger

		makeBlocks := func(layerID types.LayerID) (blocksA, blocksB, blocksC []*types.Block) {
			// simulate producing blocks in parallel
			blocksA = generateBlocks(t, layerID, layerSize, alg1.BaseBlock, atxdb1, 1)
			blocksB = generateBlocks(t, layerID, layerSize, alg2.BaseBlock, atxdb2, 1)
			blocksC = generateBlocks(t, layerID, layerSize, alg3.BaseBlock, atxdb3, 1)

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

			// these blocks will be nearly identical but they will have different base blocks, since the set of blocks
			// for recent layers has been bifurcated, so we have to generate and store blocks separately to simulate
			// an ongoing partition.
			blocksA := generateBlocks(t, layerID, layerSize, alg1.BaseBlock, atxdb1, 1)
			var blockIDsA, blockIDsB, blockIDsC []types.BlockID
			for _, block := range blocksA {
				blockIDsA = append(blockIDsA, block.ID())
				r.NoError(mdb1.AddBlock(block))
			}
			r.NoError(mdb1.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsA))
			alg1.HandleIncomingLayer(context.TODO(), layerID)

			blocksB := generateBlocks(t, layerID, layerSize, alg2.BaseBlock, atxdb2, 1)
			for _, block := range blocksB {
				blockIDsB = append(blockIDsB, block.ID())
				r.NoError(mdb2.AddBlock(block))
			}
			r.NoError(mdb2.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsB))
			alg2.HandleIncomingLayer(context.TODO(), layerID)

			blocksC := generateBlocks(t, layerID, layerSize, alg3.BaseBlock, atxdb3, 1)
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
		blocksA := generateBlocks(t, layerID, layerSize, alg1.BaseBlock, atxdb1, 1)
		var blockIDsA, blockIDsB, blockIDsC []types.BlockID
		for _, block := range blocksA {
			blockIDsA = append(blockIDsA, block.ID())
			r.NoError(mdb1.AddBlock(block))
		}
		r.NoError(mdb1.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsA))
		alg1.HandleIncomingLayer(context.TODO(), layerID)

		blocksB := generateBlocks(t, layerID, layerSize, alg2.BaseBlock, atxdb2, 1)
		for _, block := range blocksB {
			blockIDsB = append(blockIDsB, block.ID())
			r.NoError(mdb2.AddBlock(block))
		}
		r.NoError(mdb2.SaveLayerInputVectorByID(context.TODO(), layerID, blockIDsB))
		alg2.HandleIncomingLayer(context.TODO(), layerID)

		blocksC := generateBlocks(t, layerID, layerSize, alg3.BaseBlock, atxdb3, 1)
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

func TestOutOfOrderLayersAreVerified(t *testing.T) {
	// increase layer size reduce test flakiness
	s := sim.New(sim.WithLayerSize(defaultTestLayerSize * 10))
	s.Setup()

	ctx := context.Background()
	cfg := defaultConfig(t, s.State.MeshDB, s.State.AtxDB)
	tortoise := NewVerifyingTortoise(ctx, cfg)

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
	cfg := defaultConfig(b, s.State.MeshDB, s.State.AtxDB)
	cfg.LayerSize = size

	var layers []types.LayerID
	for i := 0; i < 200; i++ {
		layers = append(layers, s.Next())
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tortoise := NewVerifyingTortoise(ctx, cfg)
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
	cfg := defaultConfig(b, s.State.MeshDB, s.State.AtxDB)
	cfg.LayerSize = size
	cfg.WindowSize = 100
	tortoise := NewVerifyingTortoise(ctx, cfg)

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

func TestBlockHasGoodBeacon(t *testing.T) {
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
func tortoiseVoting(tortoise *ThreadSafeVerifyingTortoise) sim.VotesGenerator {
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
	cfg := defaultConfig(t, s.State.MeshDB, s.State.AtxDB)
	tortoise := NewVerifyingTortoise(ctx, cfg)

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
	cfg := defaultConfig(t, s.State.MeshDB, s.State.AtxDB)
	cfg.LayerSize = size
	cfg.WindowSize = 10
	tortoise := NewVerifyingTortoise(ctx, cfg)

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
			cfg := defaultConfig(t, s.State.MeshDB, s.State.AtxDB)
			cfg.LayerSize = size
			cfg.WindowSize = tc.window
			cfg.Log = logtest.New(t)
			tortoise := NewVerifyingTortoise(ctx, cfg)

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
	s.Setup(sim.WithSetupUnitsRange(1, 1))

	ctx := context.Background()
	cfg := defaultConfig(t, s.State.MeshDB, s.State.AtxDB)
	cfg.LayerSize = size
	cfg.Hdist = hdist
	cfg.Zdist = hdist
	cfg.ConfidenceParam = 0

	var (
		tortoise       = NewVerifyingTortoise(ctx, cfg)
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
	cfg := defaultConfig(t, s.State.MeshDB, s.State.AtxDB)
	cfg.LayerSize = size
	cfg.WindowSize = 1
	cfg.Hdist = 1 // for eviction
	cfg.Zdist = 1

	var (
		tortoise       = NewVerifyingTortoise(ctx, cfg)
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
	delete(tortoise.trtl.BlockOpinionsByLayer, genesis.Add(1))
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
			cfg := defaultConfig(t, s.State.MeshDB, s.State.AtxDB)
			cfg.LayerSize = size
			cfg.Hdist = hdist
			cfg.Zdist = hdist
			tortoise := NewVerifyingTortoise(ctx, cfg)
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
