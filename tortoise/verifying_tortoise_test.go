package tortoise

import (
	"errors"
	"fmt"
	"strconv"
	"testing"

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
	require.Equal(t, int(layers-1), int(trtl.verified))
}

func TestTurtle_HandleIncomingLayer_VoteNegative(t *testing.T) {
	layers := types.LayerID(10)
	avgPerLayer := 10
	voteNegative := 5
	trtl := turtleSanity(t, layers, avgPerLayer, voteNegative, 0)
	require.Equal(t, int(layers-1), int(trtl.verified))
}

func TestTurtle_HandleIncomingLayer_VoteAbstain(t *testing.T) {
	layers := types.LayerID(10)
	avgPerLayer := 10
	trtl := turtleSanity(t, layers, avgPerLayer, 0, 10)
	require.Equal(t, 0, int(trtl.verified), "when all votes abstain verification should stay at first layer and advance")
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
	gen := types.NewExistingBlock(0, []byte("genesis"))
	require.NoError(t, msh.AddBlock(gen))
	trtl.init(gen.ID())

	var l types.LayerID
	for l = 1; l <= layers; l++ {
		fmt.Println("choosing base block layer ", l)
		b, lists, err := trtl.BaseBlock(l)
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
	gen := types.NewExistingBlock(0, []byte("genesis"))
	require.NoError(t, msh.AddBlock(gen))
	trtl.init(gen.ID())

	var l types.LayerID
	for l = 1; l <= layers; l++ {
		fmt.Println("choosing base block layer ", l)
		b, lists, err := trtl.BaseBlock(l)
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

	require.Equal(t, 5, trtl.verified, "verification should'nt go further after layer couldn't be verified,"+
		"even if future layers were successfully verified ")
}
