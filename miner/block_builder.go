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
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/oracle"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"math/rand"
	"sync"
	"time"
)

// MaxTransactionsPerBlock indicates the maximum transactions a block can reference
const MaxTransactionsPerBlock = 200 //todo: move to config

const defaultGasLimit = 10
const defaultFee = 1

// IncomingTxProtocol is the protocol identifier for tx received by gossip that is used by the p2p
const IncomingTxProtocol = "TxGossip"

// AtxsPerBlockLimit indicates the maximum number of atxs a block can reference
const AtxsPerBlockLimit = 100

type signer interface {
	Sign(m []byte) []byte
}

type txValidator interface {
	AddressExists(addr types.Address) bool
	ValidateNonceAndBalance(transaction *types.Transaction) error
}

type atxValidator interface {
	SyntacticallyValidateAtx(atx *types.ActivationTx) error
}

type syncer interface {
	FetchPoetProof(poetProofRef []byte) error
	ListenToGossip() bool
	IsSynced() bool
}

type txPool interface {
	GetTxsForBlock(numOfTxs int, getState func(addr types.Address) (nonce, balance uint64, err error)) ([]types.TransactionId, error)
	Put(id types.TransactionId, item *types.Transaction)
	Invalidate(id types.TransactionId)
}

type projector interface {
	GetProjection(addr types.Address) (nonce, balance uint64, err error)
}

// BlockBuilder is the struct that orchestrates the building of blocks, it is responsible for receiving hare results.
// referencing txs and atxs from mem pool and referencing them in the created block
// it is also responsible for listening to the clock and querying when a block should be created according to the block oracle
type BlockBuilder struct {
	log.Log
	signer
	minerID          types.NodeId
	rnd              *rand.Rand
	hdist            types.LayerID
	beginRoundEvent  chan types.LayerID
	stopChan         chan struct{}
	txGossipChannel  chan service.GossipMessage
	atxGossipChannel chan service.GossipMessage
	hareResult       hareResultProvider
	AtxPool          *AtxMemPool
	TransactionPool  txPool
	mu               sync.Mutex
	network          p2p.Service
	weakCoinToss     weakCoinProvider
	meshProvider     meshProvider
	blockOracle      oracle.BlockOracle
	txValidator      txValidator
	atxValidator     atxValidator
	syncer           syncer
	started          bool
	atxsPerBlock     int // number of atxs to select per block
	projector        projector
}

// NewBlockBuilder creates a struct of block builder type.
func NewBlockBuilder(minerID types.NodeId, sgn signer, net p2p.Service, beginRoundEvent chan types.LayerID, hdist int,
	txPool txPool, atxPool *AtxMemPool, weakCoin weakCoinProvider, orph meshProvider, hare hareResultProvider,
	blockOracle oracle.BlockOracle, txValidator txValidator, atxValidator atxValidator, syncer syncer, atxsPerBlock int,
	projector projector, lg log.Log) *BlockBuilder {

	seed := binary.BigEndian.Uint64(md5.New().Sum([]byte(minerID.Key)))

	return &BlockBuilder{
		minerID:          minerID,
		signer:           sgn,
		hdist:            types.LayerID(hdist),
		Log:              lg,
		rnd:              rand.New(rand.NewSource(int64(seed))),
		beginRoundEvent:  beginRoundEvent,
		stopChan:         make(chan struct{}),
		AtxPool:          atxPool,
		TransactionPool:  txPool,
		txGossipChannel:  net.RegisterGossipProtocol(IncomingTxProtocol, priorityq.Low),
		atxGossipChannel: net.RegisterGossipProtocol(activation.AtxProtocol, priorityq.Low),
		hareResult:       hare,
		mu:               sync.Mutex{},
		network:          net,
		weakCoinToss:     weakCoin,
		meshProvider:     orph,
		blockOracle:      blockOracle,
		txValidator:      txValidator,
		atxValidator:     atxValidator,
		syncer:           syncer,
		started:          false,
		atxsPerBlock:     atxsPerBlock,
		projector:        projector,
	}

}

// Start starts the process of creating a block, it listens for txs and atxs received by gossip, and starts querying
// blopck oracly chen it should create a block. this function returns an error if Start was already called once
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

// Close stops listeners and stops trying to create block in layers
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

type hareResultProvider interface {
	GetResult(lid types.LayerID) ([]types.BlockID, error)
}

type weakCoinProvider interface {
	GetResult() bool
}

type meshProvider interface {
	LayerBlockIds(index types.LayerID) ([]types.BlockID, error)
	GetOrphanBlocksBefore(l types.LayerID) ([]types.BlockID, error)
	GetBlock(id types.BlockID) (*types.Block, error)
}

//used from external API call to dd transaction
func (t *BlockBuilder) addTransaction(tx *types.Transaction) error {
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

	bottom = types.LayerID(0)
	top = id - 1
	if id > hdist {
		bottom = id - hdist
	}

	return bottom, top
}

func filterUnknownBlocks(blocks []types.BlockID, validate func(id types.BlockID) (*types.Block, error)) []types.BlockID {
	var filtered []types.BlockID
	for _, b := range blocks {
		if _, e := validate(b); e == nil {
			filtered = append(filtered, b)
		}
	}

	return filtered
}

func (t *BlockBuilder) getVotes(id types.LayerID) ([]types.BlockID, error) {
	var votes []types.BlockID = nil

	// if genesis
	if id == config.Genesis {
		return nil, errors.New("cannot create blockBytes in genesis layer")
	}

	// if genesis+1
	if id == config.Genesis+1 {
		return append(votes, mesh.GenesisBlock.Id()), nil
	}

	// not genesis, get from hare
	bottom, top := calcHdistRange(id, t.hdist)

	if res, err := t.hareResult.GetResult(bottom); err != nil { // no result for bottom, take the whole layer
		t.With().Warning("could not get result for bottom layer. adding the whole layer instead", log.Err(err),
			log.Uint64("bottom", uint64(bottom)), log.Uint64("top", uint64(top)), log.Uint64("hdist", uint64(t.hdist)))
		ids, e := t.meshProvider.LayerBlockIds(bottom)
		if e != nil {
			t.With().Error("Could not set votes to whole layer", log.Err(e))
			return nil, e
		}

		// set votes to whole layer
		votes = ids
	} else { // got result, just set
		votes = res
	}

	// add rest of hdist range
	for i := bottom + 1; i <= top; i++ {
		res, err := t.hareResult.GetResult(i)
		if err != nil {
			t.With().Warning("could not get result for layer in range", log.LayerId(uint64(i)), log.Err(err),
				log.Uint64("bottom", uint64(bottom)), log.Uint64("top", uint64(top)), log.Uint64("hdist", uint64(t.hdist)))
			continue
		}
		votes = append(votes, res...)
	}

	votes = filterUnknownBlocks(votes, t.meshProvider.GetBlock)
	return votes, nil
}

func (t *BlockBuilder) createBlock(id types.LayerID, atxID types.AtxId, eligibilityProof types.BlockEligibilityProof,
	txids []types.TransactionId, atxids []types.AtxId) (*types.Block, error) {

	votes, err := t.getVotes(id)
	if err != nil {
		return nil, err
	}

	viewEdges, err := t.meshProvider.GetOrphanBlocksBefore(id)
	if err != nil {
		return nil, err
	}

	b := types.MiniBlock{
		BlockHeader: types.BlockHeader{
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

	blockBytes, err := types.InterfaceToBytes(b)
	if err != nil {
		return nil, err
	}

	bl := &types.Block{MiniBlock: b, Signature: t.signer.Sign(blockBytes)}

	bl.Initialize()

	t.Log.Event().Info("block created",
		bl.Id(),
		bl.LayerIndex,
		log.String("miner_id", bl.MinerId().String()),
		log.Int("tx_count", len(bl.TxIds)),
		log.Int("atx_count", len(bl.AtxIds)),
		log.Int("view_edges", len(bl.ViewEdges)),
		log.Int("vote_count", len(bl.BlockVotes)),
		bl.ATXID,
		log.Uint32("eligibility_counter", bl.EligibilityProof.J),
	)
	return bl, nil
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
			if !t.syncer.ListenToGossip() {
				// not accepting txs when not synced
				continue
			}
			if data == nil {
				continue
			}

			tx, err := types.BytesAsTransaction(data.Bytes())
			if err != nil {
				t.With().Error("cannot parse incoming TX", log.Err(err))
				continue
			}
			if err := tx.CalcAndSetOrigin(); err != nil {
				t.With().Error("failed to calc transaction origin", log.TxId(tx.Id().ShortString()), log.Err(err))
				continue
			}
			if !t.txValidator.AddressExists(tx.Origin()) {
				t.With().Error("transaction origin does not exist", log.String("transaction", tx.String()),
					log.TxId(tx.Id().ShortString()), log.String("origin", tx.Origin().Short()), log.Err(err))
				continue
			}
			if err := t.txValidator.ValidateNonceAndBalance(tx); err != nil {
				t.With().Error("nonce and balance validation failed", log.TxId(tx.Id().ShortString()), log.Err(err))
				continue
			}
			t.Log.With().Info("got new tx",
				log.TxId(tx.Id().ShortString()),
				log.Uint64("nonce", tx.AccountNonce),
				log.Uint64("amount", tx.Amount),
				log.Uint64("fee", tx.Fee),
				log.Uint64("gas", tx.GasLimit),
				log.String("recipient", tx.Recipient.String()),
				log.String("origin", tx.Origin().String()))
			data.ReportValidation(IncomingTxProtocol)
			t.TransactionPool.Put(tx.Id(), tx)
		}
	}
}

// ValidateAndAddTxToPool validates the provided tx nonce and balance with projector and puts it in the transaction pool
// it returns an error if the provided tx is not valid
func (t *BlockBuilder) ValidateAndAddTxToPool(tx *types.Transaction) error {
	err := t.txValidator.ValidateNonceAndBalance(tx)
	if err != nil {
		return err
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
			if !t.syncer.ListenToGossip() {
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
	atx, err := types.BytesAsAtx(data.Bytes())
	if err != nil {
		t.Error("cannot parse incoming ATX")
		return
	}
	atx.CalcAndSetId()

	commitmentStr := "nil"
	if atx.Commitment != nil {
		commitmentStr = atx.Commitment.String()
	}

	challenge := ""
	h, err := atx.NIPSTChallenge.Hash()
	if err == nil && h != nil {
		challenge = h.String()

	}

	t.With().Info("got new ATX",
		log.String("sender_id", atx.NodeId.ShortString()),
		log.AtxId(atx.ShortString()),
		log.String("prev_atx_id", atx.PrevATXId.ShortString()),
		log.String("pos_atx_id", atx.PositioningAtx.ShortString()),
		log.LayerId(uint64(atx.PubLayerIdx)),
		log.Uint32("active_set", atx.ActiveSetSize),
		log.Int("view", len(atx.View)),
		log.Uint64("sequence_number", atx.Sequence),
		log.String("commitment", commitmentStr),
		log.Int("atx_size", len(data.Bytes())),
		log.String("NIPSTChallenge", challenge),
	)

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

	err = t.atxValidator.SyntacticallyValidateAtx(atx)
	events.Publish(events.ValidAtx{Id: atx.ShortString(), Valid: err == nil})
	if err != nil {
		t.Warning("received syntactically invalid ATX %v: %v", atx.ShortString(), err)
		// TODO: blacklist peer
		return
	}

	t.AtxPool.Put(atx)
	data.ReportValidation(activation.AtxProtocol)
	t.With().Info("stored and propagated new syntactically valid ATX", log.AtxId(atx.ShortString()))
}

func (t *BlockBuilder) acceptBlockData() {
	for {
		select {

		case <-t.stopChan:
			return

		case layerID := <-t.beginRoundEvent:
			if !t.syncer.IsSynced() {
				t.Debug("builder got layer %v not synced yet", layerID)
				continue
			}

			t.Debug("builder got layer %v", layerID)
			atxID, proofs, err := t.blockOracle.BlockEligible(layerID)
			if err != nil {
				events.Publish(events.DoneCreatingBlock{Eligible: true, Layer: uint64(layerID), Error: "failed to check for block eligibility"})
				t.With().Error("failed to check for block eligibility", log.LayerId(uint64(layerID)), log.Err(err))
				continue
			}
			if len(proofs) == 0 {
				events.Publish(events.DoneCreatingBlock{Eligible: false, Layer: uint64(layerID), Error: ""})
				t.With().Info("Notice: not eligible for blocks in layer", log.LayerId(uint64(layerID)))
				continue
			}
			// TODO: include multiple proofs in each block and weigh blocks where applicable

			var atxList []types.AtxId
			for _, atx := range t.AtxPool.GetAllItems() {
				atxList = append(atxList, atx.Id())
			}

			for _, eligibilityProof := range proofs {
				txList, err := t.TransactionPool.GetTxsForBlock(MaxTransactionsPerBlock, t.projector.GetProjection)
				if err != nil {
					events.Publish(events.DoneCreatingBlock{Eligible: true, Layer: uint64(layerID), Error: "failed to get txs for block"})
					t.With().Error("failed to get txs for block", log.LayerId(uint64(layerID)), log.Err(err))
					continue
				}
				blk, err := t.createBlock(layerID, atxID, eligibilityProof, txList, atxList)
				if err != nil {
					events.Publish(events.DoneCreatingBlock{Eligible: true, Layer: uint64(layerID), Error: "cannot create new block"})
					t.Error("cannot create new block, %v ", err)
					continue
				}
				go func() {
					bytes, err := types.InterfaceToBytes(blk)
					if err != nil {
						t.Log.Error("cannot serialize block %v", err)
						events.Publish(events.DoneCreatingBlock{Eligible: true, Layer: uint64(layerID), Error: "cannot serialize block"})
						return
					}
					err = t.network.Broadcast(config.NewBlockProtocol, bytes)
					if err != nil {
						t.Log.Error("cannot send block %v", err)
					}
					events.Publish(events.DoneCreatingBlock{Eligible: true, Layer: uint64(layerID), Error: ""})
				}()
			}
		}
	}
}
