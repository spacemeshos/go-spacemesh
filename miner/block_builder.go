// Package miner is responsible for creating valid blocks that contain valid activation transactions and transactions
package miner

import (
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"math/rand"
	"sync"
	"time"
)

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
	GetTxsForBlock(numOfTxs int, getState func(addr types.Address) (nonce, balance uint64, err error)) ([]types.TransactionID, []*types.Transaction, error)
}

type projector interface {
	GetProjection(addr types.Address) (nonce, balance uint64, err error)
}

type blockOracle interface {
	BlockEligible(layerID types.LayerID) (types.ATXID, []types.BlockEligibilityProof, error)
}

type atxDb interface {
	GetEpochAtxs(epochID types.EpochID) []types.ATXID
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
	//AtxPool         atxPool
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
	txsPerBlock     int // max number of tx to select per block
	layersPerEpoch  uint16
	projector       projector
	db              database.Database
	layerPerEpoch   uint16
}

// Config is the block builders configuration struct
type Config struct {
	MinerID        types.NodeID
	Hdist          int
	AtxsPerBlock   int
	LayersPerEpoch uint16
	TxsPerBlock    int
}

// NewBlockBuilder creates a struct of block builder type.
func NewBlockBuilder(config Config, sgn signer, net p2p.Service, beginRoundEvent chan types.LayerID, weakCoin weakCoinProvider, orph meshProvider, hare hareResultProvider, blockOracle blockOracle, syncer syncer, projector projector, txPool txPool, atxDB atxDb, lg log.Log) *BlockBuilder {

	seed := binary.BigEndian.Uint64(md5.New().Sum([]byte(config.MinerID.Key)))

	db, err := database.Create("builder", 16, 16, lg)
	if err != nil {
		lg.Panic("cannot create block builder DB %v", err)
	}

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
		txsPerBlock:     config.TxsPerBlock,
		projector:       projector,
		AtxDb:           atxDB,
		TransactionPool: txPool,
		db:              db,
		layerPerEpoch:   config.LayersPerEpoch,
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
	t.db.Close()
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
	AddBlockWithTxs(blk *types.Block, txs []*types.Transaction, atxs []*types.ActivationTx) error
}

func calcHdistRange(id types.LayerID, hdist types.LayerID) (bottom types.LayerID, top types.LayerID) {
	if hdist == 0 {
		log.Panic("hdist cannot be zero")
	}

	if id < types.GetEffectiveGenesis() {
		log.Panic("cannot get range from before effective genesis %v g: %v", id, types.GetEffectiveGenesis())
	}

	bottom = types.LayerID(types.GetEffectiveGenesis())
	top = id - 1
	if id > hdist+types.GetEffectiveGenesis() {
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
	if id <= types.GetEffectiveGenesis() {
		return nil, errors.New("cannot create blockBytes in genesis layer")
	}

	// if genesis+1
	if id == types.GetEffectiveGenesis()+1 {
		return append(votes, mesh.GenesisBlock().ID()), nil
	}

	// not genesis, get from hare
	bottom, top := calcHdistRange(id, t.hdist)

	if res, err := t.hareResult.GetResult(bottom); err != nil { // no result for bottom, take the whole layer
		t.With().Warning("Could not get result for bottom layer. Adding the whole layer instead.", log.Err(err),
			log.FieldNamed("bottom", bottom), log.FieldNamed("top", top), log.FieldNamed("hdist", t.hdist))
		ids, e := t.meshProvider.LayerBlockIds(bottom)
		if e != nil {
			t.With().Error("could not set votes to whole layer", log.Err(e))
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
			t.With().Warning("could not get result for layer in range", i, log.Err(err),
				log.FieldNamed("bottom", bottom), log.FieldNamed("top", top), log.FieldNamed("hdist", t.hdist))
			continue
		}
		votes = append(votes, res...)
	}

	votes = filterUnknownBlocks(votes, t.meshProvider.GetBlock)
	return votes, nil
}

func getEpochKey(ID types.EpochID) []byte {
	return []byte(fmt.Sprintf("e_%v", ID))
}

func (t *BlockBuilder) storeRefBlock(epoch types.EpochID, blockID types.BlockID) error {
	return t.db.Put(getEpochKey(epoch), blockID.Bytes())
}

func (t *BlockBuilder) getRefBlock(epoch types.EpochID) (blockID types.BlockID, err error) {
	bts, err := t.db.Get(getEpochKey(epoch))
	if err != nil {
		return
	}
	err = types.BytesToInterface(bts, &blockID)
	return
}

func (t *BlockBuilder) createBlock(id types.LayerID, atxID types.ATXID, eligibilityProof types.BlockEligibilityProof, txids []types.TransactionID) (*types.Block, error) {

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
		TxIDs: txids,
	}
	epoch := id.GetEpoch()
	refBlock, err := t.getRefBlock(epoch)
	if err != nil {
		atxs := t.AtxDb.GetEpochAtxs(id.GetEpoch() - 1)
		b.ActiveSet = &atxs
	} else {
		b.RefBlock = &refBlock
	}

	blockBytes, err := types.InterfaceToBytes(b)
	if err != nil {
		return nil, err
	}

	bl := &types.Block{MiniBlock: b, Signature: t.signer.Sign(blockBytes)}

	bl.Initialize()

	if b.ActiveSet != nil {
		t.Log.Info("storing ref block for epoch %v id %v", epoch, bl.ID())
		err := t.storeRefBlock(epoch, bl.ID())
		if err != nil {
			t.Log.Error("cannot store ref block for epoch %v err %v", epoch, err)
			//todo: panic?
		}
	}

	t.Log.Event().Info("block created",
		bl.ID(),
		bl.LayerIndex,
		bl.LayerIndex.GetEpoch(),
		bl.MinerID(),
		log.Int("tx_count", len(bl.TxIDs)),
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
				events.ReportDoneCreatingBlock(true, uint64(layerID), "failed to check for block eligibility")
				t.With().Error("failed to check for block eligibility", layerID, log.Err(err))
				continue
			}
			if len(proofs) == 0 {
				events.ReportDoneCreatingBlock(false, uint64(layerID), "")
				t.With().Info("Notice: not eligible for blocks in layer", layerID)
				continue
			}
			// TODO: include multiple proofs in each block and weigh blocks where applicable

			//reducedAtxList := selectAtxs(atxList, t.atxsPerBlock)
			for _, eligibilityProof := range proofs {
				txList, txs, err := t.TransactionPool.GetTxsForBlock(t.txsPerBlock, t.projector.GetProjection)
				if err != nil {
					events.ReportDoneCreatingBlock(true, uint64(layerID), "failed to get txs for block")
					t.With().Error("failed to get txs for block", layerID, log.Err(err))
					continue
				}
				blk, err := t.createBlock(layerID, atxID, eligibilityProof, txList)
				if err != nil {
					events.ReportDoneCreatingBlock(true, uint64(layerID), "cannot create new block")
					t.Error("cannot create new block, %v ", err)
					continue
				}
				err = t.meshProvider.AddBlockWithTxs(blk, txs, nil)
				if err != nil {
					events.ReportDoneCreatingBlock(true, uint64(layerID), "failed to store block")
					t.With().Error("failed to store block", blk.ID(), log.Err(err))
					continue
				}
				go func() {
					bytes, err := types.InterfaceToBytes(blk)
					if err != nil {
						t.Log.Error("cannot serialize block %v", err)
						events.ReportDoneCreatingBlock(true, uint64(layerID), "cannot serialize block")
						return
					}
					err = t.network.Broadcast(config.NewBlockProtocol, bytes)
					if err != nil {
						t.Log.Error("cannot send block %v", err)
					}
					events.ReportDoneCreatingBlock(true, uint64(layerID), "")
				}()
			}
		}
	}
}
