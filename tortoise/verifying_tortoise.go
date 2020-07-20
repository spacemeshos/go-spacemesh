package tortoise

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

// MaxExceptionList is the maximum number of exceptions we agree to accept in a block
const MaxExceptionList = 100

//We keep a table that records, for each block, its votes about every previous block
//We keep a vector containing our vote totals (positive and negative) for every previous block
//When a new block arrives, we look up the block it points to in our table,
// and add the corresponding vector (multiplied by the block weight) to our own vote-totals vector.
//We then add the vote difference vector and the explicit vote vector to our vote-totals vector.

type blockDataProvider interface {
	GetBlock(id types.BlockID) (*types.Block, error)
	LayerBlockIds(l types.LayerID) (ids []types.BlockID, err error)

	SaveBlockVote(id types.BlockID, input []byte) error
	Persist(key []byte, v interface{}) error
	Retrieve(key []byte, v interface{}) (interface{}, error)
}

type hareResultsProvider interface {
	GetResult(lid types.LayerID) ([]types.BlockID, error)
}

func blockMapToArray(m map[types.BlockID]struct{}) []types.BlockID {
	arr := make([]types.BlockID, len(m))
	i := 0
	for b := range m {
		arr[i] = b
		i++
	}
	return types.SortBlockIDs(arr)
}

type turtle struct {
	logger log.Log

	bdp blockDataProvider
	hrp hareResultsProvider

	Last  types.LayerID
	Hdist types.LayerID
	Evict types.LayerID

	avgLayerSize int

	GoodBlocksArr   []types.BlockID
	GoodBlocksIndex map[types.BlockID]int

	Verified types.LayerID

	BlocksToBlocks      []opinion
	BlocksToBlocksIndex map[types.BlockID]int
}

func (t *turtle) SetLogger(log2 log.Log) {
	t.logger = log2
}

// NewTurtle creates a new verifying tortoise algorithm instance. XXX: maybe rename?
func NewTurtle(bdp blockDataProvider, hrp hareResultsProvider, hdist, avgLayerSize int) *turtle {
	t := &turtle{
		logger:              log.NewDefault("trtl"),
		Hdist:               types.LayerID(hdist),
		bdp:                 bdp,
		hrp:                 hrp,
		Last:                0,
		avgLayerSize:        avgLayerSize,
		GoodBlocksArr:       make([]types.BlockID, 0, 10),
		GoodBlocksIndex:     make(map[types.BlockID]int),
		BlocksToBlocks:      make([]opinion, 0, 1),
		BlocksToBlocksIndex: make(map[types.BlockID]int),
	}
	return t
}

func (t *turtle) init(genesisLayer *types.Layer) {
	for _, blk := range genesisLayer.Blocks() {
		id := blk.ID()
		t.BlocksToBlocks = append(t.BlocksToBlocks, opinion{
			blockIDLayerTuple: blockIDLayerTuple{
				id,
				blk.LayerIndex,
			},
			blocksOpinion: make(map[types.BlockID]vec),
		})
		t.BlocksToBlocksIndex[blk.ID()] = 0
		t.GoodBlocksArr = append(t.GoodBlocksArr, id)
		t.GoodBlocksIndex[id] = 0
	}
}

func (t *turtle) evict() {
	// Don't evict before we've Verified more than hdist
	// TODO: fix potential leak when we can't verify but keep receiving layers
	if t.Verified <= t.Hdist {
		return
	}
	// The window is the last [Verified - hdist] layers.
	window := t.Verified - t.Hdist
	t.logger.Info("Window starts %v", window)
	// evict from last evicted to the beginning of our window.
	for lyr := t.Evict; lyr < window; lyr++ {
		t.logger.Info("removing lyr %v", lyr)
		ids, err := t.bdp.LayerBlockIds(lyr)
		if err != nil {
			t.logger.With().Error("could not get layer ids for layer ", log.LayerID(lyr.Uint64()), log.Err(err))
			continue
		}
		for _, id := range ids {
			idx, ok := t.BlocksToBlocksIndex[id]
			if !ok {
				continue
			}
			delete(t.BlocksToBlocksIndex, id)
			t.BlocksToBlocks = removeOpinion(t.BlocksToBlocks, idx)
			// FIX indexes
			for i, op := range t.BlocksToBlocks {
				t.BlocksToBlocksIndex[op.BlockID] = i
			}
			goodidx, ok := t.GoodBlocksIndex[id]
			if !ok {
				continue
			}
			delete(t.GoodBlocksIndex, id)
			t.GoodBlocksArr = removeBlockIndex(t.GoodBlocksArr, goodidx)
			for i, gb := range t.GoodBlocksArr {
				t.GoodBlocksIndex[gb] = i
			}
			t.logger.Debug("evict block %v from maps ", id)
		}
	}
	t.Evict = window
}

func removeBlockIndex(b []types.BlockID, i int) []types.BlockID {
	return append(b[:i], b[i+1:]...)
}

func removeOpinion(o []opinion, i int) []opinion {
	return append(o[:i], o[i+1:]...)
}

func (t *turtle) inputVector(l types.LayerID, b types.BlockID) vec {
	if l == 0 {
		return support
	}

	// TODO: Pull these from db/sync if we are syncing
	res, err := t.hrp.GetResult(l)
	if err != nil {
		return abstain
	}

	m := make(map[types.BlockID]vec, len(res))
	wasIncluded := false

	for _, bl := range res {
		m[bl] = support
		if bl == b {
			wasIncluded = true
		}
	}

	if !wasIncluded {
		return against
	}

	return support
}

func (t *turtle) inputVectorForLayer(l types.LayerID) map[types.BlockID]vec {
	lyr, err := t.bdp.LayerBlockIds(l)
	if err != nil {
		return nil
	}

	lyrResult := make(map[types.BlockID]vec, len(lyr))

	// TODO: Pull these from db/sync if we are syncing. \
	res, err := t.hrp.GetResult(l)
	if err != nil {
		// hare didn't finish hence we don't have opinion
		// TODO: get hare opinion when hare finishes
		for _, b := range lyr {
			lyrResult[b] = abstain
		}
		return lyrResult
	}
	for _, b := range res {
		lyrResult[b] = support
	}
	for _, b := range lyr {
		if _, ok := lyrResult[b]; !ok {
			lyrResult[b] = against
		}
	}
	return lyrResult
}

func (t *turtle) BaseBlock() (types.BlockID, [][]types.BlockID, error) {
	for i := len(t.BlocksToBlocks) - 1; i >= 0; i-- {
		if _, ok := t.GoodBlocksIndex[t.BlocksToBlocks[i].BlockID]; !ok {
			continue
		}
		afn, err := t.opinionMatches(t.BlocksToBlocks[i].LayerID, t.BlocksToBlocks[i])
		if err != nil {
			continue
		}
		t.logger.Info("Chose baseblock %v against: %v, for: %v, neutral: %v", t.BlocksToBlocks[i].id, len(afn[0]), len(afn[1]), len(afn[2]))
		return t.BlocksToBlocks[i].BlockID, [][]types.BlockID{blockMapToArray(afn[0]), blockMapToArray(afn[1]), blockMapToArray(afn[2])}, nil
	}
	// TODO: special error encoding when exceeding excpetion list size
	return types.BlockID{0}, nil, errors.New("no base block that fits the limit")
}

func (t *turtle) opinionMatches(layerid types.LayerID, opinion2 opinion) ([]map[types.BlockID]struct{}, error) {
	a := make(map[types.BlockID]struct{})
	f := make(map[types.BlockID]struct{})
	n := make(map[types.BlockID]struct{})

	for b, o := range opinion2.blocksOpinion {
		blk, err := t.bdp.GetBlock(b)
		if err != nil {
			panic("block not found")
		}
		inputVote := t.inputVector(blk.LayerIndex, b)
		t.logger.Debug("looking on %v vote for block %v in layer %v", opinion2.BlockID, b, layerid)

		if inputVote == against && simplifyVote(o) != against {
			t.logger.Debug("added diff %v to against", b)
			a[b] = struct{}{}
			continue
		}

		if inputVote == support && simplifyVote(o) != support {
			t.logger.Debug("added diff %v to support", b)
			f[b] = struct{}{}
			continue
		}

		if inputVote == abstain && simplifyVote(o) != abstain {
			t.logger.Debug("added diff %v to neutral", b)
			n[b] = struct{}{}
			continue
		}
	}

	//TODO - Add orphan blocks 0as neutral as well, or after hare results add them as for/against.
	for blk, v := range t.inputVectorForLayer(layerid) {
		if v == support {
			t.logger.Debug("added orphan %v to support", blk)
			f[blk] = struct{}{}
			continue
		}
		if v == against {
			t.logger.Debug("added orphan %v to agianst", blk)
			a[blk] = struct{}{}
			continue
		}
		if v == abstain {
			t.logger.Debug("added orphan %v to neutral", blk)
			n[blk] = struct{}{}
			continue
		}
	}

	if len(a)+len(f)+len(n) > MaxExceptionList {
		return nil, errors.New(" matches too much exceptions")
	}

	return []map[types.BlockID]struct{}{a, f, n}, nil
}

func (t *turtle) BlockWeight(voting, voted types.BlockID) int {
	return 1
}

func (t *turtle) saveOpinion() error {
	for i := t.Verified - t.Hdist; i < t.Verified; i++ {
		lyr := t.inputVectorForLayer(i)
		if lyr == nil {
			return errors.New("no layer data")
		}
		for blk, v := range lyr {

			if err := t.bdp.SaveBlockVote(blk, voteFromVec(v).Bytes()); err != nil {
				return err
			}
			if simplifyVote(v) == support {
				events.Publish(events.ValidBlock{ID: blk.String(), Valid: true})
			} else {
				t.logger.With().Warning("block is contextually invalid", blk.Field(), log.String("vote", v.String()))
			}
		}
	}
	return nil
}

//Persist saves the current tortoise state to the database
func (t *turtle) persist() error {
	if err := t.saveOpinion(); err != nil {
		return err
	}
	return t.bdp.Persist(mesh.TORTOISE, t)
}

//RecoverTortoise retrieve latest saved tortoise from the database
func RecoverVerifyingTortoise(mdb retriever) (interface{}, error) {
	return mdb.Retrieve(mesh.TORTOISE, &turtle{})
}

func (t *turtle) processBlock(block *types.Block) error {
	baseidx, ok := t.BlocksToBlocksIndex[block.BaseBlock]
	if !ok {
		panic("base block not found")
	}

	if len(t.BlocksToBlocks) < baseidx {
		panic("base block not in array")
	}

	baseBlockOpinion := t.BlocksToBlocks[baseidx]
	blockid := block.ID()

	thisBlockOpinions := make(map[types.BlockID]vec)
	for blk, vote := range baseBlockOpinion.blocksOpinion {
		thisBlockOpinions[blk] = vote
	}

	for _, b := range block.ForDiff {
		thisBlockOpinions[b] = thisBlockOpinions[b].Add(support.Multiply(t.BlockWeight(blockid, b)))
	}
	for _, b := range block.AgainstDiff {
		thisBlockOpinions[b] = thisBlockOpinions[b].Add(against.Multiply(t.BlockWeight(blockid, b)))
	}

	//TODO: neutral ?

	t.BlocksToBlocks = append(t.BlocksToBlocks, opinion{blockIDLayerTuple: blockIDLayerTuple{
		BlockID: blockid,
		LayerID: block.LayerIndex,
	}, blocksOpinion: thisBlockOpinions})
	t.BlocksToBlocksIndex[blockid] = len(t.BlocksToBlocks) - 1

	return nil
}

//HandleIncomingLayer processes all layer block votes
//returns the old pbase and new pbase after taking into account the blocks votes
func (t *turtle) HandleIncomingLayer(newlyr *types.Layer) (types.LayerID, types.LayerID) {

	// TODO: handle late blocks

	// update tables with blocks
	defer t.evict()

	for _, b := range newlyr.Blocks() {
		err := t.processBlock(b)
		if err != nil {
			panic("something is wrong " + err.Error())
		}
	}

	// Mark good blocks

markingLoop:
	for _, b := range newlyr.Blocks() {
		if _, good := t.GoodBlocksIndex[b.BaseBlock]; !good {
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
			if t.inputVector(exblk.LayerIndex, exblk.ID()) != support {
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
			if t.inputVector(exblk.LayerIndex, exblk.ID()) != against {
				continue markingLoop
			}
		}

		for exneu := range b.NeutralDiff {
			exblk, err := t.bdp.GetBlock(b.NeutralDiff[exneu])
			if err != nil {
				panic(fmt.Sprint("err , ", err))
			}
			if exblk.LayerIndex < baseBlock.LayerIndex {
				continue markingLoop
			}
			if t.inputVector(exblk.LayerIndex, exblk.ID()) != abstain {
				continue markingLoop
			}
		}
		t.logger.Info("marking %v of layer %v as good", b.ID(), b.LayerIndex)
		t.GoodBlocksArr = append(t.GoodBlocksArr, b.ID())
		t.GoodBlocksIndex[b.ID()] = len(t.GoodBlocksArr) - 1
	}

	// Count good blocks votes..
	wasVerified := t.Verified
	t.logger.Info("Trying to advance from layer %v to %v", wasVerified, newlyr.Index())
	i := wasVerified + 1
loop:
	for ; i < newlyr.Index(); i++ {
		t.logger.Info("Verifying layer %v", i)

		input := t.inputVectorForLayer(i)
		if len(input) == 0 {
			break
		}

		for blk, vote := range input {
			sum := abstain
			//t.logger.Info("counting votes for block %v", blk)
			for _, vopinion := range t.BlocksToBlocks {

				t.logger.Debug("Checking %v opinion on %v", vopinion.BlockID, blk)

				if vopinion.LayerID <= i {
					t.logger.Debug("%v is older than %v", vopinion.BlockID, blk)
					continue
				}

				opinionVote, ok := vopinion.blocksOpinion[blk]
				if !ok {
					t.logger.Debug("%v has no opinion on %v", vopinion.BlockID, blk)
					continue
				}

				_, isgood := t.GoodBlocksIndex[vopinion.BlockID]
				if !isgood {
					t.logger.Debug("%v is not good hence not counting", vopinion.BlockID)
					continue
				}

				t.logger.Debug("adding %v opinion = %v to the vote sum on %v", vopinion.id, opinionVote, blk)
				//t.logger.Info("block %v is good and voting vote %v", vopinion.id, opinionVote)
				sum = sum.Add(opinionVote.Multiply(t.BlockWeight(vopinion.BlockID, blk)))
			}

			gop := globalOpinion(sum, t.avgLayerSize, float64(i-wasVerified))
			t.logger.Info("Global opinion on blk %v (lyr:%v) is %v (from:%v)", blk, i, gop, sum)
			if gop != vote || gop == abstain {
				// TODO: trigger self healing after a while ?
				t.logger.Warning("The global opinion is different from vote, global: %v, vote: %v", gop, vote)
				break loop
			}
		}

		t.logger.Info("Verified layer %v", i)
		t.Verified = i

	}

	return wasVerified, t.Verified
}
