package txs

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	vm "github.com/spacemeshos/go-spacemesh/genvm"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	"github.com/spacemeshos/go-spacemesh/system"
)

// CSConfig is the config for the conservative state/cache.
type CSConfig struct {
	LayerSize          uint32
	LayersPerEpoch     uint32
	BlockGasLimit      uint64
	NumTXsPerProposal  int
	OptFilterThreshold int
}

func defaultCSConfig() CSConfig {
	return CSConfig{
		LayerSize:          50,
		LayersPerEpoch:     3,
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
	cdb    *datastore.CachedDB
	cache  *cache
	mu     sync.Mutex
}

// NewConservativeState returns a ConservativeState.
func NewConservativeState(state vmState, cdb *datastore.CachedDB, opts ...ConservativeStateOpt) *ConservativeState {
	cs := &ConservativeState{
		vmState: state,
		cfg:     defaultCSConfig(),
		logger:  log.NewNop(),
		cdb:     cdb,
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
	return nonce.Counter, balance
}

// GenerateBlock combined the transactions in the proposals and put them in a stable order.
// if optimistic filtering is ON, it also executes the transactions in situ.
// the selection steps are:
//  0. do optimistic filtering if the proposals agree on the mesh hash and state root
//     this mean the following transactions will be filtered out. transactions that
//     - fail nonce check
//     - fail balance check
//     - are already applied in previous layer
//     if the proposals don't agree on the mesh hash and state root, we keep all transactions
//  1. put the output of step 0 in a stable order
//  2. pick the transactions in step 1 until the gas limit runs out.
func (cs *ConservativeState) GenerateBlock(ctx context.Context, lid types.LayerID, proposals []*types.Proposal) (*types.Block, bool, error) {
	logger := cs.logger.WithContext(ctx).WithName("block").WithFields(lid)
	myHash, err := cs.GetMeshHash(lid.Sub(1))
	if err != nil {
		logger.With().Warning("failed to get mesh hash", lid, log.Err(err))
		myHash = types.EmptyLayerHash
	}
	md, err := checkStateConsensus(logger, cs.cfg, lid, proposals, myHash, cs)
	if err != nil {
		return nil, false, err
	}
	tickHeight, rewards, err := extractCoinbasesAndHeight(logger, cs.cdb, proposals, cs.cfg)
	if err != nil {
		return nil, false, err
	}
	var (
		b = &types.Block{
			InnerBlock: types.InnerBlock{
				LayerIndex: lid,
				TickHeight: tickHeight,
				Rewards:    rewards,
			},
		}
		tids     []types.TransactionID
		gasLimit = cs.cfg.BlockGasLimit
	)
	if md.optFilter {
		gasLimit = 0
	}
	if len(md.mtxs) > 0 {
		blockSeed := types.CalcProposalsHash32(types.ToProposalIDs(proposals), nil).Bytes()
		tids, err = getBlockTXs(logger, md, blockSeed, gasLimit)
		if err != nil {
			return nil, false, err
		}
	}
	if !md.optFilter {
		b.TxIDs = tids
		b.Initialize()
		return b, false, nil
	}

	logger.With().Info("executing txs in situ", log.Int("num_txs", len(tids)))
	cs.mu.Lock()
	defer cs.mu.Unlock()

	inState, err := layers.GetLastApplied(cs.cdb)
	if err != nil {
		return nil, false, fmt.Errorf("generate block get applied: %w", err)
	}
	if lid != inState.Add(1) {
		return nil, false, fmt.Errorf("%w: applying %v, instate %v", errLayerNotInOrder, lid, inState)
	}

	ineffective, executed, err := cs.vmExecute(lid, types.EmptyBlockID, tids, b.Rewards)
	if err != nil {
		return nil, false, fmt.Errorf("execute layer %v txs in vm: %w", lid, err)
	}
	// create the block and use the correct block ID to update the cache
	b.TxIDs = make([]types.TransactionID, 0, len(executed))
	for _, tx := range executed {
		b.TxIDs = append(b.TxIDs, tx.ID)
	}
	b.Initialize()
	for _, tx := range executed {
		tx.Block = b.ID()
	}
	err = cs.updateCache(ctx, lid, b.ID(), executed, ineffective)
	return b, true, err
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
	if err := cs.cache.Add(ctx, cs.cdb.Database, tx, received, false); err != nil {
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

	return cs.cache.RevertToLayer(cs.cdb.Database, revertTo)
}

// ApplyLayer applies the transactions specified by the ids to the state.
func (cs *ConservativeState) ApplyLayer(ctx context.Context, lid types.LayerID, block *types.Block) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	inState, err := layers.GetLastApplied(cs.cdb)
	if err != nil {
		return err
	}
	if !lid.After(inState) {
		// this is
		cs.logger.WithContext(ctx).With().Info("layer already applied", lid)
		return nil
	}
	if block == nil {
		return cs.applyEmptyLayer(ctx, lid)
	}

	ineffective, results, err := cs.vmExecute(lid, block.ID(), block.TxIDs, block.Rewards)
	if err != nil {
		return fmt.Errorf("execute txs in vm %v/%v: %w", lid, block.ID(), err)
	}
	return cs.updateCache(ctx, lid, block.ID(), results, ineffective)
}

func (cs *ConservativeState) vmExecute(
	lid types.LayerID,
	bid types.BlockID,
	tids []types.TransactionID,
	rewards []types.AnyReward,
) ([]types.Transaction, []types.TransactionWithResult, error) {
	executable, err := cs.GetExecutableTxs(tids)
	if err != nil {
		return nil, nil, err
	}
	return cs.vmState.Apply(
		vm.ApplyContext{Layer: lid, Block: bid},
		executable,
		rewards,
	)
}

func (cs *ConservativeState) updateCache(
	ctx context.Context,
	lid types.LayerID,
	bid types.BlockID,
	results []types.TransactionWithResult,
	ineffective []types.Transaction,
) error {
	t0 := time.Now()
	if err := cs.cache.ApplyLayer(ctx, cs.cdb.Database, lid, bid, results, ineffective); err != nil {
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
	return cs.cache.ApplyLayer(ctx, cs.cdb.Database, lid, types.EmptyBlockID, nil, nil)
}

// GetProjection returns the projected nonce and balance for an account, including
// pending transactions that are paced in proposals/blocks but not yet applied to the state.
func (cs *ConservativeState) GetProjection(addr types.Address) (uint64, uint64) {
	return cs.cache.GetProjection(addr)
}

// LinkTXsWithProposal associates the transactions to a proposal.
func (cs *ConservativeState) LinkTXsWithProposal(lid types.LayerID, pid types.ProposalID, tids []types.TransactionID) error {
	return cs.cache.LinkTXsWithProposal(cs.cdb.Database, lid, pid, tids)
}

// LinkTXsWithBlock associates the transactions to a block.
func (cs *ConservativeState) LinkTXsWithBlock(lid types.LayerID, bid types.BlockID, tids []types.TransactionID) error {
	return cs.cache.LinkTXsWithBlock(cs.cdb.Database, lid, bid, tids)
}

// AddToDB adds a transaction to the database.
func (cs *ConservativeState) AddToDB(tx *types.Transaction) error {
	return transactions.Add(cs.cdb, tx, time.Now())
}

// HasTx returns true if transaction exists in the cache.
func (cs *ConservativeState) HasTx(tid types.TransactionID) (bool, error) {
	if cs.cache.Has(tid) {
		return true, nil
	}

	has, err := transactions.Has(cs.cdb, tid)
	if err != nil {
		return false, fmt.Errorf("has tx: %w", err)
	}
	return has, nil
}

// GetMeshHash gets the aggregated layer hash at the specified layer.
func (cs *ConservativeState) GetMeshHash(lid types.LayerID) (types.Hash32, error) {
	return layers.GetAggregatedHash(cs.cdb, lid)
}

// GetMeshTransaction retrieves a tx by its id.
func (cs *ConservativeState) GetMeshTransaction(tid types.TransactionID) (*types.MeshTransaction, error) {
	return transactions.Get(cs.cdb, tid)
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
		if mtx, err = transactions.Get(cs.cdb, tid); err != nil {
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
		mtx, err := transactions.Get(cs.cdb, tid)
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
	return transactions.GetByAddress(cs.cdb, from, to, address)
}
