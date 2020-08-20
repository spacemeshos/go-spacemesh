package tortoise

import (
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/stretchr/testify/require"
)

const defaultTestHdist = 5

func requireVote(t *testing.T, trtl *turtle, vote vec, blocks ...types.BlockID) {
	for _, i := range blocks {
		sum := abstain
		for _, vopinion := range trtl.BlocksToBlocks {

			blk, _ := trtl.bdp.GetBlock(i)

			if vopinion.LayerID <= blk.LayerIndex {
				continue
			}

			opinionVote, ok := vopinion.blocksOpinion[i]
			if !ok {
				continue
			}

			//t.logger.Info("block %v is good and voting vote %v", vopinion.id, opinionVote)
			sum = sum.Add(opinionVote.Multiply(trtl.BlockWeight(vopinion.BlockID, i)))
		}
		require.Equal(t, globalOpinion(sum, trtl.avgLayerSize, 1), vote)
	}
}

func TestTurtle_HandleIncomingLayerHappyFlow(t *testing.T) {
	log.DebugMode(true)
	layers := types.LayerID(10)
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
	layers := types.LayerID(10)
	avgPerLayer := 10
	voteNegative := 5
	trtl, negs, _ := turtleSanity(t, layers, avgPerLayer, voteNegative, 0)
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
	requireVote(t, trtl, against, negs...)
	requireVote(t, trtl, support, poblkids...)
}

func TestTurtle_HandleIncomingLayer_VoteAbstain(t *testing.T) {
	layers := types.LayerID(10)
	avgPerLayer := 10
	trtl, negs, abs := turtleSanity(t, layers, avgPerLayer, 0, 10)
	require.Equal(t, 0, int(trtl.Verified), "when all votes abstain verification should stay at first layer and advance")
	requireVote(t, trtl, against, negs...)
	requireVote(t, trtl, abstain, abs...)
	poblkids := make([]types.BlockID, 0, avgPerLayer*int(layers))
	for l := types.LayerID(0); l < layers; l++ {
		lids, _ := trtl.bdp.LayerBlockIds(l)
		for _, lid := range lids {
			if !inArr(lid, negs) && !inArr(lid, abs) {
				poblkids = append(poblkids, lid)
			}
		}
	}
	requireVote(t, trtl, support, poblkids...)
}

func turtleSanity(t testing.TB, layers types.LayerID, blocksPerLayer, voteNegative int, voteAbstain int) (trtl *turtle, negative []types.BlockID, abstains []types.BlockID) {
	msh := getInMemMesh()

	abstainCount := 0

	hm := func(l types.LayerID) (ids []types.BlockID, err error) {
		if l == 0 {
			return types.BlockIDs(mesh.GenesisLayer().Blocks()), nil
		}

		if voteAbstain > 0 && abstainCount <= voteAbstain {
			abstainCount += 1
		}

		if voteAbstain > 0 && abstainCount >= int(layers)-voteAbstain {
			all, _ := msh.LayerBlockIds(l)
			abstains = append(abstains, all...)
			return nil, errors.New("hare didn't finish")
		}

		if voteNegative == 0 {
			return msh.LayerBlockIds(l)
		}

		blks, err := msh.LayerBlockIds(l)
		if err != nil {
			panic("db err")
		}
		negative = append(negative, blks[:voteNegative]...)
		return blks[voteNegative:], nil
	}

	trtl = NewTurtle(msh, defaultTestHdist, blocksPerLayer)
	gen := mesh.GenesisLayer()
	require.NoError(t, AddLayer(msh, gen))
	trtl.init(gen)

	var l types.LayerID
	for l = 1; l <= layers; l++ {
		turtleMakeAndProcessLayer(l, trtl, blocksPerLayer, msh, hm)
		fmt.Println("Handled ", l, "========================================================================")
	}

	return
}

func turtleMakeAndProcessLayer(l types.LayerID, trtl *turtle, blocksPerLayer int, msh *mesh.DB, hm func(id types.LayerID) ([]types.BlockID, error)) {
	fmt.Println("choosing base block layer ", l)
	b, lists, err := trtl.BaseBlock(hm)
	fmt.Println("the base block for ", l, "is ", b)
	if err != nil {
		panic(fmt.Sprint("no base - ", err))
	}
	lyr := types.NewLayer(l)
	blocks, err := hm(l - 1)
	if err != nil {
		blocks = nil
	}
	if err := msh.SaveLayerInputVector(l-1, blocks); err != nil {
		panic("db is fucked up")
	}

	for i := 0; i < blocksPerLayer; i++ {
		blk := types.NewExistingBlock(l, []byte(strconv.Itoa(i)))

		blk.BaseBlock = b
		blk.AgainstDiff = lists[0]
		blk.ForDiff = lists[1]
		blk.NeutralDiff = lists[2]
		//if blocks != nil {
		//	blk.ForDiff = append(blk.ForDiff, blocks...)
		//badblocks:
		//	for _, bi := range prevlyr {
		//		for _, bj := range blocks {
		//			if bi == bj {
		//				continue badblocks
		//			}
		//		}
		//		blk.AgainstDiff = append(blk.AgainstDiff, bi)
		//	}
		//} else {
		//	blks, err := msh.LayerBlockIds(l-1)
		//	if err != nil {
		//		panic("db err")
		//	}
		//	blk.NeutralDiff = append(blk.NeutralDiff, blks...)
		//}

		lyr.AddBlock(blk)
		err = msh.AddBlock(blk)
		if err != nil {
			fmt.Println("Err inserting to db - ", err)
		}
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
			return msh.LayerBlockIds(id)
		})
	}

	// next up two layers that didn't finish
	newlastlyr := types.LayerID(len(layerfuncs))
	for i := newlastlyr; i <= newlastlyr+2; i++ {
		layerfuncs = append(layerfuncs, func(id types.LayerID) (ids []types.BlockID, err error) {
			fmt.Println("Giving bad result for layer ", id)
			return nil, errors.New("idontknow")
		})
	}

	// more good layers
	newlastlyr = types.LayerID(len(layerfuncs))
	for i := newlastlyr; i <= newlastlyr+5; i++ {
		layerfuncs = append(layerfuncs, func(id types.LayerID) (ids []types.BlockID, err error) {
			return msh.LayerBlockIds(id)
		})
	}

	trtl := NewTurtle(msh, defaultTestHdist, blocksPerLayer)
	gen := mesh.GenesisLayer()
	require.NoError(t, AddLayer(msh, gen))
	trtl.init(gen)

	var l types.LayerID
	for l = 1; l <= layers; l++ {
		turtleMakeAndProcessLayer(l, trtl, blocksPerLayer, msh, layerfuncs[l])
		fmt.Println("Handled ", l, "========================================================================")
	}

	require.Equal(t, 5, trtl.Verified, "verification should'nt go further after layer couldn't be Verified,"+
		"even if future layers were successfully Verified ")
	//todo: also check votes with requireVote
}

type baseBlockProvider func(getres func(id types.LayerID) ([]types.BlockID, error)) (types.BlockID, [][]types.BlockID, error)
type inputVectorProvider func(l types.LayerID) ([]types.BlockID, error)

func createTurtleLayer(l types.LayerID, msh *mesh.DB, bbp baseBlockProvider, ivp inputVectorProvider, blocksPerLayer int) *types.Layer {
	fmt.Println("choosing base block layer ", l)
	b, lists, err := bbp(ivp)
	fmt.Println("the base block for ", l, "is ", b)
	if err != nil {
		panic(fmt.Sprint("no base - ", err))
	}
	lyr := types.NewLayer(l)

	prevlyr, err := msh.LayerBlockIds(l - 1)
	if err != nil {
		panic(err)
	}
	blocks, err := ivp(l - 1)
	if err != nil {
		blocks = nil
	}
	if err := msh.SaveLayerInputVector(l-1, blocks); err != nil {
		panic("db is fucked up")
	}

	for i := 0; i < blocksPerLayer; i++ {
		blk := types.NewExistingBlock(l, []byte(strconv.Itoa(i)))

		blk.BaseBlock = b
		blk.AgainstDiff = lists[0]
		blk.ForDiff = lists[1]
		blk.NeutralDiff = lists[2]
		if blocks != nil {
			blk.ForDiff = append(blk.ForDiff, blocks...)
		badblocks:
			for _, bi := range prevlyr {
				for _, bj := range blocks {
					if bi == bj {
						continue badblocks
					}
				}
				blk.AgainstDiff = append(blk.AgainstDiff, bi)
			}
		} else {
			blks, err := msh.LayerBlockIds(l - 1)
			if err != nil {
				panic("db err")
			}
			blk.NeutralDiff = append(blk.NeutralDiff, blks...)
		}

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
		(defaultTestHdist+2)*avgPerLayer)
}

func TestTurtle_Eviction2(t *testing.T) {
	layers := types.LayerID(defaultTestHdist * 14)
	avgPerLayer := 30
	voteNegative := 5
	trtl, _, _ := turtleSanity(t, layers, avgPerLayer, voteNegative, 0)
	require.Equal(t, len(trtl.BlocksToBlocks),
		(defaultTestHdist+2)*avgPerLayer)
}

func TestTurtle_Recovery(t *testing.T) {

	mdb := getPersistentMash()

	getHareResults := func(l types.LayerID) ([]types.BlockID, error) {
		return mdb.LayerBlockIds(l)
	}

	lg := log.New(t.Name(), "", "")
	alg := NewVerifyingTortoise(3, mdb, 5, lg)
	l := mesh.GenesisLayer()
	AddLayer(mdb, l)

	l1 := createTurtleLayer(1, mdb, alg.BaseBlock, getHareResults, 3)
	AddLayer(mdb, l1)

	l1res, _ := getHareResults(1)
	alg.HandleIncomingLayer(l1, l1res)
	alg.Persist()

	l2 := createTurtleLayer(2, mdb, alg.BaseBlock, getHareResults, 3)
	AddLayer(mdb, l2)
	l2res, _ := getHareResults(2)
	alg.HandleIncomingLayer(l2, l2res)
	alg.Persist()

	require.Equal(t, alg.LatestComplete(), types.LayerID(1))

	l31 := createTurtleLayer(3, mdb, alg.BaseBlock, getHareResults, 4)

	l32 := createTurtleLayer(3, mdb, func(func(l types.LayerID) ([]types.BlockID, error)) (types.BlockID, [][]types.BlockID, error) {
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
		alg := NewRecoveredVerifyingTortoise(mdb, lg)

		l2res, _ := getHareResults(2)
		alg.HandleIncomingLayer(l2, l2res)

		l3 := createTurtleLayer(3, mdb, alg.BaseBlock, getHareResults, 3)
		AddLayer(mdb, l3)
		l3res, _ := getHareResults(3)
		alg.HandleIncomingLayer(l3, l3res)
		alg.Persist()

		l4 := createTurtleLayer(4, mdb, alg.BaseBlock, getHareResults, 3)
		AddLayer(mdb, l4)
		l4res, _ := getHareResults(4)
		alg.HandleIncomingLayer(l4, l4res)
		alg.Persist()

		assert.True(t, alg.LatestComplete() == 3)
		return
	}()

	l3res, _ := getHareResults(3)
	alg.HandleIncomingLayer(l32, l3res) //crash
}
