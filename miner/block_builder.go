package miner

import (
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/oracle"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	meshSync "github.com/spacemeshos/go-spacemesh/sync"
	"github.com/spacemeshos/go-spacemesh/types"
	"math/big"
	"math/rand"
	"sync"
	"time"
)

const MaxTransactionsPerBlock = 200 //todo: move to config
const MaxAtxPerBlock = 200          //todo: move to config

const DefaultGasLimit = 10
const DefaultGas = 1

const IncomingTxProtocol = "TxGossip"

type AtxProcessor interface {
	ProcessAtx(atx *types.ActivationTx, fromBlock bool)
}

type BlockBuilder struct {
	log.Log
	minerID          types.NodeId
	rnd              *rand.Rand
	beginRoundEvent  chan types.LayerID
	stopChan         chan struct{}
	newTrans         chan *types.SerializableTransaction
	newAtx           chan *types.ActivationTx
	txGossipChannel  chan service.GossipMessage
	atxGossipChannel chan service.GossipMessage
	hareResult       HareResultProvider
	AtxQueue         []*types.ActivationTx
	transactionQueue []*types.SerializableTransaction
	mu               sync.Mutex
	network          p2p.Service
	weakCoinToss     WeakCoinProvider
	orphans          OrphanBlockProvider
	blockOracle      oracle.BlockOracle
	processAtx       func(atx *types.ActivationTx, isfromBlock bool)
	started          bool
}

func NewBlockBuilder(minerID types.NodeId, net p2p.Service, beginRoundEvent chan types.LayerID, weakCoin WeakCoinProvider, orph OrphanBlockProvider, hare HareResultProvider, blockOracle oracle.BlockOracle, processAtx func(atx *types.ActivationTx, isFromBLock bool), lg log.Log) BlockBuilder {

	seed := binary.BigEndian.Uint64(md5.New().Sum([]byte(minerID.Key)))

	return BlockBuilder{
		minerID:          minerID,
		Log:              lg,
		rnd:              rand.New(rand.NewSource(int64(seed))),
		beginRoundEvent:  beginRoundEvent,
		stopChan:         make(chan struct{}),
		newTrans:         make(chan *types.SerializableTransaction),
		newAtx:           make(chan *types.ActivationTx),
		txGossipChannel:  net.RegisterGossipProtocol(IncomingTxProtocol),
		atxGossipChannel: net.RegisterGossipProtocol(activation.AtxProtocol),
		hareResult:       hare,
		AtxQueue:         make([]*types.ActivationTx, 0, 10),
		transactionQueue: make([]*types.SerializableTransaction, 0, 10),
		mu:               sync.Mutex{},
		network:          net,
		weakCoinToss:     weakCoin,
		orphans:          orph,
		blockOracle:      blockOracle,
		processAtx:       processAtx,
		started:          false,
	}

}

func Transaction2SerializableTransaction(tx *mesh.Transaction) *types.SerializableTransaction {
	return &types.SerializableTransaction{
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
	go t.listenForAtx()
	return nil
}

func (t *BlockBuilder) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.started {
		return fmt.Errorf("already stopped")
	}
	t.started = false
	close(t.stopChan)
	return nil
}

type HareResultProvider interface {
	GetResult(id types.LayerID) ([]types.BlockID, error)
}

type WeakCoinProvider interface {
	GetResult() bool
}

type OrphanBlockProvider interface {
	GetOrphanBlocksBefore(l types.LayerID) ([]types.BlockID, error)
}

//used from external API call?
func (t *BlockBuilder) AddTransaction(nonce uint64, origin, destination address.Address, amount *big.Int) error {
	if !t.started {
		return fmt.Errorf("BlockBuilderStopped")
	}
	t.newTrans <- types.NewSerializableTransaction(nonce, origin, destination, amount, big.NewInt(DefaultGas), DefaultGasLimit)
	return nil
}

func (t *BlockBuilder) createBlock(id types.LayerID, atxID types.AtxId, eligibilityProof types.BlockEligibilityProof,
	txs []*types.SerializableTransaction, atx []*types.ActivationTx) (*types.Block, error) {

	var res []types.BlockID = nil
	var err error
	if id == config.Genesis {
		return nil, errors.New("cannot create block in genesis layer")
	} else if id == config.Genesis+1 {
		res = append(res, config.GenesisId)
	} else {
		res, err = t.hareResult.GetResult(id - 1)
		if err != nil {
			return nil, errors.New(fmt.Sprintf("didn't receive hare result for layer %v %v", id-1, err))
		}
	}

	viewEdges, err := t.orphans.GetOrphanBlocksBefore(id)
	if err != nil {
		return nil, err
	}

	b := types.Block{
		BlockHeader: types.BlockHeader{
			Id:               types.BlockID(t.rnd.Int63()),
			LayerIndex:       id,
			MinerID:          t.minerID,
			ATXID:            atxID,
			EligibilityProof: eligibilityProof,
			Data:             nil,
			Coin:             t.weakCoinToss.GetResult(),
			Timestamp:        time.Now().UnixNano(),
			BlockVotes:       res,
			ViewEdges:        viewEdges,
		},
		ATXs: atx,
		Txs:  txs,
	}
	atxs := " "
	for _, x := range b.ATXs {
		atxs += "," + x.ShortId()
	}

	t.Log.Info("I've created a block in layer %v. id: %v, num of transactions: %v, votes: %d, viewEdges: %d atx %v, atxs:%v",
		b.LayerIndex, b.Id, len(b.Txs), len(b.BlockVotes), len(b.ViewEdges), atxID.String()[:5], atxs)
	return &b, nil
}

func (t *BlockBuilder) listenForTx() {
	t.Log.Info("start listening for txs")
	for {
		select {
		case <-t.stopChan:
			return
		case data := <-t.txGossipChannel:
			if data != nil {
				x, err := types.BytesAsTransaction(data.Bytes())
				t.Log.With().Info("got new tx", log.String("sender", x.Origin.String()), log.String("receiver", x.Recipient.String()),
					log.String("amount", x.AmountAsBigInt().String()), log.Uint64("nonce", x.AccountNonce), log.Bool("valid", err != nil))
				if err != nil {
					t.Log.Error("cannot parse incoming TX")
					break
				}
				data.ReportValidation(IncomingTxProtocol)
				t.newTrans <- x
			}
		}
	}
}

func (t *BlockBuilder) listenForAtx() {
	t.Log.Info("start listening for atxs")
	for {
		select {
		case <-t.stopChan:
			return
		case data := <-t.atxGossipChannel:
			if data != nil {
				x, err := types.BytesAsAtx(data.Bytes())
				/*t.Log.With().Info("got new atx", log.String("sender", x., log.String("receiver", x.Recipient.String()),
				log.String("amount", x.AmountAsBigInt().String()), log.Uint64("nonce", x.AccountNonce), log.Bool("valid", err != nil))*/
				if err != nil {
					t.Log.Error("cannot parse incoming ATX")
					break
				}
				//t.processAtx(x)
				data.ReportValidation(activation.AtxProtocol)
				t.newAtx <- x
			}
		}
	}
}

func (t *BlockBuilder) acceptBlockData() {
	for {
		select {

		case <-t.stopChan:
			return

		case id := <-t.beginRoundEvent:
			atxID, proofs, err := t.blockOracle.BlockEligible(types.LayerID(id))
			if err != nil {
				t.Error("failed to check for block eligibility: %v ", err)
				continue
			}
			if len(proofs) == 0 {
				t.Error("no PROOFFSSSS detected")
				break
			}
			// TODO: include multiple proofs in each block and weigh blocks where applicable

			txList := t.transactionQueue[:common.Min(len(t.transactionQueue), MaxTransactionsPerBlock)]
			t.transactionQueue = t.transactionQueue[common.Min(len(t.transactionQueue), MaxTransactionsPerBlock):]

			atxList := t.AtxQueue[:common.Min(len(t.AtxQueue), MaxAtxPerBlock)]
			t.AtxQueue = t.AtxQueue[common.Min(len(t.AtxQueue), MaxAtxPerBlock):]

			for _, eligibilityProof := range proofs {
				blk, err := t.createBlock(types.LayerID(id), atxID, eligibilityProof, txList, atxList)
				if err != nil {
					t.Error("cannot create new block, %v ", err)
					continue
				}
				go func() {
					bytes, err := types.BlockAsBytes(*blk)
					if err != nil {
						t.Log.Error("cannot serialize block %v", err)
						return
					}
					t.network.Broadcast(meshSync.NewBlockProtocol, bytes)
				}()
			}

		case tx := <-t.newTrans:
			t.transactionQueue = append(t.transactionQueue, tx)
		case atx := <-t.newAtx:
			t.AtxQueue = append(t.AtxQueue, atx)
		}
	}
}
