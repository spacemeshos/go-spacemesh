package miner

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/oracle"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	meshSync "github.com/spacemeshos/go-spacemesh/sync"
	"math/big"
	"math/rand"
	"sync"
	"time"
)

const MaxTransactionsPerBlock = 200 //todo: move to config

const DefaultGasLimit = 10
const DefaultGas = 1

const IncomingTxProtocol = "TxGossip"

type BlockBuilder struct {
	minerID string // could be a pubkey or what ever. the identity we're claiming to be as miners.
	log.Log
	beginRoundEvent  chan mesh.LayerID
	stopChan         chan struct{}
	newTrans         chan *mesh.SerializableTransaction
	txGossipChannel  chan service.GossipMessage
	hareResult       HareResultProvider
	transactionQueue []mesh.SerializableTransaction
	mu               sync.Mutex
	network          p2p.Service
	weakCoinToss     WeakCoinProvider
	orphans          OrphanBlockProvider
	blockOracle      oracle.BlockOracle
	started          bool
}

func NewBlockBuilder(minerID string, net p2p.Service, beginRoundEvent chan mesh.LayerID, weakCoin WeakCoinProvider,
	orph OrphanBlockProvider, hare HareResultProvider, blockOracle oracle.BlockOracle, lg log.Log) BlockBuilder {
	return BlockBuilder{
		minerID:          minerID,
		Log:              lg,
		beginRoundEvent:  beginRoundEvent,
		stopChan:         make(chan struct{}),
		newTrans:         make(chan *mesh.SerializableTransaction),
		txGossipChannel:  net.RegisterGossipProtocol(IncomingTxProtocol),
		hareResult:       hare,
		transactionQueue: make([]mesh.SerializableTransaction, 0, 10),
		mu:               sync.Mutex{},
		network:          net,
		weakCoinToss:     weakCoin,
		orphans:          orph,
		blockOracle:      blockOracle,
		started:          false,
	}

}

func Transaction2SerializableTransaction(tx *mesh.Transaction) mesh.SerializableTransaction {
	return mesh.SerializableTransaction{
		AccountNonce: tx.AccountNonce,
		Origin:       tx.Origin,
		Recipient:    tx.Recipient,
		Amount:       tx.Amount.Bytes(),
		Payload:      tx.Payload,
		GasLimit:     tx.GasLimit,
		Price:        tx.Price.Bytes(),
	}
}

func (t *BlockBuilder) Start() error {
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

func (t *BlockBuilder) Stop() error {
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
	GetOrphanBlocksBefore(l mesh.LayerID) ([]mesh.BlockID, error)
}

//used from external API call?
func (t *BlockBuilder) AddTransaction(nonce uint64, origin, destination address.Address, amount *big.Int) error {
	if !t.started {
		return fmt.Errorf("BlockBuilderStopped")
	}
	t.newTrans <- mesh.NewSerializableTransaction(nonce, origin, destination, amount, big.NewInt(DefaultGas), DefaultGasLimit)
	return nil
}

func (t *BlockBuilder) createBlock(id mesh.LayerID, txs []mesh.SerializableTransaction) (*mesh.Block, error) {
	var res []mesh.BlockID = nil
	var err error
	if id == config.Genesis {
		return nil, errors.New("cannot create block in genesis layer ")
	} else if id == config.Genesis+1 {
		res = append(res, config.GenesisId)
	} else {
		res, err = t.hareResult.GetResult(id - 1)
		if err != nil {
			return nil, errors.New(fmt.Sprintf("didnt receive hare result for layer %v", id-1))
		}
	}

	viewEdges, err := t.orphans.GetOrphanBlocksBefore(id)
	if err != nil {
		return nil, err
	}

	b := mesh.Block{
		MinerID:    t.minerID,
		Id:         mesh.BlockID(rand.Int63()),
		LayerIndex: id,
		Data:       nil,
		Coin:       t.weakCoinToss.GetResult(),
		Timestamp:  time.Now().UnixNano(),
		Txs:        txs,
		BlockVotes: res,
		ViewEdges:  viewEdges,
	}

	t.Log.Info("Iv'e created block in layer %v id %v, num of transactions %v votes %d viewEdges %d", b.LayerIndex, b.Id, len(b.Txs), len(b.BlockVotes), len(b.ViewEdges))
	return &b, nil
}

func (t *BlockBuilder) listenForTx() {
	t.Log.Info("start listening for txs")
	for {
		select {
		case <-t.stopChan:
			return
		case data := <-t.txGossipChannel:
			x, err := mesh.BytesAsTransaction(bytes.NewReader(data.Bytes()))
			t.Log.With().Info("got new tx", log.String("sender", x.Origin.String()), log.String("receiver", x.Recipient.String()),
				log.String("amount", x.AmountAsBigInt().String()), log.Uint64("nonce", x.AccountNonce), log.Bool("valid", err != nil))
			if err != nil {
				t.Log.Error("cannot parse incoming TX")
				data.ReportValidation(IncomingTxProtocol, false)
				break
			}
			data.ReportValidation(IncomingTxProtocol, true)
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
			if !t.blockOracle.BlockEligible(mesh.LayerID(id), t.minerID) {
				break
			}

			txList := t.transactionQueue[:common.Min(len(t.transactionQueue), MaxTransactionsPerBlock)]
			t.transactionQueue = t.transactionQueue[common.Min(len(t.transactionQueue), MaxTransactionsPerBlock):]
			blk, err := t.createBlock(mesh.LayerID(id), txList)
			if err != nil {
				t.Error("cannot create new block, %v ", err)
				continue
			}
			go func() {
				bytes, err := mesh.BlockAsBytes(*blk)
				if err != nil {
					t.Log.Error("cannot serialize block %v", err)
					return
				}
				t.network.Broadcast(meshSync.NewBlockProtocol, bytes)
			}()

		case tx := <-t.newTrans:
			t.transactionQueue = append(t.transactionQueue, *tx)
		}
	}
}
