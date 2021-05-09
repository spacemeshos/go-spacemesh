// Package miner is responsible for creating valid blocks that contain valid activation transactions and transactions
package miner

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"sync"

	"github.com/spacemeshos/go-spacemesh/blocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

const defaultGasLimit = 10
const defaultFee = 1

// AtxsPerBlockLimit indicates the maximum number of atxs a block can reference
const AtxsPerBlockLimit = 100

type signer interface {
	Sign(m []byte) []byte
}

type syncer interface {
	GetPoetProof(ctx context.Context, poetProofRef types.Hash32) error
	ListenToGossip() bool
	IsSynced(context.Context) bool
}

type txPool interface {
	GetTxsForBlock(numOfTxs int, getState func(addr types.Address) (nonce, balance uint64, err error)) ([]types.TransactionID, []*types.Transaction, error)
}

type projector interface {
	GetProjection(addr types.Address) (nonce, balance uint64, err error)
}

type blockOracle interface {
	BlockEligible(layerID types.LayerID) (types.ATXID, []types.BlockEligibilityProof, []types.ATXID, error)
}

type baseBlockProvider interface {
	BaseBlock() (types.BlockID, [][]types.BlockID, error)
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
	AtxDb           atxDb
	TransactionPool txPool
	mu              sync.Mutex
	network         p2p.Service
	weakCoinToss    weakCoinProvider
	meshProvider    meshProvider
	baseBlockP      baseBlockProvider
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
func NewBlockBuilder(config Config, sgn signer, net p2p.Service, beginRoundEvent chan types.LayerID, weakCoin weakCoinProvider, orph meshProvider, bbp baseBlockProvider, hare hareResultProvider, blockOracle blockOracle, syncer syncer, projector projector, txPool txPool, atxDB atxDb, lg log.Log) *BlockBuilder {
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
		baseBlockP:      bbp,
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
func (t *BlockBuilder) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		return fmt.Errorf("already started")
	}

	t.started = true
	go t.createBlockLoop(log.WithNewSessionID(ctx))
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
	AddBlockWithTxs(blk *types.Block) error
}

func calcHdistRange(id types.LayerID, hdist types.LayerID) (bottom types.LayerID, top types.LayerID) {
	if hdist == 0 {
		log.Panic("hdist cannot be zero")
	}

	if id < types.GetEffectiveGenesis() {
		log.Panic("cannot get range from before effective genesis %v g: %v", id, types.GetEffectiveGenesis())
	}

	bottom = types.GetEffectiveGenesis()
	top = id - 1
	if id > hdist+bottom {
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
	var votes []types.BlockID

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

	// first try to get the hare result for this layer and use that as our vote. if that fails, we just vote for the
	// whole layer (i.e., all of the blocks we received).
	if res, err := t.hareResult.GetResult(bottom); err != nil { // no result for bottom, take the whole layer
		t.With().Warning("could not get hare result for bottom layer, adding votes for the whole layer instead",
			log.Err(err),
			log.FieldNamed("bottom", bottom),
			log.FieldNamed("top", top),
			log.FieldNamed("hdist", t.hdist))
		ids, e := t.meshProvider.LayerBlockIds(bottom)
		if e != nil {
			t.With().Error("could not get block ids for layer", bottom, log.Err(e))
			return nil, e
		}
		t.With().Warning("adding votes for all blocks in layer", log.Int("num_blocks", len(ids)))

		// set votes to whole layer
		votes = ids
	} else { // got result, just set
		votes = res
	}

	// add rest of hdist range
	for i := bottom + 1; i <= top; i++ {
		res, err := t.hareResult.GetResult(i)
		if err != nil {
			t.With().Warning("could not get hare result for layer in hdist range, not adding votes for layer",
				i,
				log.Err(err),
				log.FieldNamed("bottom", bottom),
				log.FieldNamed("top", top),
				log.FieldNamed("hdist", t.hdist))
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

func (t *BlockBuilder) createBlock(id types.LayerID, atxID types.ATXID, eligibilityProof types.BlockEligibilityProof, txids []types.TransactionID, activeSet []types.ATXID) (*types.Block, error) {

	if id <= types.GetEffectiveGenesis() {
		return nil, errors.New("cannot create blockBytes in genesis layer")
	}

	// TODO: use this instead have logic inside trtl
	/*votes, err := t.getVotes(id)
	votes, err := t.getVotes(id)
	if err != nil {
		return nil, err
	}

	viewEdges, err := t.meshProvider.GetOrphanBlocksBefore(id)
	if err != nil {
		return nil, err
	}*/

	base, diffs, err := t.baseBlockP.BaseBlock()
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
			BaseBlock:        base,
			AgainstDiff:      diffs[0],
			ForDiff:          diffs[1],
			NeutralDiff:      diffs[2],
		},
		TxIDs: txids,
	}
	epoch := id.GetEpoch()
	refBlock, err := t.getRefBlock(epoch)
	if err != nil {
		t.With().Debug("creating block with active set (no reference block for epoch)",
			log.Int("active_set_size", len(activeSet)),
			log.FieldNamed("ref_block", refBlock),
			log.Err(err))
		atxs := activeSet
		b.ActiveSet = &atxs
	} else {
		t.With().Debug("creating block with reference block (no active set)",
			log.Int("active_set_size", len(activeSet)),
			log.FieldNamed("ref_block", refBlock))
		b.RefBlock = &refBlock
	}

	blockBytes, err := types.InterfaceToBytes(b)
	if err != nil {
		return nil, err
	}

	bl := &types.Block{MiniBlock: b, Signature: t.signer.Sign(blockBytes)}

	bl.Initialize()

	if b.ActiveSet != nil {
		t.With().Info("storing ref block", epoch, bl.ID())
		err := t.storeRefBlock(epoch, bl.ID())
		if err != nil {
			t.With().Error("cannot store ref block", epoch, log.Err(err))
			//todo: panic?
		}
	}

	t.Event().Info("block created", bl.Fields()...)
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

func (t *BlockBuilder) createBlockLoop(ctx context.Context) {
	logger := t.WithContext(ctx)
	for {
		select {

		case <-t.stopChan:
			return

		case layerID := <-t.beginRoundEvent:
			logger.With().Debug("builder got layer", layerID)
			if !t.syncer.IsSynced(ctx) {
				logger.Debug("not synced yet, not building a block in this round")
				continue
			}
			if layerID.GetEpoch().IsGenesis() {
				continue
			}

			atxID, proofs, atxs, err := t.blockOracle.BlockEligible(layerID)
			if err != nil {
				events.ReportDoneCreatingBlock(true, uint64(layerID), "failed to check for block eligibility")
				logger.With().Error("failed to check for block eligibility", layerID, log.Err(err))
				continue
			}
			if len(proofs) == 0 {
				events.ReportDoneCreatingBlock(false, uint64(layerID), "")
				logger.With().Info("not eligible for blocks in layer", layerID, layerID.GetEpoch())
				continue
			}
			// TODO: include multiple proofs in each block and weigh blocks where applicable

			logger.With().Info("eligible for one or more blocks in layer",
				log.Int("count", len(proofs)),
				layerID)
			for _, eligibilityProof := range proofs {
				txList, _, err := t.TransactionPool.GetTxsForBlock(t.txsPerBlock, t.projector.GetProjection)
				if err != nil {
					events.ReportDoneCreatingBlock(true, uint64(layerID), "failed to get txs for block")
					logger.With().Error("failed to get txs for block", layerID, log.Err(err))
					continue
				}
				blk, err := t.createBlock(layerID, atxID, eligibilityProof, txList, atxs)
				if err != nil {
					events.ReportDoneCreatingBlock(true, uint64(layerID), "cannot create new block")
					logger.With().Error("failed to create new block", log.Err(err))
					continue
				}
				err = t.meshProvider.AddBlockWithTxs(blk)
				if err != nil {
					events.ReportDoneCreatingBlock(true, uint64(layerID), "failed to store block")
					logger.With().Error("failed to store block", blk.ID(), log.Err(err))
					continue
				}
				go func() {
					bytes, err := types.InterfaceToBytes(blk)
					if err != nil {
						logger.With().Error("failed to serialize block", log.Err(err))
						events.ReportDoneCreatingBlock(true, uint64(layerID), "cannot serialize block")
						return
					}

					// generate a new requestID for the new block message
					blockCtx := log.WithNewRequestID(ctx, layerID, blk.ID())
					if err = t.network.Broadcast(blockCtx, blocks.NewBlockProtocol, bytes); err != nil {
						logger.WithContext(blockCtx).With().Error("failed to send block", log.Err(err))
					}
					events.ReportDoneCreatingBlock(true, uint64(layerID), "")
				}()
			}
		}
	}
}
