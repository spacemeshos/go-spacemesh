package txs

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
	txtypes "github.com/spacemeshos/go-spacemesh/txs/types"
)

const (
	maxTXsPerAcct  = 100
	maxTXsPerNonce = 100
)

var (
	errBadNonce            = errors.New("bad nonce")
	errNonceTooBig         = errors.New("nonce too big")
	errInsufficientBalance = errors.New("insufficient balance")
	errTooManyNonce        = errors.New("account has too many nonce pending")
	errLayerNotInOrder     = errors.New("layers not applied in order")
)

// a candidate for the mempool.
type candidate struct {
	// this is the best tx among all the txs with the same nonce
	best        *txtypes.NanoTX
	postBalance uint64
}

func (s *candidate) id() types.TransactionID {
	return s.best.ID
}

func (s *candidate) layer() types.LayerID {
	return s.best.Layer
}

func (s *candidate) block() types.BlockID {
	return s.best.Block
}

func (s *candidate) nonce() uint64 {
	return s.best.Nonce.Counter
}

func (s *candidate) maxSpending() uint64 {
	return s.best.MaxSpending()
}

type accountCache struct {
	addr         types.Address
	txsByNonce   *list.List
	startNonce   uint64
	startBalance uint64
	// moreInDB is used to indicate that an account has pending txs in db that need to be
	// reconsidered for mempool after a layer is applied.
	// - there are too many nonces for an account in the mempool. the extra (higher nonce) txs are in db only.
	// - txs deemed insufficient balance by the conservative state, but can be feasible after a layer applied
	//   (that may contain incoming funds for that account)
	// - a better tx arrived (higher fee) and made higher nonce txs infeasible due to insufficient balance
	//   deemed by conservative state.
	moreInDB bool

	cachedTXs map[types.TransactionID]*txtypes.NanoTX // shared with the cache instance
}

func (ac *accountCache) nextNonce() uint64 {
	if ac.txsByNonce.Len() == 0 {
		return ac.startNonce
	}
	return ac.txsByNonce.Back().Value.(*candidate).nonce() + 1
}

func (ac *accountCache) availBalance() uint64 {
	if ac.txsByNonce.Len() == 0 {
		return ac.startBalance
	}
	return ac.txsByNonce.Back().Value.(*candidate).postBalance
}

func (ac *accountCache) precheck(logger log.Log, ntx *txtypes.NanoTX) (*list.Element, *candidate, error) {
	if ac.txsByNonce.Len() >= maxTXsPerAcct {
		ac.moreInDB = true
		return nil, nil, errTooManyNonce
	}
	balance := ac.startBalance
	var prev *list.Element
	for e := ac.txsByNonce.Back(); e != nil; e = e.Prev() {
		cand := e.Value.(*candidate)
		if cand.nonce() > ntx.Nonce.Counter {
			continue
		}
		if cand.nonce() == ntx.Nonce.Counter {
			balance = cand.postBalance + cand.maxSpending()
		} else {
			balance = cand.postBalance
		}
		prev = e
		break
	}
	if balance < ntx.MaxSpending() {
		ac.moreInDB = true
		logger.With().Debug("insufficient balance",
			ntx.ID,
			ntx.Principal,
			log.Uint64("nonce", ntx.Nonce.Counter),
			log.Uint64("cons_balance", balance),
			log.Uint64("cons_spending", ntx.MaxSpending()))
		return nil, nil, errInsufficientBalance
	}
	return prev, &candidate{best: ntx, postBalance: balance - ntx.MaxSpending()}, nil
}

func (ac *accountCache) accept(logger log.Log, ntx *txtypes.NanoTX, blockSeed []byte) error {
	var (
		added, prev *list.Element
		cand        *candidate
		replaced    *txtypes.NanoTX
		err         error
	)
	prev, cand, err = ac.precheck(logger, ntx)
	if err != nil {
		return err
	}

	if prev == nil { // insert at the first position
		added = ac.txsByNonce.PushFront(cand)
	} else if prevCand := prev.Value.(*candidate); prevCand.nonce() < ntx.Nonce.Counter {
		added = ac.txsByNonce.InsertAfter(cand, prev)
	} else { // existing nonce
		if !ntx.Better(prevCand.best, blockSeed) {
			return nil
		}
		added = prev
		replaced = prevCand.best
		delete(ac.cachedTXs, prevCand.best.ID)
		prevCand.best = ntx
		prevCand.postBalance = cand.postBalance
	}
	ac.cachedTXs[ntx.ID] = ntx

	if replaced != nil {
		logger.With().Debug("better transaction replaced for nonce",
			log.Stringer("better", ntx.ID),
			log.Stringer("replaced", replaced.ID),
			log.Uint64("nonce", ntx.Nonce.Counter),
			log.Uint64("max_spending", ntx.MaxSpending()),
			log.Uint64("post_balance", cand.postBalance),
			log.Uint64("avail_balance", ac.availBalance()))
	} else {
		logger.With().Debug("new nonce added",
			ntx.ID,
			log.Uint64("nonce", ntx.Nonce.Counter),
			log.Uint64("max_spending", ntx.MaxSpending()),
			log.Uint64("post_balance", cand.postBalance),
			log.Uint64("avail_balance", ac.availBalance()))
	}

	// propagate the balance change
	next := added.Next()
	newBalance := cand.postBalance
	for next != nil {
		nextCand := next.Value.(*candidate)
		if newBalance >= nextCand.maxSpending() {
			newBalance -= nextCand.maxSpending()
			nextCand.postBalance = newBalance
			next = next.Next()
			logger.With().Debug("updated next balance",
				log.Uint64("nonce", nextCand.nonce()),
				log.Uint64("post_balance", nextCand.postBalance),
				log.Uint64("avail_balance", ac.availBalance()))
			continue
		}
		ac.moreInDB = true
		rm := next
		next = next.Next()
		removed := ac.txsByNonce.Remove(rm).(*candidate)
		delete(ac.cachedTXs, removed.id())
		logger.With().Debug("tx made infeasible by new/better transaction",
			removed.id(),
			log.Uint64("nonce", removed.nonce()),
			log.Uint64("max_spending", ntx.MaxSpending()))
	}
	return nil
}

func nonceMarshaller(any interface{}) log.ArrayMarshaler {
	return log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
		var allNonce []uint64
		noncesList, ok := any.([]uint64)
		if ok {
			allNonce = noncesList
		} else if nonce2ID, ok := any.(map[uint64]types.TransactionID); ok {
			allNonce = make([]uint64, 0, len(nonce2ID))
			for nonce := range nonce2ID {
				allNonce = append(allNonce, nonce)
			}
		} else if nonce2TXs, ok := any.(map[uint64][]*txtypes.NanoTX); ok {
			allNonce = make([]uint64, 0, len(nonce2TXs))
			for nonce := range nonce2TXs {
				allNonce = append(allNonce, nonce)
			}
		}
		sort.Slice(allNonce, func(i, j int) bool { return allNonce[i] < allNonce[j] })
		for _, nonce := range allNonce {
			encoder.AppendUint64(nonce)
		}
		return nil
	})
}

func (ac *accountCache) addBatch(logger log.Log, nonce2TXs map[uint64][]*txtypes.NanoTX, blockSeed []byte) error {
	logger.With().Debug("account has pending txs", log.Int("num_pending", len(nonce2TXs)))
	var (
		nextNonce   = ac.nextNonce()
		balance     = ac.availBalance()
		sortedNonce = make([]uint64, 0, len(nonce2TXs))
		added       = make([]uint64, 0, len(nonce2TXs))
	)
	for nonce := range nonce2TXs {
		if nonce < nextNonce {
			continue
		}
		sortedNonce = append(sortedNonce, nonce)
	}
	sort.Slice(sortedNonce, func(i, j int) bool { return sortedNonce[i] < sortedNonce[j] })
	for _, nonce := range sortedNonce {
		best := findBest(nonce2TXs[nonce], balance, blockSeed)
		if best == nil {
			logger.With().Warning("no feasible transactions at nonce",
				log.Uint64("nonce", nonce),
				log.Uint64("balance", balance))
			continue
		}

		logger.With().Debug("found best in nonce txs",
			best.ID,
			log.Uint64("nonce", nonce),
			log.Uint64("fee", best.Fee()))

		if err := ac.accept(logger, best, blockSeed); err != nil {
			if errors.Is(err, errTooManyNonce) {
				break
			}
			continue
		}
		added = append(added, nonce)
		balance = ac.availBalance()
	}

	ac.moreInDB = len(sortedNonce) > len(added)
	if len(added) > 0 {
		logger.With().Debug("added batch to account pool", log.Array("batch", nonceMarshaller(added)))
	} else {
		logger.With().Debug("no feasible txs from batch", log.Array("batch", nonceMarshaller(nonce2TXs)))
	}
	return nil
}

func findBest(ntxs []*txtypes.NanoTX, balance uint64, blockSeed []byte) *txtypes.NanoTX {
	var best *txtypes.NanoTX
	for _, ntx := range ntxs {
		if balance >= ntx.MaxSpending() &&
			(best == nil || ntx.Better(best, blockSeed)) {
			best = ntx
		}
	}
	return best
}

// adding a tx to the account cache. possible outcomes:
//   - nonce is smaller than the next nonce in state: reject from cache
//   - too many txs present: reject from cache
//   - nonce already exists in the cache:
//     if it is better than the best candidate in that nonce group, swap
//   - nonce not present: add to cache.
func (ac *accountCache) add(logger log.Log, tx *types.Transaction, received time.Time) error {
	if tx.Nonce.Counter < ac.startNonce {
		logger.With().Warning("nonce too small",
			tx.ID,
			log.Uint64("next_nonce", ac.startNonce),
			log.Uint64("tx_nonce", tx.Nonce.Counter))
		return errBadNonce
	}

	ntx := txtypes.NewNanoTX(&types.MeshTransaction{
		Transaction: *tx,
		Received:    received,
		LayerID:     types.LayerID{},
		BlockID:     types.EmptyBlockID,
	})

	err := ac.accept(logger, ntx, nil)
	if err != nil {
		if errors.Is(err, errTooManyNonce) {
			mempoolTxCount.WithLabelValues(tooManyNonce).Inc()
		} else if errors.Is(err, errInsufficientBalance) {
			mempoolTxCount.WithLabelValues(balanceTooSmall).Inc()
		}
		return err
	}
	mempoolTxCount.WithLabelValues(mempool).Inc()
	return nil
}

func (ac *accountCache) addPendingFromNonce(logger log.Log, db *sql.Database, nonce uint64, applied types.LayerID) error {
	mtxs, err := transactions.GetAcctPendingFromNonce(db, ac.addr, nonce)
	if err != nil {
		logger.With().Error("failed to get more pending txs from db", log.Err(err))
		return err
	}

	if len(mtxs) == 0 {
		ac.moreInDB = false
		return nil
	}

	if applied != (types.LayerID{}) {
		// we just applied a layer, need to update layer/block for the pending txs
		for i, mtx := range mtxs {
			nextLayer, nextBlock, err := transactions.SetNextLayer(db, mtx.ID, applied)
			if err != nil {
				logger.With().Error("failed to reset layer",
					mtx.ID,
					log.Uint64("nonce", nonce),
					log.Stringer("applied", applied))
				return err
			}
			mtxs[i].LayerID = nextLayer
			mtxs[i].BlockID = nextBlock
			if nextLayer != (types.LayerID{}) {
				logger.With().Debug("next layer found", mtx.ID, nextLayer)
			}
		}
	}

	byPrincipal := groupTXsByPrincipal(logger, mtxs)
	if _, ok := byPrincipal[ac.addr]; !ok {
		logger.Panic("no txs for account after grouping")
	}
	return ac.addBatch(logger, byPrincipal[ac.addr], nil)
}

// find the first nonce without a layer.
// a nonce with a valid layer indicates that it's already packed in a proposal/block.
func (ac *accountCache) getMempool(logger log.Log) []*txtypes.NanoTX {
	bests := make([]*txtypes.NanoTX, 0, maxTXsPerAcct)
	offset := 0
	found := false
	for e := ac.txsByNonce.Front(); e != nil; e = e.Next() {
		cand := e.Value.(*candidate)
		if !found && cand.layer() == (types.LayerID{}) {
			found = true
		} else if found && cand.layer() != (types.LayerID{}) {
			logger.With().Warning("some proposals/blocks packed txs out of order",
				cand.id(),
				cand.layer(),
				cand.block(),
				log.Uint64("nonce", cand.nonce()))
		}
		if found {
			bests = append(bests, cand.best)
		} else {
			offset++
		}
	}
	if len(bests) > 0 {
		logger.With().Debug("account in mempool",
			log.Int("offset", offset),
			log.Int("size", ac.txsByNonce.Len()),
			log.Int("added", len(bests)),
			log.Uint64("from", bests[0].Nonce.Counter),
			log.Uint64("to", bests[len(bests)-1].Nonce.Counter))
	} else {
		logger.With().Debug("account has no txs for mempool",
			log.Int("offset", offset),
			log.Int("size", ac.txsByNonce.Len()))
	}
	return bests
}

func (ac *accountCache) applyLayer(logger log.Log, db *sql.Database, applied []types.TransactionWithResult) error {
	logger = logger.WithFields(ac.addr)
	if err := db.WithTx(context.Background(), func(dbtx *sql.Tx) error {
		// nonce order doesn't matter here
		for _, tx := range applied {
			err := transactions.AddResult(dbtx, tx.ID, &tx.TransactionResult)
			if err != nil {
				logger.With().Error("failed to add result",
					tx.ID,
					log.Uint64("nonce", tx.Nonce.Counter),
					log.Err(err))
				return fmt.Errorf("apply %w", err)
			}
			if err = transactions.DiscardByAcctNonce(dbtx, tx.ID, tx.Layer, tx.Principal, tx.Nonce.Counter); err != nil {
				logger.With().Error("failed to discard at nonce",
					tx.ID,
					log.Uint64("nonce", tx.Nonce.Counter),
					log.Err(err))
				return fmt.Errorf("apply discard %w", err)
			}
		}
		// txs that were rejected from cache due to nonce too low are discarded here
		if err := transactions.DiscardNonceBelow(dbtx, ac.addr, ac.startNonce); err != nil {
			logger.With().Error("failed to discard txs with lower nonce",
				log.Uint64("nonce", ac.startNonce))
			return err
		}
		return nil
	}); err != nil {
		logger.With().Error("failed to apply layer", log.Err(err))
		return err
	}
	return nil
}

// NOTE: this is the only point in time when we reconsider those previously rejected txs,
// because applying a layer changes the conservative balance in the cache.
func (ac *accountCache) resetAfterApply(logger log.Log, db *sql.Database, nextNonce, newBalance uint64, applied types.LayerID) error {
	logger = logger.WithFields(ac.addr)
	logger.With().Debug("resetting to nonce", log.Uint64("nonce", nextNonce))
	for e := ac.txsByNonce.Front(); e != nil; e = e.Next() {
		delete(ac.cachedTXs, e.Value.(*candidate).id())
	}
	ac.txsByNonce = list.New()
	ac.startNonce = nextNonce
	ac.startBalance = newBalance
	return ac.addPendingFromNonce(logger, db, ac.startNonce, applied)
}

func (ac *accountCache) shouldEvict() bool {
	return ac.txsByNonce.Len() == 0 && !ac.moreInDB
}

type stateFunc func(types.Address) (uint64, uint64)

type cache struct {
	logger log.Log
	stateF stateFunc

	mu        sync.Mutex
	pending   map[types.Address]*accountCache
	cachedTXs map[types.TransactionID]*txtypes.NanoTX // shared with accountCache instances
}

func newCache(s stateFunc, logger log.Log) *cache {
	return &cache{
		logger:    logger,
		stateF:    s,
		pending:   make(map[types.Address]*accountCache),
		cachedTXs: make(map[types.TransactionID]*txtypes.NanoTX),
	}
}

func groupTXsByPrincipal(logger log.Log, mtxs []*types.MeshTransaction) map[types.Address]map[uint64][]*txtypes.NanoTX {
	byPrincipal := make(map[types.Address]map[uint64][]*txtypes.NanoTX)
	for _, mtx := range mtxs {
		principal := mtx.Principal
		if _, ok := byPrincipal[principal]; !ok {
			byPrincipal[principal] = make(map[uint64][]*txtypes.NanoTX)
		}
		if _, ok := byPrincipal[principal][mtx.Nonce.Counter]; !ok {
			byPrincipal[principal][mtx.Nonce.Counter] = make([]*txtypes.NanoTX, 0, maxTXsPerNonce)
		}
		if len(byPrincipal[principal][mtx.Nonce.Counter]) < maxTXsPerNonce {
			byPrincipal[principal][mtx.Nonce.Counter] = append(byPrincipal[principal][mtx.Nonce.Counter], txtypes.NewNanoTX(mtx))
		} else {
			logger.With().Warning("too many txs in same nonce. ignoring tx",
				mtx.ID,
				principal,
				log.Uint64("nonce", mtx.Nonce.Counter),
				log.Uint64("fee", mtx.Fee()))
		}
	}
	return byPrincipal
}

// buildFromScratch builds the cache from database.
func (c *cache) buildFromScratch(db *sql.Database) error {
	mtxs, err := transactions.GetAllPending(db)
	if err != nil {
		c.logger.Error("failed to get all pending txs", log.Err(err))
		return err
	}
	return c.BuildFromTXs(mtxs, nil)
}

// BuildFromTXs builds the cache from the provided transactions.
func (c *cache) BuildFromTXs(mtxs []*types.MeshTransaction, blockSeed []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pending = make(map[types.Address]*accountCache)
	toCleanup := make(map[types.Address]struct{})
	for _, tx := range mtxs {
		toCleanup[tx.Principal] = struct{}{}
	}
	defer c.cleanupAccounts(toCleanup)

	byPrincipal := groupTXsByPrincipal(c.logger, mtxs)
	acctsAdded := 0
	for principal, nonce2TXs := range byPrincipal {
		c.createAcctIfNotPresent(principal)
		if err := c.pending[principal].addBatch(c.logger, nonce2TXs, blockSeed); err != nil {
			return err
		}
		if c.pending[principal].shouldEvict() {
			c.logger.With().Warning("account has pending txs but none feasible",
				principal,
				log.Array("batch", nonceMarshaller(nonce2TXs)))
		} else {
			acctsAdded++
		}
	}
	c.logger.Info("added pending tx for %d accounts", acctsAdded)
	return nil
}

func (c *cache) createAcctIfNotPresent(addr types.Address) {
	if _, ok := c.pending[addr]; !ok {
		nextNonce, balance := c.stateF(addr)
		c.logger.With().Debug("created account with nonce/balance",
			addr,
			log.Uint64("nonce", nextNonce),
			log.Uint64("balance", balance))
		c.pending[addr] = &accountCache{
			addr:         addr,
			startNonce:   nextNonce,
			startBalance: balance,
			txsByNonce:   list.New(),
			cachedTXs:    c.cachedTXs,
		}
	}
}

func (c *cache) MoreInDB(addr types.Address) bool {
	acct, ok := c.pending[addr]
	if !ok {
		return false
	}
	return acct.moreInDB
}

func (c *cache) cleanupAccounts(accounts map[types.Address]struct{}) {
	for addr := range accounts {
		if _, ok := c.pending[addr]; ok && c.pending[addr].shouldEvict() {
			delete(c.pending, addr)
		}
	}
}

//   - errInsufficientBalance:
//     conservative cache is conservative in that it only counts principal's spending for pending transactions.
//     a tx rejected due to insufficient balance MAY become feasible after a layer is applied (principal
//     received incoming funds). when we receive a errInsufficientBalance tx, we should store it in db and
//     re-evaluate it after each layer is applied.
//   - errNonceTooBig: transactions are gossiped/synced out of nonce order. when we receive a errNonceTooBig tx,
//     we should store it in db and retrieve it when the nonce gap is filled in the cache.
//   - errTooManyNonce: when a principal has way too many nonces, we don't want to blow up the memory. they should
//     be stored in db and retrieved after each earlier nonce is applied.
func acceptable(err error) bool {
	return err == nil || errors.Is(err, errInsufficientBalance) || errors.Is(err, errNonceTooBig) || errors.Is(err, errTooManyNonce)
}

func (c *cache) Add(ctx context.Context, db *sql.Database, tx *types.Transaction, received time.Time, mustPersist bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	principal := tx.Principal
	c.createAcctIfNotPresent(principal)
	defer c.cleanupAccounts(map[types.Address]struct{}{principal: {}})
	logger := c.logger.WithContext(ctx).WithFields(principal)
	err := c.pending[principal].add(logger, tx, received)
	if acceptable(err) {
		err = nil
		mempoolTxCount.WithLabelValues(accepted).Inc()
	}
	if err == nil || mustPersist {
		if dbErr := transactions.Add(db, tx, received); dbErr != nil {
			return dbErr
		}
	}
	return err
}

// Get gets a transaction from the cache.
func (c *cache) Get(tid types.TransactionID) *txtypes.NanoTX {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cachedTXs[tid]
}

// Has returns true if transaction exists in the cache.
func (c *cache) Has(tid types.TransactionID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.has(tid)
}

func (c *cache) has(tid types.TransactionID) bool {
	return c.cachedTXs[tid] != nil
}

// LinkTXsWithProposal associates the transactions to a proposal.
func (c *cache) LinkTXsWithProposal(db *sql.Database, lid types.LayerID, pid types.ProposalID, tids []types.TransactionID) error {
	if len(tids) == 0 {
		return nil
	}
	if err := addToProposal(db, lid, pid, tids); err != nil {
		c.logger.With().Error("failed to link txs to proposal in db", log.Err(err))
		return err
	}
	return c.updateLayer(lid, types.EmptyBlockID, tids)
}

// LinkTXsWithBlock associates the transactions to a block.
func (c *cache) LinkTXsWithBlock(db *sql.Database, lid types.LayerID, bid types.BlockID, tids []types.TransactionID) error {
	if len(tids) == 0 {
		return nil
	}
	if err := addToBlock(db, lid, bid, tids); err != nil {
		return err
	}
	return c.updateLayer(lid, bid, tids)
}

// updateLayer associates the transactions to a layer and optionally a block.
// A transaction is tagged with a layer when it's included in a proposal/block.
// If a transaction is included in multiple proposals/blocks in different layers,
// the lowest layer is retained.
func (c *cache) updateLayer(lid types.LayerID, bid types.BlockID, tids []types.TransactionID) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, ID := range tids {
		if _, ok := c.cachedTXs[ID]; !ok {
			// transaction is not considered best in its nonce group
			return nil
		}
		c.cachedTXs[ID].UpdateLayerMaybe(lid, bid)
	}
	return nil
}

// ApplyLayer retires the applied transactions from the cache and updates the balances.
func (c *cache) ApplyLayer(
	ctx context.Context,
	db *sql.Database,
	lid types.LayerID,
	bid types.BlockID,
	results []types.TransactionWithResult,
	ineffective []types.Transaction,
) error {
	logger := c.logger.WithContext(ctx).WithFields(lid, bid)
	if err := checkApplyOrder(logger, db, lid); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	toCleanup := make(map[types.Address]struct{})
	toReset := make(map[types.Address]struct{})
	byPrincipal := make(map[types.Address][]types.TransactionWithResult)
	for _, rst := range results {
		toCleanup[rst.Principal] = struct{}{}
		if !c.has(rst.ID) {
			rawTxCount.WithLabelValues(updated).Inc()
			if err := transactions.Add(db, &rst.Transaction, time.Now()); err != nil {
				return err
			}
		}

		principal := rst.Principal
		if _, ok := byPrincipal[principal]; !ok {
			byPrincipal[principal] = make([]types.TransactionWithResult, 0, maxTXsPerAcct)
		}
		byPrincipal[principal] = append(byPrincipal[principal], rst)
		events.ReportResult(rst)
	}

	for _, tx := range ineffective {
		if tx.TxHeader == nil {
			logger.With().Warning("tx header not parsed", tx.ID)
			continue
		}
		if !c.has(tx.ID) {
			rawTxCount.WithLabelValues(updated).Inc()
			if err := transactions.Add(db, &tx, time.Now()); err != nil {
				return err
			}
		}

		toCleanup[tx.Principal] = struct{}{}
		if _, ok := byPrincipal[tx.Principal]; ok {
			continue
		}
		if _, ok := c.pending[tx.Principal]; !ok {
			continue
		}
		toReset[tx.Principal] = struct{}{}
	}
	defer c.cleanupAccounts(toCleanup)

	for principal, applied := range byPrincipal {
		c.createAcctIfNotPresent(principal)
		nextNonce, balance := c.stateF(principal)
		logger.With().Debug("new account nonce/balance",
			principal,
			log.Uint64("nonce", nextNonce),
			log.Uint64("balance", balance))
		t0 := time.Now()
		if err := c.pending[principal].applyLayer(logger, db, applied); err != nil {
			logger.With().Error("failed to apply layer to principal", principal, log.Err(err))
			return err
		}
		acctApplyDuration.Observe(float64(time.Since(t0)))
		t1 := time.Now()
		if err := c.pending[principal].resetAfterApply(logger, db, nextNonce, balance, lid); err != nil {
			logger.With().Error("failed to reset cache for principal", principal, log.Err(err))
			return err
		}
		acctResetDuration.Observe(float64(time.Since(t1)))
	}

	for principal, cash := range c.pending {
		if _, ok := toCleanup[principal]; ok {
			continue
		}
		if cash.moreInDB {
			toReset[principal] = struct{}{}
		}
	}
	for principal := range toReset {
		nextNonce, balance := c.stateF(principal)
		t2 := time.Now()
		if err := c.pending[principal].resetAfterApply(logger, db, nextNonce, balance, lid); err != nil {
			logger.With().Error("failed to reset cache for principal", principal, log.Err(err))
			return err
		}
		acctResetDuration.Observe(float64(time.Since(t2)))
	}
	return nil
}

func (c *cache) RevertToLayer(db *sql.Database, revertTo types.LayerID) error {
	if err := undoLayers(db, revertTo.Add(1)); err != nil {
		return err
	}

	if err := c.buildFromScratch(db); err != nil {
		c.logger.With().Error("failed to build from scratch after revert", log.Err(err))
		return err
	}
	return nil
}

// GetProjection returns the projected nonce and balance for an account, including
// pending transactions that are paced in proposals/blocks but not yet applied to the state.
func (c *cache) GetProjection(addr types.Address) (uint64, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.pending[addr]; !ok {
		return c.stateF(addr)
	}
	return c.pending[addr].nextNonce(), c.pending[addr].availBalance()
}

// GetMempool returns all the transactions that eligible for a proposal/block.
func (c *cache) GetMempool(logger log.Log) map[types.Address][]*txtypes.NanoTX {
	c.mu.Lock()
	defer c.mu.Unlock()

	all := make(map[types.Address][]*txtypes.NanoTX)
	logger.With().Info("cache has pending accounts", log.Int("num_acct", len(c.pending)))
	for addr, accCache := range c.pending {
		txs := accCache.getMempool(logger.WithFields(addr))
		if len(txs) > 0 {
			all[addr] = txs
		}
	}
	return all
}

// checkApplyOrder returns an error if layers were not applied in order.
func checkApplyOrder(logger log.Log, db *sql.Database, toApply types.LayerID) error {
	lastApplied, err := layers.GetLastApplied(db)
	if err != nil {
		logger.With().Error("failed to get last applied layer", log.Err(err))
		return fmt.Errorf("cache get last applied %w", err)
	}
	if toApply != lastApplied.Add(1) {
		logger.With().Error("layer not applied in order",
			log.Stringer("expected", lastApplied.Add(1)),
			log.Stringer("to_apply", toApply))
		return errLayerNotInOrder
	}
	return nil
}

func addToProposal(db *sql.Database, lid types.LayerID, pid types.ProposalID, tids []types.TransactionID) error {
	return db.WithTx(context.Background(), func(dbtx *sql.Tx) error {
		for _, tid := range tids {
			if err := transactions.AddToProposal(dbtx, tid, lid, pid); err != nil {
				return fmt.Errorf("add2prop %w", err)
			}
			_, err := transactions.UpdateIfBetter(dbtx, tid, lid, types.EmptyBlockID)
			if err != nil {
				return fmt.Errorf("add2prop update %w", err)
			}
		}
		return nil
	})
}

func addToBlock(db *sql.Database, lid types.LayerID, bid types.BlockID, tids []types.TransactionID) error {
	return db.WithTx(context.Background(), func(dbtx *sql.Tx) error {
		for _, tid := range tids {
			if err := transactions.AddToBlock(dbtx, tid, lid, bid); err != nil {
				return fmt.Errorf("add2block %w", err)
			}
			_, err := transactions.UpdateIfBetter(dbtx, tid, lid, bid)
			if err != nil {
				return fmt.Errorf("add2block update %w", err)
			}
		}
		return nil
	})
}

func undoLayers(db *sql.Database, from types.LayerID) error {
	return db.WithTx(context.Background(), func(dbtx *sql.Tx) error {
		tids, err := transactions.UndoLayers(dbtx, from)
		if err != nil {
			return fmt.Errorf("undo %w", err)
		}
		for _, tid := range tids {
			if _, _, err = transactions.SetNextLayer(dbtx, tid, from.Sub(1)); err != nil {
				return fmt.Errorf("reset for undo %w", err)
			}
		}
		return nil
	})
}
