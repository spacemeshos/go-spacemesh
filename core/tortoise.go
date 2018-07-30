package core

import (	"github.com/golang-collections/go-datastructures/bitarray"
	"github.com/spacemeshos/go-spacemesh/log"
)


type LayerQueue chan *Layer
type NewIdQueue chan uint64


type Network interface {
	SendMessage(message []byte)
	RegisterProtocol(name string) chan []byte
}

type Algorithm struct {
	network           Network
	block2Id          map[BLockId]uint64
	allBlocks         map[BLockId]*Block
	layerQueue        LayerQueue
	idQueue           NewIdQueue
	posVotes          []bitarray.BitArray
	visibilityMap     []bitarray.BitArray
	layers            map[uint64]*Layer
	layerSize         uint64
	cachedLayers      uint64
	remainingBlockIds uint64
	totalBlocks uint64
}

func NewAlgorithm(net Network, layerSize uint64, cachedLayers uint64) Algorithm{
	totBlocks := layerSize*cachedLayers
	trtl := Algorithm{
		network:           net,
		block2Id:          make(map[BLockId]uint64),
		allBlocks:         make(map[BLockId]*Block),
		layerQueue:        make(LayerQueue, cachedLayers +1),
		remainingBlockIds: totBlocks,
		totalBlocks: totBlocks,
		posVotes:          make([]bitarray.BitArray,totBlocks),
		visibilityMap:     make([]bitarray.BitArray,totBlocks),
		layers:            make(map[uint64]*Layer),
		layerSize:         layerSize,
	}
	return trtl
}

func (alg *Algorithm) GlobalVotingAvg() uint64 {
	return 10
}


func (alg *Algorithm) LayerVotingAvg() uint64 {
	return 3
}

func (alg *Algorithm) IsTortoiseValid(originBlock *Block, targetBlockId uint64, visibleBlocks bitarray.BitArray) bool {
	voteFor, voteAgainst := alg.countTotalVotesForBlock(targetBlockId, visibleBlocks)

	/*if err != nil {
		log.Error("Failed to count votes")
	}*/
	if voteFor > alg.GlobalVotingAvg() {
		return true
	}
	if voteAgainst > alg.GlobalVotingAvg(){
		return false
	}

	voteFor, voteAgainst = alg.CountVotesInLastLayer(originBlock) //??

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


func (alg *Algorithm) getLayerById(layerId uint64) *Layer{
	return alg.layers[layerId]
}

func (alg *Algorithm) CountVotesInLastLayer(block *Block) (uint64, uint64){
	var voteFor, voteAgainst uint64 = 0,0
	l := alg.getLayerById(block.layerNum +1)
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
	blockMap := bitarray.NewBitArray(alg.layerSize)
	visibilityMap := bitarray.NewBitArray(alg.layerSize)
	// Count direct voters
	for blockId,vote := range origin.blockVotes { //todo: check for double votes
		//todo: assert that block exists
		visibilityMap.SetBit(alg.block2Id[blockId])
		visibilityMap.Or(alg.visibilityMap[alg.block2Id[blockId]])
		if vote{
			blockMap.SetBit(alg.block2Id[blockId])
		}
	}

	// Go over all other blocks that exist and calculate the origin blocks votes for them
	for _, targetBlockIdx := range alg.block2Id {
		if val, err := visibilityMap.GetBit(targetBlockIdx); err != nil && val {
			if alg.IsTortoiseValid(origin,targetBlockIdx, visibilityMap) {
				blockMap.SetBit(targetBlockIdx)
			}
		}
	}
	return &blockMap, &visibilityMap
}

func (alg *Algorithm) countTotalVotesForBlock(targetId uint64, visibleBlocks bitarray.BitArray) (uint64, uint64){
	//targetId := alg.block2Id[target.id]
	var posVotes, conVotes uint64
	for _, blockIdx := range alg.block2Id {
		//todo: this check is done twice, where should it be done?
		if val, err := visibleBlocks.GetBit(blockIdx); err != nil && val {
			if set, _ := alg.posVotes[blockIdx].GetBit(targetId); set {
				posVotes++
			} else {
				conVotes++
			}
		}

	}
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
		alg.zeroBitColumn(id)
	}
	delete(alg.layers, l.layerNum)
}

func (alg *Algorithm) assignIdForBlock(blk *Block) uint64{
	//todo: should this section be protected by a mutex?
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

func (alg *Algorithm) HandleIncomingLayer(l Layer){
	//todo: thread safety
	alg.layers[l.layerNum] = &l
	//blockBitArrays := make(map[Block]bitarray.BitArray)
	if len(alg.layerQueue) > int(alg.cachedLayers) {
		layer := <-alg.layerQueue
		alg.recycleLayer(layer)
	}
	for _, originBlock := range l.blocks{
		//todo: what to do if block is invalid?
		if originBlock.IsSyntacticallyValid() {
			votesBM, visibleBM := alg.createBlockVotingMap(&originBlock)

			blockId := alg.assignIdForBlock(&originBlock)
			alg.posVotes[blockId] = votesBM
			alg.visibilityMap[blockId] = visibleBM
		}

	}
}

func (block *Block) IsContextuallyValid() bool {
	return true
}

func (block *Block) IsSyntacticallyValid() bool {
	return true
}

