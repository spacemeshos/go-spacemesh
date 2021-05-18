package tortoise

import (
	"context"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/config"
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
	db, _ := mesh.NewPersistentMeshDB(fmt.Sprintf(path+
		"/"), 10, log.NewDefault("ninja_tortoise").WithOptions(log.Nop))
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

func createTurtleLayer(l types.LayerID, msh *mesh.DB, bbp baseBlockProvider, ivp inputVectorProvider, blocksPerLayer int) *types.Layer {
	fmt.Println("choosing base block for layer", l)
	msh.InputVectorBackupFunc = ivp
	b, lists, err := bbp(context.TODO())
	fmt.Println("the base block for layer", l, "is", b, ". exception lists:")
	fmt.Println("\tagainst\t", lists[0])
	fmt.Println("\tfor\t", lists[1])
	fmt.Println("\tneutral\t", lists[2])
	if err != nil {
		panic(fmt.Sprint("no base block for layer:", err))
	}
	lyr := types.NewLayer(l)

	blocks, err := ivp(l - 1)
	if err != nil {
		blocks = nil
	}
	if err := msh.SaveLayerInputVectorByID(l-1, blocks); err != nil {
		panic("database error")
	}

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

	getHareResults := func(l types.LayerID) ([]types.BlockID, error) {
		return mdb.LayerBlockIds(l)
	}

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

	getHareResults := func(l types.LayerID) ([]types.BlockID, error) {
		return mdb.LayerBlockIds(l)
	}

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

	getHareResults := func(l types.LayerID) ([]types.BlockID, error) {
		return mdb.LayerBlockIds(l)
	}
	mdb.InputVectorBackupFunc = getHareResults

	// no rerun
	l1 := createTurtleLayer(types.GetEffectiveGenesis()+1, mdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
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
	log.DebugMode(true)
	r := require.New(t)
	mdb, teardown := getPersistentMesh()
	defer func() {
		require.NoError(t, teardown())
	}()
	lg := log.NewDefault(t.Name())
	alg := verifyingTortoise(context.TODO(), defaultTestLayerSize, mdb, defaultTestHdist, defaultTestZdist, defaultTestConfidenceParam, defaultTestWindowSize, defaultTestRerunInterval, lg)

	getHareResults := func(l types.LayerID) ([]types.BlockID, error) {
		return mdb.LayerBlockIds(l)
	}
	mdb.InputVectorBackupFunc = getHareResults
	l0 := mesh.GenesisLayer()

	// recent layer missing from mesh: should abstain and keep waiting
	l1ID := l0.Index().Add(1)
	opinionVec, err := alg.trtl.layerOpinionVector(context.TODO(), l1ID)
	r.NoError(err)
	r.Nil(opinionVec)
	//r.Len(opinionVec, 0, "expected empty opinion vector")

	// hare failed for layer: should vote against all blocks
	mdb.InvalidateLayer(context.TODO(), l1ID)
	opinionVec, err = alg.trtl.layerOpinionVector(context.TODO(), l1ID)
	r.NoError(err)
	r.Equal(make([]types.BlockID, 0, 0), opinionVec)

	// old layer missing from mesh: should vote against all blocks
	// simulate old layer by advancing Last
	l2ID := l1ID.Add(1)
	alg.trtl.Last = types.LayerID(defaultTestZdist) + l2ID.Add(10)
	opinionVec, err = alg.trtl.layerOpinionVector(context.TODO(), l2ID)
	r.NoError(err)
	r.Equal(make([]types.BlockID, 0, 0), opinionVec)
}

func TestBaseBlock(t *testing.T) {
	log.DebugMode(true)
	r := require.New(t)
	mdb, teardown := getPersistentMesh()
	defer func() {
		require.NoError(t, teardown())
	}()
	lg := log.NewDefault(t.Name())
	alg := verifyingTortoise(context.TODO(), defaultTestLayerSize, mdb, defaultTestHdist, defaultTestZdist, defaultTestConfidenceParam, defaultTestWindowSize, defaultTestRerunInterval, lg)

	getHareResults := func(l types.LayerID) ([]types.BlockID, error) {
		return mdb.LayerBlockIds(l)
	}
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

	l1 := createTurtleLayer(types.GetEffectiveGenesis()+1, mdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l1))
	alg.HandleIncomingLayer(context.TODO(), l1.Index())
	expectBaseBlockLayer(l1.Index(), 0, defaultTestLayerSize, 0)

	l2 := createTurtleLayer(types.GetEffectiveGenesis()+2, mdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	require.NoError(t, addLayerToMesh(mdb, l2))
	alg.HandleIncomingLayer(context.TODO(), l2.Index())
	require.Equal(t, int(types.GetEffectiveGenesis()+1), int(alg.LatestComplete()))
	expectBaseBlockLayer(l2.Index(), 0, defaultTestLayerSize, 0)

	// add a layer that's not in the mesh
	l3 := createTurtleLayer(types.GetEffectiveGenesis()+3, mdb, alg.BaseBlock, getHareResults, defaultTestLayerSize)
	//require.NoError(t, addLayerToMesh(mdb, l2))
	alg.HandleIncomingLayer(context.TODO(), l3.Index())

	// mark all blocks bad
	alg.trtl.GoodBlocksIndex = make(map[types.BlockID]struct{}, 0)
	baseBlockID, exceptions, err := alg.BaseBlock(context.TODO())
	r.Equal(errNoBaseBlockFound, err)
	r.Equal(types.BlockID{0}, baseBlockID)
	r.Nil(exceptions)

	//l3a := createTurtleLayer(types.GetEffectiveGenesis()+3, mdb, alg.BaseBlock, getHareResults, 4)
	//l3b := createTurtleLayer(types.GetEffectiveGenesis()+3, mdb, func(context.Context) (types.BlockID, [][]types.BlockID, error) {
	//	diffs := make([][]types.BlockID, 3)
	//	diffs[0] = make([]types.BlockID, 0)    // against
	//	diffs[1] = types.BlockIDs(l0.Blocks()) // support
	//	diffs[2] = make([]types.BlockID, 0)    // neutral
	//	return l3a.Blocks()[0].ID(), diffs, nil
	//}, getHareResults, 5)
}