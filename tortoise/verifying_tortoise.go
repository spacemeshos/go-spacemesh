package tortoise

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

const MaxExceptionList = 100

//We keep a table that records, for each block, its votes about every previous block
//We keep a vector containing our vote totals (positive and negative) for every previous block
//When a new block arrives, we look up the block it points to in our table,
// and add the corresponding vector (multiplied by the block weight) to our own vote-totals vector.
//We then add the vote difference vector and the explicit vote vector to our vote-totals vector.

type opinion struct {
	id            types.BlockID
	blocksOpinion map[types.BlockID]vec
}

type blockDataProvider interface {
	GetBlock(id types.BlockID) (*types.Block, error)
	LayerBlockIds(l types.LayerID) (ids []types.BlockID, err error)
}

type hareResultsProvider interface {
	GetResult(lid types.LayerID) ([]types.BlockID, error)
}

type turtle struct {
	bdp          blockDataProvider
	hrp          hareResultsProvider
	Last         types.LayerID
	avgLayerSize int

	goodBlocks      map[types.BlockID]struct{}
	goodBlocksArr   []types.BlockID
	goodBlocksIndex map[types.BlockID]int

	inputVector map[types.BlockID]vec

	blocksToBlocks      []opinion
	blocksToBlocksIndex map[types.BlockID]int
	indexToLayerID      map[int]types.LayerID

	totalVotes map[types.BlockID]vec
}

func NewTurtle(bdp blockDataProvider, hrp hareResultsProvider, avgLayerSize int) *turtle {
	t := &turtle{
		bdp:                 bdp,
		hrp:                 hrp,
		Last:                0,
		avgLayerSize:        avgLayerSize,
		goodBlocks:          make(map[types.BlockID]struct{}),
		goodBlocksArr:       make([]types.BlockID, 0, 10),
		goodBlocksIndex:     make(map[types.BlockID]int),
		inputVector:         make(map[types.BlockID]vec),
		blocksToBlocks:      make([]opinion, 0, 1),
		blocksToBlocksIndex: make(map[types.BlockID]int),
		indexToLayerID:      make(map[int]types.LayerID),
		totalVotes:          make(map[types.BlockID]vec),
	}
	return t
}

func (t *turtle) init(genesisBlock types.BlockID) {
	t.blocksToBlocks = append(t.blocksToBlocks, opinion{
		id:            genesisBlock,
		blocksOpinion: make(map[types.BlockID]vec),
	})
	t.blocksToBlocksIndex[genesisBlock] = 0
	t.totalVotes[genesisBlock] = support
	t.goodBlocksArr[0] = genesisBlock
	t.goodBlocksIndex[genesisBlock] = 0
}

func convertToArray(m map[types.BlockID]struct{}) []types.BlockID {
	arr := make([]types.BlockID, len(m))
	i := 0
	for b := range m {
		arr[i] = b
		i++
	}
	return types.SortBlockIDs(arr)
}

func (t *turtle) BaseBlock() (types.BlockID, [][]types.BlockID, error) {
	for i := len(t.blocksToBlocks) - 1; i >= 0; i-- {
		afn, err := t.opinionMatches(t.blocksToBlocks[i])
		if err != nil {
			continue
		}
		return t.blocksToBlocks[i].id, [][]types.BlockID{convertToArray(afn[0]), convertToArray(afn[1]), convertToArray(afn[2])}, nil
	}
	// TODO: special error encoding when exceeding excpetion list size
	return types.BlockID{0}, nil, errors.New("no base block that fits the limit")
}

func (t *turtle) opinionMatches(opinion2 opinion) ([]map[types.BlockID]struct{}, error) {
	a := make(map[types.BlockID]struct{})
	f := make(map[types.BlockID]struct{})
	n := make(map[types.BlockID]struct{})

	for b, o := range opinion2.blocksOpinion {
		totalOpinion, ok := t.totalVotes[b]
		if !ok {
			a[b] = struct{}{}
			continue
		}

		// todo: it should be identical?
		if totalOpinion[0] > totalOpinion[1] && !(o[0] > o[1]) {
			f[b] = struct{}{}
			continue
		}

		if totalOpinion[1] > totalOpinion[0] && !(o[1] > o[0]) {
			a[b] = struct{}{}
		}

		if totalOpinion[0] == totalOpinion[1] && !(o[0] == o[1]) {
			n[b] = struct{}{}
		}

		//TODO - Add orphan blocks 0as neutral as well, or after hare results add them as for/against.

	}

	if len(a)+len(f)+len(n) > MaxExceptionList {
		return nil, errors.New(" matches too much exceptions")
	}

	return []map[types.BlockID]struct{}{a, f, n}, nil
}

func (t *turtle) BlockWeight(voting, voted types.BlockID) int {
	return 1
}

func (t *turtle) processBlock(block *types.Block) error {
	baseidx, ok := t.blocksToBlocksIndex[block.BaseBlock]
	if !ok {
		panic("base block not foudn")
	}

	if len(t.blocksToBlocks) < baseidx {
		panic("base block not in array")
	}

	baseBlockOpinion := t.blocksToBlocks[baseidx]

	blockid := block.ID()

	for b, v := range baseBlockOpinion.blocksOpinion {
		t.totalVotes[b] = t.totalVotes[b].Add(v.Multiply(t.BlockWeight(blockid, b)))
	}

	for _, b := range block.ForDiff {
		t.totalVotes[b] = t.totalVotes[b].Add(support.Multiply(t.BlockWeight(blockid, b)))
	}
	for _, b := range block.AgainstDiff {
		t.totalVotes[b] = t.totalVotes[b].Add(against.Multiply(t.BlockWeight(blockid, b)))
	}

	blocksOpinion := make(map[types.BlockID]vec)

	for b, v := range t.totalVotes {
		blocksOpinion[b] = v
	}

	//TODO: neutral ?

	t.blocksToBlocks = append(t.blocksToBlocks, opinion{id: blockid, blocksOpinion: blocksOpinion})
	t.blocksToBlocksIndex[blockid] = len(t.blocksToBlocks) - 1
	t.totalVotes[blockid] = abstain

	return nil
}

//HandleIncomingLayer processes all layer block votes
//returns the old pbase and new pbase after taking into account the blocks votes
func (t *turtle) HandleIncomingLayer(newlyr *types.Layer) (types.LayerID, types.LayerID) {

	//1 Mark the genesis block as “good”
	//2 Go over all blocks, in order.
	//	For block i , mark it “good” if (1) its base block is marked good and (2) its exception list consists only of blocks that appear after the base block and are consistent with the input vote vector.
	//3 Count the votes for the input vote vector by summing the weight of the good blocks (of course, each good block’s weight only counts for the blocks preceding it).
	//4 Declare the vote vector “verified” up to position k if the total weight exceeds the confidence threshold in all positions up to k .

	// took from previous tortoise
	//if newlyr.Index() > t.Last {
	//	t.Last = newlyr.Index()
	//}

	// insert input vector from hare

	if newlyr.Index() != genesis {
		blocks := newlyr.Blocks()
		hareBlks, err := t.hrp.GetResult(newlyr.Index())
		if err != nil {
			//TODO : put neutral
		}
	lyrblk:
		for _, blk := range blocks {
			blkid := blk.ID()
			for _, hblk := range hareBlks {
				if blkid == hblk {
					t.inputVector[hblk] = support
					continue lyrblk
				}
				t.inputVector[hblk] = against
			}
		}
	}

	// update tables with blocks

	for _, b := range newlyr.Blocks() {
		err := t.processBlock(b)
		if err != nil {
			panic("something is wrong " + err.Error())
		}
	}

	if newlyr.Index() == genesis {
		// special case genesis ?
	}

	// Mark good blocks

markingLoop:
	for _, b := range newlyr.Blocks() {
		if _, good := t.goodBlocksIndex[b.BaseBlock]; !good {
			continue markingLoop
		}
		baseBlock, err := t.bdp.GetBlock(b.BaseBlock)
		if err != nil {
			panic(fmt.Sprint("block not found ", b.BaseBlock, "err", err))
		}

		for exfor := range b.ForDiff {
			exblk, err := t.bdp.GetBlock(b.ForDiff[exfor])
			if err != nil {
				panic(fmt.Sprint("err , ", err))
			}
			if exblk.LayerIndex < baseBlock.LayerIndex {
				continue markingLoop
			}
			if t.inputVector[exblk.ID()] != support {
				continue markingLoop
			}
		}

		for exag := range b.AgainstDiff {
			exblk, err := t.bdp.GetBlock(b.AgainstDiff[exag])
			if err != nil {
				panic(fmt.Sprint("err , ", err))
			}
			if exblk.LayerIndex < baseBlock.LayerIndex {
				continue markingLoop
			}
			if t.inputVector[exblk.ID()] != against {
				continue markingLoop
			}
		}

		for exneu := range b.NeutralDiff {
			exblk, err := t.bdp.GetBlock(b.AgainstDiff[exneu])
			if err != nil {
				panic(fmt.Sprint("err , ", err))
			}
			if exblk.LayerIndex < baseBlock.LayerIndex {
				continue markingLoop
			}
			if t.inputVector[exblk.ID()] != abstain {
				continue markingLoop
			}
		}

		//vote := globalOpinion(t.totalVotes[b.ID()], t.avgLayerSize, float64(1))
		if t.inputVector[b.ID()] == support {
			t.goodBlocksArr = append(t.goodBlocksArr, b.ID())
			t.goodBlocksIndex[b.ID()] = len(t.goodBlocksArr) - 1
		}
	}

	//todo: Verify good blocks
	//3 Count the votes for the input vote vector
	// by summing the weight of the good blocks
	// (of course, each good block’s weight only counts for the blocks preceding it).

	for _, gb := range t.goodBlocksArr {
		idx := t.blocksToBlocksIndex[gb]
		opmap := t.blocksToBlocks[idx]

		opmap.blocksOpinion
	}

	return 0, 0
}
