package tortoise

import (
	"context"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/signing"
	"strconv"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/stretchr/testify/require"
)

func init() {
	types.SetLayersPerEpoch(4)
}

type meshWrapper struct {
	blockDataProvider
	inputVectorBackupFn      func(types.LayerID) ([]types.BlockID, error)
	saveContextualValidityFn func(types.BlockID, types.LayerID, bool) error
}

func (mw meshWrapper) SaveContextualValidity(bid types.BlockID, lid types.LayerID, valid bool) error {
	if mw.saveContextualValidityFn != nil {
		return mw.saveContextualValidityFn(bid, lid, valid)
	}
	return mw.blockDataProvider.SaveContextualValidity(bid, lid, valid)
}

func (mw meshWrapper) GetLayerInputVectorByID(lid types.LayerID) ([]types.BlockID, error) {
	if mw.inputVectorBackupFn != nil {
		return mw.inputVectorBackupFn(lid)
	}
	return mw.blockDataProvider.GetLayerInputVectorByID(lid)
}

func getPersistentMesh(t *testing.T) *mesh.DB {
	db, err := mesh.NewPersistentMeshDB(t.TempDir(), 10, log.NewDefault("ninja_tortoise").WithOptions(log.Nop))
	require.NoError(t, err)
	return db
}

func getInMemMesh() *mesh.DB {
	return mesh.NewMemMeshDB(log.NewDefault(""))
}

func addLayerToMesh(m *mesh.DB, layer *types.Layer) error {
	// add blocks to mDB
	for _, bl := range layer.Blocks() {
		if err := m.AddBlock(bl); err != nil {
			return err
		}
	}
	return nil
}

var (
	defaultTestLayerSize       = 3
	defaultTestHdist           = config.DefaultConfig().Hdist
	defaultTestZdist           = config.DefaultConfig().Zdist
	defaultTestWindowSize      = 30 // make test faster
	defaultTestGlobalThreshold = uint8(60)
	defaultTestLocalThreshold  = uint8(20)
	defaultTestRerunInterval   = time.Hour
	defaultTestConfidenceParam = config.DefaultConfig().ConfidenceParam
)

func requireVote(t *testing.T, trtl *turtle, vote vec, blocks ...types.BlockID) {
	for _, i := range blocks {
		sum := abstain
		blk, _ := trtl.bdp.GetBlock(i)

		for l := trtl.Last; l > blk.LayerIndex; l-- {
			trtl.logger.With().Debug("counting votes of blocks",
				l,
				i,
				log.FieldNamed("block_layer", blk.LayerIndex))

			for bid, opinionVote := range trtl.BlockOpinionsByLayer[l] {
				opinionVote, ok := opinionVote.BlockOpinions[i]
				if !ok {
					continue
				}

				//t.logger.Info("block %v is good and voting vote %v", vopinion.id, opinionVote)
				sum = sum.Add(opinionVote.Multiply(trtl.BlockWeight(bid, i)))
			}
		}
		globalOpinion := calculateOpinionWithThreshold(trtl.logger, sum, trtl.AvgLayerSize, trtl.GlobalThreshold, 1)
		require.Equal(t, vote, globalOpinion, "test block %v expected vote %v but got %v", i, vote, sum)
	}
}

func TestTurtle_HandleIncomingLayerHappyFlow(t *testing.T) {
	topLayer := types.GetEffectiveGenesis() + 28
	avgPerLayer := 10
	voteNegative := 0
	trtl, _, _ := turtleSanity(t, topLayer, avgPerLayer, voteNegative, 0)
	require.Equal(t, int(topLayer-1), int(trtl.Verified))
	blkids := make([]types.BlockID, 0, avgPerLayer*int(topLayer))
	for l := types.LayerID(0); l < topLayer; l++ {
		lids, _ := trtl.bdp.LayerBlockIds(l)
		blkids = append(blkids, lids...)
	}
	requireVote(t, trtl, support, blkids...)
}

func inArr(id types.BlockID, list []types.BlockID) bool {
	for _, l := range list {
		if l == id {
			return true
		}
	}
	return false
}

func TestTurtle_HandleIncomingLayer_VoteNegative(t *testing.T) {
	lyrsAfterGenesis := types.LayerID(10)
	layers := types.GetEffectiveGenesis() + lyrsAfterGenesis
	avgPerLayer := 10
	voteNegative := 2
	trtl, negs, abs := turtleSanity(t, layers, avgPerLayer, voteNegative, 0)
	require.Equal(t, int(layers-1), int(trtl.Verified))
	poblkids := make([]types.BlockID, 0, avgPerLayer*int(layers))
	for l := types.LayerID(0); l < layers; l++ {
		lids, _ := trtl.bdp.LayerBlockIds(l)
		for _, lid := range lids {
			if !inArr(lid, negs) {
				poblkids = append(poblkids, lid)
			}
		}
	}
	require.Len(t, abs, 0)
	require.Equal(t, len(negs), int(lyrsAfterGenesis-1)*voteNegative) // don't count last layer because no one is voting on it
	requireVote(t, trtl, against, negs...)
	requireVote(t, trtl, support, poblkids...)
}

func TestTurtle_HandleIncomingLayer_VoteAbstain(t *testing.T) {
	layers := types.LayerID(10)
	avgPerLayer := 10
	trtl, _, abs := turtleSanity(t, layers, avgPerLayer, 0, 10)
	require.Equal(t, int(types.GetEffectiveGenesis()), int(trtl.Verified), "when all votes abstain verification should stay at first layer")
	requireVote(t, trtl, abstain, abs...)
}

// voteNegative - the number of blocks to vote negative per layer
// voteAbstain - the number of layers to vote abstain because we always abstain on a whole layer
func turtleSanity(t *testing.T, numLayers types.LayerID, blocksPerLayer, voteNegative, voteAbstain int) (trtl *turtle, negative, abstain []types.BlockID) {
	msh := getInMemMesh()
	newlyrs := make(map[types.LayerID]struct{})

	inputVectorFn := func(l types.LayerID) (ids []types.BlockID, err error) {
		if l < mesh.GenesisLayer().Index() {
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

		//if voteNegative >= len(blks) {
		//	if !exist {
		//		negative = append(negative, blks...)
		//	}
		//	return []types.BlockID{}, nil
		//}
		sorted := types.SortBlockIDs(blks)

		if !exist && l != numLayers {
			negative = append(negative, sorted[:voteNegative]...)
		}
		return sorted[voteNegative:], nil
	}

	trtl = defaultTurtle(t)
	trtl.AvgLayerSize = blocksPerLayer
	trtl.bdp = msh
	trtl.init(context.TODO(), mesh.GenesisLayer())

	var l types.LayerID
	for l = mesh.GenesisLayer().Index() + 1; l <= numLayers; l++ {
		makeAndProcessLayer(t, l, trtl, blocksPerLayer, msh, inputVectorFn)
		fmt.Println("handled layer", l, "========================================================================")
	}

	return
}

func makeAndProcessLayer(t *testing.T, l types.LayerID, trtl *turtle, blocksPerLayer int, msh *mesh.DB, inputVectorFn func(id types.LayerID) ([]types.BlockID, error)) {
	makeLayer(t, l, trtl, blocksPerLayer, msh, inputVectorFn)

	// write blocks to database first; the verifying tortoise will subsequently read them
	if blocks, err := inputVectorFn(l); err != nil {
		trtl.logger.With().Warning("error from input vector fn", log.Err(err))
	} else {
		// save blocks to db for this layer
		require.NoError(t, msh.SaveLayerInputVectorByID(l, blocks))
	}

	require.NoError(t, trtl.HandleIncomingLayer(context.TODO(), l))
}

func makeLayer(t *testing.T, l types.LayerID, trtl *turtle, blocksPerLayer int, msh *mesh.DB, inputVectorFn func(id types.LayerID) ([]types.BlockID, error)) *types.Layer {
	fmt.Println("choosing base block for layer", l)
	oldInputVectorFn := msh.InputVectorBackupFunc
	defer func() {
		msh.InputVectorBackupFunc = oldInputVectorFn
	}()
	msh.InputVectorBackupFunc = inputVectorFn
	b, lists, err := trtl.BaseBlock(context.TODO())
	require.NoError(t, err)
	fmt.Println("base block for layer", l, "is", b)
	fmt.Println("exception lists for layer", l, ":", lists)
	lyr := types.NewLayer(l)

	for i := 0; i < blocksPerLayer; i++ {
		blk := &types.Block{
			MiniBlock: types.MiniBlock{
				BlockHeader: types.BlockHeader{
					LayerIndex: l,
					Data:       []byte(strconv.Itoa(i))},
				TxIDs: nil,
			}}
		blk.BaseBlock = b
		blk.AgainstDiff = lists[0]
		blk.ForDiff = lists[1]
		blk.NeutralDiff = lists[2]
		blk.Signature = signing.NewEdSigner().Sign(b.Bytes())
		blk.Initialize()
		lyr.AddBlock(blk)
		require.NoError(t, msh.AddBlock(blk))
	}

	return lyr
}

func Test_TurtleAbstainsInMiddle(t *testing.T) {
	layers := types.LayerID(15)
	blocksPerLayer := 10

	msh := getInMemMesh()

	layerfuncs := make([]func(id types.LayerID) (ids []types.BlockID, err error), 0, int(layers))

	// first 5 layers incl genesis just work
	for i := types.LayerID(0); i <= 5; i++ {
		layerfuncs = append(layerfuncs, func(id types.LayerID) (ids []types.BlockID, err error) {
			fmt.Println("giving good results for layer", id)
			return msh.LayerBlockIds(id)
		})
	}

	// next up two layers that didn't finish
	newlastlyr := types.LayerID(len(layerfuncs))
	for i := newlastlyr; i < newlastlyr+2; i++ {
		layerfuncs = append(layerfuncs, func(id types.LayerID) (ids []types.BlockID, err error) {
			fmt.Println("giving bad result for layer", id)
			return nil, errors.New("idontknow")
		})
	}

	// more good layers
	newlastlyr = types.LayerID(len(layerfuncs))
	for i := newlastlyr; i < newlastlyr+(layers-newlastlyr); i++ {
		layerfuncs = append(layerfuncs, func(id types.LayerID) (ids []types.BlockID, err error) {
			return msh.LayerBlockIds(id)
		})
	}

	trtl := defaultTurtle(t)
	trtl.AvgLayerSize = blocksPerLayer
	trtl.bdp = msh
	gen := mesh.GenesisLayer()
	trtl.init(context.TODO(), gen)

	var l types.LayerID
	for l = types.GetEffectiveGenesis() + 1; l < layers; l++ {
		makeAndProcessLayer(t, l, trtl, blocksPerLayer, msh, layerfuncs[l-types.GetEffectiveGenesis()-1])
		fmt.Println("handled layer", l, "verified layer", trtl.Verified, "========================================================================")
	}

	require.Equal(t, int(types.GetEffectiveGenesis()+5), int(trtl.Verified), "verification should advance after hare finishes")
	//todo: also check votes with requireVote
}

type baseBlockProvider func(context.Context) (types.BlockID, [][]types.BlockID, error)
type inputVectorProvider func(l types.LayerID) ([]types.BlockID, error)

func generateBlocks(l types.LayerID, n int, bbp baseBlockProvider) (blocks []*types.Block) {
	fmt.Println("choosing base block for layer", l)
	b, lists, err := bbp(context.TODO())
	if err != nil {
		panic(fmt.Sprint("no base block for layer: ", err))
	}
	fmt.Println("the base block for layer", l, "is", b, ". exception lists:")
	fmt.Println("\tagainst\t", lists[0])
	fmt.Println("\tfor\t", lists[1])
	fmt.Println("\tneutral\t", lists[2])

	for i := 0; i < n; i++ {
		blk := &types.Block{
			MiniBlock: types.MiniBlock{
				BlockHeader: types.BlockHeader{
					LayerIndex: l,
					Data:       []byte(strconv.Itoa(i))},
				TxIDs: nil,
			}}
		blk.BaseBlock = b
		blk.AgainstDiff = lists[0]
		blk.ForDiff = lists[1]
		blk.NeutralDiff = lists[2]
		blk.Signature = signing.NewEdSigner().Sign(b.Bytes())
		blk.Initialize()
		blocks = append(blocks, blk)
	}
	return
}

func createTurtleLayer(l types.LayerID, msh *mesh.DB, bbp baseBlockProvider, ivp inputVectorProvider, blocksPerLayer int) *types.Layer {
	msh.InputVectorBackupFunc = ivp
	blocks, err := ivp(l - 1)
	if err != nil {
		blocks = nil
	}
	if err := msh.SaveLayerInputVectorByID(l-1, blocks); err != nil {
		panic("database error")
	}
	lyr := types.NewLayer(l)
	for _, block := range generateBlocks(l, blocksPerLayer, bbp) {
		lyr.AddBlock(block)
	}

	return lyr
}

func TestTurtle_Eviction(t *testing.T) {
	layers := types.LayerID(defaultTestWindowSize * 3)
	avgPerLayer := 10
	voteNegative := 0
	trtl, _, _ := turtleSanity(t, layers, avgPerLayer, voteNegative, 0)
	require.Equal(t, defaultTestWindowSize+2, len(trtl.BlockOpinionsByLayer))
	count := 0
	for _, blks := range trtl.BlockOpinionsByLayer {
		count += len(blks)
	}
	require.Equal(t, (defaultTestWindowSize+2)*avgPerLayer, count)
	require.Equal(t, (defaultTestWindowSize+2)*avgPerLayer, len(trtl.GoodBlocksIndex)) // all blocks should be good
}

//func TestTurtle_Eviction2(t *testing.T) {
//	layers := types.LayerID(defaultTestHdist * 14)
//	avgPerLayer := 30
//	voteNegative := 5
//	trtl, _, _ := turtleSanity(t, layers, avgPerLayer, voteNegative, 0)
//	require.Equal(t, len(trtl.BlockOpinionsByLayer),
//		(defaultTestHdist+2)*avgPerLayer)
//}

func TestAddToMesh(t *testing.T) {
	mdb := getPersistentMesh(t)

	getHareResults := mdb.LayerBlockIds

	mdb.InputVectorBackupFunc = getHareResults
	alg := defaultAlgorithm(t, mdb)
	l := mesh.GenesisLayer()

	log.With().Info("genesis is", l.Index(), types.BlockIdsField(types.BlockIDs(l.Blocks())))
	log.With().Info("genesis is", l.Blocks()[0].Fields()...)

	l1 := createTurtleLayer(types.GetEffectiveGenesis()+1, mdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l1))

	log.With().Info("first is", l1.Index(), types.BlockIdsField(types.BlockIDs(l1.Blocks())))
	log.With().Info("first bb is", l1.Index(), l1.Blocks()[0].BaseBlock, types.BlockIdsField(l1.Blocks()[0].ForDiff))

	alg.HandleIncomingLayer(context.TODO(), l1.Index())

	l2 := createTurtleLayer(types.GetEffectiveGenesis()+2, mdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l2))
	alg.HandleIncomingLayer(context.TODO(), l2.Index())

	require.Equal(t, int(types.GetEffectiveGenesis()+1), int(alg.LatestComplete()))

	l3a := createTurtleLayer(types.GetEffectiveGenesis()+3, mdb, alg.BaseBlock, getHareResults, defaultTestLayerSize+1)

	// this should fail as the blocks for this layer have not been added to the mesh yet
	alg.HandleIncomingLayer(context.TODO(), l3a.Index())
	require.Equal(t, int(types.GetEffectiveGenesis()+1), int(alg.LatestComplete()))

	l3 := createTurtleLayer(types.GetEffectiveGenesis()+3, mdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l3))
	alg.HandleIncomingLayer(context.TODO(), l3.Index())

	l4 := createTurtleLayer(types.GetEffectiveGenesis()+4, mdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l4))
	alg.HandleIncomingLayer(context.TODO(), l4.Index())
	require.Equal(t, int(types.GetEffectiveGenesis()+3), int(alg.LatestComplete()), "wrong latest complete layer")
}

func TestPersistAndRecover(t *testing.T) {
	mdb := getPersistentMesh(t)

	getHareResults := mdb.LayerBlockIds

	mdb.InputVectorBackupFunc = getHareResults
	alg := defaultAlgorithm(t, mdb)

	l1 := createTurtleLayer(types.GetEffectiveGenesis()+1, mdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l1))
	alg.HandleIncomingLayer(context.TODO(), l1.Index())
	require.NoError(t, alg.Persist(context.TODO()))

	l2 := createTurtleLayer(types.GetEffectiveGenesis()+2, mdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l2))
	alg.HandleIncomingLayer(context.TODO(), l2.Index())
	require.NoError(t, alg.Persist(context.TODO()))
	require.Equal(t, int(types.GetEffectiveGenesis()+1), int(alg.LatestComplete()))

	// now recover
	alg2 := recoveredVerifyingTortoise(mdb, alg.logger)
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

	l3 := createTurtleLayer(types.GetEffectiveGenesis()+3, mdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l3))
	alg.HandleIncomingLayer(context.TODO(), l3.Index())
	alg2.HandleIncomingLayer(context.TODO(), l3.Index())

	// expect identical results
	require.Equal(t, int(types.GetEffectiveGenesis()+2), int(alg.LatestComplete()), "wrong latest complete layer")
	require.Equal(t, int(types.GetEffectiveGenesis()+2), int(alg2.LatestComplete()), "wrong latest complete layer")
}

func TestRerunInterval(t *testing.T) {
	r := require.New(t)
	mdb := getPersistentMesh(t)
	alg := defaultAlgorithm(t, mdb)
	lastRerun := alg.lastRerun

	mdb.InputVectorBackupFunc = mdb.LayerBlockIds

	// no rerun
	l1 := createTurtleLayer(types.GetEffectiveGenesis()+1, mdb, alg.BaseBlock, mdb.LayerBlockIds, defaultTestLayerSize)
	alg.HandleIncomingLayer(context.TODO(), l1.Index())
	r.Equal(lastRerun, alg.lastRerun)

	// force a rerun
	alg.lastRerun = time.Now().Add(-alg.trtl.RerunInterval)
	alg.HandleIncomingLayer(context.TODO(), l1.Index())
	r.NotEqual(lastRerun, alg.lastRerun)
	lastRerun = alg.lastRerun

	// no rerun
	alg.HandleIncomingLayer(context.TODO(), l1.Index())
	r.Equal(lastRerun, alg.lastRerun)
}

func TestLayerOpinionVector(t *testing.T) {
	r := require.New(t)
	mdb := getPersistentMesh(t)
	alg := defaultAlgorithm(t, mdb)

	mdb.InputVectorBackupFunc = mdb.LayerBlockIds
	l0 := mesh.GenesisLayer()

	// recent layer missing from mesh: should abstain and keep waiting
	l1ID := l0.Index().Add(1)
	opinionVec, err := alg.trtl.layerOpinionVector(context.TODO(), l1ID)
	r.NoError(err)
	r.Nil(opinionVec)

	// hare failed for layer: should vote against all blocks
	mdb.InputVectorBackupFunc = func(types.LayerID) ([]types.BlockID, error) {
		return nil, mesh.ErrInvalidLayer
	}
	opinionVec, err = alg.trtl.layerOpinionVector(context.TODO(), l1ID)
	r.NoError(err)
	r.Equal(make([]types.BlockID, 0, 0), opinionVec)

	// old layer missing from mesh: should vote against all blocks
	// older than zdist, not as old as hdist
	// simulate old layer by advancing Last
	l2ID := l1ID.Add(1)
	alg.trtl.Last = types.LayerID(defaultTestZdist) + l2ID.Add(1)
	mdb.InputVectorBackupFunc = mdb.LayerBlockIds
	opinionVec, err = alg.trtl.layerOpinionVector(context.TODO(), l2ID)
	r.NoError(err)
	r.Equal(make([]types.BlockID, 0, 0), opinionVec)

	// very old layer (more than hdist layers back)
	// if the layer isn't in the mesh, it's an error
	alg.trtl.Last = types.LayerID(defaultTestHdist) + l2ID.Add(1)
	opinionVec, err = alg.trtl.layerOpinionVector(context.TODO(), l2ID)
	r.Equal(database.ErrNotFound, err)
	r.Nil(opinionVec)

	// same layer in mesh, but no contextual validity info
	// expect error about missing weak coin
	alg.trtl.Last = l0.Index()
	l1 := createTurtleLayer(l1ID, mdb, alg.BaseBlock, mdb.LayerBlockIds, defaultTestLayerSize)
	for _, b := range l1.Blocks() {
		r.NoError(mdb.AddBlock(b))
	}
	alg.trtl.Last = l1ID
	l2 := createTurtleLayer(l2ID, mdb, alg.BaseBlock, mdb.LayerBlockIds, defaultTestLayerSize)
	for _, b := range l2.Blocks() {
		r.NoError(mdb.AddBlock(b))
	}
	alg.trtl.Last = types.LayerID(defaultTestHdist) + l2ID.Add(1)
	opinionVec, err = alg.trtl.layerOpinionVector(context.TODO(), l2ID)
	r.Contains(err.Error(), errstrNoCoinflip)
	r.Nil(opinionVec)

	// coinflip true: expect support for all layer blocks
	mdb.RecordCoinflip(context.TODO(), alg.trtl.Last, true)
	opinionVec, err = alg.trtl.layerOpinionVector(context.TODO(), l2ID)
	r.NoError(err)
	r.Equal(l2.Hash(), types.CalcBlocksHash32(types.SortBlockIDs(opinionVec), nil))

	// coinflip false: expect vote against all blocks in layer
	mdb.RecordCoinflip(context.TODO(), alg.trtl.Last, false)
	opinionVec, err = alg.trtl.layerOpinionVector(context.TODO(), l2ID)
	r.NoError(err)
	r.Equal(make([]types.BlockID, 0, 0), opinionVec)

	// for a verified layer, we expect the set of contextually valid blocks
	for _, b := range l2.Blocks() {
		r.NoError(mdb.SaveContextualValidity(b.ID(), l2ID, true))
	}
	alg.trtl.Verified = l2ID
	opinionVec, err = alg.trtl.layerOpinionVector(context.TODO(), l2ID)
	r.NoError(err)
	// this is the easiest way to compare a set of blockIDs
	r.Equal(l2.Hash(), types.CalcBlocksHash32(types.SortBlockIDs(opinionVec), nil))
}

func TestBaseBlock(t *testing.T) {
	r := require.New(t)
	mdb := getPersistentMesh(t)
	alg := defaultAlgorithm(t, mdb)

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
	l1 := createTurtleLayer(types.GetEffectiveGenesis()+1, mdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l1))
	alg.HandleIncomingLayer(context.TODO(), l1.Index())
	expectBaseBlockLayer(l1.Index(), 0, defaultTestLayerSize, 0)

	l2 := createTurtleLayer(types.GetEffectiveGenesis()+2, mdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l2))
	alg.HandleIncomingLayer(context.TODO(), l2.Index())
	require.Equal(t, int(types.GetEffectiveGenesis()+1), int(alg.LatestComplete()))
	expectBaseBlockLayer(l2.Index(), 0, defaultTestLayerSize, 0)

	// add a layer that's not in the mesh and make sure it does not advance
	l3 := createTurtleLayer(types.GetEffectiveGenesis()+3, mdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	alg.HandleIncomingLayer(context.TODO(), l3.Index())
	require.Equal(t, int(types.GetEffectiveGenesis()+1), int(alg.LatestComplete()))
	expectBaseBlockLayer(l2.Index(), 0, defaultTestLayerSize, 0)

	// mark all blocks bad
	alg.trtl.GoodBlocksIndex = make(map[types.BlockID]struct{}, 0)
	baseBlockID, exceptions, err := alg.BaseBlock(context.TODO())
	r.Equal(errNoBaseBlockFound, err)
	r.Equal(types.BlockID{0}, baseBlockID)
	r.Nil(exceptions)
}

func defaultTurtle(t *testing.T) *turtle {
	mdb := getPersistentMesh(t)
	return newTurtle(
		mdb,
		defaultTestHdist,
		defaultTestZdist,
		defaultTestConfidenceParam,
		defaultTestWindowSize,
		defaultTestLayerSize,
		defaultTestGlobalThreshold,
		defaultTestLocalThreshold,
		defaultTestRerunInterval,
	)
}

func TestCloneTurtle(t *testing.T) {
	r := require.New(t)
	trtl := defaultTurtle(t)
	trtl.AvgLayerSize++ // make sure defaults aren't being read
	trtl.Last = 10      // state should not be cloned
	trtl2 := trtl.cloneTurtle()
	r.Equal(trtl.bdp, trtl2.bdp)
	r.Equal(trtl.Hdist, trtl2.Hdist)
	r.Equal(trtl.Zdist, trtl2.Zdist)
	r.Equal(trtl.ConfidenceParam, trtl2.ConfidenceParam)
	r.Equal(trtl.WindowSize, trtl2.WindowSize)
	r.Equal(trtl.AvgLayerSize, trtl2.AvgLayerSize)
	r.Equal(trtl.GlobalThreshold, trtl2.GlobalThreshold)
	r.Equal(trtl.LocalThreshold, trtl2.LocalThreshold)
	r.Equal(trtl.RerunInterval, trtl2.RerunInterval)
	r.NotEqual(trtl.Last, trtl2.Last)
}

func defaultAlgorithm(t *testing.T, mdb *mesh.DB) *ThreadSafeVerifyingTortoise {
	return verifyingTortoise(
		context.TODO(),
		defaultTestLayerSize,
		mdb,
		defaultTestHdist,
		defaultTestZdist,
		defaultTestConfidenceParam,
		defaultTestWindowSize,
		defaultTestGlobalThreshold,
		defaultTestLocalThreshold,
		defaultTestRerunInterval,
		log.NewDefault(t.Name()),
	)
}

func TestGetSingleInputVector(t *testing.T) {
	r := require.New(t)
	mdb := getPersistentMesh(t)
	alg := defaultAlgorithm(t, mdb)

	l1ID := types.GetEffectiveGenesis() + 1
	blocks := generateBlocks(l1ID, 2, alg.BaseBlock)

	// no input vector for layer
	vec, err := alg.trtl.getSingleInputVectorFromDB(context.TODO(), l1ID, blocks[0].ID())
	r.Equal(database.ErrNotFound, err)
	r.Equal(abstain, vec)

	// block included in input vector
	r.NoError(mdb.SaveLayerInputVectorByID(l1ID, []types.BlockID{blocks[0].ID()}))
	vec, err = alg.trtl.getSingleInputVectorFromDB(context.TODO(), l1ID, blocks[0].ID())
	r.NoError(err)
	r.Equal(support, vec)

	// block not included in input vector
	vec, err = alg.trtl.getSingleInputVectorFromDB(context.TODO(), l1ID, blocks[1].ID())
	r.NoError(err)
	r.Equal(against, vec)
}

func TestCheckBlockAndGetInputVector(t *testing.T) {
	r := require.New(t)
	mdb := getPersistentMesh(t)
	alg := defaultAlgorithm(t, mdb)

	l1ID := types.GetEffectiveGenesis() + 1
	blocks := generateBlocks(l1ID, 3, alg.BaseBlock)
	diffList := []types.BlockID{blocks[0].ID()}

	// missing block
	r.False(alg.trtl.checkBlockAndGetInputVector(context.TODO(), diffList, "foo", support, l1ID))

	// exception block older than base block
	blocks[0].LayerIndex = mesh.GenesisLayer().Index()
	r.NoError(mdb.AddBlock(blocks[0]))
	r.False(alg.trtl.checkBlockAndGetInputVector(context.TODO(), diffList, "foo", support, l1ID))

	// missing input vector for layer
	r.NoError(mdb.AddBlock(blocks[1]))
	diffList[0] = blocks[1].ID()
	r.False(alg.trtl.checkBlockAndGetInputVector(context.TODO(), diffList, "foo", support, l1ID))

	// good
	r.NoError(mdb.SaveLayerInputVectorByID(l1ID, diffList))
	r.True(alg.trtl.checkBlockAndGetInputVector(context.TODO(), diffList, "foo", support, l1ID))

	// vote differs from input vector
	diffList[0] = blocks[2].ID()
	r.NoError(mdb.AddBlock(blocks[2]))
	r.False(alg.trtl.checkBlockAndGetInputVector(context.TODO(), diffList, "foo", support, l1ID))
}

func TestCalculateExceptions(t *testing.T) {
	r := require.New(t)
	mdb := getPersistentMesh(t)
	alg := defaultAlgorithm(t, mdb)

	// helper function for checking votes
	expectVotes := func(votes []map[types.BlockID]struct{}, numAgainst, numFor, numNeutral int) {
		r.Len(votes, 3, "vote vector size is wrong")
		r.Len(votes[0], numAgainst) // against
		r.Len(votes[1], numFor)     // for
		r.Len(votes[2], numNeutral) // neutral
	}

	// genesis layer
	l0ID := types.GetEffectiveGenesis()
	votes, err := alg.trtl.calculateExceptions(context.TODO(), l0ID, Opinion{})
	r.NoError(err)
	// expect votes in support of all genesis blocks
	expectVotes(votes, 0, len(mesh.GenesisLayer().Blocks()), 0)

	// layer greater than last processed: expect support only for genesis
	l1ID := l0ID.Add(1)
	votes, err = alg.trtl.calculateExceptions(context.TODO(), l1ID, Opinion{})
	r.NoError(err)
	expectVotes(votes, 0, 1, 0)

	// now advance the processed layer
	alg.trtl.Last = l1ID

	// missing layer data
	votes, err = alg.trtl.calculateExceptions(context.TODO(), l1ID, Opinion{})
	r.Equal(database.ErrNotFound, err)
	r.Nil(votes)

	// layer opinion vector is nil (abstains): recent layer, in mesh, no input vector
	alg.trtl.Last = l0ID
	l1 := createTurtleLayer(l1ID, mdb, alg.BaseBlock, mdb.LayerBlockIds, defaultTestLayerSize)
	r.NoError(addLayerToMesh(mdb, l1))
	alg.trtl.Last = l1ID
	mdb.InputVectorBackupFunc = nil
	votes, err = alg.trtl.calculateExceptions(context.TODO(), l1ID, Opinion{})
	r.NoError(err)
	// expect no against, FOR only for l0, and NEUTRAL for l1
	expectVotes(votes, 0, len(mesh.GenesisLayer().Blocks()), defaultTestLayerSize)

	// adding diffs for: support all blocks in the layer
	mdb.InputVectorBackupFunc = mdb.LayerBlockIds
	votes, err = alg.trtl.calculateExceptions(context.TODO(), l1ID, Opinion{})
	r.NoError(err)
	expectVotes(votes, 0, len(mesh.GenesisLayer().Blocks())+defaultTestLayerSize, 0)

	// adding diffs against: vote against all blocks in the layer
	mdb.InputVectorBackupFunc = nil
	// we cannot store an empty vector here (it comes back as nil), so just put another block ID in it
	r.NoError(mdb.SaveLayerInputVectorByID(l1ID, []types.BlockID{mesh.GenesisBlock().ID()}))
	votes, err = alg.trtl.calculateExceptions(context.TODO(), l1ID, Opinion{})
	r.NoError(err)
	expectVotes(votes, defaultTestLayerSize, len(mesh.GenesisLayer().Blocks()), 0)

	// compare opinions: all agree, no exceptions
	mdb.InputVectorBackupFunc = mdb.LayerBlockIds
	opinion := Opinion{BlockOpinions: map[types.BlockID]vec{
		mesh.GenesisBlock().ID(): support,
		l1.Blocks()[0].ID():      support,
		l1.Blocks()[1].ID():      support,
		l1.Blocks()[2].ID():      support,
	}}
	votes, err = alg.trtl.calculateExceptions(context.TODO(), l1ID, opinion)
	r.NoError(err)
	expectVotes(votes, 0, 0, 0)

	// compare opinions: all disagree, adds exceptions
	opinion = Opinion{BlockOpinions: map[types.BlockID]vec{
		mesh.GenesisBlock().ID(): against,
		l1.Blocks()[0].ID():      against,
		l1.Blocks()[1].ID():      against,
		l1.Blocks()[2].ID():      against,
	}}
	votes, err = alg.trtl.calculateExceptions(context.TODO(), l1ID, opinion)
	r.NoError(err)
	expectVotes(votes, 0, 4, 0)

	l2ID := l1ID.Add(1)
	l3ID := l2ID.Add(1)
	var l3 *types.Layer

	// exceeding max exceptions
	t.Run("exceeding max exceptions", func(t *testing.T) {
		alg.trtl.MaxExceptions = 10
		l2 := createTurtleLayer(l2ID, mdb, alg.BaseBlock, mdb.LayerBlockIds, alg.trtl.MaxExceptions+1)
		for _, block := range l2.Blocks() {
			r.NoError(mdb.AddBlock(block))
		}
		l3 = createTurtleLayer(l3ID, mdb, alg.BaseBlock, mdb.LayerBlockIds, defaultTestLayerSize)
		alg.trtl.Last = l2ID
		votes, err = alg.trtl.calculateExceptions(context.TODO(), l2ID, Opinion{})
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
		l4 := createTurtleLayer(l4ID, mdb, alg.BaseBlock, mdb.LayerBlockIds, defaultTestLayerSize)
		for _, block := range l4.Blocks() {
			r.NoError(mdb.AddBlock(block))
		}
		l5ID := l4ID.Add(1)
		createTurtleLayer(l5ID, mdb, alg.BaseBlock, mdb.LayerBlockIds, defaultTestLayerSize)
		alg.trtl.Last = l4ID
		votes, err = alg.trtl.calculateExceptions(context.TODO(), l3ID, Opinion{})
		r.NoError(err)
		// expect votes FOR the blocks in the two intervening layers between the base block layer and the last layer
		expectVotes(votes, 0, 2*defaultTestLayerSize, 0)
	})
}

func TestProcessBlock(t *testing.T) {
	r := require.New(t)
	mdb := getPersistentMesh(t)
	alg := defaultAlgorithm(t, mdb)

	// blocks in this layer will use the genesis block as their base block
	l1ID := types.GetEffectiveGenesis() + 1
	l1Blocks := generateBlocks(l1ID, 3, alg.BaseBlock)
	// add one block from the layer
	blockWithMissingBaseBlock := l1Blocks[0]
	r.NoError(mdb.AddBlock(blockWithMissingBaseBlock))
	blockWithMissingBaseBlock.BaseBlock = l1Blocks[1].ID()

	// missing base block
	err := alg.trtl.processBlock(context.TODO(), blockWithMissingBaseBlock)
	r.Equal(errBaseBlockNotInDatabase, err)

	// blocks in this layer will use a block from the previous layer as their base block
	baseBlockProviderFn := func(context.Context) (types.BlockID, [][]types.BlockID, error) {
		return blockWithMissingBaseBlock.ID(), make([][]types.BlockID, 3), nil
	}
	l2ID := l1ID.Add(1)
	l2Blocks := generateBlocks(l2ID, 3, baseBlockProviderFn)

	// base block layer missing
	alg.trtl.BlockOpinionsByLayer[l2ID] = make(map[types.BlockID]Opinion, defaultTestLayerSize)
	err = alg.trtl.processBlock(context.TODO(), l2Blocks[0])
	r.Error(err)
	r.Contains(err.Error(), errstrBaseBlockLayerMissing)

	// base block opinion missing from layer
	alg.trtl.BlockOpinionsByLayer[l1ID] = make(map[types.BlockID]Opinion, defaultTestLayerSize)
	err = alg.trtl.processBlock(context.TODO(), l2Blocks[0])
	r.Error(err)
	r.Contains(err.Error(), errstrBaseBlockNotFoundInLayer)

	// malicious (conflicting) voting pattern
	l2Blocks[0].BaseBlock = mesh.GenesisBlock().ID()
	l2Blocks[0].ForDiff = []types.BlockID{l1Blocks[1].ID()}
	l2Blocks[0].AgainstDiff = l2Blocks[0].ForDiff
	err = alg.trtl.processBlock(context.TODO(), l2Blocks[0])
	r.Error(err)
	r.Contains(err.Error(), errstrConflictingVotes)

	// add vote diffs: make sure that base block votes flow through, but that block votes override them, and that the
	// data structure is correctly updated

	// add base block to DB
	r.NoError(mdb.AddBlock(l2Blocks[0]))
	baseBlockProviderFn = func(context.Context) (types.BlockID, [][]types.BlockID, error) {
		return l2Blocks[0].ID(), make([][]types.BlockID, 3), nil
	}
	alg.trtl.BlockOpinionsByLayer[l2ID][l2Blocks[0].ID()] = Opinion{BlockOpinions: map[types.BlockID]vec{
		// these votes all disagree with the block votes, below
		l1Blocks[0].ID(): against,
		l1Blocks[1].ID(): support,
		l1Blocks[2].ID(): abstain,
	}}
	l3ID := l2ID.Add(1)
	l3Blocks := generateBlocks(l3ID, 3, baseBlockProviderFn)
	l3Blocks[0].AgainstDiff = []types.BlockID{
		l1Blocks[1].ID(),
	}
	l3Blocks[0].ForDiff = []types.BlockID{}
	l3Blocks[0].NeutralDiff = []types.BlockID{
		l1Blocks[0].ID(),
	}
	alg.trtl.BlockOpinionsByLayer[l3ID] = make(map[types.BlockID]Opinion, defaultTestLayerSize)
	err = alg.trtl.processBlock(context.TODO(), l3Blocks[0])
	r.NoError(err)
	expectedOpinionVector := Opinion{BlockOpinions: map[types.BlockID]vec{
		l1Blocks[0].ID(): abstain, // from exception
		l1Blocks[1].ID(): against, // from exception
		l1Blocks[2].ID(): abstain, // from base block
	}}
	r.Equal(expectedOpinionVector, alg.trtl.BlockOpinionsByLayer[l3ID][l3Blocks[0].ID()])
}

func TestProcessNewBlocks(t *testing.T) {
	r := require.New(t)

	mdb := getPersistentMesh(t)
	alg := defaultAlgorithm(t, mdb)
	l1ID := types.GetEffectiveGenesis() + 1
	l2ID := l1ID.Add(1)
	l1Blocks := generateBlocks(l1ID, defaultTestLayerSize, alg.BaseBlock)
	l2Blocks := generateBlocks(l2ID, defaultTestLayerSize, alg.BaseBlock)

	for _, block := range l1Blocks {
		r.NoError(mdb.AddBlock(block))
	}
	mdb.InputVectorBackupFunc = mdb.LayerBlockIds

	// empty input
	r.Nil(alg.trtl.ProcessNewBlocks(context.TODO(), []*types.Block{}))

	// input not sorted by layer
	blocksOutOfOrder := []*types.Block{l2Blocks[0], l1Blocks[0]}
	err := alg.trtl.ProcessNewBlocks(context.TODO(), blocksOutOfOrder)
	r.Equal(errNotSorted, err)

	// process some blocks: make sure opinions updated and block marked good
	l1Blocks[0].ForDiff = []types.BlockID{l1Blocks[1].ID(), l1Blocks[2].ID()}
	r.NoError(alg.trtl.ProcessNewBlocks(context.TODO(), []*types.Block{l1Blocks[0]}))
	r.Contains(alg.trtl.BlockOpinionsByLayer, l1ID)
	r.Contains(alg.trtl.BlockOpinionsByLayer[l1ID], l1Blocks[0].ID())
	r.Equal(alg.trtl.BlockOpinionsByLayer[l1ID][l1Blocks[0].ID()], Opinion{BlockOpinions: map[types.BlockID]vec{
		l1Blocks[1].ID(): support,
		l1Blocks[2].ID(): support,
	}})
	r.Contains(alg.trtl.GoodBlocksIndex, l1Blocks[0].ID())
	r.Equal(alg.trtl.GoodBlocksIndex[l1Blocks[0].ID()], struct{}{})

	// base block opinion missing: input block should also not be marked good
	l1Blocks[1].BaseBlock = l1Blocks[2].ID()
	r.NoError(alg.trtl.ProcessNewBlocks(context.TODO(), []*types.Block{l1Blocks[1]}))
	r.NotContains(alg.trtl.GoodBlocksIndex, l1Blocks[1].ID())

	// base block not marked good: input block should also not be marked good
	alg.trtl.BlockOpinionsByLayer[l1ID][l1Blocks[2].ID()] = Opinion{BlockOpinions: map[types.BlockID]vec{}}
	l1Blocks[1].BaseBlock = l1Blocks[2].ID()
	r.NoError(alg.trtl.ProcessNewBlocks(context.TODO(), []*types.Block{l1Blocks[1]}))
	r.NotContains(alg.trtl.GoodBlocksIndex, l1Blocks[1].ID())

	// base block not found
	l1Blocks[2].BaseBlock = l2Blocks[0].ID()
	r.NoError(alg.trtl.ProcessNewBlocks(context.TODO(), []*types.Block{l1Blocks[2]}))
	r.NotContains(alg.trtl.GoodBlocksIndex, l1Blocks[2].ID())

	// diffs appear before base block layer and/or are not consistent
	// base block in L1 but this block contains a FOR vote for the genesis block in L0
	l2Blocks[0].BaseBlock = l1Blocks[0].ID()
	r.NoError(alg.trtl.ProcessNewBlocks(context.TODO(), []*types.Block{l2Blocks[0]}))
	r.NotContains(alg.trtl.GoodBlocksIndex, l2Blocks[0].ID())

	// test eviction
	r.Equal(int(mesh.GenesisLayer().Index())-1, int(alg.trtl.LastEvicted))
	// move verified up a bunch to make sure eviction occurs
	alg.trtl.Verified = types.GetEffectiveGenesis() + alg.trtl.Hdist + alg.trtl.WindowSize
	r.NoError(alg.trtl.ProcessNewBlocks(context.TODO(), []*types.Block{l2Blocks[0]}))
	r.Equal(int(alg.trtl.Verified)-int(alg.trtl.WindowSize)-1, int(alg.trtl.LastEvicted))
}

func TestVerifyLayers(t *testing.T) {
	log.DebugMode(true)
	r := require.New(t)

	mdb := getPersistentMesh(t)
	alg := defaultAlgorithm(t, mdb)
	l1ID := types.GetEffectiveGenesis() + 1
	l2ID := l1ID.Add(1)
	l2Blocks := generateBlocks(l2ID, defaultTestLayerSize, alg.BaseBlock)
	l3ID := l2ID.Add(1)
	l3Blocks := generateBlocks(l3ID, defaultTestLayerSize, alg.BaseBlock)
	l4ID := l3ID.Add(1)
	l4Blocks := generateBlocks(l4ID, defaultTestLayerSize, alg.BaseBlock)
	l5ID := l4ID.Add(1)
	l5Blocks := generateBlocks(l5ID, defaultTestLayerSize, alg.BaseBlock)
	l6ID := l5ID.Add(1)
	l6Blocks := generateBlocks(l6ID, defaultTestLayerSize, alg.BaseBlock)

	// layer missing in database
	alg.trtl.Last = l2ID
	err := alg.trtl.verifyLayers(context.TODO())
	r.Error(err)
	r.Contains(err.Error(), errstrCantFindLayer)

	// empty layer: local opinion vector is nil, abstains on all blocks in layer
	// no contextual validity data recorded
	// layer should be verified
	r.NoError(mdb.AddZeroBlockLayer(l1ID))

	mdbWrapper := meshWrapper{
		blockDataProvider: mdb,
		saveContextualValidityFn: func(types.BlockID, types.LayerID, bool) error {
			r.Fail("should not save contextual validity")
			return nil
		},
	}
	alg.trtl.bdp = mdbWrapper
	err = alg.trtl.verifyLayers(context.TODO())
	r.NoError(err)
	r.Equal(int(l1ID), int(alg.trtl.Verified))

	for _, block := range l2Blocks {
		r.NoError(mdb.AddBlock(block))
	}
	// L3 blocks support all L2 blocks
	l2SupportVec := Opinion{BlockOpinions: map[types.BlockID]vec{
		l2Blocks[0].ID(): support,
		l2Blocks[1].ID(): support,
		l2Blocks[2].ID(): support,
	}}
	alg.trtl.BlockOpinionsByLayer[l3ID] = map[types.BlockID]Opinion{
		l3Blocks[0].ID(): l2SupportVec,
		l3Blocks[1].ID(): l2SupportVec,
		l3Blocks[2].ID(): l2SupportVec,
	}
	alg.trtl.Last = l3ID

	// voting blocks not marked good, both global and local opinion is abstain, verified layer does not advance
	err = alg.trtl.verifyLayers(context.TODO())
	r.NoError(err)
	r.Equal(int(l1ID), int(alg.trtl.Verified))

	// now mark voting blocks good
	alg.trtl.GoodBlocksIndex[l3Blocks[0].ID()] = struct{}{}
	alg.trtl.GoodBlocksIndex[l3Blocks[1].ID()] = struct{}{}
	alg.trtl.GoodBlocksIndex[l3Blocks[2].ID()] = struct{}{}

	// consensus doesn't match: fail to verify candidate layer
	// global opinion: good, local opinion: abstain
	err = alg.trtl.verifyLayers(context.TODO())
	r.NoError(err)
	r.Equal(int(l1ID), int(alg.trtl.Verified))

	// mark local opinion of L2 good so verified layer advances
	// do the reverse for L3: local opinion is good, global opinion is undecided
	var l2BlockIDs, l3BlockIDs, l4BlockIDs, l5BlockIDs []types.BlockID
	for _, block := range l2Blocks {
		l2BlockIDs = append(l2BlockIDs, block.ID())
	}
	mdbWrapper.inputVectorBackupFn = mdb.LayerBlockIds
	mdbWrapper.saveContextualValidityFn = nil
	alg.trtl.bdp = mdbWrapper
	for _, block := range l3Blocks {
		r.NoError(mdb.AddBlock(block))
		l3BlockIDs = append(l3BlockIDs, block.ID())
	}
	for _, block := range l4Blocks {
		r.NoError(mdb.AddBlock(block))
		l4BlockIDs = append(l4BlockIDs, block.ID())
	}
	for _, block := range l5Blocks {
		r.NoError(mdb.AddBlock(block))
		l5BlockIDs = append(l5BlockIDs, block.ID())
	}
	r.NoError(mdb.SaveLayerInputVectorByID(l3ID, l3BlockIDs))
	r.NoError(mdb.SaveLayerInputVectorByID(l4ID, l4BlockIDs))
	r.NoError(mdb.SaveLayerInputVectorByID(l5ID, l5BlockIDs))
	l4Votes := Opinion{BlockOpinions: map[types.BlockID]vec{
		// support these so global opinion is support
		l2Blocks[0].ID(): support,
		l2Blocks[1].ID(): support,
		l2Blocks[2].ID(): support,

		// abstain
		l3Blocks[0].ID(): abstain,
		l3Blocks[1].ID(): abstain,
		l3Blocks[2].ID(): abstain,
	}}
	alg.trtl.BlockOpinionsByLayer[l4ID] = map[types.BlockID]Opinion{
		l4Blocks[0].ID(): l4Votes,
		l4Blocks[1].ID(): l4Votes,
		l4Blocks[2].ID(): l4Votes,
	}
	alg.trtl.Last = l4ID
	alg.trtl.GoodBlocksIndex[l4Blocks[0].ID()] = struct{}{}
	alg.trtl.GoodBlocksIndex[l4Blocks[1].ID()] = struct{}{}
	alg.trtl.GoodBlocksIndex[l4Blocks[2].ID()] = struct{}{}
	l5Votes := Opinion{BlockOpinions: map[types.BlockID]vec{
		l2Blocks[0].ID(): support,
		l2Blocks[1].ID(): support,
		l2Blocks[2].ID(): support,
		l3Blocks[0].ID(): abstain,
		l3Blocks[1].ID(): abstain,
		l3Blocks[2].ID(): abstain,
		l4Blocks[0].ID(): support,
		l4Blocks[1].ID(): support,
		l4Blocks[2].ID(): support,
	}}
	alg.trtl.BlockOpinionsByLayer[l5ID] = map[types.BlockID]Opinion{
		l5Blocks[0].ID(): l5Votes,
		l5Blocks[1].ID(): l5Votes,
		l5Blocks[2].ID(): l5Votes,
	}
	alg.trtl.GoodBlocksIndex[l5Blocks[0].ID()] = struct{}{}
	alg.trtl.GoodBlocksIndex[l5Blocks[1].ID()] = struct{}{}
	alg.trtl.GoodBlocksIndex[l5Blocks[2].ID()] = struct{}{}
	l6Votes := Opinion{BlockOpinions: map[types.BlockID]vec{
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
	}}
	alg.trtl.BlockOpinionsByLayer[l6ID] = map[types.BlockID]Opinion{
		l6Blocks[0].ID(): l6Votes,
		l6Blocks[1].ID(): l6Votes,
		l6Blocks[2].ID(): l6Votes,
	}
	alg.trtl.GoodBlocksIndex[l6Blocks[0].ID()] = struct{}{}
	alg.trtl.GoodBlocksIndex[l6Blocks[1].ID()] = struct{}{}
	alg.trtl.GoodBlocksIndex[l6Blocks[2].ID()] = struct{}{}

	// verified layer advances one step, but L3 is not verified because global opinion is undecided, so verification
	// stops there
	t.Run("global opinion undecided", func(t *testing.T) {
		err = alg.trtl.verifyLayers(context.TODO())
		r.NoError(err)
		r.Equal(int(l2ID), int(alg.trtl.Verified))
	})

	l4Votes = Opinion{BlockOpinions: map[types.BlockID]vec{
		l2Blocks[0].ID(): support,
		l2Blocks[1].ID(): support,
		l2Blocks[2].ID(): support,

		// change from abstain to support
		l3Blocks[0].ID(): support,
		l3Blocks[1].ID(): support,
		l3Blocks[2].ID(): support,
	}}

	// weight not exceeded
	t.Run("weight not exceeded", func(t *testing.T) {
		// modify vote so one block votes in support of L3 blocks, two blocks continue to abstain, so threshold not met
		alg.trtl.BlockOpinionsByLayer[l4ID][l4Blocks[0].ID()] = l4Votes
		err = alg.trtl.verifyLayers(context.TODO())
		r.NoError(err)
		r.Equal(int(l2ID), int(alg.trtl.Verified))
	})

	t.Run("self-healing", func(t *testing.T) {
		// test self-healing: self-healing can verify layers that are stuck for specific reasons, i.e., where local and
		// global opinion differ (or local opinion is missing).
		alg.trtl.Hdist = 1
		alg.trtl.Zdist = 1
		alg.trtl.ConfidenceParam = 1

		// add more votes in favor of l3 blocks
		alg.trtl.BlockOpinionsByLayer[l4ID][l4Blocks[1].ID()] = l4Votes
		alg.trtl.BlockOpinionsByLayer[l4ID][l4Blocks[2].ID()] = l4Votes
		l5Votes := Opinion{BlockOpinions: map[types.BlockID]vec{
			l2Blocks[0].ID(): support,
			l2Blocks[1].ID(): support,
			l2Blocks[2].ID(): support,
			l3Blocks[0].ID(): support,
			l3Blocks[1].ID(): support,
			l3Blocks[2].ID(): support,
			l4Blocks[0].ID(): support,
			l4Blocks[1].ID(): support,
			l4Blocks[2].ID(): support,
		}}
		alg.trtl.BlockOpinionsByLayer[l5ID] = map[types.BlockID]Opinion{
			l5Blocks[0].ID(): l5Votes,
			l5Blocks[1].ID(): l5Votes,
			l5Blocks[2].ID(): l5Votes,
		}
		l6Votes := Opinion{BlockOpinions: map[types.BlockID]vec{
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
		}}
		alg.trtl.BlockOpinionsByLayer[l6ID] = map[types.BlockID]Opinion{
			l6Blocks[0].ID(): l6Votes,
			l6Blocks[1].ID(): l6Votes,
			l6Blocks[2].ID(): l6Votes,
		}

		// simulate a layer that's older than the LayerCutoff, and older than Zdist+ConfidenceParam, but not verified,
		// that has no contextually valid blocks in the database. this should trigger self-healing.

		// perform some surgery: erase just enough good blocks data so that ordinary tortoise verification fails for
		// one layer, triggering self-healing, but leave enough good blocks data so that ordinary tortoise can
		// subsequently verify a later layer after self-healing has finished. this works because self-healing does not
		// rely on local data, including the set of good blocks.
		delete(alg.trtl.GoodBlocksIndex, l4Blocks[0].ID())
		delete(alg.trtl.GoodBlocksIndex, l4Blocks[1].ID())
		delete(alg.trtl.GoodBlocksIndex, l4Blocks[2].ID())
		delete(alg.trtl.GoodBlocksIndex, l5Blocks[0].ID())

		// self-healing should advance verification two steps, over the
		// previously stuck layer (l3) and the following layer (l4) since it's old enough, then hand control back to the
		// ordinary verifying tortoise which should continue and verify l5.
		alg.trtl.Last = l4ID + alg.trtl.Hdist + 1
		err = alg.trtl.verifyLayers(context.TODO())
		r.NoError(err)
		r.Equal(int(l5ID), int(alg.trtl.Verified))
	})
}

func TestVoteVectorForLayer(t *testing.T) {
	r := require.New(t)

	mdb := getPersistentMesh(t)
	alg := defaultAlgorithm(t, mdb)
	l1ID := types.GetEffectiveGenesis() + 1
	l1Blocks := generateBlocks(l1ID, 3, alg.BaseBlock)
	var blockIDs []types.BlockID
	for _, block := range l1Blocks {
		blockIDs = append(blockIDs, block.ID())
	}

	// empty input: expect empty output
	emptyVec := make([]types.BlockID, 0, 0)
	voteMap := alg.trtl.voteVectorForLayer(emptyVec, emptyVec)
	r.Equal(map[types.BlockID]vec{}, voteMap)

	// nil input vector: abstain on all blocks in layer
	voteMap = alg.trtl.voteVectorForLayer(blockIDs, nil)
	r.Len(blockIDs, 3)
	r.Equal(map[types.BlockID]vec{
		blockIDs[0]: abstain,
		blockIDs[1]: abstain,
		blockIDs[2]: abstain,
	}, voteMap)

	// empty input vector: vote against everything
	voteMap = alg.trtl.voteVectorForLayer(blockIDs, make([]types.BlockID, 0, 0))
	r.Len(blockIDs, 3)
	r.Equal(map[types.BlockID]vec{
		blockIDs[0]: against,
		blockIDs[1]: against,
		blockIDs[2]: against,
	}, voteMap)

	// adds support for blocks in input vector
	voteMap = alg.trtl.voteVectorForLayer(blockIDs, blockIDs[1:])
	r.Len(blockIDs, 3)
	r.Equal(map[types.BlockID]vec{
		blockIDs[0]: against,
		blockIDs[1]: support,
		blockIDs[2]: support,
	}, voteMap)
}

func TestSumVotesForBlock(t *testing.T) {
	r := require.New(t)

	mdb := getPersistentMesh(t)
	alg := defaultAlgorithm(t, mdb)

	// store a bunch of votes against a block
	l1ID := types.GetEffectiveGenesis() + 1
	l1Blocks := generateBlocks(l1ID, 4, alg.BaseBlock)
	blockWeReallyDislike := l1Blocks[0]
	blockWeReallyLike := l1Blocks[1]
	blockWeReallyDontCare := l1Blocks[2]
	blockWeNeverSaw := l1Blocks[3]
	l2ID := l1ID.Add(1)
	l2Blocks := generateBlocks(l2ID, 9, alg.BaseBlock)
	alg.trtl.BlockOpinionsByLayer[l2ID] = map[types.BlockID]Opinion{
		l2Blocks[0].ID(): {BlockOpinions: map[types.BlockID]vec{blockWeReallyDislike.ID(): against}},
		l2Blocks[1].ID(): {BlockOpinions: map[types.BlockID]vec{blockWeReallyDislike.ID(): against}},
		l2Blocks[2].ID(): {BlockOpinions: map[types.BlockID]vec{blockWeReallyDislike.ID(): against}},
	}

	// test filter
	filterPassAll := func(types.BlockID) bool { return true }
	filterRejectAll := func(types.BlockID) bool { return false }

	// if we reject all blocks, we expect an abstain outcome
	alg.trtl.Last = l2ID
	sum := alg.trtl.sumVotesForBlock(context.TODO(), blockWeReallyDislike.ID(), l2ID, filterRejectAll)
	r.Equal(abstain, sum)

	// if we allow all blocks to vote, we expect an against outcome
	sum = alg.trtl.sumVotesForBlock(context.TODO(), blockWeReallyDislike.ID(), l2ID, filterPassAll)
	r.Equal(against.Multiply(3), sum)

	// add more blocks
	alg.trtl.BlockOpinionsByLayer[l2ID] = map[types.BlockID]Opinion{
		l2Blocks[0].ID(): {BlockOpinions: map[types.BlockID]vec{blockWeReallyDislike.ID(): against}},
		l2Blocks[1].ID(): {BlockOpinions: map[types.BlockID]vec{blockWeReallyDislike.ID(): against}},
		l2Blocks[2].ID(): {BlockOpinions: map[types.BlockID]vec{blockWeReallyDislike.ID(): against}},
		l2Blocks[3].ID(): {BlockOpinions: map[types.BlockID]vec{blockWeReallyLike.ID(): support}},
		l2Blocks[4].ID(): {BlockOpinions: map[types.BlockID]vec{blockWeReallyLike.ID(): support}},
		l2Blocks[5].ID(): {BlockOpinions: map[types.BlockID]vec{blockWeReallyDontCare.ID(): abstain}},
		l2Blocks[6].ID(): {BlockOpinions: map[types.BlockID]vec{}},
		l2Blocks[7].ID(): {BlockOpinions: map[types.BlockID]vec{}},
		l2Blocks[8].ID(): {BlockOpinions: map[types.BlockID]vec{}},
	}
	// some blocks explicitly vote against, others have no opinion
	sum = alg.trtl.sumVotesForBlock(context.TODO(), blockWeReallyDislike.ID(), l2ID, filterPassAll)
	r.Equal(against.Multiply(9), sum)
	// some blocks vote for, others have no opinion
	sum = alg.trtl.sumVotesForBlock(context.TODO(), blockWeReallyLike.ID(), l2ID, filterPassAll)
	r.Equal(support.Multiply(2).Add(against.Multiply(7)), sum)
	// one block votes neutral, others have no opinion
	sum = alg.trtl.sumVotesForBlock(context.TODO(), blockWeReallyDontCare.ID(), l2ID, filterPassAll)
	r.Equal(abstain.Multiply(1).Add(against.Multiply(8)), sum)

	// vote missing: counts against
	sum = alg.trtl.sumVotesForBlock(context.TODO(), blockWeNeverSaw.ID(), l2ID, filterPassAll)
	r.Equal(against.Multiply(9), sum)
}

func TestHealing(t *testing.T) {
	log.DebugMode(true)
	r := require.New(t)

	mdb := getPersistentMesh(t)
	alg := defaultAlgorithm(t, mdb)

	// store a bunch of votes against a block
	l0ID := types.GetEffectiveGenesis()
	l1ID := l0ID.Add(1)
	l1 := makeLayer(t, l1ID, alg.trtl, defaultTestLayerSize, mdb, mdb.LayerBlockIds)
	alg.trtl.Last = l1ID
	l2ID := l1ID.Add(1)
	makeLayer(t, l2ID, alg.trtl, defaultTestLayerSize, mdb, mdb.LayerBlockIds)

	checkVerifiedLayer := func(layerID types.LayerID) {
		r.Equal(int(layerID), int(alg.trtl.Verified), "got unexpected value for last verified layer")
	}

	// don't attempt to heal recent layers
	t.Run("don't heal recent layers", func(t *testing.T) {
		checkVerifiedLayer(l0ID)

		// while bootstrapping there should be no healing
		alg.trtl.selfHealing(context.TODO(), l2ID)
		checkVerifiedLayer(l0ID)

		// later, healing should not occur on layers not at least Hdist back
		alg.trtl.Last = alg.trtl.Hdist.Add(1)
		alg.trtl.selfHealing(context.TODO(), l2ID)
		checkVerifiedLayer(l0ID)
	})

	// next, initiate healing without block votes, using the weak coin toss

	// missing coinflip
	t.Run("healing fails when layer coinflip is missing", func(t *testing.T) {
		checkVerifiedLayer(l0ID)
		alg.trtl.Last = alg.trtl.Hdist + l2ID
		alg.trtl.selfHealing(context.TODO(), l2ID)
		checkVerifiedLayer(l0ID)
	})

	// coinflip true
	t.Run("healing confirms blocks when coinflip is true", func(t *testing.T) {
		checkVerifiedLayer(l0ID)
		mdb.RecordCoinflip(context.TODO(), l1ID, true)
		alg.trtl.selfHealing(context.TODO(), l2ID)
		checkVerifiedLayer(l1ID)
		validBlocks, err := mdb.ContextuallyValidBlock(l1ID)
		r.NoError(err)
		l1BlockIDs := make(map[types.BlockID]struct{}, len(l1.Blocks()))
		for _, block := range l1.Blocks() {
			l1BlockIDs[block.ID()] = struct{}{}
		}
		r.Equal(l1BlockIDs, validBlocks)
	})

	// coinflip false
	l3ID := l2ID.Add(1)
	t.Run("healing does not confirm blocks when coinflip is false", func(t *testing.T) {
		checkVerifiedLayer(l1ID)
		mdb.RecordCoinflip(context.TODO(), l2ID, false)
		alg.trtl.selfHealing(context.TODO(), l3ID)
		checkVerifiedLayer(l2ID)
		validBlocks, err := mdb.ContextuallyValidBlock(l2ID)
		r.NoError(err)
		r.Equal(make(map[types.BlockID]struct{}, 0), validBlocks)
	})

	// next, attempt healing by counting block votes (global opinion)

	// make sure vote of non-good blocks are counted
	l4ID := l3ID.Add(1)
	t.Run("count vote of non-good blocks", func(t *testing.T) {
		checkVerifiedLayer(l2ID)

		// advance further so we can heal this layer
		topLayer := alg.trtl.Hdist + l3ID

		// fill in the interim layer data
		alg.trtl.Last = l2ID
		for i := l2ID.Add(1); i <= topLayer; i++ {
			makeLayer(t, i, alg.trtl, defaultTestLayerSize, mdb, mdb.LayerBlockIds)

			// process votes of layer blocks but don't attempt to verify a layer
			r.NoError(alg.trtl.handleLayerBlocks(context.TODO(), i))
		}

		alg.trtl.selfHealing(context.TODO(), l4ID)
		checkVerifiedLayer(l3ID)
	})

	// can heal when half of votes are missing (doesn't meet threshold)
	l5ID := l4ID.Add(1)
	t.Run("can heal when lots of votes are missing", func(t *testing.T) {
		checkVerifiedLayer(l3ID)
		alg.trtl.Last = alg.trtl.Hdist + l4ID
		mdb.RecordCoinflip(context.TODO(), l4ID, true)
		alg.trtl.selfHealing(context.TODO(), l5ID)
		checkVerifiedLayer(l4ID)
	})

	// can heal when global and local opinion differ
	// make sure contextual validity is updated

	// can heal when global opinion is undecided (split 50/50)

	// can "re-heal" when new information arrives (simulate partition ending/reorg)

}

//func TestRevert(t *testing.T) {
//	r := require.New(t)
//
//  // test revert/reprocess state
//
//}
