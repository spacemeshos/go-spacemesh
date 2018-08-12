package core

import (	"github.com/golang-collections/go-datastructures/bitarray"
	"github.com/spacemeshos/go-spacemesh/log"
	"fmt"
)


type LayerQueue chan *Layer
type NewIdQueue chan uint32


type Algorithm struct {
	block2Id          map[BLockId]uint32
	allBlocks         map[BLockId]*Block
	layerQueue        LayerQueue
	idQueue           NewIdQueue
	posVotes          []bitarray.BitArray
	visibilityMap     []bitarray.BitArray
	layers            map[uint64]*Layer
	layerSize         uint32
	cachedLayers      uint32
	remainingBlockIds uint32
	totalBlocks uint32
}

func NewAlgorithm(layerSize uint32, cachedLayers uint32) Algorithm{
	totBlocks := layerSize*cachedLayers
	trtl := Algorithm{
		block2Id:          make(map[BLockId]uint32),
		allBlocks:         make(map[BLockId]*Block),
		layerQueue:        make(LayerQueue, cachedLayers +1),
		idQueue:           make(NewIdQueue,layerSize),
		remainingBlockIds: totBlocks,
		totalBlocks: totBlocks,
		posVotes:          make([]bitarray.BitArray,totBlocks),
		visibilityMap:     make([]bitarray.BitArray,totBlocks),
		layers:            make(map[uint64]*Layer),
		layerSize:         layerSize,
		cachedLayers:		cachedLayers,
	}
	return trtl
}

func (alg *Algorithm) GlobalVotingAvg() uint64 {
	return 10
}


func (alg *Algorithm) LayerVotingAvg() uint64 {
	return 3
}

func (alg *Algorithm) IsTortoiseValid(originBlock *Block, targetBlock BLockId, targetBlockIdx uint64, visibleBlocks bitarray.BitArray) bool {
	//log.Info("calculating votes for: %v on target id: %v",alg.block2Id[originBlock.id], targetBlockId)
	voteFor, voteAgainst := alg.countTotalVotesForBlock(targetBlockIdx, visibleBlocks)

	/*if err != nil {
		log.Error("Failed to count votes")
	}*/
	if voteFor > alg.GlobalVotingAvg() {
		return true
	}
	if voteAgainst > alg.GlobalVotingAvg(){
		return false
	}

	voteFor, voteAgainst = alg.CountVotesInLastLayer(alg.allBlocks[targetBlock]) //??

	/*if err != nil {
		log.Error("Failed to count votes")
	}*/
	if voteFor > alg.LayerVotingAvg() {
		return true
	}
	if voteAgainst > alg.LayerVotingAvg(){
		return false
	}

	return originBlock.coin
}


func (alg *Algorithm) getLayerById(layerId uint64) (*Layer, error){
	if _, ok := alg.layers[layerId]; !ok {
		return nil, fmt.Errorf("layer id not found %v", layerId)
	}
	return alg.layers[layerId], nil
}

func (alg *Algorithm) CountVotesInLastLayer(block *Block) (uint64, uint64){
	var voteFor, voteAgainst uint64 = 0,0
	l, er := alg.getLayerById(block.layerNum +1)
	if er != nil {
		log.Error("%v", er)
		return 0,0
	}
	for _,visibleBlock := range l.blocks {
		if vote, ok := visibleBlock.blockVotes[block.id]; ok {
			if vote {
				voteFor++
			} else {
				voteAgainst++
			}
		}
	}
	return voteFor, voteAgainst
}



func (alg *Algorithm) createBlockVotingMap(origin *Block) (*bitarray.BitArray, *bitarray.BitArray){
	//log.Info("creating block voting map for %v", origin.id)
	blockMap := bitarray.NewBitArray(uint64(alg.totalBlocks))
	visibilityMap := bitarray.NewBitArray(uint64(alg.totalBlocks))
	// Count direct voters
	for blockId,vote := range origin.blockVotes { //todo: check for double votes
		//todo: assert that block exists
		targetBlockId := uint64(alg.block2Id[blockId])
		visibilityMap.SetBit(targetBlockId)
		targetMap := alg.visibilityMap[targetBlockId]
		visibilityMap = visibilityMap.Or(targetMap)
		//log.Info("target map %v new visibility map %v", targetMap.ToNums(), visibilityMap.ToNums())
		if vote{
			blockMap.SetBit(targetBlockId)
		}
	}

	// Go over all other blocks that exist and calculate the origin blocks votes for them
	for blockId, targetBlockIdx := range alg.block2Id {
		if _, ok := origin.blockVotes[blockId]; ok {
			continue
		}
		val, err := visibilityMap.GetBit(uint64(targetBlockIdx))
		if err == nil && val {
			if alg.IsTortoiseValid(origin,blockId ,uint64(targetBlockIdx), visibilityMap) {
				blockMap.SetBit(uint64(targetBlockIdx))
			}
		}
	}
	return &blockMap, &visibilityMap
}

func (alg *Algorithm) countTotalVotesForBlock(targetIdx uint64, visibleBlocks bitarray.BitArray) (uint64, uint64){
	//targetId := alg.block2Id[target.id]
	var posVotes, conVotes uint64 = 0,0
	for blockIdx:=0; blockIdx< len(alg.block2Id); blockIdx++{//_, blockIdx := range alg.block2Id {
		//if this block sees our
		//if alg.allBlocks[targetID].layerNum
		if val, err := visibleBlocks.GetBit(uint64(blockIdx)); val{ //if this block is visible from our target
			if val, err = alg.visibilityMap[blockIdx].GetBit(targetIdx); err == nil && val {
				if set, _ := alg.posVotes[blockIdx].GetBit(targetIdx); set {
					posVotes++
				} else {
					conVotes++
				}
			}
		}

	}
	//log.Info("target block id: %v, for :%v, against %v", targetId, posVotes, conVotes)
	return posVotes, conVotes
}

func (alg *Algorithm) zeroBitColumn(idx uint64){
	for row, bitvec := range alg.posVotes {
		bitvec.ClearBit(idx)
		alg.visibilityMap[row].ClearBit(idx)
	}
}

func (alg *Algorithm) recycleLayer(l *Layer){
	for _, block := range l.blocks{
		id := alg.block2Id[block.id]
		alg.idQueue <- alg.block2Id[block.id]
		delete(alg.block2Id, block.id)
		delete(alg.allBlocks, block.id)
		alg.zeroBitColumn(uint64(id))
	}
	delete(alg.layers, l.layerNum)
}

func (alg *Algorithm) assignIdForBlock(blk *Block) uint32{
	//todo: should this section be protected by a mutex?
	alg.allBlocks[blk.id] = blk
	if len(alg.idQueue) > 0 {
		id := <- alg.idQueue
		alg.block2Id[blk.id] = id
		return id
	}
	if alg.remainingBlockIds > 0 {
		newId := alg.totalBlocks - alg.remainingBlockIds
		alg.block2Id[blk.id] = newId
		alg.remainingBlockIds--

		return newId
	} else {
		log.Error("Cannot find id for block, something went wrong")
		panic("Cannot find id for block, something went wrong")
		return 0
	}

}

func (alg *Algorithm) HandleIncomingLayer(l *Layer){
	log.Info("received layer id %v =======================================", l.layerNum)
	//todo: thread safety
	alg.layers[l.layerNum] = l
	alg.layerQueue <- l
	//blockBitArrays := make(map[Block]bitarray.BitArray)
	if len(alg.layerQueue) >= int(alg.cachedLayers ) {
		layer := <-alg.layerQueue
		alg.recycleLayer(layer)
	}
	for _, originBlock := range l.blocks{
		//todo: what to do if block is invalid?
		if originBlock.IsSyntacticallyValid() {
			votesBM, visibleBM := alg.createBlockVotingMap(&originBlock)

			blockId := alg.assignIdForBlock(&originBlock)
			alg.posVotes[blockId] = *votesBM
			alg.visibilityMap[blockId] = *visibleBM
		}

	}
}

func (block *Block) IsContextuallyValid() bool {
	return true
}

func (block *Block) IsSyntacticallyValid() bool {
	return true
}

