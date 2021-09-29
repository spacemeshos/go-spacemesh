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
	"time"

	"github.com/spacemeshos/go-spacemesh/blocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/miner/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

const (
	defaultGasLimit = 10
	defaultFee      = 1
)

// AtxsPerBlockLimit indicates the maximum number of atxs a block can reference
const AtxsPerBlockLimit = 100

const blockBuildDurationErrorThreshold = 10 * time.Second

type signer interface {
	Sign(m []byte) []byte
}

type syncer interface {
	IsSynced(context.Context) bool
}

type txPool interface {
	GetTxsForBlock(numOfTxs int, getState func(addr types.Address) (nonce, balance uint64, err error)) ([]types.TransactionID, []*types.Transaction, error)
}

type projector interface {
	GetProjection(types.Address) (nonce, balance uint64, err error)
}

type blockOracle interface {
	BlockEligible(types.LayerID) (types.ATXID, []types.BlockEligibilityProof, []types.ATXID, error)
}

type baseBlockProvider interface {
	BaseBlock(context.Context) (types.BlockID, [][]types.BlockID, error)
}

// BlockBuilder is the struct that orchestrates the building of blocks, it is responsible for receiving hare results.
// referencing txs and atxs from mem pool and referencing them in the created block
// it is also responsible for listening to the clock and querying when a block should be created according to the block oracle
type BlockBuilder struct {
	log.Log
	signer
	minerID         types.NodeID
	rnd             *rand.Rand
	hdist           uint32
	beginRoundEvent chan types.LayerID
	stopChan        chan struct{}
	TransactionPool txPool
	mu              sync.Mutex
	network         p2p.Service
	meshProvider    meshProvider
	baseBlockP      baseBlockProvider
	blockOracle     blockOracle
	beaconProvider  blocks.BeaconGetter
	syncer          syncer
	wg              sync.WaitGroup
	started         bool
	atxsPerBlock    int // number of atxs to select per block
	txsPerBlock     int // max number of tx to select per block
	projector       projector
	db              database.Database
	layersPerEpoch  uint32
}

// Config is the block builders configuration struct
type Config struct {
	DBPath         string
	MinerID        types.NodeID
	Hdist          uint32
	AtxsPerBlock   int
	LayersPerEpoch uint32
	TxsPerBlock    int
}

// NewBlockBuilder creates a struct of block builder type.
func NewBlockBuilder(
	config Config,
	sgn signer,
	net p2p.Service,
	beginRoundEvent chan types.LayerID,
	orph meshProvider,
	bbp baseBlockProvider,
	blockOracle blockOracle,
	beaconProvider blocks.BeaconGetter,
	syncer syncer,
	projector projector,
	txPool txPool,
	logger log.Log,
) *BlockBuilder {
	seed := binary.BigEndian.Uint64(md5.New().Sum([]byte(config.MinerID.Key)))

	var db *database.LDBDatabase
	if len(config.DBPath) == 0 {
		db = database.NewMemDatabase()
	} else {
		var err error
		db, err = database.NewLDBDatabase(config.DBPath, 16, 16, logger)
		if err != nil {
			logger.With().Panic("cannot create block builder database", log.Err(err))
		}
	}

	return &BlockBuilder{
		minerID:         config.MinerID,
		signer:          sgn,
		hdist:           config.Hdist,
		Log:             logger,
		rnd:             rand.New(rand.NewSource(int64(seed))),
		beginRoundEvent: beginRoundEvent,
		stopChan:        make(chan struct{}),
		mu:              sync.Mutex{},
		network:         net,
		meshProvider:    orph,
		baseBlockP:      bbp,
		blockOracle:     blockOracle,
		beaconProvider:  beaconProvider,
		syncer:          syncer,
		started:         false,
		atxsPerBlock:    config.AtxsPerBlock,
		txsPerBlock:     config.TxsPerBlock,
		projector:       projector,
		TransactionPool: txPool,
		db:              db,
		layersPerEpoch:  config.LayersPerEpoch,
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
	t.wg.Add(1)
	go func() {
		t.createBlockLoop(log.WithNewSessionID(ctx))
		t.wg.Done()
	}()
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
	t.wg.Wait()
	return nil
}

type meshProvider interface {
	AddBlockWithTxs(context.Context, *types.Block) error
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
	copy(blockID[:], bts)
	return
}

// stopped returns if we should stop
func (t *BlockBuilder) stopped() bool {
	select {
	case <-t.stopChan:
		return true
	default:
		return false
	}
}

func (t *BlockBuilder) createBlock(
	ctx context.Context,
	id types.LayerID,
	atxID types.ATXID,
	eligibilityProof types.BlockEligibilityProof,
	txids []types.TransactionID,
	activeSet []types.ATXID,
) (*types.Block, error) {
	logger := t.WithContext(ctx)
	if !id.After(types.GetEffectiveGenesis()) {
		return nil, errors.New("cannot create blockBytes in genesis layer")
	}

	// get the most up-to-date base block, and a list of diffs versus local opinion
	base, diffs, err := t.baseBlockP.BaseBlock(ctx)
	if err != nil {
		return nil, err
	}

	b := types.MiniBlock{
		BlockHeader: types.BlockHeader{
			LayerIndex:       id,
			ATXID:            atxID,
			EligibilityProof: eligibilityProof,
			Data:             nil,
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
		logger.With().Debug("creating block with active set (no reference block for epoch)",
			log.Int("active_set_size", len(activeSet)),
			log.FieldNamed("ref_block", refBlock),
			log.Err(err))
		atxs := activeSet
		b.ActiveSet = &atxs
		beacon, bErr := t.beaconProvider.GetBeacon(epoch)
		if bErr != nil {
			return nil, bErr
		}
		b.TortoiseBeacon = beacon
	} else {
		logger.With().Debug("creating block with reference block (no active set)",
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
		logger.With().Debug("storing ref block", epoch, bl.ID())
		if err := t.storeRefBlock(epoch, bl.ID()); err != nil {
			logger.With().Error("cannot store ref block", epoch, log.Err(err))
			// todo: panic?
		}
	}

	logger.Event().Info("block created", bl.Fields()...)
	return bl, nil
}

func (t *BlockBuilder) createBlockLoop(ctx context.Context) {
	logger := t.WithContext(ctx)
	for {
		select {

		case <-t.stopChan:
			return

		case layerID := <-t.beginRoundEvent:
			logger := logger.WithFields(layerID, layerID.GetEpoch())
			logger.Info("builder got layer")
			if !t.syncer.IsSynced(ctx) {
				logger.Info("not synced yet, not building a block in this round")
				continue
			}
			if layerID.GetEpoch().IsGenesis() {
				continue
			}

			started := time.Now()

			atxID, proofs, atxs, err := t.blockOracle.BlockEligible(layerID)
			if err != nil {
				if errors.Is(err, blocks.ErrMinerHasNoATXInPreviousEpoch) {
					logger.With().Info("node has no ATX in previous epoch and is not eligible for blocks")
					events.ReportDoneCreatingBlock(true, layerID.Uint32(), "not eligible to produce block")
					continue
				}
				events.ReportDoneCreatingBlock(true, layerID.Uint32(), "failed to check for block eligibility")
				logger.With().Error("failed to check for block eligibility", layerID, log.Err(err))
				continue
			}
			if len(proofs) == 0 {
				events.ReportDoneCreatingBlock(false, layerID.Uint32(), "")
				logger.With().Info("not eligible for blocks in layer", layerID, layerID.GetEpoch())
				continue
			}
			// TODO: include multiple proofs in each block and weigh blocks where applicable

			logger.With().Info("eligible for one or more blocks in layer", log.Int("count", len(proofs)))
			for _, eligibilityProof := range proofs {
				txList, _, err := t.TransactionPool.GetTxsForBlock(t.txsPerBlock, t.projector.GetProjection)
				if err != nil {
					events.ReportDoneCreatingBlock(false, layerID.Uint32(), "failed to get txs for block")
					logger.With().Error("failed to get txs for block", layerID, log.Err(err))
					continue
				}
				blk, err := t.createBlock(ctx, layerID, atxID, eligibilityProof, txList, atxs)
				if err != nil {
					events.ReportDoneCreatingBlock(true, layerID.Uint32(), "cannot create new block")
					logger.With().Error("failed to create new block", log.Err(err))
					continue
				}

				t.saveBlockBuildDurationMetric(ctx, started, layerID, blk.ID())

				if t.stopped() {
					return
				}
				if err := t.meshProvider.AddBlockWithTxs(ctx, blk); err != nil {
					events.ReportDoneCreatingBlock(true, layerID.Uint32(), "failed to store block")
					logger.With().Error("failed to store block", blk.ID(), log.Err(err))
					continue
				}
				go func() {
					bytes, err := types.InterfaceToBytes(blk)
					if err != nil {
						logger.With().Error("failed to serialize block", log.Err(err))
						events.ReportDoneCreatingBlock(true, layerID.Uint32(), "cannot serialize block")
						return
					}

					// generate a new requestID for the new block message
					blockCtx := log.WithNewRequestID(ctx, layerID, blk.ID())
					if err = t.network.Broadcast(blockCtx, blocks.NewBlockProtocol, bytes); err != nil {
						logger.WithContext(blockCtx).With().Error("failed to send block", log.Err(err))
					}
					events.ReportDoneCreatingBlock(true, layerID.Uint32(), "")
				}()
			}
		}
	}
}

func (t *BlockBuilder) saveBlockBuildDurationMetric(ctx context.Context, started time.Time, layerID types.LayerID, blockID types.BlockID) {
	elapsed := time.Since(started)
	if elapsed > blockBuildDurationErrorThreshold {
		t.WithContext(ctx).WithFields(layerID, layerID.GetEpoch()).With().
			Error("block building took too long ", log.Duration("elapsed", elapsed))
	}

	metrics.BlockBuildDuration.Observe(float64(elapsed / time.Millisecond))
}
