package txs

import (
	"errors"
	"fmt"
	"math/rand"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/svm"
)

var (
	errBadNonce            = errors.New("bad nonce")
	errInsufficientBalance = errors.New("insufficient balance")
)

// ConservativeState provides the conservative version of the SVM state by taking into accounts of
// nonce and balances for pending transactions in un-applied blocks and mempool.
type ConservativeState struct {
	*svm.SVM

	logger log.Log
	pool   *TxPool
}

// NewConservativeState returns a ConservativeState.
func NewConservativeState(state *svm.SVM, pool *TxPool, logger log.Log) *ConservativeState {
	return &ConservativeState{
		SVM:    state,
		pool:   pool,
		logger: logger,
	}
}

func (cs *ConservativeState) getState(addr types.Address) (uint64, uint64) {
	return cs.GetNonce(addr), cs.GetBalance(addr)
}

func getRandIdxs(numOfTxs, spaceSize int) map[uint64]struct{} {
	idxs := make(map[uint64]struct{})
	for len(idxs) < numOfTxs {
		rndInt := rand.Uint64()
		idx := rndInt % uint64(spaceSize)
		idxs[idx] = struct{}{}
	}
	return idxs
}

// SelectTXsForProposal picks a specific number of random txs for miner to pack in a proposal.
func (cs *ConservativeState) SelectTXsForProposal(numOfTxs int) ([]types.TransactionID, []*types.Transaction, error) {
	txIds, txs, err := cs.pool.getMempoolCandidateTXs(numOfTxs, cs.getState)
	if err != nil {
		return nil, nil, err
	}

	if len(txIds) <= numOfTxs {
		return txIds, txs, nil
	}

	var (
		ret    []types.TransactionID
		retTXs []*types.Transaction
	)
	for idx := range getRandIdxs(numOfTxs, len(txIds)) {
		// noinspection GoNilness
		ret = append(ret, txIds[idx])
		retTXs = append(retTXs, txs[idx])
	}

	return ret, retTXs, nil
}

// GetProjection returns the projected nonce and balance for the address with pending transactions
// in un-applied blocks and mempool.
func (cs *ConservativeState) GetProjection(addr types.Address) (uint64, uint64, error) {
	nonce, balance := cs.getState(addr)
	return cs.pool.GetProjection(addr, nonce, balance, meshAndMempool)
}

func (cs *ConservativeState) validateNonceAndBalance(tx *types.Transaction) error {
	origin := tx.Origin()
	nonce, balance, err := cs.GetProjection(origin)
	if err != nil {
		return fmt.Errorf("failed to project state for account %v: %v", origin.Short(), err)
	}
	if tx.AccountNonce != nonce {
		return fmt.Errorf("%w: expected: %d, actual: %d", errBadNonce, nonce, tx.AccountNonce)
	}
	if (tx.Amount + tx.GetFee()) > balance { // TODO: Fee represents the absolute fee here, as a temporarily hack
		return fmt.Errorf("%w: available: %d, want to spend: %d[amount]+%d[fee]=%d",
			errInsufficientBalance, balance, tx.Amount, tx.GetFee(), tx.Amount+tx.GetFee())
	}
	return nil
}

// Invalidate removes transaction from pool.
func (cs *ConservativeState) Invalidate(id types.TransactionID) {
	cs.pool.Invalidate(id)
}

// AddTxToMempool adds the provided transaction to the mempool after checking nonce and balance.
func (cs *ConservativeState) AddTxToMempool(tx *types.Transaction, checkValidity bool) error {
	if checkValidity {
		if err := cs.validateNonceAndBalance(tx); err != nil {
			return err
		}
	}
	cs.pool.Put(tx.ID(), tx)
	return nil
}

// Get returns transaction with the given id or an error if the id is not found.
func (cs *ConservativeState) Get(id types.TransactionID) (*types.Transaction, error) {
	return cs.pool.Get(id)
}
