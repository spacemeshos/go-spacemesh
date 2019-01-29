package mesh

import (
	"bytes"
	"fmt"
	"github.com/davecgh/go-xdr/xdr2"
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/common"
	"io"
	"math/big"
	"time"
)

type BlockID uint32
type LayerID uint32

type Block struct {
	Id          BlockID
	LayerIndex  LayerID
	MinerID string
	Data        []byte
	Coin        bool
	Timestamp   int64
	Txs         []SerializableTransaction
	BlockVotes 	[]BlockID
	ViewEdges   []BlockID
}

type SerializableTransaction struct{
	AccountNonce 	uint64
	Price			[]byte
	GasLimit		uint64
	Recipient 		*common.Address
	Origin			common.Address //todo: remove this, should be calculated from sig.
	Amount       	[]byte
	Payload      	[]byte
}

func NewBlock(coin bool, data []byte, ts time.Time, layerId LayerID) *Block {
	b := Block{
		Id:         BlockID(uuid.New().ID()),
		LayerIndex: layerId,
		BlockVotes: make([]BlockID,0,10),
		ViewEdges:  make([]BlockID,0,10),
		Timestamp:  ts.UnixNano(),
		Data:       data,
		Coin:       coin,
	}
	return &b
}

func  NewSerializableTransaction(nonce uint64, origin, recepient common.Address, amount, price *big.Int, gasLimit uint64) *SerializableTransaction{
	return &SerializableTransaction{
		AccountNonce: nonce,
		Price:            price.Bytes(),
		GasLimit:        gasLimit,
		Recipient:        &recepient,
		Origin:            origin,//todo: remove this, should be calculated from sig.
		Amount:        amount.Bytes(),
		Payload:        nil,
	}
}

func (b Block) ID() BlockID {
	return b.Id
}

func (b Block) Layer() LayerID {
	return b.LayerIndex
}

func (b *Block) AddVote(id BlockID){
	//todo: do this in a sorted manner
	b.BlockVotes = append(b.BlockVotes, id)
}

func (b *Block) AddView(id BlockID){
	//todo: do this in a sorted manner
	b.ViewEdges = append(b.ViewEdges, id)
}

func (b *Block) AddTransaction(sr *SerializableTransaction){
	b.Txs = append(b.Txs, *sr)
}

type Layer struct {
	blocks []*Block
	index  LayerID
}

func (l *Layer) Index() LayerID {
	return l.index
}

func (l *Layer) Blocks() []*Block {
	return l.blocks
}

func (l *Layer) Hash() []byte {
	return []byte("some hash representing the layer")
}

func (l *Layer) AddBlock(block *Block) {
	block.LayerIndex = l.index
	l.blocks = append(l.blocks, block)
}

func (l *Layer) SetBlocks(blocks []*Block){
	l.blocks = blocks
}

func NewExistingLayer(idx LayerID, blocks []*Block) *Layer {
	l := Layer{
		blocks: blocks,
		index:  idx,
	}
	return &l
}

func BlockAsBytes(block Block) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &block); err != nil {
		return nil, fmt.Errorf("error marshalling block ids %v", err)
	}
	return w.Bytes(), nil
}

func BytesAsBlock(buf io.Reader) (Block, error){
	b := Block{}
	_, err := xdr.Unmarshal(buf, &b)
	if err != nil {
		return b,err
	}
	return b, nil
}

func TransactionAsBytes(tx *SerializableTransaction) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &tx); err != nil {
		return nil, fmt.Errorf("error marshalling block ids %v", err)
	}
	return w.Bytes(), nil
}

func BytesAsTransaction(buf io.Reader) (*SerializableTransaction, error){
	b := SerializableTransaction{}
	_, err := xdr.Unmarshal(buf, &b)
	if err != nil {
		return &b,err
	}
	return &b, nil
}

func NewExistingBlock(id BlockID, layerIndex LayerID, data []byte) *Block {
	b := Block{
		Id:         BlockID(id),
		BlockVotes: make([]BlockID,0,10),
		ViewEdges:  make([]BlockID,0,10),
		LayerIndex: LayerID(layerIndex),
		Data:       data,
	}
	return &b
}

func NewLayer(layerIndex LayerID) *Layer {
	return &Layer{
		index: layerIndex,
		blocks:make([]*Block, 0, 10),
	}
}


