package miner

import (
	"bytes"
	"fmt"
	"github.com/davecgh/go-xdr/xdr2"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/state"
	meshSync "github.com/spacemeshos/go-spacemesh/sync"
	"math/big"
	"math/rand"
	"sync"
	"time"
)

const MaxTransactionsPerBlock = 200 //todo: move to config

type BlockBuilder struct{
	beginRoundEvent chan mesh.LayerID
	stopChan		chan struct{}
	newTrans		chan *state.Transaction
	hareResult		chan HareResult
	transactionQueue []SerializableTransaction
	hareRes			HareResult
	mu sync.Mutex
	network p2p.Service
	weakCoinToss	WeakCoinProvider
	orphans			OrphanBlockProvider
	started			bool
}

type Block struct {
	Id         	mesh.BlockID
	LayerIndex 	mesh.LayerID
	Data       	[]byte
	Coin       	bool
	Timestamp  	time.Time
	Txs			[]SerializableTransaction
	HareResult	[]mesh.BlockID
	Orphans		[]mesh.BlockID
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


func NewBlockBuilder(net p2p.Service, beginRoundEvent chan mesh.LayerID, weakCoin WeakCoinProvider, orph OrphanBlockProvider) BlockBuilder{
	return BlockBuilder{
		beginRoundEvent:beginRoundEvent,
		stopChan:make(chan struct{}),
		newTrans:make(chan *state.Transaction),
		hareResult:make(chan HareResult),
		transactionQueue:make([]SerializableTransaction,0,10),
		hareRes:nil,
		mu:sync.Mutex{},
		network:net,
		weakCoinToss:weakCoin,
		orphans:orph,
		started: false,
	}
}

func Transaction2SerializableTransaction(tx *state.Transaction) SerializableTransaction{
	return SerializableTransaction{
		AccountNonce:tx.AccountNonce,
		Origin:tx.Origin,
		Recipient:tx.Recipient,
		Amount:tx.Amount.Bytes(),
		Payload:tx.Payload,
		GasLimit:tx.GasLimit,
		Price:tx.Price.Bytes(),
	}
}

func (t *BlockBuilder) Start() error{
	if t.started {
		return fmt.Errorf("already started")
	}

	t.started = true
	go t.acceptBlockData()
	return nil
}

func (t *BlockBuilder) Stop() error{
	if !t.started {
		return fmt.Errorf("already stopped")
	}
	t.stopChan <- struct{}{}
	return nil
}

type HareResult interface {
	GetResult() []mesh.BlockID
}

type WeakCoinProvider interface {
	GetResult() bool
}

type OrphanBlockProvider interface {
	GetOrphans() []mesh.BlockID
}

func (t *BlockBuilder) CommitHareResult(res HareResult) error{
	if !t.started{
		return fmt.Errorf("BlockBuilderStopped")
	}
	t.hareResult <- res
	return nil
}

func (t *BlockBuilder) AddTransaction(nonce uint64, origin, destination common.Address, amount *big.Int) error{
	if !t.started{
		return fmt.Errorf("BlockBuilderStopped")
	}
	tx := state.NewTransaction(nonce,origin,destination,amount)
	t.newTrans <- tx
	return nil
}

func (t *BlockBuilder) resetAfterCreateBlock(){
	t.hareRes = nil
}

func (t *BlockBuilder) createBlock(id mesh.LayerID, txs []SerializableTransaction) Block {
	b := Block{
		Id : mesh.BlockID(rand.Int63()),
		LayerIndex:id,
		Data:nil,
		Coin: t.weakCoinToss.GetResult(),
		Timestamp:time.Now(),
		Txs : make([]SerializableTransaction, len(txs)),
		HareResult : t.hareRes.GetResult(),
		Orphans:t.orphans.GetOrphans(),
	}
	copy(b.Txs, txs)
	t.resetAfterCreateBlock()

	return b
}
func BlockAsBytes(block Block) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &block); err != nil {
		return nil, fmt.Errorf("error marshalling block ids %v", err)
	}
	return w.Bytes(), nil
}

func (t *BlockBuilder) acceptBlockData(){
	for{
		select {
		case <-t.stopChan:
			t.started = false
			return

		case id := <-t.beginRoundEvent:
			txList := t.transactionQueue[:common.Min(len(t.transactionQueue), MaxTransactionsPerBlock)]
			t.transactionQueue = t.transactionQueue[common.Min(len(t.transactionQueue), MaxTransactionsPerBlock):]
			blk := t.createBlock(id, txList)
			go func(){
				bytes , err := BlockAsBytes(blk)
				if err != nil {
					log.Error("cannot serialize block %v", err)
					return
				}
				t.network.Broadcast(meshSync.BlockProtocol,bytes)
			}()

		case tx := <- t.newTrans:
			t.transactionQueue = append(t.transactionQueue,Transaction2SerializableTransaction(tx))

		case res := <- t.hareResult:
			//todo: what if we get two results for same layer?
			//todo: what if we got the results of another layer?
			if t.hareRes != nil{
				panic("Different hare results received in same time frame")
			}
			t.hareRes = res
		}
	}
}
