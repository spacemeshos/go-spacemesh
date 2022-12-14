package txs

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	vm "github.com/spacemeshos/go-spacemesh/genvm"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	"github.com/spacemeshos/go-spacemesh/system"
)

// CSConfig is the config for the conservative state/cache.
type CSConfig struct {
	BlockGasLimit      uint64
	NumTXsPerProposal  int
	OptFilterThreshold int
}

func defaultCSConfig() CSConfig {
	return CSConfig{
		BlockGasLimit:      math.MaxUint64,
		NumTXsPerProposal:  100,
		OptFilterThreshold: 90,
	}
}

// ConservativeStateOpt for configuring conservative state.
type ConservativeStateOpt func(cs *ConservativeState)

// WithCSConfig defines the config used for the conservative state.
func WithCSConfig(cfg CSConfig) ConservativeStateOpt {
	return func(cs *ConservativeState) {
		cs.cfg = cfg
	}
}

// WithLogger defines logger for conservative state.
func WithLogger(logger log.Log) ConservativeStateOpt {
	return func(cs *ConservativeState) {
		cs.logger = logger
	}
}

// ConservativeState provides the conservative version of the VM state by taking into accounts of
// nonce and balances for pending transactions in un-applied blocks and mempool.
type ConservativeState struct {
	vmState

	logger log.Log
	cfg    CSConfig
	db     *sql.Database
	cache  *cache
}

// NewConservativeState returns a ConservativeState.
func NewConservativeState(state vmState, db *sql.Database, opts ...ConservativeStateOpt) *ConservativeState {
	cs := &ConservativeState{
		vmState: state,
		cfg:     defaultCSConfig(),
		logger:  log.NewNop(),
		db:      db,
	}
	for _, opt := range opts {
		opt(cs)
	}
	cs.cache = newCache(cs.getState, cs.logger)
	return cs
}

func (cs *ConservativeState) getState(addr types.Address) (uint64, uint64) {
	nonce, err := cs.vmState.GetNonce(addr)
	if err != nil {
		cs.logger.With().Fatal("failed to get nonce", log.Err(err))
	}
	balance, err := cs.vmState.GetBalance(addr)
	if err != nil {
		cs.logger.With().Fatal("failed to get balance", log.Err(err))
	}
	return nonce, balance
}

// SelectBlockTXs combined the transactions in the proposals and put them in a stable order.
// the steps are:
//  0. do optimistic filtering if the proposals agree on the mesh hash and state root
//     this mean the following transactions will be filtered out. transactions that
//     - fail nonce check
//     - fail balance check
//     - are already applied in previous layer
//     if the proposals don't agree on the mesh hash and state root, we keep all transactions
//  1. put the output of step 0 in a stable order
//  2. pick the transactions in step 1 until the gas limit runs out.
func (cs *ConservativeState) SelectBlockTXs(lid types.LayerID, proposals []*types.Proposal) ([]types.TransactionID, error) {
	myHash, err := cs.GetMeshHash(lid.Sub(1))
	if err != nil {
		cs.logger.With().Warning("failed to get mesh hash", lid, log.Err(err))
		// if we don't have hash for that layer, other nodes probably don't either
		myHash = types.EmptyLayerHash
	}

	md, err := checkStateConsensus(cs.logger, cs.cfg, lid, proposals, myHash, cs)
	if err != nil {
		return nil, err
	}

	if len(md.mtxs) == 0 {
		return nil, nil
	}

	logger := cs.logger.WithName("block").WithFields(lid)
	blockSeed := types.CalcProposalsHash32(types.ToProposalIDs(proposals), nil).Bytes()
	return getBlockTXs(logger, md, cs.getState, blockSeed, cs.cfg.BlockGasLimit)
}

// SelectProposalTXs picks a specific number of random txs for miner to pack in a proposal.
func (cs *ConservativeState) SelectProposalTXs(lid types.LayerID, numEligibility int) []types.TransactionID {
	logger := cs.logger.WithFields(lid)
	mi := newMempoolIterator(logger, cs.cache, cs.cfg.BlockGasLimit)
	predictedBlock, byAddrAndNonce := mi.PopAll()
	numTXs := numEligibility * cs.cfg.NumTXsPerProposal
	return getProposalTXs(logger.WithFields(lid), numTXs, predictedBlock, byAddrAndNonce)
}

// Validation initializes validation request.
func (cs *ConservativeState) Validation(raw types.RawTx) system.ValidationRequest {
	return cs.vmState.Validation(raw)
}

// AddToCache adds the provided transaction to the conservative cache.
func (cs *ConservativeState) AddToCache(ctx context.Context, tx *types.Transaction) error {
	received := time.Now()
	if err := cs.cache.Add(ctx, cs.db, tx, received, false); err != nil {
		return err
	}
	events.ReportNewTx(types.LayerID{}, tx)
	events.ReportAccountUpdate(tx.Principal)
	return nil
}

// RevertState reverts the VM state and database to the given layer.
func (cs *ConservativeState) RevertState(revertTo types.LayerID) error {
	err := cs.vmState.Revert(revertTo)
	if err != nil {
		return fmt.Errorf("vm revert %v: %w", revertTo, err)
	}

	return cs.cache.RevertToLayer(cs.db, revertTo)
}

// ApplyLayer applies the transactions specified by the ids to the state.
func (cs *ConservativeState) ApplyLayer(ctx context.Context, lid types.LayerID, block *types.Block) error {
	if block == nil {
		return cs.applyEmptyLayer(ctx, lid)
	}

	logger := cs.logger.WithFields(block.LayerIndex, block.ID())
	logger.Debug("applying layer to conservative state")

	executable, err := cs.GetExecutableTxs(block.TxIDs)
	if err != nil {
		return err
	}

	ineffective, results, err := cs.vmState.Apply(
		vm.ApplyContext{Layer: block.LayerIndex, Block: block.ID()},
		executable,
		block.Rewards,
	)
	if err != nil {
		return fmt.Errorf("apply layer: %w", err)
	}

	logger.With().Debug("applying layer to cache",
		log.Int("num_txs_skipped", len(ineffective)),
		log.Int("num_txs_applied", len(results)),
	)
	t0 := time.Now()
	if err = cs.cache.ApplyLayer(ctx, cs.db, block.LayerIndex, block.ID(), results, ineffective); err != nil {
		return err
	}
	cacheApplyDuration.Observe(float64(time.Since(t0)))
	return nil
}

func (cs *ConservativeState) applyEmptyLayer(ctx context.Context, lid types.LayerID) error {
	_, _, err := cs.vmState.Apply(vm.ApplyContext{Layer: lid}, nil, nil)
	if err != nil {
		return fmt.Errorf("apply empty layer: %w", err)
	}
	return cs.cache.ApplyLayer(ctx, cs.db, lid, types.EmptyBlockID, nil, nil)
}

// GetProjection returns the projected nonce and balance for an account, including
// pending transactions that are paced in proposals/blocks but not yet applied to the state.
func (cs *ConservativeState) GetProjection(addr types.Address) (uint64, uint64) {
	return cs.cache.GetProjection(addr)
}

// LinkTXsWithProposal associates the transactions to a proposal.
func (cs *ConservativeState) LinkTXsWithProposal(lid types.LayerID, pid types.ProposalID, tids []types.TransactionID) error {
	return cs.cache.LinkTXsWithProposal(cs.db, lid, pid, tids)
}

// LinkTXsWithBlock associates the transactions to a block.
func (cs *ConservativeState) LinkTXsWithBlock(lid types.LayerID, bid types.BlockID, tids []types.TransactionID) error {
	return cs.cache.LinkTXsWithBlock(cs.db, lid, bid, tids)
}

// AddToDB adds a transaction to the database.
func (cs *ConservativeState) AddToDB(tx *types.Transaction) error {
	return transactions.Add(cs.db, tx, time.Now())
}

// HasTx returns true if transaction exists in the cache.
func (cs *ConservativeState) HasTx(tid types.TransactionID) (bool, error) {
	if cs.cache.Has(tid) {
		return true, nil
	}

	has, err := transactions.Has(cs.db, tid)
	if err != nil {
		return false, fmt.Errorf("has tx: %w", err)
	}
	return has, nil
}

// GetMeshHash gets the aggregated layer hash at the specified layer.
func (cs *ConservativeState) GetMeshHash(lid types.LayerID) (types.Hash32, error) {
	return layers.GetAggregatedHash(cs.db, lid)
}

// GetMeshTransaction retrieves a tx by its id.
func (cs *ConservativeState) GetMeshTransaction(tid types.TransactionID) (*types.MeshTransaction, error) {
	return transactions.Get(cs.db, tid)
}

// GetMeshTransactions retrieves a list of txs by their id's.
func (cs *ConservativeState) GetMeshTransactions(ids []types.TransactionID) ([]*types.MeshTransaction, map[types.TransactionID]struct{}) {
	missing := make(map[types.TransactionID]struct{})
	mtxs := make([]*types.MeshTransaction, 0, len(ids))
	for _, tid := range ids {
		var (
			mtx *types.MeshTransaction
			err error
		)
		if mtx, err = transactions.Get(cs.db, tid); err != nil {
			cs.logger.With().Warning("could not get tx", tid, log.Err(err))
			missing[tid] = struct{}{}
		} else {
			mtxs = append(mtxs, mtx)
		}
	}
	return mtxs, missing
}

// GetExecutableTxs retrieves a list of txs filtering transaction that were previously executed.
func (cs *ConservativeState) GetExecutableTxs(ids []types.TransactionID) ([]types.Transaction, error) {
	txs := make([]types.Transaction, 0, len(ids))
	for _, tid := range ids {
		mtx, err := transactions.Get(cs.db, tid)
		if err != nil {
			return nil, err
		}
		if mtx.State == types.APPLIED {
			continue
		}
		if mtx.TxHeader == nil {
			rawTxCount.WithLabelValues(rawFromDB).Inc()
		}
		txs = append(txs, mtx.Transaction)
	}
	return txs, nil
}

// GetTransactionsByAddress retrieves txs for a single address in between layers [from, to].
// Guarantees that transaction will appear exactly once, even if origin and recipient is the same, and in insertion order.
func (cs *ConservativeState) GetTransactionsByAddress(from, to types.LayerID, address types.Address) ([]*types.MeshTransaction, error) {
	return transactions.GetByAddress(cs.db, from, to, address)
}
