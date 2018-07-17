package core

import ("github.com/spacemeshos/go-spacemesh/log"
	"github.com/golang-collections/go-datastructures/bitarray"
)


type LayerQueue chan Layer



type Network interface {
	SendMessage(message []byte)
	RegisterProtocol(name string) chan []byte
}

type Algorithm struct {
	network       Network
	posVotes      [200*200]bitarray.BitArray
	visibilityMap [200*200]bitarray.BitArray
	block2Id      map[BLockId]uint64
	allBlocks     map[BLockId]Block
	layerQueue    LayerQueue
	layerSize     uint64
}

func NewAlgorithm(net Network, layerSize uint64) Algorithm{
	trtl := Algorithm{
		network:    net,
		block2Id:   make(map[BLockId]uint64),
		allBlocks:  make(map[BLockId]Block),
		layerQueue: make(LayerQueue, layerSize),
		layerSize:  layerSize,
	}
	for i,_ := range trtl.visibilityMap{
		trtl.visibilityMap[i] = bitarray.NewBitArray(layerSize)
	}
	return trtl
}

func GlobalVotingAvg() int {
	return 0
}


func LayerVotingAvg() int {
	return 0
}

func GetNextLayer(currentLayer int64) (*Layer, error){
	return nil,nil
}

func (block *Block) IsTortoiseValid() bool {
	voteFor, voteAgainst, err := block.CountVotes() //??

	if err != nil {
		log.Error("Failed to count votes")
	}
	if voteFor > GlobalVotingAvg() {
		return true
	}
	if voteAgainst > GlobalVotingAvg(){
		return false
	}

	voteFor, voteAgainst, err = block.CountVotesInLastLayer() //??

	if err != nil {
		log.Error("Failed to count votes")
	}
	if voteFor > LayerVotingAvg() {
		return true
	}
	if voteAgainst > LayerVotingAvg(){
		return false
	}

	return block.coin
}


func voteByView(origin Block, target Block){
	//for
}

func (alg *Algorithm) createBlockImmediateVotingMap(origin *Block) bitarray.BitArray{
	blockMap := bitarray.NewBitArray(alg.layerSize)
	visibilityMap := bitarray.NewBitArray(alg.layerSize)
	for blockId,vote := range origin.visibleBlocks {
		visibilityMap.SetBit(alg.block2Id[blockId])
		visibilityMap.Or(alg.visibilityMap[alg.block2Id[blockId]])
		if vote{
			blockMap.SetBit(alg.block2Id[blockId])
		}
	}
	return blockMap
}

func (alg *Algorithm) swapLayer(layer *Layer, blockBitArrays map[Block]bitarray.BitArray){
	for _,block := range layer.blocks{
		alg.posVotes[alg.block2Id[block.id]] = bitarray.NewBitArray(alg.layerSize)
	}

}

func (alg *Algorithm) handleIncomingLayer(l Layer){
	//todo: thread safety
	blockBitArrays := make(map[Block]bitarray.BitArray)
	for _, block := range l.blocks{
		blockBitArrays[block] = alg.createBlockImmediateVotingMap(&block)
	}

	if len(alg.layerQueue) > int(alg.layerSize) {
		layer := <-alg.layerQueue
		alg.swapLayer(&layer, blockBitArrays)
	}
}

func (block *Block) IsContextuallyValid() bool {
	return true
}

func (block *Block) IsSyntacticallyValid() bool {
	return true
}

func (block *Block) GetLayerNum() int64 {
	return 0
}

func (block *Block) GetVotesForBlock(b Block) (int, int) {
	if v, ok := block.visibleBlocks[b.id]; ok {
		if v {
			return 1,0
		} else {
			return 0,1
		}
	}
	for visibleBlock := range block.visibleBlocks{

	}

	return 0,0
}

func (block *Block) CountVotes() (int,int,error){
	//foe each node that views this one, calculate voting
	votesFor := 0
	votesAgainst := 0
	for {
		layer, err := GetNextLayer(block.GetLayerNum())
		if err != nil {
			break
		}
		block.countVotesForLayer(*layer)
	}
	return votesFor,votesAgainst,nil
}

func (block *Block) countVotesForLayer(l Layer) (int,int,error){
	votesFor := 0
	votesAgainst := 0
	for _,bl := range l.blocks{
		up, down := bl.GetVotesForBlock(*block)
		votesFor += up
		votesAgainst += down
	}
	return votesFor,votesAgainst, nil
}

func (block *Block) CountVotesInLastLayer() (int,int,error){
	layer, err := GetNextLayer(block.GetLayerNum())
	if err != nil {
		return 0,0,nil
	}
	block.countVotesForLayer(*layer)
}
