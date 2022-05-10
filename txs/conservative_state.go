package txs

import (
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// ConservativeState provides the conservative version of the SVM state by taking into accounts of
// nonce and balances for pending transactions in un-applied blocks and mempool.
type ConservativeState struct {
	svmState
	*cache

	logger log.Log
}

// NewConservativeState returns a ConservativeState.
func NewConservativeState(state svmState, db *sql.Database, logger log.Log) *ConservativeState {
	cs := &ConservativeState{
		svmState: state,
		logger:   logger,
	}
	cs.cache = newCache(newStore(db), cs.getState, logger)
	return cs
}

func (cs *ConservativeState) getState(addr types.Address) (uint64, uint64) {
	nonce, err := cs.svmState.GetNonce(addr)
	if err != nil {
		cs.logger.Fatal("failed to get nonce", log.Err(err))
	}
	balance, err := cs.svmState.GetBalance(addr)
	if err != nil {
		cs.logger.Fatal("failed to get balance", log.Err(err))
	}
	return nonce, balance
}

// SelectTXsForProposal picks a specific number of random txs for miner to pack in a proposal.
func (cs *ConservativeState) SelectTXsForProposal(numTXs int) ([]types.TransactionID, error) {
	mi, err := newMempoolIterator(cs.logger, cs.cache, numTXs)
	if err != nil {
		return nil, fmt.Errorf("create mempool iterator: %w", err)
	}
	return mi.PopAll(), nil
}

// AddToCache adds the provided transaction to the conservative cache.
func (cs *ConservativeState) AddToCache(tx *types.Transaction, newTX bool) error {
	received := time.Now()
	// save all new transactions as long as they are syntactically correct
	if newTX {
		if err := cs.cache.AddToDB(tx, received); err != nil {
			return err
		}
		events.ReportNewTx(types.LayerID{}, tx)
		events.ReportAccountUpdate(tx.Origin())
		events.ReportAccountUpdate(tx.GetRecipient())
	}
	return cs.cache.Add(tx, received)
}

// RevertState reverts the SVM state and database to the given layer.
func (cs *ConservativeState) RevertState(revertTo types.LayerID) (types.Hash32, error) {
	root, err := cs.svmState.Revert(revertTo)
	if err != nil {
		return root, fmt.Errorf("svm rewind to %v: %w", revertTo, err)
	}

	return root, cs.cache.RevertToLayer(revertTo)
}

// ApplyLayer applies the transactions specified by the ids to the state.
func (cs *ConservativeState) ApplyLayer(toApply *types.Block) ([]*types.Transaction, error) {
	logger := cs.logger.WithFields(toApply.LayerIndex, toApply.ID())
	logger.Info("applying layer to conservative state")

	if err := cs.cache.CheckApplyOrder(toApply.LayerIndex); err != nil {
		return nil, err
	}

	txs, err := cs.getTXsToApply(toApply)
	if err != nil {
		return nil, err
	}

	failedTxs, err := cs.svmState.ApplyLayer(toApply.LayerIndex, txs, toApply.Rewards)
	if err != nil {
		logger.With().Error("failed to apply layer txs",
			toApply.LayerIndex,
			log.Int("num_failed_txs", len(failedTxs)),
			log.Err(err))
		// TODO: We want to panic here once we have a way to "remember" that we didn't apply these txs
		//  e.g. persist the last layer transactions were applied from and use that instead of `oldVerified`
		return failedTxs, fmt.Errorf("apply layer: %w", err)
	}

	if err := cs.cache.ApplyLayer(toApply.LayerIndex, toApply.ID(), txs); err != nil {
		return failedTxs, err
	}
	return failedTxs, nil
}

func (cs *ConservativeState) getTXsToApply(toApply *types.Block) ([]*types.Transaction, error) {
	mtxs, missing := cs.GetMeshTransactions(toApply.TxIDs)
	if len(missing) > 0 {
		return nil, fmt.Errorf("find txs %v for applying layer %v", missing, toApply.LayerIndex)
	}
	txs := make([]*types.Transaction, 0, len(mtxs))
	for _, mtx := range mtxs {
		// some TXs in the block may be already applied previously
		if mtx.State == types.APPLIED {
			continue
		}
		txs = append(txs, &mtx.Transaction)
	}
	return txs, nil
}

// Transactions exports the transactions DB.
func (cs *ConservativeState) Transactions() database.Getter {
	return &txFetcher{tp: cs.cache.tp}
}

type txFetcher struct {
	tp txProvider
}

// Get transaction blob, by transaction id.
func (f *txFetcher) Get(hash []byte) ([]byte, error) {
	id := types.TransactionID{}
	copy(id[:], hash)
	return f.tp.GetBlob(id)
}
