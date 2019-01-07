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

const DefaultGasLimit =10
const DefaultGas = 1

type BlockBuilder struct{
	beginRoundEvent chan mesh.LayerID
	stopChan		chan struct{}
	newTrans		chan *state.Transaction
	hareResult		HareResultProvider
	transactionQueue []SerializableTransaction
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
	Txs        	[]SerializableTransaction
	VotePattern []mesh.BlockID
	View       	[]mesh.BlockID
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


func NewBlockBuilder(net p2p.Service, beginRoundEvent chan mesh.LayerID, weakCoin WeakCoinProvider,
													orph OrphanBlockProvider, hare HareResultProvider) BlockBuilder{
	return BlockBuilder{
		beginRoundEvent:beginRoundEvent,
		stopChan:make(chan struct{}),
		newTrans:make(chan *state.Transaction),
		hareResult:hare,
		transactionQueue:make([]SerializableTransaction,0,10),
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
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		return fmt.Errorf("already started")
	}

	t.started = true
	go t.acceptBlockData()
	return nil
}

func (t *BlockBuilder) Stop() error{
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.started {
		return fmt.Errorf("already stopped")
	}
	t.stopChan <- struct{}{}
	return nil
}

type HareResultProvider interface {
	GetResult(id mesh.LayerID) ([]mesh.BlockID, error)
}

type WeakCoinProvider interface {
	GetResult() bool
}

type OrphanBlockProvider interface {
	GetOrphans() []mesh.BlockID
}


func (t *BlockBuilder) AddTransaction(nonce uint64, origin, destination common.Address, amount *big.Int) error{
	if !t.started{
		return fmt.Errorf("BlockBuilderStopped")
	}
	t.newTrans <- state.NewTransaction(nonce,origin,destination,amount, DefaultGasLimit, big.NewInt(DefaultGas))
	return nil
}

func (t *BlockBuilder) createBlock(id mesh.LayerID, txs []SerializableTransaction) Block {
	res, err := t.hareResult.GetResult(id)
	if err != nil {
		log.Error("didnt receive hare result for layer %v", id)
	}
	b := Block{
		Id :         mesh.BlockID(rand.Int63()),
		LayerIndex:  id,
		Data:        nil,
		Coin:        t.weakCoinToss.GetResult(),
		Timestamp:   time.Now(),
		Txs :        txs,
		VotePattern : res,
		View:        t.orphans.GetOrphans(),
	}

	return b
}
func BlockAsBytes(block Block) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &block); err != nil {
		return nil, fmt.Errorf("error marshalling block ids %v", err)
	}
	return w.Bytes(), nil
}

func (t *BlockBuilder) acceptBlockData() {
	for {
		select {
			case <-t.stopChan:
				t.started = false
				return

			case id := <-t.beginRoundEvent:
				txList := t.transactionQueue[:common.Min(len(t.transactionQueue), MaxTransactionsPerBlock)]
				t.transactionQueue = t.transactionQueue[common.Min(len(t.transactionQueue), MaxTransactionsPerBlock):]
				blk := t.createBlock(id, txList)
				go func() {
					bytes, err := BlockAsBytes(blk)
					if err != nil {
						log.Error("cannot serialize block %v", err)
						return
					}
					t.network.Broadcast(meshSync.BlockProtocol, bytes)
				}()

			case tx := <- t.newTrans:
				t.transactionQueue = append(t.transactionQueue,Transaction2SerializableTransaction(tx))
		}
	}
}
