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

type hareMock struct {
	GetResultFunc func(l types.LayerID) ([]types.BlockID, error)
}

func (hm *hareMock) GetResult(l types.LayerID) ([]types.BlockID, error) {
	if hm.GetResultFunc != nil {
		return hm.GetResultFunc(l)
	}
	return nil, errors.New("not implemented")
}

func TestTurtle_HandleIncomingLayerHappyFlow(t *testing.T) {
	layers := types.LayerID(10)
	avgPerLayer := 10
	voteNegative := 0
	trtl := turtleSanity(t, layers, avgPerLayer, voteNegative, 0)
	require.Equal(t, int(layers-1), int(trtl.Verified))
}

func TestTurtle_HandleIncomingLayer_VoteNegative(t *testing.T) {
	layers := types.LayerID(10)
	avgPerLayer := 10
	voteNegative := 5
	trtl := turtleSanity(t, layers, avgPerLayer, voteNegative, 0)
	require.Equal(t, int(layers-1), int(trtl.Verified))
}

func TestTurtle_HandleIncomingLayer_VoteAbstain(t *testing.T) {
	layers := types.LayerID(10)
	avgPerLayer := 10
	trtl := turtleSanity(t, layers, avgPerLayer, 0, 10)
	require.Equal(t, 0, int(trtl.Verified), "when all votes abstain verification should stay at first layer and advance")
}

func turtleSanity(t testing.TB, layers types.LayerID, blocksPerLayer, voteNegative int, voteAbstain int) *turtle {
	msh := getInMemMesh()

	abstainCount := 0

	hm := &hareMock{GetResultFunc: func(l types.LayerID) (ids []types.BlockID, err error) {
		if l == 0 {
			return msh.LayerBlockIds(l)
		}

		if voteAbstain > 0 && abstainCount <= voteAbstain {
			abstainCount += 1
		}

		if voteAbstain > 0 && abstainCount >= int(layers)-voteAbstain {
			return nil, errors.New("hare didn't finish")
		}

		if voteNegative == 0 {
			return msh.LayerBlockIds(l)
		}

		blks, err := msh.LayerBlockIds(l)
		if err != nil {
			panic("db err")
		}
		return blks[voteNegative:], nil
	}}

	trtl := NewTurtle(msh, hm, defaultTestHdist, blocksPerLayer)
	gen := mesh.GenesisLayer()
	require.NoError(t, AddLayer(msh, gen))
	trtl.init(gen)

	var l types.LayerID
	for l = 1; l <= layers; l++ {
		fmt.Println("choosing base block layer ", l)
		b, lists, err := trtl.BaseBlock()
		fmt.Println("the base block for ", l, "is ", b)
		if err != nil {
			panic(fmt.Sprint("no base - ", err))
		}
		lyr := types.NewLayer(l)
		for i := 0; i < blocksPerLayer; i++ {
			blk := types.NewExistingBlock(l, []byte(strconv.Itoa(i)))

			blk.BaseBlock = b
			blk.AgainstDiff = lists[0]
			blk.ForDiff = lists[1]
			blk.NeutralDiff = lists[2]

			lyr.AddBlock(blk)
			err = msh.AddBlock(blk)
			if err != nil {
				fmt.Println("Err inserting to db - ", err)
			}
		}
		trtl.HandleIncomingLayer(lyr)
		fmt.Println("Handled ", l, "========================================================================")
	}

	return trtl
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

	hm := &hareMock{GetResultFunc: func(l types.LayerID) (ids []types.BlockID, err error) {
		return layerfuncs[l](l)
	}}

	trtl := NewTurtle(msh, hm, defaultTestHdist, blocksPerLayer)
	trtl.init(mesh.GenesisLayer())

	var l types.LayerID
	for l = 1; l <= layers; l++ {
		fmt.Println("choosing base block layer ", l)
		b, lists, err := trtl.BaseBlock()
		fmt.Println("the base block for ", l, "is ", b)
		if err != nil {
			panic(fmt.Sprint("no base - ", err))
		}
		lyr := types.NewLayer(l)
		for i := 0; i < blocksPerLayer; i++ {
			blk := types.NewExistingBlock(l, []byte(strconv.Itoa(i)))

			blk.BaseBlock = b
			blk.AgainstDiff = lists[0]
			blk.ForDiff = lists[1]
			blk.NeutralDiff = lists[2]

			lyr.AddBlock(blk)
			err = msh.AddBlock(blk)
			if err != nil {
				fmt.Println("Err inserting to db - ", err)
			}
		}
		trtl.HandleIncomingLayer(lyr)
		fmt.Println("Handled ", l, "========================================================================")
	}

	require.Equal(t, 5, trtl.Verified, "verification should'nt go further after layer couldn't be Verified,"+
		"even if future layers were successfully Verified ")
}

type baseBlockProvider func() (types.BlockID, [][]types.BlockID, error)

func createTurtleLayer(index types.LayerID, bbp baseBlockProvider, blocksPerLayer int) *types.Layer {
	fmt.Println("choosing base block layer ", index)
	b, lists, err := bbp()
	fmt.Println("the base block for ", index, "is ", b)
	if err != nil {
		panic(fmt.Sprint("no base - ", err))
	}
	lyr := types.NewLayer(index)
	for i := 0; i < blocksPerLayer; i++ {
		blk := types.NewExistingBlock(index, []byte(strconv.Itoa(i)))
		blk.BaseBlock = b
		blk.AgainstDiff = lists[0]
		blk.ForDiff = lists[1]
		blk.NeutralDiff = lists[2]

		lyr.AddBlock(blk)
	}
	return lyr
}

func TestTurtle_Eviction(t *testing.T) {
	layers := types.LayerID(defaultTestHdist * 10)
	avgPerLayer := 10
	voteNegative := 0
	trtl := turtleSanity(t, layers, avgPerLayer, voteNegative, 0)
	require.Equal(t, len(trtl.BlocksToBlocks),
		(defaultTestHdist+2)*avgPerLayer)
}

func TestTurtle_Eviction2(t *testing.T) {
	layers := types.LayerID(defaultTestHdist * 14)
	avgPerLayer := 30
	voteNegative := 5
	trtl := turtleSanity(t, layers, avgPerLayer, voteNegative, 0)
	require.Equal(t, len(trtl.BlocksToBlocks),
		(defaultTestHdist+2)*avgPerLayer)
}

func TestTurtle_Recovery(t *testing.T) {

	mdb := getPersistentMash()

	hm := &hareMock{GetResultFunc: func(l types.LayerID) (ids []types.BlockID, err error) {
		if l == 0 {
			return mdb.LayerBlockIds(l)
		}
		return mdb.LayerBlockIds(l)
	}}

	lg := log.New(t.Name(), "", "")
	alg := NewVerifyingTortoise(3, mdb, hm, 5, lg)
	l := mesh.GenesisLayer()
	AddLayer(mdb, l)

	l1 := createTurtleLayer(1, alg.BaseBlock, 3)
	AddLayer(mdb, l1)

	alg.HandleIncomingLayer(l1)
	alg.Persist()

	l2 := createTurtleLayer(2, alg.BaseBlock, 3)
	AddLayer(mdb, l2)
	alg.HandleIncomingLayer(l2)
	alg.Persist()

	require.Equal(t, alg.Verified(), types.LayerID(1))

	l31 := createTurtleLayer(3, alg.BaseBlock, 4)

	l32 := createTurtleLayer(3, func() (types.BlockID, [][]types.BlockID, error) {
		diffs := make([][]types.BlockID, 3)
		diffs[0] = make([]types.BlockID, 0)
		diffs[1] = types.BlockIDs(l.Blocks())
		diffs[2] = make([]types.BlockID, 0)

		return l31.Blocks()[0].ID(), diffs, nil
	}, 5)

	defer func() {
		if r := recover(); r != nil {
			t.Log("Recovered from", r)
		}
		alg := NewRecoveredVerifyingTortoise(mdb, hm, lg)

		alg.HandleIncomingLayer(l2)

		l3 := createTurtleLayer(3, alg.BaseBlock, 3)
		AddLayer(mdb, l3)
		alg.HandleIncomingLayer(l3)
		alg.Persist()

		l4 := createTurtleLayer(4, alg.BaseBlock, 3)
		AddLayer(mdb, l4)
		alg.HandleIncomingLayer(l4)
		alg.Persist()

		assert.True(t, alg.Verified() == 3)
		return
	}()

	alg.HandleIncomingLayer(l32) //crash
}
