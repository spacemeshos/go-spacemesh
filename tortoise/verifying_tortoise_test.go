package tortoise

import (
	"context"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/signing"
	"os"
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

const Path = "../tmp/tortoise/"

func getPersistentMesh() (*mesh.DB, func() error) {
	path := Path + "ninja_tortoise"
	teardown := func() error { return os.RemoveAll(path) }
	if err := teardown(); err != nil {
		panic(err)
	}
	db, _ := mesh.NewPersistentMeshDB(fmt.Sprintf(path+"/"), 10, log.NewDefault("ninja_tortoise").WithOptions(log.Nop))
	return db, teardown
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
		globalOpinion := calculateGlobalOpinion(trtl.logger, sum, trtl.AvgLayerSize, 1)
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

	trtl = newTurtle(msh, defaultTestHdist, defaultTestZdist, defaultTestConfidenceParam, defaultTestWindowSize, blocksPerLayer, defaultTestRerunInterval)
	trtl.init(context.TODO(), mesh.GenesisLayer())

	var l types.LayerID
	for l = mesh.GenesisLayer().Index() + 1; l <= numLayers; l++ {
		turtleMakeAndProcessLayer(t, l, trtl, blocksPerLayer, msh, inputVectorFn)
		fmt.Println("handled layer", l, "========================================================================")
	}

	return
}

func turtleMakeAndProcessLayer(t *testing.T, l types.LayerID, trtl *turtle, blocksPerLayer int, msh *mesh.DB, inputVectorFn func(id types.LayerID) ([]types.BlockID, error)) {
	fmt.Println("choosing base block for layer", l)
	msh.InputVectorBackupFunc = inputVectorFn
	b, lists, err := trtl.BaseBlock(context.TODO())
	fmt.Println("base block for layer", l, "is", b)
	if err != nil {
		panic(fmt.Sprint("no base block found:", err))
	}
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
		err = msh.AddBlock(blk)
		if err != nil {
			fmt.Println("error adding block to database:", err)
		}
	}

	// write blocks to database first; the verifying tortoise will subsequently read them
	blocks, err := inputVectorFn(l)
	if err == nil {
		// save blocks to db for this layer
		require.NoError(t, msh.SaveLayerInputVectorByID(l, blocks))
	}

	if err := trtl.HandleIncomingLayer(context.TODO(), lyr.Index()); err != nil {
		trtl.logger.With().Warning("got error from HandleIncomingLayer", log.Err(err))
	}
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

	trtl := newTurtle(msh, defaultTestHdist, defaultTestZdist, defaultTestConfidenceParam, defaultTestWindowSize, blocksPerLayer, defaultTestRerunInterval)
	gen := mesh.GenesisLayer()
	trtl.init(context.TODO(), gen)

	var l types.LayerID
	for l = types.GetEffectiveGenesis() + 1; l < layers; l++ {
		turtleMakeAndProcessLayer(t, l, trtl, blocksPerLayer, msh, layerfuncs[l-types.GetEffectiveGenesis()-1])
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
	fmt.Println("the base block for layer", l, "is", b, ". exception lists:")
	fmt.Println("\tagainst\t", lists[0])
	fmt.Println("\tfor\t", lists[1])
	fmt.Println("\tneutral\t", lists[2])
	if err != nil {
		panic(fmt.Sprint("no base block for layer:", err))
	}

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
	blocks, err := ivp(l-1)
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
	mdb, teardown := getPersistentMesh()
	defer func() {
		require.NoError(t, teardown())
	}()

	getHareResults := mdb.LayerBlockIds

	mdb.InputVectorBackupFunc = getHareResults

	lg := log.NewDefault(t.Name())
	alg := verifyingTortoise(context.TODO(), defaultTestLayerSize, mdb, defaultTestHdist, defaultTestZdist, defaultTestConfidenceParam, defaultTestWindowSize, defaultTestRerunInterval, lg)
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
	mdb, teardown := getPersistentMesh()
	defer func() {
		require.NoError(t, teardown())
	}()

	getHareResults := mdb.LayerBlockIds

	mdb.InputVectorBackupFunc = getHareResults

	lg := log.NewDefault(t.Name())
	alg := verifyingTortoise(context.TODO(), defaultTestLayerSize, mdb, defaultTestHdist, defaultTestZdist, defaultTestConfidenceParam, defaultTestWindowSize, defaultTestRerunInterval, lg)

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
	alg2 := recoveredVerifyingTortoise(mdb, lg)
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
	mdb, teardown := getPersistentMesh()
	defer func() {
		require.NoError(t, teardown())
	}()
	lg := log.NewDefault(t.Name())
	alg := verifyingTortoise(context.TODO(), defaultTestLayerSize, mdb, defaultTestHdist, defaultTestZdist, defaultTestConfidenceParam, defaultTestWindowSize, defaultTestRerunInterval, lg)
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
	mdb, teardown := getPersistentMesh()
	defer func() {
		require.NoError(t, teardown())
	}()
	lg := log.NewDefault(t.Name())
	alg := verifyingTortoise(context.TODO(), defaultTestLayerSize, mdb, defaultTestHdist, defaultTestZdist, defaultTestConfidenceParam, defaultTestWindowSize, defaultTestRerunInterval, lg)

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
	l2 := createTurtleLayer(l2ID, mdb, alg.BaseBlock, mdb.LayerBlockIds, defaultTestLayerSize)
	for _, b := range l2.Blocks() {
		r.NoError(mdb.AddBlock(b))
	}
	opinionVec, err = alg.trtl.layerOpinionVector(context.TODO(), l2ID)
	r.NoError(err)
	r.Equal(make([]types.BlockID, 0, 0), opinionVec)

	// otherwise, we expect the set of contextually valid blocks
	for _, b := range l2.Blocks() {
		r.NoError(mdb.SaveContextualValidity(b.ID(), l2ID, true))
	}
	opinionVec, err = alg.trtl.layerOpinionVector(context.TODO(), l2ID)
	r.NoError(err)
	// this is the easiest way to compare a set of blockIDs
	r.Equal(l2.Hash(), types.CalcBlocksHash32(types.SortBlockIDs(opinionVec), nil))
}

func TestBaseBlock(t *testing.T) {
	r := require.New(t)
	mdb, teardown := getPersistentMesh()
	defer func() {
		require.NoError(t, teardown())
	}()
	lg := log.NewDefault(t.Name())
	alg := verifyingTortoise(context.TODO(), defaultTestLayerSize, mdb, defaultTestHdist, defaultTestZdist, defaultTestConfidenceParam, defaultTestWindowSize, defaultTestRerunInterval, lg)

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

func TestCloneTurtle(t *testing.T) {
	r := require.New(t)
	mdb, teardown := getPersistentMesh()
	defer func() {
		require.NoError(t, teardown())
	}()
	trtl := newTurtle(mdb, defaultTestHdist, defaultTestZdist, defaultTestConfidenceParam, defaultTestWindowSize, defaultTestLayerSize, defaultTestRerunInterval)
	trtl.AvgLayerSize += 1 // make sure defaults aren't being read
	trtl.Last = 10 // state should not be cloned
	trtl2 := trtl.cloneTurtle()
	r.Equal(trtl.bdp, trtl2.bdp)
	r.Equal(trtl.Hdist, trtl2.Hdist)
	r.Equal(trtl.Zdist, trtl2.Zdist)
	r.Equal(trtl.ConfidenceParam, trtl2.ConfidenceParam)
	r.Equal(trtl.WindowSize, trtl2.WindowSize)
	r.Equal(trtl.AvgLayerSize, trtl2.AvgLayerSize)
	r.Equal(trtl.RerunInterval, trtl2.RerunInterval)
	r.NotEqual(trtl.Last, trtl2.Last)
}

func TestGetSingleInputVector(t *testing.T) {
	r := require.New(t)
	mdb, teardown := getPersistentMesh()
	defer func() {
		require.NoError(t, teardown())
	}()
	lg := log.NewDefault(t.Name())
	alg := verifyingTortoise(context.TODO(), defaultTestLayerSize, mdb, defaultTestHdist, defaultTestZdist, defaultTestConfidenceParam, defaultTestWindowSize, defaultTestRerunInterval, lg)

	l1ID := types.GetEffectiveGenesis()+1
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
	mdb, teardown := getPersistentMesh()
	defer func() {
		require.NoError(t, teardown())
	}()
	lg := log.NewDefault(t.Name())
	alg := verifyingTortoise(context.TODO(), defaultTestLayerSize, mdb, defaultTestHdist, defaultTestZdist, defaultTestConfidenceParam, defaultTestWindowSize, defaultTestRerunInterval, lg)

	l1ID := types.GetEffectiveGenesis()+1
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
	mdb, teardown := getPersistentMesh()
	defer func() {
		require.NoError(t, teardown())
	}()

	lg := log.NewDefault(t.Name())
	alg := verifyingTortoise(context.TODO(), defaultTestLayerSize, mdb, defaultTestHdist, defaultTestZdist, defaultTestConfidenceParam, defaultTestWindowSize, defaultTestRerunInterval, lg)

	// helper function for checking votes
	expectVotes := func(votes []map[types.BlockID]struct{}, numAgainst, numFor, numNeutral int) {
		r.Len(votes, 3, "vote vector size is wrong")
		r.Len(votes[0], numAgainst) // against
		r.Len(votes[1], numFor) // for
		r.Len(votes[2], numNeutral) // neutral
	}

	// genesis layer
	l0ID := types.GetEffectiveGenesis()
	votes, err := alg.trtl.calculateExceptions(context.TODO(), l0ID, Opinion{})
	r.NoError(err)
	// expect votes in support of all genesis blocks
	expectVotes(votes, 0, len(mesh.GenesisLayer().Blocks()), 0)

	// no processed data: expect only support for genesis blocks
	l1ID := l0ID.Add(1)
	votes, err = alg.trtl.calculateExceptions(context.TODO(), l1ID, Opinion{})
	r.NoError(err)
	expectVotes(votes, 0, len(mesh.GenesisLayer().Blocks()), 0)

	// now advance the processed layer
	alg.trtl.Last = l1ID

	// missing layer data
	votes, err = alg.trtl.calculateExceptions(context.TODO(), l1ID, Opinion{})
	r.Equal(database.ErrNotFound, err)
	r.Nil(votes)

	// layer opinion vector is nil (abstains): recent layer, in mesh, no input vector
	l1 := createTurtleLayer(l1ID, mdb, alg.BaseBlock, mdb.LayerBlockIds, defaultTestLayerSize)
	r.NoError(addLayerToMesh(mdb, l1))
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
		l1.Blocks()[0].ID(): support,
		l1.Blocks()[1].ID(): support,
		l1.Blocks()[2].ID(): support,
	}}
	votes, err = alg.trtl.calculateExceptions(context.TODO(), l1ID, opinion)
	r.NoError(err)
	expectVotes(votes, 0, 0, 0)

	// compare opinions: all disagree, adds exceptions
	opinion = Opinion{BlockOpinions: map[types.BlockID]vec{
		mesh.GenesisBlock().ID(): against,
		l1.Blocks()[0].ID(): against,
		l1.Blocks()[1].ID(): against,
		l1.Blocks()[2].ID(): against,
	}}
	votes, err = alg.trtl.calculateExceptions(context.TODO(), l1ID, opinion)
	r.NoError(err)
	expectVotes(votes, 0, 4, 0)

	// exceeding max exceptions
	alg.trtl.MaxExceptions = 10
	l2ID := l1ID.Add(1)
	l3ID := l2ID.Add(1)
	l2 := createTurtleLayer(l2ID, mdb, alg.BaseBlock, mdb.LayerBlockIds, alg.trtl.MaxExceptions+1)
	for _, block := range l2.Blocks() {
		r.NoError(mdb.AddBlock(block))
	}
	l3 := createTurtleLayer(l3ID, mdb, alg.BaseBlock, mdb.LayerBlockIds, defaultTestLayerSize)
	alg.trtl.Last = l2ID
	votes, err = alg.trtl.calculateExceptions(context.TODO(), l2ID, Opinion{})
	r.Error(err)
	r.Contains(err.Error(), errstrTooManyExceptions, "expected too many exceptions error")
	r.Nil(votes)

	// TODO: test adding base block opinion in support of a block that disagrees with the local opinion, e.g., a block
	//   that this node has not seen yet. See https://github.com/spacemeshos/go-spacemesh/issues/2424.

	// advance the evicted layer until the exception layer slides outside the sliding window
	for _, block := range l3.Blocks() {
		r.NoError(mdb.AddBlock(block))
	}
	l4ID := l3ID.Add(1)
	l4 := createTurtleLayer(l4ID, mdb, alg.BaseBlock, mdb.LayerBlockIds, defaultTestLayerSize)
	for _, block := range l4.Blocks() {
		r.NoError(mdb.AddBlock(block))
	}
	l5ID := l4ID.Add(1)
	createTurtleLayer(l5ID, mdb, alg.BaseBlock, mdb.LayerBlockIds, defaultTestLayerSize)
	alg.trtl.LastEvicted = l2ID
	alg.trtl.Last = l4ID
	votes, err = alg.trtl.calculateExceptions(context.TODO(), l3ID, Opinion{})
	r.NoError(err)
	// expect votes FOR the blocks in the two intervening layers between the base block layer and the last layer
	expectVotes(votes, 0, 2*defaultTestLayerSize, 0)
}

//func TestProcessBlock(t *testing.T) {
//	r := require.New(t)
//
//}
//
//func TestProcessNewBlocks(t *testing.T) {
//	r := require.New(t)
//
//}
//
//func TestVerifyLayers(t *testing.T) {
//	r := require.New(t)
//
//}
//
//func TestSumVotesForBlock(t *testing.T) {
//	r := require.New(t)
//
//}
//
//func TestHealing(t *testing.T) {
//	r := require.New(t)
//
//}
//
//func TestRevert(t *testing.T) {
//	r := require.New(t)
//
//}