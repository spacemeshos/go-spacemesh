package tortoise

import (
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/stretchr/testify/require"
)

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
	trtl := turtleSanity(t, layers, avgPerLayer, voteNegative)
	require.Equal(t, int(layers-1), int(trtl.verified))
}

func TestTurtle_HandleIncomingLayer_VoteNegative(t *testing.T) {
	layers := types.LayerID(10)
	avgPerLayer := 10
	voteNegative := 5
	trtl := turtleSanity(t, layers, avgPerLayer, voteNegative)
	require.Equal(t, int(layers-1), int(trtl.verified))
}

func turtleSanity(t testing.TB, layers types.LayerID, blocksPerLayer, voteNegative int) *turtle {

	msh := getInMemMesh()

	hm := &hareMock{GetResultFunc: func(l types.LayerID) (ids []types.BlockID, err error) {
		if l == 0 {
			return msh.LayerBlockIds(l)
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

	trtl := NewTurtle(msh, hm, blocksPerLayer)
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
