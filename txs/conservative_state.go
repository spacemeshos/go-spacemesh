package txs

import (
	"fmt"
	"math"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
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
	*cache

	logger log.Log
	cfg    CSConfig
}

// NewConservativeState returns a ConservativeState.
func NewConservativeState(state vmState, db *sql.Database, opts ...ConservativeStateOpt) *ConservativeState {
	cs := &ConservativeState{
		vmState: state,
		cfg:     defaultCSConfig(),
		logger:  log.NewNop(),
	}
	for _, opt := range opts {
		opt(cs)
	}
	cs.cache = newCache(newStore(db), cs.getState, cs.logger)
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

// SelectBlockTXs combined the transactions in the proposals and put them in a stable order.
// the steps are:
// 0. do optimistic filtering if the proposals agree on the mesh hash and state root
//    this mean the following transactions will be filtered out. transactions that
//    - fail nonce check
//    - fail balance check
//    - are already applied in previous layer
//    if the proposals don't agree on the mesh hash and state root, we keep all transactions
// 1. put the output of step 0 in a stable order
// 2. pick the transactions in step 1 until the gas limit runs out.
func (cs *ConservativeState) SelectBlockTXs(lid types.LayerID, proposals []*types.Proposal) ([]types.TransactionID, error) {
	myHash, err := cs.cache.GetMeshHash(lid.Sub(1))
	if err != nil {
		cs.logger.With().Warning("failed to get mesh hash", lid, log.Err(err))
		// if we don't have hash for that layer, other nodes probably don't either
		myHash = types.EmptyLayerHash
	}

	md, err := checkStateConsensus(cs.logger, cs.cfg, lid, proposals, myHash, cs.GetMeshTransaction)
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
func (cs *ConservativeState) SelectProposalTXs(numEligibility int) []types.TransactionID {
	mi := newMempoolIterator(cs.logger, cs.cache, cs.cfg.BlockGasLimit)
	predictedBlock, byAddrAndNonce := mi.PopAll()
	numTXs := numEligibility * cs.cfg.NumTXsPerProposal
	return getProposalTXs(cs.logger, numTXs, predictedBlock, byAddrAndNonce)
}

// Validation initializes validation request.
func (cs *ConservativeState) Validation(raw types.RawTx) system.ValidationRequest {
	return cs.vmState.Validation(raw)
}

// AddToCache adds the provided transaction to the conservative cache.
func (cs *ConservativeState) AddToCache(tx *types.Transaction) error {
	received := time.Now()
	// save all new transactions as long as they are syntactically correct
	if err := cs.cache.AddToDB(tx, received); err != nil {
		return err
	}
	events.ReportNewTx(types.LayerID{}, tx)
	events.ReportAccountUpdate(tx.Principal)

	return cs.cache.Add(tx, received, nil)
}

// AddToDB ...
func (cs *ConservativeState) AddToDB(tx *types.Transaction) error {
	return cs.cache.AddToDB(tx, time.Now())
}

// RevertState reverts the VM state and database to the given layer.
func (cs *ConservativeState) RevertState(revertTo types.LayerID) (types.Hash32, error) {
	root, err := cs.vmState.Revert(revertTo)
	if err != nil {
		return root, fmt.Errorf("vm revert %v: %w", revertTo, err)
	}

	return root, cs.cache.RevertToLayer(revertTo)
}

// ApplyLayer applies the transactions specified by the ids to the state.
func (cs *ConservativeState) ApplyLayer(toApply *types.Block) ([]types.TransactionID, error) {
	logger := cs.logger.WithFields(toApply.LayerIndex, toApply.ID())
	logger.Info("applying layer to conservative state")

	if err := cs.cache.CheckApplyOrder(toApply.LayerIndex); err != nil {
		return nil, err
	}

	txs, raw, err := cs.getTXsToApply(toApply)
	if err != nil {
		return nil, err
	}

	skipped, err := cs.vmState.Apply(toApply.LayerIndex, raw, toApply.Rewards)
	if err != nil {
		logger.With().Error("failed to apply layer txs",
			toApply.LayerIndex,
			log.Int("num_failed_txs", len(skipped)),
			log.Err(err))
		// TODO: We want to panic here once we have a way to "remember" that we didn't apply these txs
		//  e.g. persist the last layer transactions were applied from and use that instead of `oldVerified`
		return nil, fmt.Errorf("apply layer: %w", err)
	}

	finalList := txs
	if len(skipped) > 0 {
		finalList = make([]*types.Transaction, 0, len(txs))
		failed := make(map[types.TransactionID]struct{})
		for _, id := range skipped {
			failed[id] = struct{}{}
		}
		for _, tx := range txs {
			if _, ok := failed[tx.ID]; !ok {
				finalList = append(finalList, tx)
			}
		}
	}

	logger.With().Info("applying layer to cache",
		log.Int("num_txs_failed", len(skipped)),
		log.Int("num_txs_final", len(finalList)))
	if _, errs := cs.cache.ApplyLayer(toApply.LayerIndex, toApply.ID(), finalList); len(errs) > 0 {
		return nil, errs[0]
	}
	return skipped, nil
}

func (cs *ConservativeState) getTXsToApply(toApply *types.Block) ([]*types.Transaction, []types.RawTx, error) {
	mtxs, missing := cs.GetMeshTransactions(toApply.TxIDs)
	if len(missing) > 0 {
		return nil, nil, fmt.Errorf("find txs %v for applying layer %v", missing, toApply.LayerIndex)
	}
	txs := make([]*types.Transaction, 0, len(toApply.TxIDs))
	raw := make([]types.RawTx, 0, len(toApply.TxIDs))
	for _, mtx := range mtxs {
		// some TXs in the block may be already applied previously
		if mtx.State == types.APPLIED {
			continue
		}
		// txs without header were saved by syncer without validation
		if mtx.TxHeader == nil {
			cs.logger.With().Debug("verifying synced transaction",
				toApply.ID(),
				toApply.LayerIndex,
				mtx.ID,
			)
			req := cs.vmState.Validation(mtx.RawTx)
			header, err := req.Parse()
			if err != nil {
				return nil, nil, fmt.Errorf("parsing %s: %w", mtx.ID, err)
			}
			if !req.Verify() {
				return nil, nil, fmt.Errorf("applying block %s with invalid tx %s", toApply.ID(), mtx.ID)
			}
			mtx.TxHeader = header
			// updating header also updates principal/nonce indexes
			if err := cs.tp.AddHeader(mtx.ID, header); err != nil {
				return nil, nil, err
			}
			// restore cache consistency (e.g nonce/balance) so that gossiped
			// transactions can be added successfully
			if err := cs.cache.Add(&mtx.Transaction, mtx.Received, nil); err != nil {
				return nil, nil, err
			}
		}
		txs = append(txs, &mtx.Transaction)
		raw = append(raw, mtx.RawTx)
	}
	return txs, raw, nil
}
