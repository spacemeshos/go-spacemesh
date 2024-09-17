package mesh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	"github.com/spacemeshos/go-spacemesh/txs"
)

var (
	ErrLayerNotInOrder = errors.New("layers not applied in order")
	ErrLayerApplied    = errors.New("layer already applied")
)

type Executor struct {
	logger   *zap.Logger
	db       sql.Executor
	atxsdata *atxsdata.Data
	vm       vmState
	cs       conservativeState

	mu sync.Mutex
}

func NewExecutor(db sql.Executor, atxsdata *atxsdata.Data, vm vmState, cs conservativeState, lg *zap.Logger) *Executor {
	return &Executor{
		logger:   lg,
		db:       db,
		atxsdata: atxsdata,
		vm:       vm,
		cs:       cs,
	}
}

// Revert reverts the VM state and conservative cache to the given layer.
func (e *Executor) Revert(ctx context.Context, revertTo types.LayerID) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.vm.Revert(revertTo); err != nil {
		return fmt.Errorf("revert state: %w", err)
	}
	if err := e.cs.RevertCache(revertTo); err != nil {
		return fmt.Errorf("revert cache: %w", err)
	}
	root, err := e.vm.GetStateRoot()
	if err != nil {
		return fmt.Errorf("get state hash: %w", err)
	}
	e.logger.Info("reverted state",
		log.ZContext(ctx),
		zap.Stringer("state_hash", root),
		zap.Uint32("revert_to", revertTo.Uint32()),
	)
	return nil
}

// ExecuteOptimistic executes the specified transactions and returns a block that contains
// only successfully executed transactions.
func (e *Executor) ExecuteOptimistic(
	ctx context.Context,
	lid types.LayerID,
	tickHeight uint64,
	rewards []types.AnyReward,
	tids []types.TransactionID,
) (*types.Block, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	start := time.Now()

	if err := e.checkOrder(lid); err != nil {
		return nil, err
	}
	executable, err := e.getExecutableTxs(tids)
	if err != nil {
		return nil, err
	}
	crewards, err := e.convertRewards(lid, rewards)
	if err != nil {
		return nil, err
	}
	ineffective, executed, err := e.vm.Apply(lid, executable, crewards)
	if err != nil {
		return nil, fmt.Errorf("apply txs optimistically: %w", err)
	}
	b := &types.Block{
		InnerBlock: types.InnerBlock{
			LayerIndex: lid,
			TickHeight: tickHeight,
			Rewards:    rewards,
		},
	}
	for _, tx := range executed {
		b.TxIDs = append(b.TxIDs, tx.ID)
	}
	b.Initialize()
	updateResults(b.ID(), executed)
	if err = e.cs.UpdateCache(ctx, lid, b.ID(), executed, ineffective); err != nil {
		return nil, fmt.Errorf("update cache: %w", err)
	}
	state, err := e.vm.GetStateRoot()
	if err != nil {
		return nil, fmt.Errorf("get state hash: %w", err)
	}
	e.logger.Debug("optimistically executed block",
		log.ZContext(ctx),
		zap.Uint32("lid", lid.Uint32()),
		zap.Stringer("block", b.ID()),
		zap.Stringer("state_hash", state),
		zap.Duration("duration", time.Since(start)),
		zap.Int("count", len(executed)),
		zap.Int("skipped", len(ineffective)),
		zap.Int("rewards", len(b.Rewards)),
	)
	return b, nil
}

// Execute transactions in the specified block and update the conservative cache.
func (e *Executor) Execute(ctx context.Context, lid types.LayerID, block *types.Block) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	start := time.Now()
	if err := e.checkOrder(lid); err != nil {
		return err
	}
	if block == nil {
		return e.executeEmpty(ctx, lid)
	}

	executable, err := e.getExecutableTxs(block.TxIDs)
	if err != nil {
		return err
	}
	rewards, err := e.convertRewards(lid, block.Rewards)
	if err != nil {
		return err
	}
	ineffective, executed, err := e.vm.Apply(block.LayerIndex, executable, rewards)
	if err != nil {
		return fmt.Errorf("apply block: %w", err)
	}
	updateResults(block.ID(), executed)
	if err = e.cs.UpdateCache(ctx, block.LayerIndex, block.ID(), executed, ineffective); err != nil {
		return fmt.Errorf("update cache: %w", err)
	}
	state, err := e.vm.GetStateRoot()
	if err != nil {
		return fmt.Errorf("get state hash: %w", err)
	}
	e.logger.Debug("executed block",
		log.ZContext(ctx),
		zap.Uint32("lid", lid.Uint32()),
		zap.Stringer("block", block.ID()),
		zap.Stringer("state_hash", state),
		zap.Duration("duration", time.Since(start)),
		zap.Int("count", len(executed)),
		zap.Int("rewards", len(rewards)),
	)
	return nil
}

func (e *Executor) convertRewards(lid types.LayerID, rewards []types.AnyReward) ([]types.CoinbaseReward, error) {
	res := make([]types.CoinbaseReward, 0, len(rewards))
	for _, r := range rewards {
		atx := e.atxsdata.Get(lid.GetEpoch(), r.AtxID)
		if atx == nil {
			return nil, fmt.Errorf("execute: missing atx %s/%s", lid.GetEpoch(), r.AtxID.ShortString())
		}
		res = append(res, types.CoinbaseReward{
			SmesherID: atx.Node,
			Coinbase:  atx.Coinbase,
			Weight:    r.Weight,
		})
	}
	sort.Slice(res, func(i, j int) bool {
		return bytes.Compare(res[i].Coinbase.Bytes(), res[j].Coinbase.Bytes()) < 0
	})
	return res, nil
}

func (e *Executor) executeEmpty(ctx context.Context, lid types.LayerID) error {
	start := time.Now()
	if _, _, err := e.vm.Apply(lid, nil, nil); err != nil {
		return fmt.Errorf("apply empty layer: %w", err)
	}
	if err := e.cs.UpdateCache(ctx, lid, types.EmptyBlockID, nil, nil); err != nil {
		return fmt.Errorf("update cache: %w", err)
	}
	state, err := e.vm.GetStateRoot()
	if err != nil {
		return fmt.Errorf("get state hash: %w", err)
	}
	e.logger.Info("executed empty layer",
		log.ZContext(ctx),
		zap.Uint32("lid", lid.Uint32()),
		zap.Stringer("state_hash", state),
		zap.Duration("duration", time.Since(start)),
	)
	return nil
}

func (e *Executor) checkOrder(lid types.LayerID) error {
	inState, err := layers.GetLastApplied(e.db)
	if err != nil {
		return fmt.Errorf("executor get last applied: %w", err)
	}
	if !lid.After(inState) {
		return fmt.Errorf("%w: %v", ErrLayerApplied, lid)
	}
	if lid != inState.Add(1) {
		return fmt.Errorf("%w: %v, instate %v", ErrLayerNotInOrder, lid, inState)
	}
	return nil
}

func updateResults(bid types.BlockID, executed []types.TransactionWithResult) {
	for i := range executed {
		executed[i].Block = bid
	}
}

// getExecutableTxs retrieves a list of txs filtering transaction that were previously executed.
func (e *Executor) getExecutableTxs(ids []types.TransactionID) ([]types.Transaction, error) {
	etxs := make([]types.Transaction, 0, len(ids))
	for _, tid := range ids {
		mtx, err := transactions.Get(e.db, tid)
		if err != nil {
			return nil, fmt.Errorf("executor get tx: %w", err)
		}
		if mtx.State == types.APPLIED {
			continue
		}
		if mtx.TxHeader == nil {
			txs.RawTxCount.WithLabelValues(txs.RawFromDB).Inc()
		}
		etxs = append(etxs, mtx.Transaction)
	}
	return etxs, nil
}
