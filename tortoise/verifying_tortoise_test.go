package tortoise

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/assert"
	"os"
	"strconv"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/stretchr/testify/require"
)

func init() {
	types.SetLayersPerEpoch(4)
}

const Path = "../tmp/tortoise/"

const inmem = 1
const disc = 2

const memType = inmem

func getPersistentMash() (*mesh.DB, func() error) {
	path := Path + "ninje_tortoise"
	teardown := func() error { return os.RemoveAll(path) }
	if err := teardown(); err != nil {
		panic(err)
	}
	db, _ := mesh.NewPersistentMeshDB(fmt.Sprintf(path+
		"/"), 10, log.NewDefault("ninje_tortoise").WithOptions(log.Nop))
	return db, teardown
}

func persistenceTeardown() {
	os.RemoveAll(Path)
}

func getInMemMesh() *mesh.DB {
	return mesh.NewMemMeshDB(log.NewDefault(""))
}

func AddLayer(m *mesh.DB, layer *types.Layer) error {
	//add blocks to mDB
	for _, bl := range layer.Blocks() {
		if err := m.AddBlock(bl); err != nil {
			return err
		}
	}
	return nil
}

var defaultTestHdist = config.DefaultConfig().Hdist

func requireVote(t *testing.T, trtl *turtle, vote vec, blocks ...types.BlockID) {
	for _, i := range blocks {
		sum := abstain
		blk, _ := trtl.bdp.GetBlock(i)

		for l := trtl.Last; l > blk.LayerIndex; l-- {

			trtl.logger.Info("Counting votes of blocks in layer %v on %v (lyr: %v)", l, i.String(), blk.LayerIndex)

			for bid, opinionVote := range trtl.BlocksToBlocks[l] {
				opinionVote, ok := opinionVote.BlocksOpinion[i]
				if !ok {
					continue
				}

				//t.logger.Info("block %v is good and voting vote %v", vopinion.id, opinionVote)
				sum = sum.Add(opinionVote.Multiply(trtl.BlockWeight(bid, i)))
			}
		}
		gop := globalOpinion(sum, trtl.AvgLayerSize, 1)
		if gop != vote {
			require.Fail(t, fmt.Sprintf("crashing test block %v should be %v but %v", i, vote, sum))
		}
	}
}

func TestTurtle_HandleIncomingLayerHappyFlow(t *testing.T) {
	layers := types.GetEffectiveGenesis() + 28
	avgPerLayer := 10
	voteNegative := 0
	trtl, _, _ := turtleSanity(t, layers, avgPerLayer, voteNegative, 0)
	require.Equal(t, int(layers-1), int(trtl.Verified))
	blkids := make([]types.BlockID, 0, avgPerLayer*int(layers))
	for l := types.LayerID(0); l < layers; l++ {
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
	require.Equal(t, int(types.GetEffectiveGenesis()), int(trtl.Verified), "when all votes abstain verification should stay at first layer and advance")
	requireVote(t, trtl, abstain, abs...)
}

// voteNegative - the amoutn of blocks to vote negative per layer
// voteAbstain - the amoutn of layers to vote abstain because we always abstain on a whole layer
func turtleSanity(t testing.TB, layers types.LayerID, blocksPerLayer, voteNegative int, voteAbstain int) (trtl *turtle, negative []types.BlockID, abstains []types.BlockID) {
	msh := getInMemMesh()

	newlyrs := make(map[types.LayerID]struct{})

	hm := func(l types.LayerID) (ids []types.BlockID, err error) {
		if l < mesh.GenesisLayer().Index() {
			panic("should'nt happen")
		}
		if l == mesh.GenesisLayer().Index() {
			return types.BlockIDs(mesh.GenesisLayer().Blocks()), nil
		}

		_, exist := newlyrs[l]

		if !exist && l != layers {
			newlyrs[l] = struct{}{}
		}

		blks, err := msh.LayerBlockIds(l)
		if err != nil {
			panic("db err")
		}

		if voteAbstain > 0 {
			if !exist && l != layers {
				voteAbstain--
				abstains = append(abstains, blks...)
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

		if !exist && l != layers {
			negative = append(negative, sorted[:voteNegative]...)
		}
		return sorted[voteNegative:], nil
	}

	trtl = newTurtle(msh, defaultTestHdist, blocksPerLayer)
	gen := mesh.GenesisLayer()
	trtl.init(gen)

	var l types.LayerID
	for l = mesh.GenesisLayer().Index() + 1; l <= layers; l++ {
		turtleMakeAndProcessLayer(l, trtl, blocksPerLayer, msh, hm)
		fmt.Println("Handled ", l, "========================================================================")
	}

	return
}

func turtleMakeAndProcessLayer(l types.LayerID, trtl *turtle, blocksPerLayer int, msh *mesh.DB, hm func(id types.LayerID) ([]types.BlockID, error)) {
	fmt.Println("choosing base block layer ", l)
	msh.InputVectorBackupFunc = hm
	b, lists, err := trtl.BaseBlock()
	fmt.Println("the base block for ", l, "is ", b)
	if err != nil {
		panic(fmt.Sprint("no base - ", err))
	}
	lyr := types.NewLayer(l)
	//if err := msh.SaveLayerInputVector(l, blocks); err != nil {
	//	panic("db is fucked up")
	//}

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
			fmt.Println("Err inserting to db - ", err)
		}
	}

	blocks, err := hm(l)
	if err != nil {
		blocks = nil
	}

	if blocks == nil {
		trtl.HandleIncomingLayer(lyr, nil)
	} else {
		trtl.HandleIncomingLayer(lyr, blocks)
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
			fmt.Println("Giveing good results for layer", id)
			return msh.LayerBlockIds(id)
		})
	}

	// next up two layers that didn't finish
	newlastlyr := types.LayerID(len(layerfuncs))
	for i := newlastlyr; i < newlastlyr+2; i++ {
		layerfuncs = append(layerfuncs, func(id types.LayerID) (ids []types.BlockID, err error) {
			fmt.Println("Giving bad result for layer ", id)
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

	trtl := newTurtle(msh, defaultTestHdist, blocksPerLayer)
	gen := mesh.GenesisLayer()
	trtl.init(gen)

	var l types.LayerID
	for l = types.GetEffectiveGenesis() + 1; l < layers; l++ {
		turtleMakeAndProcessLayer(l, trtl, blocksPerLayer, msh, layerfuncs[l-types.GetEffectiveGenesis()-1])
		fmt.Println("Handled ", l, " Verified ", trtl.Verified, "========================================================================")
	}

	require.Equal(t, int(types.GetEffectiveGenesis()+5), int(trtl.Verified), "verification should advance after hare finishes")
	//todo: also check votes with requireVote
}

type baseBlockProvider func() (types.BlockID, [][]types.BlockID, error)
type inputVectorProvider func(l types.LayerID) ([]types.BlockID, error)

func createTurtleLayer(l types.LayerID, msh *mesh.DB, bbp baseBlockProvider, ivp inputVectorProvider, blocksPerLayer int) *types.Layer {
	fmt.Println("choosing base block layer ", l)
	msh.InputVectorBackupFunc = ivp
	b, lists, err := bbp()
	fmt.Println("the base block for ", l, "is ", b)
	fmt.Println("Against ", lists[0])
	fmt.Println("For ", lists[1])
	fmt.Println("Neutral ", lists[2])
	if err != nil {
		panic(fmt.Sprint("no base - ", err))
	}
	lyr := types.NewLayer(l)

	blocks, err := ivp(l - 1)
	if err != nil {
		blocks = nil
	}
	if err := msh.SaveLayerInputVectorByID(l-1, blocks); err != nil {
		panic("db is fucked up")
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
	layers := types.LayerID(defaultTestHdist * 10)
	avgPerLayer := 10
	voteNegative := 0
	trtl, _, _ := turtleSanity(t, layers, avgPerLayer, voteNegative, 0)
	require.Equal(t, len(trtl.BlocksToBlocks),
		(defaultTestHdist + 2))
	count := 0
	for _, blks := range trtl.BlocksToBlocks {
		count += len(blks)
	}
	require.Equal(t, count,
		(defaultTestHdist+2)*avgPerLayer)
	require.Equal(t, len(trtl.GoodBlocksIndex),
		(defaultTestHdist+2)*avgPerLayer) // all blocks should be good
}

//func TestTurtle_Eviction2(t *testing.T) {
//	layers := types.LayerID(defaultTestHdist * 14)
//	avgPerLayer := 30
//	voteNegative := 5
//	trtl, _, _ := turtleSanity(t, layers, avgPerLayer, voteNegative, 0)
//	require.Equal(t, len(trtl.BlocksToBlocks),
//		(defaultTestHdist+2)*avgPerLayer)
//}

func TestTurtle_Recovery(t *testing.T) {
	log.DebugMode(true)
	mdb, teardown := getPersistentMash()

	getHareResults := func(l types.LayerID) ([]types.BlockID, error) {
		return mdb.LayerBlockIds(l)
	}

	mdb.InputVectorBackupFunc = getHareResults

	lg := log.NewDefault(t.Name())
	alg := verifyingTortoise(3, mdb, 5, lg)
	l := mesh.GenesisLayer()

	log.With().Info("The genesis is ", l.Index(), types.BlockIdsField(types.BlockIDs(l.Blocks())))
	log.With().Info("The genesis is ", l.Blocks()[0].Fields()...)

	l1 := createTurtleLayer(types.GetEffectiveGenesis()+1, mdb, alg.BaseBlock, getHareResults, 3)
	require.NoError(t, AddLayer(mdb, l1))

	l1res, _ := getHareResults(types.GetEffectiveGenesis() + 1)

	log.With().Info("The first is ", l1.Index(), types.BlockIdsField(types.BlockIDs(l1.Blocks())))
	log.With().Info("The first bb is ", l1.Index(), l1.Blocks()[0].BaseBlock, types.BlockIdsField(l1.Blocks()[0].ForDiff))

	alg.HandleIncomingLayer(l1, l1res)
	require.NoError(t, alg.Persist())

	l2 := createTurtleLayer(types.GetEffectiveGenesis()+2, mdb, alg.BaseBlock, getHareResults, 3)
	require.NoError(t, AddLayer(mdb, l2))
	l2res, _ := getHareResults(types.GetEffectiveGenesis() + 2)
	alg.HandleIncomingLayer(l2, l2res)

	require.NoError(t, alg.Persist())

	require.Equal(t, int(types.GetEffectiveGenesis()+1), int(alg.LatestComplete()))

	l31 := createTurtleLayer(types.GetEffectiveGenesis()+3, mdb, alg.BaseBlock, getHareResults, 4)

	l32 := createTurtleLayer(types.GetEffectiveGenesis()+3, mdb, func() (types.BlockID, [][]types.BlockID, error) {
		diffs := make([][]types.BlockID, 3)
		diffs[0] = make([]types.BlockID, 0)
		diffs[1] = types.BlockIDs(l.Blocks())
		diffs[2] = make([]types.BlockID, 0)

		return l31.Blocks()[0].ID(), diffs, nil
	}, getHareResults, 5)

	defer func() {
		if r := recover(); r != nil {
			t.Log("Recovered from", r)
		}

		log.Info("I've recovered")

		alg := recoveredVerifyingTortoise(mdb, lg)

		l2res, _ := getHareResults(types.GetEffectiveGenesis() + 2)
		alg.HandleIncomingLayer(l2, l2res)

		l3 := createTurtleLayer(types.GetEffectiveGenesis()+3, mdb, alg.BaseBlock, getHareResults, 3)
		AddLayer(mdb, l3)
		l3res, _ := getHareResults(types.GetEffectiveGenesis() + 3)
		alg.HandleIncomingLayer(l3, l3res)
		alg.Persist()

		l4 := createTurtleLayer(types.GetEffectiveGenesis()+4, mdb, alg.BaseBlock, getHareResults, 3)
		AddLayer(mdb, l4)
		l4res, _ := getHareResults(types.GetEffectiveGenesis() + 4)
		alg.HandleIncomingLayer(l4, l4res)
		alg.Persist()
		assert.True(t, alg.LatestComplete() == types.GetEffectiveGenesis()+3)

		require.NoError(t, teardown())

	}()

	l3res, _ := getHareResults(types.GetEffectiveGenesis() + 3)
	alg.HandleIncomingLayer(l32, l3res) //crash
}
