package miner

import (
	"bytes"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/oracle"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
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

const TxGossipChannel = "TxGossip"

type BlockBuilder struct{
	minerID fmt.Stringer // could be a pubkey or what ever. the identity we're claiming to be as miners.

	beginRoundEvent chan mesh.LayerID
	stopChan		chan struct{}
	newTrans		chan *mesh.SerializableTransaction
	txGossipChannel chan service.Message
	hareResult		HareResultProvider
	transactionQueue []mesh.SerializableTransaction
	mu sync.Mutex
	network p2p.Service
	weakCoinToss	WeakCoinProvider
	orphans			OrphanBlockProvider
	blockOracle oracle.BlockOracle
	started			bool
}




func NewBlockBuilder(minerID fmt.Stringer, net p2p.Service, beginRoundEvent chan mesh.LayerID, weakCoin WeakCoinProvider,
													orph OrphanBlockProvider, hare HareResultProvider, blockOracle oracle.BlockOracle) BlockBuilder{
	return BlockBuilder{
		minerID: minerID,
		beginRoundEvent:beginRoundEvent,
		stopChan: make(chan struct{}),
		newTrans: make(chan *mesh.SerializableTransaction),
		txGossipChannel: net.RegisterProtocol(TxGossipChannel),
		hareResult: hare,
		transactionQueue: make([]mesh.SerializableTransaction,0,10),
		mu: sync.Mutex{},
		network: net,
		weakCoinToss: weakCoin,
		orphans: orph,
		blockOracle: blockOracle,
		started: false,
	}

}


func Transaction2SerializableTransaction(tx *state.Transaction) mesh.SerializableTransaction{
	return mesh.SerializableTransaction{
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
	go t.listenForTx()
	return nil
}

func (t *BlockBuilder) Stop() error{
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.started {
		return fmt.Errorf("already stopped")
	}
	t.started = false
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

//used from external API call?
func (t *BlockBuilder) AddTransaction(nonce uint64, origin, destination common.Address, amount *big.Int) error{
	if !t.started{
		return fmt.Errorf("BlockBuilderStopped")
	}
	t.newTrans <- mesh.NewSerializableTransaction(nonce,origin,destination,amount, big.NewInt(DefaultGas), DefaultGasLimit)
	return nil
}

func (t *BlockBuilder) createBlock(minerID string, id mesh.LayerID, txs []mesh.SerializableTransaction) mesh.Block {
	res, err := t.hareResult.GetResult(id)
	if err != nil {
		log.Error("didnt receive hare result for layer %v", id)
	}
	b := mesh.Block{
		MinerID: minerID,
		Id :          mesh.BlockID(rand.Int63()),
		LayerIndex:   id,
		Data:         nil,
		Coin:         t.weakCoinToss.GetResult(),
		Timestamp:    time.Now().UnixNano(),
		Txs :         txs,
		BlockVotes : res,
		ViewEdges:    t.orphans.GetOrphans(),
	}

	return b
}

func (t *BlockBuilder) listenForTx(){
	for {
		select {
		case <-t.stopChan:
			return
		case data := <- t.txGossipChannel:
			x, err := mesh.BytesAsTransaction(bytes.NewReader(data.Bytes()))
			if err != nil {
				log.Error("cannot parse incoming TX")
				break
			}
			t.newTrans <- x
		}
	}
}


func (t *BlockBuilder) acceptBlockData() {
	for {
		select {
			case <-t.stopChan:
				return

			case id := <-t.beginRoundEvent:
				if t.blockOracle.Validate(id, t.minerID) {
					txList := t.transactionQueue[:common.Min(len(t.transactionQueue), MaxTransactionsPerBlock)]
					t.transactionQueue = t.transactionQueue[common.Min(len(t.transactionQueue), MaxTransactionsPerBlock):]
					blk := t.createBlock(t.minerID.String(), id, txList)
					go func() {
						bytes, err := mesh.BlockAsBytes(blk)
						if err != nil {
							log.Error("cannot serialize block %v", err)
							return
						}
						t.network.Broadcast(meshSync.NewBlock, bytes)
					}()
				}
				// todo: else what do we do with all these txs ?!

			case tx := <- t.newTrans:
				t.transactionQueue = append(t.transactionQueue,*tx)
		}
	}
}
