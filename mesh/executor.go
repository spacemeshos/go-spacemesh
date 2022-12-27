package mesh

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	vm "github.com/spacemeshos/go-spacemesh/genvm"
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
	logger log.Log
	db     sql.Executor
	vm     vmState
	cs     conservativeState

	mu sync.Mutex
}

func NewExecutor(db sql.Executor, vm vmState, cs conservativeState, lg log.Log) *Executor {
	return &Executor{
		logger: lg,
		db:     db,
		vm:     vm,
		cs:     cs,
	}
}

// Revert reverts the VM state and conservative cache to the given layer.
func (e *Executor) Revert(ctx context.Context, revertTo types.LayerID) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	logger := e.logger.WithContext(ctx).WithFields(log.Stringer("revert_to", revertTo))
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
	logger.Event().Info("reverted state", log.Stringer("state_hash", root))
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

	logger := e.logger.WithContext(ctx).WithFields(lid)
	if err := e.checkOrder(lid); err != nil {
		return nil, err
	}
	executable, err := e.getExecutableTxs(tids)
	if err != nil {
		return nil, err
	}
	ineffective, executed, err := e.vm.Apply(vm.ApplyContext{Layer: lid}, executable, rewards)
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
	logger.Event().Info("optimistically executed block", b.ID(), log.Stringer("state_hash", state))
	return b, nil
}

// Execute transactions in the specified block and update the conservative cache.
func (e *Executor) Execute(ctx context.Context, lid types.LayerID, block *types.Block) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.checkOrder(lid); err != nil {
		return err
	}
	if block == nil {
		return e.executeEmpty(ctx, lid)
	}

	logger := e.logger.WithContext(ctx).WithFields(lid, block.ID())
	executable, err := e.getExecutableTxs(block.TxIDs)
	if err != nil {
		return err
	}
	ineffective, executed, err := e.vm.Apply(vm.ApplyContext{Layer: block.LayerIndex}, executable, block.Rewards)
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
	logger.Event().Info("executed block", block.ID(), log.Stringer("state_hash", state))
	return nil
}

func (e *Executor) executeEmpty(ctx context.Context, lid types.LayerID) error {
	logger := e.logger.WithContext(ctx).WithFields(lid)
	if _, _, err := e.vm.Apply(vm.ApplyContext{Layer: lid}, nil, nil); err != nil {
		return fmt.Errorf("apply empty layer: %w", err)
	}
	if err := e.cs.UpdateCache(ctx, lid, types.EmptyBlockID, nil, nil); err != nil {
		return fmt.Errorf("update cache: %w", err)
	}
	state, err := e.vm.GetStateRoot()
	if err != nil {
		return fmt.Errorf("get state hash: %w", err)
	}
	logger.Event().Info("executed empty layer", log.Stringer("state_hash", state))
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
	for _, tx := range executed {
		tx.Block = bid
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
