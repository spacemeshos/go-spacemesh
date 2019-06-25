package miner

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/oracle"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/types"
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
	ProcessAtx(atx *types.ActivationTx)
}

type Signer interface {
	Sign(m []byte) []byte
}

type BlockBuilder struct {
	log.Log
	Signer
	minerID          types.NodeId
	rnd              *rand.Rand
	beginRoundEvent  chan types.LayerID
	stopChan         chan struct{}
	txGossipChannel  chan service.GossipMessage
	atxGossipChannel chan service.GossipMessage
	hareResult       HareResultProvider
	AtxPool          *MemPool
	TransactionPool  *MemPool
	mu               sync.Mutex
	network          p2p.Service
	weakCoinToss     WeakCoinProvider
	orphans          OrphanBlockProvider
	blockOracle      oracle.BlockOracle
	processAtx       func(atx *types.ActivationTx)
	started          bool
}

func NewBlockBuilder(minerID types.NodeId, sgn Signer, net p2p.Service,
	beginRoundEvent chan types.LayerID,
	txPool *MemPool,
	atxPool *MemPool,
	weakCoin WeakCoinProvider,
	orph OrphanBlockProvider,
	hare HareResultProvider,
	blockOracle oracle.BlockOracle,
	processAtx func(atx *types.ActivationTx),
	lg log.Log) BlockBuilder {

	seed := binary.BigEndian.Uint64(md5.New().Sum([]byte(minerID.Key)))

	return BlockBuilder{
		minerID:          minerID,
		Signer:           sgn,
		Log:              lg,
		rnd:              rand.New(rand.NewSource(int64(seed))),
		beginRoundEvent:  beginRoundEvent,
		stopChan:         make(chan struct{}),
		AtxPool:          atxPool,
		TransactionPool:  txPool,
		txGossipChannel:  net.RegisterGossipProtocol(IncomingTxProtocol),
		atxGossipChannel: net.RegisterGossipProtocol(activation.AtxProtocol),
		hareResult:       hare,
		mu:               sync.Mutex{},
		network:          net,
		weakCoinToss:     weakCoin,
		orphans:          orph,
		blockOracle:      blockOracle,
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
func (t *BlockBuilder) AddTransaction(tx *types.SerializableTransaction) error {
	if !t.started {
		return fmt.Errorf("BlockBuilderStopped")
	}
	t.TransactionPool.Put(types.GetTransactionId(tx), tx)
	return nil
}

func (t *BlockBuilder) createBlock(id types.LayerID, atxID types.AtxId, eligibilityProof types.BlockEligibilityProof,
	txs []*types.SerializableTransaction, atx []*types.ActivationTx) (*types.Block, error) {

	var votes []types.BlockID = nil
	var err error
	if id == config.Genesis {
		return nil, errors.New("cannot create blockBytes in genesis layer")
	} else if id == config.Genesis+1 {
		votes = append(votes, config.GenesisId)
	} else {
		votes, err = t.hareResult.GetResult(id - 1)
		if err != nil {
			return nil, errors.New(fmt.Sprintf("didn't receive hare result for layer %v %v", id-1, err))
		}
	}

	viewEdges, err := t.orphans.GetOrphanBlocksBefore(id)
	if err != nil {
		return nil, err
	}

	var txids []types.TransactionId
	for _, t := range txs {
		txids = append(txids, types.GetTransactionId(t))
	}

	var atxids []types.AtxId
	for _, t := range atx {
		atxids = append(atxids, t.Id())
	}

	b := types.MiniBlock{
		BlockHeader: types.BlockHeader{
			Id:               types.BlockID(t.rnd.Int63()),
			LayerIndex:       id,
			MinerID:          t.minerID,
			ATXID:            atxID,
			EligibilityProof: eligibilityProof,
			Data:             nil,
			Coin:             t.weakCoinToss.GetResult(),
			Timestamp:        time.Now().UnixNano(),
			BlockVotes:       votes,
			ViewEdges:        viewEdges,
		},
		AtxIds: atxids,
		TxIds:  txids,
	}

	t.Log.Info("I've created a block in layer %v. id: %v, num of transactions: %v, votes: %d, viewEdges: %d atx %v, atxs:%v",
		b.LayerIndex, b.Id, len(b.TxIds), len(b.BlockVotes), len(b.ViewEdges), b.ATXID.ShortId(), len(b.AtxIds))

	blockBytes, err := types.InterfaceToBytes(b)
	if err != nil {
		return nil, err
	}

	return &types.Block{MiniBlock: b, Signature: t.Signer.Sign(blockBytes)}, nil
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
				if err != nil {
					t.Log.Error("cannot parse incoming TX")
					break
				}
				id := types.GetTransactionId(x)
				t.Log.Info("got new tx %v", hex.EncodeToString(id[:]))
				t.TransactionPool.Put(types.GetTransactionId(x), x)
				data.ReportValidation(IncomingTxProtocol)
			}
		}
	}
}

func (t *BlockBuilder) listenForAtx() {
	t.Info("start listening for atxs")
	for {
		select {
		case <-t.stopChan:
			return
		case data := <-t.atxGossipChannel:
			if data != nil {
				x, err := types.BytesAsAtx(data.Bytes())
				if err != nil {
					t.Error("cannot parse incoming ATX")
					break
				}
				t.Info("got new ATX %v", hex.EncodeToString(x.Id().Bytes()))

				//todo fetch from neighbour
				if x.Nipst == nil {
					t.Error("nill nipst in gossip")
					break
				}

				t.AtxPool.Put(x.Id(), x)
				data.ReportValidation(activation.AtxProtocol)
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
			atxID, proofs, err := t.blockOracle.BlockEligible(id)
			if err != nil {
				t.Error("failed to check for block eligibility in layer %v: %v ", id, err)
				continue
			}
			if len(proofs) == 0 {
				t.Info("Notice: not eligible for blocks in layer %v", id)
				continue
			}
			// TODO: include multiple proofs in each block and weigh blocks where applicable

			txList := t.TransactionPool.PopItems(MaxTransactionsPerBlock).([]*types.SerializableTransaction)

			atxList := t.AtxPool.PopItems(MaxTransactionsPerBlock).([]*types.ActivationTx)

			for _, eligibilityProof := range proofs {
				blk, err := t.createBlock(types.LayerID(id), atxID, eligibilityProof, txList, atxList)
				if err != nil {
					t.Error("cannot create new block, %v ", err)
					continue
				}
				go func() {
					bytes, err := types.InterfaceToBytes(blk)
					if err != nil {
						t.Log.Error("cannot serialize block %v", err)
						return
					}
					t.network.Broadcast(config.NewBlockProtocol, bytes)
				}()
			}
		}
	}
}
