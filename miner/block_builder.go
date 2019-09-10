package miner

import (
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/oracle"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"math/rand"
	"sync"
	"time"
)

const MaxTransactionsPerBlock = 200 //todo: move to config
const MaxAtxPerBlock = 200          //todo: move to config

const DefaultGasLimit = 10
const DefaultGas = 1

const IncomingTxProtocol = "TxGossip"

const AtxsPerBlockLimit = 100

type Signer interface {
	Sign(m []byte) []byte
}

type TxValidator interface {
	AddressExists(addr types.Address) bool
	ValidateNonceAndBalance(transaction *types.Transaction) error
}

type AtxValidator interface {
	SyntacticallyValidateAtx(atx *types.ActivationTx) error
}

type Syncer interface {
	FetchPoetProof(poetProofRef []byte) error
	WeaklySynced() bool
}

type TxPool interface {
	GetTxsForBlock(numOfTxs int, seed []byte, getState func(addr types.Address) (nonce, balance uint64, err error)) ([]types.TransactionId, error)
	Put(id types.TransactionId, item *types.Transaction)
	Invalidate(id types.TransactionId)
}

type Projector interface {
	GetProjection(addr types.Address) (nonce, balance uint64, err error)
}

type BlockBuilder struct {
	log.Log
	Signer
	minerID          types.NodeId
	rnd              *rand.Rand
	hdist            types.LayerID
	beginRoundEvent  chan types.LayerID
	stopChan         chan struct{}
	txGossipChannel  chan service.GossipMessage
	atxGossipChannel chan service.GossipMessage
	hareResult       HareResultProvider
	AtxPool          *AtxMemPool
	TransactionPool  TxPool
	mu               sync.Mutex
	network          p2p.Service
	weakCoinToss     WeakCoinProvider
	orphans          OrphanBlockProvider
	blockOracle      oracle.BlockOracle
	txValidator      TxValidator
	atxValidator     AtxValidator
	syncer           Syncer
	started          bool
	atxsPerBlock     int // number of atxs to select per block
	projector        Projector
}

func NewBlockBuilder(minerID types.NodeId, sgn Signer, net p2p.Service, beginRoundEvent chan types.LayerID, hdist int,
	txPool TxPool, atxPool *AtxMemPool, weakCoin WeakCoinProvider, orph OrphanBlockProvider, hare HareResultProvider,
	blockOracle oracle.BlockOracle, txValidator TxValidator, atxValidator AtxValidator, syncer Syncer, atxsPerBlock int,
	projector Projector, lg log.Log) *BlockBuilder {

	seed := binary.BigEndian.Uint64(md5.New().Sum([]byte(minerID.Key)))

	return &BlockBuilder{
		minerID:          minerID,
		Signer:           sgn,
		hdist:            types.LayerID(hdist),
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
		atxsPerBlock:     atxsPerBlock,
		projector:        projector,
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
	GetResult(lower types.LayerID, upper types.LayerID) ([]types.BlockID, error)
}

type WeakCoinProvider interface {
	GetResult() bool
}

type OrphanBlockProvider interface {
	GetOrphanBlocksBefore(l types.LayerID) ([]types.BlockID, error)
}

//used from external API call?
func (t *BlockBuilder) AddTransaction(tx *types.Transaction) error {
	if !t.started {
		return fmt.Errorf("BlockBuilderStopped")
	}
	t.TransactionPool.Put(tx.Id(), tx)
	return nil
}

func calcHdistRange(id types.LayerID, hdist types.LayerID) (bottom types.LayerID, top types.LayerID) {
	if hdist == 0 {
		log.Panic("hdist cannot be zero")
	}

	bottom = types.LayerID(1)
	top = id - 1
	if id > hdist {
		bottom = id - hdist
	}

	return bottom, top
}

func (t *BlockBuilder) createBlock(id types.LayerID, atxID types.AtxId, eligibilityProof types.BlockEligibilityProof,
	txids []types.TransactionId, atxids []types.AtxId) (*types.Block, error) {

	var votes []types.BlockID = nil
	var err error
	if id == config.Genesis {
		return nil, errors.New("cannot create blockBytes in genesis layer")
	} else if id == config.Genesis+1 {
		votes = append(votes, config.GenesisId)
	} else { // get from hare
		bottom, top := calcHdistRange(id, t.hdist)
		votes, err = t.hareResult.GetResult(bottom, top)
		if err != nil {
			t.With().Warning("Could not get hare result during block creation",
				log.Uint64("bottom", uint64(bottom)), log.Uint64("top", uint64(top)),
				log.Uint64("hdist", uint64(t.hdist)), log.Err(err))
		}
		if votes == nil { // if no votes set to empty
			t.Info("Votes is nil. Setting votes to an empty array")
			votes = []types.BlockID{}
		}
	}

	viewEdges, err := t.orphans.GetOrphanBlocksBefore(id)
	if err != nil {
		return nil, err
	}

	b := types.MiniBlock{
		BlockHeader: types.BlockHeader{
			Id:               types.BlockID(t.rnd.Int63()),
			LayerIndex:       id,
			ATXID:            atxID,
			EligibilityProof: eligibilityProof,
			Data:             nil,
			Coin:             t.weakCoinToss.GetResult(),
			Timestamp:        time.Now().UnixNano(),
			BlockVotes:       votes,
			ViewEdges:        viewEdges,
		},
		AtxIds: selectAtxs(atxids, t.atxsPerBlock),
		TxIds:  txids,
	}

	t.Log.Event().Info(fmt.Sprintf("I've created a block in layer %v. id: %v, num of transactions: %v, votes: %d, viewEdges: %d atx %v, atxs:%v",
		b.LayerIndex, b.ID(), len(b.TxIds), len(b.BlockVotes), len(b.ViewEdges), b.ATXID.ShortString(), len(b.AtxIds)))

	blockBytes, err := types.InterfaceToBytes(b)
	if err != nil {
		return nil, err
	}

	return &types.Block{MiniBlock: b, Signature: t.Signer.Sign(blockBytes)}, nil
}

func selectAtxs(atxs []types.AtxId, atxsPerBlock int) []types.AtxId {
	if len(atxs) == 0 { // no atxs to pick from
		return atxs
	}

	if len(atxs) <= atxsPerBlock { // no need to choose
		return atxs // take all
	}

	// we have more than atxsPerBlock, choose randomly
	selected := make([]types.AtxId, 0)
	for i := 0; i < atxsPerBlock; i++ {
		idx := i + rand.Intn(len(atxs)-i)       // random index in [i, len(atxs))
		selected = append(selected, atxs[idx])  // select atx at idx
		atxs[i], atxs[idx] = atxs[idx], atxs[i] // swap selected with i so we don't choose it again
	}

	return selected
}

func (t *BlockBuilder) listenForTx() {
	t.Log.Info("start listening for txs")
	for {
		select {
		case <-t.stopChan:
			return
		case data := <-t.txGossipChannel:
			if !t.syncer.WeaklySynced() {
				// not accepting txs when not synced
				continue
			}
			if data == nil {
				continue
			}

			tx, err := types.BytesAsTransaction(data.Bytes())
			if err != nil {
				t.Log.Error("cannot parse incoming TX")
				continue
			}

			if err := t.ValidateAndAddTxToPool(tx, func() {
				t.Log.With().Info("got new tx", log.TxId(tx.Id().ShortString()))
				data.ReportValidation(IncomingTxProtocol)
			}); err != nil {
				t.With().Error("Transaction validation failed",
					log.TxId(tx.Id().ShortString()), log.String("origin", tx.Origin().String()), log.Err(err))
				continue
			}
		}
	}
}

func (t *BlockBuilder) ValidateAndAddTxToPool(tx *types.Transaction, postValidationFunc func()) error {
	if !t.txValidator.AddressExists(tx.Origin()) {
		return fmt.Errorf("transaction origin does not exist")
	}
	err := t.txValidator.ValidateNonceAndBalance(tx)
	if err != nil {
		return err
	}
	if postValidationFunc != nil {
		postValidationFunc()
	}
	t.TransactionPool.Put(tx.Id(), tx)
	return nil
}

func (t *BlockBuilder) listenForAtx() {
	t.Info("start listening for atxs")
	for {
		select {
		case <-t.stopChan:
			return
		case data := <-t.atxGossipChannel:
			if !t.syncer.WeaklySynced() {
				// not accepting atxs when not synced
				continue
			}
			t.handleGossipAtx(data)
		}
	}
}

func (t *BlockBuilder) handleGossipAtx(data service.GossipMessage) {
	if data == nil {
		return
	}
	atx, err := types.BytesAsAtx(data.Bytes(), *types.EmptyAtxId)
	if err != nil {
		t.Error("cannot parse incoming ATX")
		return
	}
	t.With().Info("got new ATX", log.AtxId(atx.ShortString()))

	//todo fetch from neighbour
	if atx.Nipst == nil {
		t.Panic("nil nipst in gossip")
		return
	}

	if err := t.syncer.FetchPoetProof(atx.GetPoetProofRef()); err != nil {
		t.Warning("received ATX (%v) with syntactically invalid or missing PoET proof (%x): %v",
			atx.ShortString(), atx.GetShortPoetProofRef(), err)
		return
	}

	id := atx.Id()
	events.Publish(events.NewAtx{Id: id.Hash32().String()})

	err = t.atxValidator.SyntacticallyValidateAtx(atx)
	events.Publish(events.ValidAtx{Id: atx.ShortString(), Valid: err == nil})
	if err != nil {
		t.Warning("received syntactically invalid ATX %v: %v", atx.ShortString(), err)
		// TODO: blacklist peer
		return
	}

	t.AtxPool.Put(atx.Id(), atx)
	data.ReportValidation(activation.AtxProtocol)
	t.With().Info("stored and propagated new syntactically valid ATX", log.AtxId(atx.ShortString()))
}

func (t *BlockBuilder) acceptBlockData() {
	for {
		select {

		case <-t.stopChan:
			return

		case layerID := <-t.beginRoundEvent:
			atxID, proofs, err := t.blockOracle.BlockEligible(layerID)
			if err != nil {
				t.With().Error("failed to check for block eligibility", log.LayerId(uint64(layerID)), log.Err(err))
				continue
			}
			if len(proofs) == 0 {
				t.With().Info("Notice: not eligible for blocks in layer", log.LayerId(uint64(layerID)))
				continue
			}
			// TODO: include multiple proofs in each block and weigh blocks where applicable

			var atxList []types.AtxId
			for _, atx := range t.AtxPool.GetAllItems() {
				atxList = append(atxList, atx.Id())
			}

			for _, eligibilityProof := range proofs {
				txList, err := t.TransactionPool.GetTxsForBlock(MaxTransactionsPerBlock, eligibilityProof.Sig, t.projector.GetProjection)
				if err != nil {
					t.With().Error("failed to get txs for block", log.LayerId(uint64(layerID)), log.Err(err))
					continue
				}
				blk, err := t.createBlock(layerID, atxID, eligibilityProof, txList, atxList)
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
					err = t.network.Broadcast(config.NewBlockProtocol, bytes)
					if err != nil {
						t.Log.Error("cannot send block %v", err)
					}
				}()
			}
		}
	}
}
