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

func TestTurtle_HandleIncomingLayer(t *testing.T) {

	//log.DebugMode(true)
	const layers types.LayerID = 40
	const blocksPerLayer = 100

	msh := getInMemMesh()

	hm := &hareMock{GetResultFunc: func(l types.LayerID) (ids []types.BlockID, err error) {
		return msh.LayerBlockIds(l)
	}}

	trtl := NewTurtle(msh, hm, blocksPerLayer)
	gen := types.NewExistingBlock(0, []byte("genesis"))
	require.NoError(t, msh.AddBlock(gen))
	trtl.init(gen.ID())

	var l types.LayerID
	for l = 1; l < layers; l++ {
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
		hres, err := hm.GetResult(lyr.Index())
		require.NoError(t, err)
		//require.Equal(t, types.SortBlockIDs(hres), types.SortBlockIDs(types.BlockIDs(lyr.Blocks())))
		for _, blk := range hres {
			fmt.Printf("BLOCK %v vote is %v\r\n", blk, trtl.inputVector(lyr.Index(), blk))
		}

		trtl.HandleIncomingLayer(lyr)
		fmt.Println("Handled ", l, "========================================================================")
	}

	require.Equal(t, int(layers-2), int(trtl.verified))

}

func TestPlay(t *testing.T) {
	vec := abstain.Add(support)
	fmt.Println(vec)
}
