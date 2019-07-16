package miner

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/address"
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

type Signer interface {
	Sign(m []byte) []byte
}

type TxValidator interface {
	ValidateTransactionSignature(tx *types.SerializableSignedTransaction) (address.Address, error)
}

type AtxValidator interface {
	SyntacticallyValidateAtx(atx *types.ActivationTx) error
}

type Syncer interface {
	FetchPoetProof(poetProofRef []byte) error
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
	AtxPool          *TypesAtxIdMemPool
	TransactionPool  *TypesTransactionIdMemPool
	mu               sync.Mutex
	network          p2p.Service
	weakCoinToss     WeakCoinProvider
	orphans          OrphanBlockProvider
	blockOracle      oracle.BlockOracle
	txValidator      TxValidator
	atxValidator     AtxValidator
	syncer           Syncer
	started          bool
}

func NewBlockBuilder(minerID types.NodeId, sgn Signer, net p2p.Service,
	beginRoundEvent chan types.LayerID,
	txPool *TypesTransactionIdMemPool,
	atxPool *TypesAtxIdMemPool,
	weakCoin WeakCoinProvider,
	orph OrphanBlockProvider,
	hare HareResultProvider,
	blockOracle oracle.BlockOracle,
	txValidator TxValidator,
	atxValidator AtxValidator,
	syncer Syncer,
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
		txValidator:      txValidator,
		atxValidator:     atxValidator,
		syncer:           syncer,
		started:          false,
	}

}

func Transaction2SerializableTransaction(tx *mesh.Transaction) *types.AddressableSignedTransaction {
	inner := types.InnerSerializableSignedTransaction{
		AccountNonce: tx.AccountNonce,
		Recipient:    *tx.Recipient,
		Amount:       tx.Amount.Uint64(),
		GasLimit:     tx.GasLimit,
		GasPrice:     tx.GasPrice.Uint64(),
	}
	sst := &types.SerializableSignedTransaction{
		InnerSerializableSignedTransaction: inner,
	}
	return &types.AddressableSignedTransaction{
		SerializableSignedTransaction: sst,
		Address:                       tx.Origin,
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
func (t *BlockBuilder) AddTransaction(tx *types.AddressableSignedTransaction) error {
	if !t.started {
		return fmt.Errorf("BlockBuilderStopped")
	}
	t.TransactionPool.Put(types.GetTransactionId(tx.SerializableSignedTransaction), tx)
	return nil
}

func (t *BlockBuilder) createBlock(id types.LayerID, atxID types.AtxId, eligibilityProof types.BlockEligibilityProof,
	txs []types.AddressableSignedTransaction, atx []types.ActivationTx) (*types.Block, error) {

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
		txids = append(txids, types.GetTransactionId(t.SerializableSignedTransaction))
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

	atxstring := ""
	for _, atx := range b.AtxIds {
		atxstring += atx.ShortId() + ", "
	}

	t.Log.Info("I've created a block in layer %v. id: %v, num of transactions: %v, votes: %d, viewEdges: %d atx %v, atxs (%v): %v",
		b.LayerIndex, b.Id, len(b.TxIds), len(b.BlockVotes), len(b.ViewEdges), b.ATXID.String()[:5], len(b.AtxIds), atxstring)

	blockBytes, err := types.InterfaceToBytes(b)
	if err != nil {
		return nil, err
	}

	return &types.Block{MiniBlock: b, Signature: t.Signer.Sign(blockBytes)}, nil
}

func (t *BlockBuilder) validateAndBuildTx(x *types.SerializableSignedTransaction) (*types.AddressableSignedTransaction, error) {
	addr, err := t.txValidator.ValidateTransactionSignature(x)
	if err != nil {
		return nil, err
	}

	return &types.AddressableSignedTransaction{SerializableSignedTransaction: x, Address: addr}, nil
}

func (t *BlockBuilder) listenForTx() {
	t.Log.Info("start listening for txs")
	for {
		select {
		case <-t.stopChan:
			return
		case data := <-t.txGossipChannel:
			if data != nil {

				x, err := types.BytesAsSignedTransaction(data.Bytes())
				if err != nil {
					t.Log.Error("cannot parse incoming TX")
					continue
				}

				id := types.GetTransactionId(x)
				fullTx, err := t.validateAndBuildTx(x)
				if err != nil {
					t.Log.Error("Transaction validation failed for id=%v, err=%v", id, err)
					continue
				}

				t.Log.Info("got new tx %v", hex.EncodeToString(id[:]))
				data.ReportValidation(IncomingTxProtocol)
				t.TransactionPool.Put(types.GetTransactionId(x), fullTx)
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
			go t.handleGossipAtx(data)
		}
	}
}

func (t *BlockBuilder) handleGossipAtx(data service.GossipMessage) {
	if data == nil {
		return
	}
	atx, err := types.BytesAsAtx(data.Bytes())
	if err != nil {
		t.Error("cannot parse incoming ATX")
		return
	}
	t.Info("got new ATX %v", atx.ShortId())

	//todo fetch from neighbour
	if atx.Nipst == nil {
		t.Panic("nil nipst in gossip")
		return
	}

	if err := t.syncer.FetchPoetProof(atx.GetPoetProofRef()); err != nil {
		t.Warning("received ATX (%v) with syntactically invalid or missing PoET proof (%x): %v",
			atx.ShortId(), atx.GetPoetProofRef()[:5], err)
		return
	}

	err = t.atxValidator.SyntacticallyValidateAtx(atx)
	if err != nil {
		t.Warning("received syntactically invalid ATX %v: %v", atx.ShortId(), err)
		// TODO: blacklist peer
		return
	}

	t.AtxPool.Put(atx.Id(), atx)
	data.ReportValidation(activation.AtxProtocol)
	t.Info("added to mempool and propagated new syntactically valid ATX: %v", atx.ShortId())
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

			txList := t.TransactionPool.PopItems(MaxTransactionsPerBlock)

			atxList := t.AtxPool.PopItems(MaxTransactionsPerBlock)

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
