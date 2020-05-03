// Package miner is responsible for creating valid blocks that contain valid activation transactions and transactions
package miner

import (
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"math/rand"
	"sync"
	"time"
)

// MaxTransactionsPerBlock indicates the maximum transactions a block can reference
const MaxTransactionsPerBlock = 200 //todo: move to config (#1924)

const defaultGasLimit = 10
const defaultFee = 1

// AtxsPerBlockLimit indicates the maximum number of atxs a block can reference
const AtxsPerBlockLimit = 100

type signer interface {
	Sign(m []byte) []byte
}

type syncer interface {
	FetchPoetProof(poetProofRef []byte) error
	ListenToGossip() bool
	IsSynced() bool
}

type txPool interface {
	GetTxsForBlock(numOfTxs int, getState func(addr types.Address) (nonce, balance uint64, err error)) ([]types.TransactionID, error)
}

type projector interface {
	GetProjection(addr types.Address) (nonce, balance uint64, err error)
}

type blockOracle interface {
	BlockEligible(layerID types.LayerID) (types.ATXID, []types.BlockEligibilityProof, error)
}

type atxDb interface {
	GetEpochAtxs(epochID types.EpochID) ([]types.ATXID)
}

type atxPool interface {
	GetAllItems() []*types.ActivationTx
}

// BlockBuilder is the struct that orchestrates the building of blocks, it is responsible for receiving hare results.
// referencing txs and atxs from mem pool and referencing them in the created block
// it is also responsible for listening to the clock and querying when a block should be created according to the block oracle
type BlockBuilder struct {
	log.Log
	signer
	minerID         types.NodeID
	rnd             *rand.Rand
	hdist           types.LayerID
	beginRoundEvent chan types.LayerID
	stopChan        chan struct{}
	hareResult      hareResultProvider
	AtxPool         atxPool
	AtxDb           atxDb
	TransactionPool txPool
	mu              sync.Mutex
	network         p2p.Service
	weakCoinToss    weakCoinProvider
	meshProvider    meshProvider
	blockOracle     blockOracle
	syncer          syncer
	started         bool
	atxsPerBlock    int // number of atxs to select per block
	projector       projector
}

type Config struct {
	MinerID      types.NodeID
	Hdist        int
	AtxsPerBlock int
}

// NewBlockBuilder creates a struct of block builder type.
func NewBlockBuilder(config Config, sgn signer, net p2p.Service, beginRoundEvent chan types.LayerID, weakCoin weakCoinProvider, orph meshProvider, hare hareResultProvider, blockOracle blockOracle, syncer syncer, projector projector, txPool txPool, atxPool atxPool, atxDB atxDb, lg log.Log) *BlockBuilder {

	seed := binary.BigEndian.Uint64(md5.New().Sum([]byte(config.MinerID.Key)))

	return &BlockBuilder{
		minerID:         config.MinerID,
		signer:          sgn,
		hdist:           types.LayerID(config.Hdist),
		Log:             lg,
		rnd:             rand.New(rand.NewSource(int64(seed))),
		beginRoundEvent: beginRoundEvent,
		stopChan:        make(chan struct{}),
		hareResult:      hare,
		mu:              sync.Mutex{},
		network:         net,
		weakCoinToss:    weakCoin,
		meshProvider:    orph,
		blockOracle:     blockOracle,
		syncer:          syncer,
		started:         false,
		atxsPerBlock:    config.AtxsPerBlock,
		projector:       projector,
		AtxDb:           atxDB,
		AtxPool:         atxPool,
		TransactionPool: txPool,
	}

}

// Start starts the process of creating a block, it listens for txs and atxs received by gossip, and starts querying
// block oracle when it should create a block. This function returns an error if Start was already called once
func (t *BlockBuilder) Start() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		return fmt.Errorf("already started")
	}

	t.started = true
	go t.createBlockLoop()
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
	GetRefBlock(id types.EpochID) types.BlockID
}

/*
//used from external API call to dd transaction
func (t *BlockBuilder) addTransaction(tx *types.Transaction) error {
	if !t.started {
		return fmt.Errorf("BlockBuilderStopped")
	}
	t.TransactionPool.Put(tx.ID(), tx)
	return nil
}*/

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
		return append(votes, mesh.GenesisBlock.ID()), nil
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
			t.With().Warning("could not get result for layer in range", log.LayerID(uint64(i)), log.Err(err),
				log.Uint64("bottom", uint64(bottom)), log.Uint64("top", uint64(top)), log.Uint64("hdist", uint64(t.hdist)))
			continue
		}
		votes = append(votes, res...)
	}

	votes = filterUnknownBlocks(votes, t.meshProvider.GetBlock)
	return votes, nil
}

func (t *BlockBuilder) createBlock(id types.LayerID, atxID types.ATXID, eligibilityProof types.BlockEligibilityProof, txids []types.TransactionID, atxids []types.ATXID) (*types.Block, error) {

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
		ATXIDs: selectAtxs(atxids, t.atxsPerBlock),
		TxIDs:  txids,
	}

	blockBytes, err := types.InterfaceToBytes(b)
	if err != nil {
		return nil, err
	}

	bl := &types.Block{MiniBlock: b, Signature: t.signer.Sign(blockBytes)}

	if eligibilityProof.J == 0 {
		atxs := t.AtxDb.GetEpochAtxs(id.GetEpoch(1))
		bl.ActiveSet = &atxs
	} else {
		refBlock := t.meshProvider.GetRefBlock(id.GetEpoch(1))
		bl.RefBlock = &refBlock
	}

	bl.Initialize()

	t.Log.Event().Info("block created",
		bl.ID(),
		bl.LayerIndex,
		log.String("miner_id", bl.MinerID().String()),
		log.Int("tx_count", len(bl.TxIDs)),
		log.Int("atx_count", len(bl.ATXIDs)),
		log.Int("view_edges", len(bl.ViewEdges)),
		log.Int("vote_count", len(bl.BlockVotes)),
		bl.ATXID,
		log.Uint32("eligibility_counter", bl.EligibilityProof.J),
	)
	return bl, nil
}

func selectAtxs(atxs []types.ATXID, atxsPerBlock int) []types.ATXID {
	if len(atxs) == 0 { // no atxs to pick from
		return atxs
	}

	if len(atxs) <= atxsPerBlock { // no need to choose
		return atxs // take all
	}

	// we have more than atxsPerBlock, choose randomly
	selected := make([]types.ATXID, 0)
	for i := 0; i < atxsPerBlock; i++ {
		idx := i + rand.Intn(len(atxs)-i)       // random index in [i, len(atxs))
		selected = append(selected, atxs[idx])  // select atx at idx
		atxs[i], atxs[idx] = atxs[idx], atxs[i] // swap selected with i so we don't choose it again
	}

	return selected
}

/*
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
			if !t.syncer.ListenToGossip() {
				// not accepting atxs when not synced
				continue
			}
			t.handleGossipAtx(data)
		}
	}
}
*/

func (t *BlockBuilder) createBlockLoop() {
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
				t.With().Error("failed to check for block eligibility", log.LayerID(uint64(layerID)), log.Err(err))
				continue
			}
			if len(proofs) == 0 {
				events.Publish(events.DoneCreatingBlock{Eligible: false, Layer: uint64(layerID), Error: ""})
				t.With().Info("Notice: not eligible for blocks in layer", log.LayerID(uint64(layerID)))
				continue
			}
			// TODO: include multiple proofs in each block and weigh blocks where applicable

			var atxList []types.ATXID
			for _, atx := range t.AtxPool.GetAllItems() {
				atxList = append(atxList, atx.ID())
			}

			for _, eligibilityProof := range proofs {
				txList, err := t.TransactionPool.GetTxsForBlock(MaxTransactionsPerBlock, t.projector.GetProjection)
				if err != nil {
					events.Publish(events.DoneCreatingBlock{Eligible: true, Layer: uint64(layerID), Error: "failed to get txs for block"})
					t.With().Error("failed to get txs for block", log.LayerID(uint64(layerID)), log.Err(err))
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
