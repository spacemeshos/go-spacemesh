package txs

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	"github.com/spacemeshos/go-spacemesh/system"
)

// CSConfig is the config for the conservative state/cache.
type CSConfig struct {
	BlockGasLimit     uint64
	NumTXsPerProposal int
}

func defaultCSConfig() CSConfig {
	return CSConfig{
		BlockGasLimit:     math.MaxUint64,
		NumTXsPerProposal: 100,
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
func WithLogger(logger *zap.Logger) ConservativeStateOpt {
	return func(cs *ConservativeState) {
		cs.logger = logger
	}
}

// ConservativeState provides the conservative version of the VM state by taking into accounts of
// nonce and balances for pending transactions in un-applied blocks and mempool.
type ConservativeState struct {
	vmState

	logger *zap.Logger
	cfg    CSConfig
	db     *sql.Database
	cache  *Cache
}

// NewConservativeState returns a ConservativeState.
func NewConservativeState(state vmState, db *sql.Database, opts ...ConservativeStateOpt) *ConservativeState {
	cs := &ConservativeState{
		vmState: state,
		cfg:     defaultCSConfig(),
		logger:  zap.NewNop(),
		db:      db,
	}
	for _, opt := range opts {
		opt(cs)
	}
	cs.cache = NewCache(cs.getState, cs.logger)
	return cs
}

func (cs *ConservativeState) getState(addr types.Address) (uint64, uint64) {
	nonce, err := cs.vmState.GetNonce(addr)
	if err != nil {
		cs.logger.Fatal("failed to get nonce", zap.Error(err))
	}
	balance, err := cs.vmState.GetBalance(addr)
	if err != nil {
		cs.logger.Fatal("failed to get balance", zap.Error(err))
	}
	return nonce, balance
}

// SelectProposalTXs picks a specific number of random txs for miner to pack in a proposal.
func (cs *ConservativeState) SelectProposalTXs(lid types.LayerID, numEligibility int) []types.TransactionID {
	logger := cs.logger.With(zap.Uint32("layer_id", lid.Uint32()))
	mi := newMempoolIterator(logger, cs.cache, cs.cfg.BlockGasLimit)
	predictedBlock, byAddrAndNonce := mi.PopAll()
	numTXs := numEligibility * cs.cfg.NumTXsPerProposal
	return getProposalTXs(logger, numTXs, predictedBlock, byAddrAndNonce)
}

func getProposalTXs(
	logger *zap.Logger,
	numTXs int,
	predictedBlock []*NanoTX,
	byAddrAndNonce map[types.Address][]*NanoTX,
) []types.TransactionID {
	if len(predictedBlock) <= numTXs {
		result := make([]types.TransactionID, 0, len(predictedBlock))
		for _, ntx := range predictedBlock {
			result = append(result, ntx.ID)
		}
		return result
	}
	// randomly select transactions from the predicted block.
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	return ShuffleWithNonceOrder(logger, rng, numTXs, predictedBlock, byAddrAndNonce)
}

// Validation initializes validation request.
func (cs *ConservativeState) Validation(raw types.RawTx) system.ValidationRequest {
	return cs.vmState.Validation(raw)
}

// AddToCache adds the provided transaction to the conservative cache.
func (cs *ConservativeState) AddToCache(ctx context.Context, tx *types.Transaction, received time.Time) error {
	if err := cs.cache.Add(ctx, cs.db, tx, received, false); err != nil {
		return err
	}
	events.ReportNewTx(0, tx)
	events.ReportAccountUpdate(tx.Principal)
	return nil
}

// RevertCache reverts the conservative cache to the given layer.
func (cs *ConservativeState) RevertCache(revertTo types.LayerID) error {
	return cs.cache.RevertToLayer(cs.db, revertTo)
}

func (cs *ConservativeState) UpdateCache(
	ctx context.Context,
	lid types.LayerID,
	bid types.BlockID,
	results []types.TransactionWithResult,
	ineffective []types.Transaction,
) error {
	t0 := time.Now()
	if err := cs.cache.ApplyLayer(ctx, cs.db, lid, bid, results, ineffective); err != nil {
		return err
	}
	cacheApplyDuration.Observe(float64(time.Since(t0)))
	return nil
}

// GetProjection returns the projected nonce and balance for an account, including
// pending transactions that are paced in proposals/blocks but not yet applied to the state.
func (cs *ConservativeState) GetProjection(addr types.Address) (uint64, uint64) {
	return cs.cache.GetProjection(addr)
}

// LinkTXsWithProposal associates the transactions to a proposal.
func (cs *ConservativeState) LinkTXsWithProposal(
	lid types.LayerID,
	pid types.ProposalID,
	tids []types.TransactionID,
) error {
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

// HasTx returns true if transaction exists in the database.
func (cs *ConservativeState) HasTx(tid types.TransactionID) (bool, error) {
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
func (cs *ConservativeState) GetMeshTransactions(
	ids []types.TransactionID,
) ([]*types.MeshTransaction, map[types.TransactionID]struct{}) {
	missing := make(map[types.TransactionID]struct{})
	mtxs := make([]*types.MeshTransaction, 0, len(ids))
	for _, tid := range ids {
		var (
			mtx *types.MeshTransaction
			err error
		)
		if mtx, err = transactions.Get(cs.db, tid); err != nil {
			cs.logger.Warn("could not get tx", zap.Stringer("tx_id", tid), zap.Error(err))
			missing[tid] = struct{}{}
		} else {
			mtxs = append(mtxs, mtx)
		}
	}
	return mtxs, missing
}

// GetTransactionsByAddress retrieves txs for a single address in between layers [from, to].
// Guarantees that transaction will appear exactly once, even if origin and recipient is the same,
// and in insertion order.
func (cs *ConservativeState) GetTransactionsByAddress(
	from, to types.LayerID,
	address types.Address,
) ([]*types.MeshTransaction, error) {
	return transactions.GetByAddress(cs.db, from, to, address)
}

// ShuffleWithNonceOrder perform a Fisher-Yates shuffle on the transactions.
// note that after shuffling, the original list of transactions are no longer in nonce order
// within the same principal. we simply check which principal occupies the spot after
// the shuffle and retrieve their transactions in nonce order.
func ShuffleWithNonceOrder(
	logger *zap.Logger,
	rng *rand.Rand,
	numTXs int,
	ntxs []*NanoTX,
	byAddrAndNonce map[types.Address][]*NanoTX,
) []types.TransactionID {
	rng.Shuffle(len(ntxs), func(i, j int) { ntxs[i], ntxs[j] = ntxs[j], ntxs[i] })
	total := min(len(ntxs), numTXs)
	result := make([]types.TransactionID, 0, total)
	packed := make(map[types.Address][]uint64)
	for _, ntx := range ntxs[:total] {
		// if a spot is taken by a principal, we add its TX for the next eligible nonce
		p := ntx.Principal
		if _, ok := byAddrAndNonce[p]; !ok {
			logger.Fatal("principal missing", zap.Stringer("address", p))
		}
		if len(byAddrAndNonce[p]) == 0 {
			logger.Fatal("txs missing", zap.Stringer("address", p))
		}
		toAdd := byAddrAndNonce[p][0]
		result = append(result, toAdd.ID)
		if _, ok := packed[p]; !ok {
			packed[p] = []uint64{toAdd.Nonce, toAdd.Nonce}
		} else {
			packed[p][1] = toAdd.Nonce
		}
		if len(byAddrAndNonce[p]) == 1 {
			delete(byAddrAndNonce, p)
		} else {
			byAddrAndNonce[p] = byAddrAndNonce[p][1:]
		}
	}
	logger.Debug("packed txs", zap.Array("ranges", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
		for addr, nonces := range packed {
			_ = encoder.AppendObject(zapcore.ObjectMarshalerFunc(func(encoder zapcore.ObjectEncoder) error {
				encoder.AddString("addr", addr.String())
				encoder.AddUint64("from", nonces[0])
				encoder.AddUint64("to", nonces[1])
				return nil
			}))
		}
		return nil
	})))
	return result
}
