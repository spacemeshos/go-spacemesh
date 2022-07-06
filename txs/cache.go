package txs

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/address"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
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
	errNonceNotInOrder     = errors.New("nonce not applied in order")
	errBadNonceInCache     = errors.New("cache contains incorrect nonce")
	errBadBalanceInCache   = errors.New("cache contains incorrect balance")
	errDupNonceApplied     = errors.New("multiple txs applied for the same nonce")
	errLayerNotInOrder     = errors.New("layers not applied in order")
)

type sameNonceTXs struct {
	best        *txtypes.NanoTX
	postBalance uint64
}

func (s *sameNonceTXs) id() types.TransactionID {
	return s.best.ID
}

func (s *sameNonceTXs) layer() types.LayerID {
	return s.best.Layer
}

func (s *sameNonceTXs) block() types.BlockID {
	return s.best.Block
}

func (s *sameNonceTXs) nonce() uint64 {
	return s.best.Nonce.Counter
}

func (s *sameNonceTXs) maxSpending() uint64 {
	return s.best.MaxSpending()
}

type accountCache struct {
	addr         address.Address
	txsByNonce   []*sameNonceTXs
	startNonce   uint64
	startBalance uint64
	// moreInDB is used to indicate that an account has pending transactions in db even though none
	// exists in cache. it is set in two scenarios:
	// - account has pending transactions at next nonce but none are feasible due to insufficient balance
	// - account has nonce gap in its transactions (transactions gossipped/synced out of nonce order)
	// note that if either these scenarios happen and stay unchanged could cause the account to stay
	// in cache forever until the node restarts. this could be a slow memory leak.
	// TTL with sub-nonce should make this problem obsolete.
	moreInDB bool

	cachedTXs map[types.TransactionID]*txtypes.NanoTX // shared with the cache instance
}

func (ac *accountCache) nextNonce() uint64 {
	return ac.startNonce + uint64(len(ac.txsByNonce))
}

func (ac *accountCache) availBalance() uint64 {
	if len(ac.txsByNonce) == 0 {
		return ac.startBalance
	}
	return ac.txsByNonce[len(ac.txsByNonce)-1].postBalance
}

func (ac *accountCache) accept(logger log.Log, ntx *txtypes.NanoTX, balance uint64, blockSeed []byte) error {
	idx := getNonceOffset(ac.startNonce, ntx.Nonce.Counter)
	if idx < 0 {
		logger.With().Error("bad nonce",
			ac.addr,
			log.Uint64("acct_nonce", ac.startNonce),
			log.Uint64("tx_nonce", ntx.Nonce.Counter))
		return errBadNonce
	}

	if balance < ntx.MaxSpending() {
		ac.moreInDB = idx == len(ac.txsByNonce)
		logger.With().Debug("insufficient balance",
			ac.addr,
			ntx.ID,
			ntx.Principal,
			log.Uint64("nonce", ntx.Nonce.Counter),
			log.Uint64("cons_balance", balance),
			log.Uint64("cons_spending", ntx.MaxSpending()))
		return errInsufficientBalance
	}

	if idx == len(ac.txsByNonce) { // new nonce
		if idx == maxTXsPerAcct {
			logger.With().Warning("account reach nonce limit in cache", ac.addr)
			return errTooManyNonce
		}
		ac.txsByNonce = append(ac.txsByNonce, &sameNonceTXs{
			best:        ntx,
			postBalance: balance - ntx.MaxSpending(),
		})
		ac.cachedTXs[ntx.ID] = ntx
		logger.With().Debug("new nonce added",
			ac.addr,
			log.Uint64("nonce", ntx.Nonce.Counter),
			log.Uint64("max_spending", ntx.MaxSpending()),
			log.Uint64("post_balance", ac.availBalance()))
		return nil
	}

	// tx for an existing nonce
	nonceTXs := ac.txsByNonce[idx]
	if !ntx.Better(nonceTXs.best, blockSeed) {
		return nil
	}

	logger.With().Debug("better transaction replaced for nonce",
		ac.addr,
		log.Stringer("better", ntx.ID),
		log.Stringer("replaced", nonceTXs.best.ID),
		log.Uint64("nonce", ntx.Nonce.Counter))
	delete(ac.cachedTXs, nonceTXs.best.ID)
	ac.cachedTXs[ntx.ID] = ntx
	nonceTXs.best = ntx
	nonceTXs.postBalance = balance - nonceTXs.maxSpending()

	// propagate the balance change
	newBalance := nonceTXs.postBalance
	toRemove := len(ac.txsByNonce)
	for i := idx + 1; i < len(ac.txsByNonce); i++ {
		if newBalance < ac.txsByNonce[i].maxSpending() {
			toRemove = i
			break
		}
		newBalance -= ac.txsByNonce[i].maxSpending()
		ac.txsByNonce[i].postBalance = newBalance
	}
	if toRemove < len(ac.txsByNonce) {
		ac.moreInDB = true
		logger.With().Debug("nonce made infeasible by new better transaction",
			ac.addr,
			log.Uint64("from_nonce", ac.startNonce+uint64(toRemove)))
		ac.removeFromOffset(toRemove)
	}
	return nil
}

func nonceMarshaller(any interface{}) log.ArrayMarshaler {
	return log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
		var allNonce []uint64
		nonce2ID, ok := any.(map[uint64]types.TransactionID)
		if ok {
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
	var (
		oldNonce  = ac.nextNonce()
		nextNonce = oldNonce
		balance   = ac.availBalance()
	)
	for len(nonce2TXs) > 0 {
		if _, ok := nonce2TXs[nextNonce]; !ok {
			logger.With().Debug("batch does not contain the next nonce",
				ac.addr,
				log.Uint64("nonce", nextNonce),
				log.Array("batch", nonceMarshaller(nonce2TXs)))
			break
		}

		best := findBest(nonce2TXs[nextNonce], balance, blockSeed)
		if best == nil {
			logger.With().Warning("no feasible transactions at nonce",
				ac.addr,
				log.Uint64("nonce", nextNonce),
				log.Uint64("balance", balance))
			break
		} else {
			logger.With().Debug("found best in nonce txs",
				ac.addr,
				best.ID,
				log.Uint64("nonce", nextNonce),
				log.Uint64("fee", best.Fee()))
		}
		if err := ac.accept(logger, best, balance, blockSeed); err != nil {
			logger.With().Warning("failed to add tx to account cache",
				ac.addr,
				best.ID,
				log.Uint64("nonce", best.Nonce.Counter),
				log.Uint64("amount", best.MaxSpend),
				log.Err(err))
			break
		}
		delete(nonce2TXs, nextNonce)
		nextNonce++
		balance = ac.availBalance()
	}

	if len(nonce2TXs) == 0 {
		ac.moreInDB = false
	} else {
		for nonce := range nonce2TXs {
			if nonce >= nextNonce {
				logger.With().Debug("transactions detected in higher nonce",
					ac.addr,
					log.Uint64("next_nonce", nextNonce),
					log.Uint64("found_nonce", nonce))
				ac.moreInDB = true
				break
			}
		}
	}
	if nextNonce > oldNonce {
		logger.With().Debug("added batch to account pool",
			ac.addr,
			log.Uint64("from_nonce", oldNonce),
			log.Uint64("to_nonce", nextNonce-1))
	} else {
		logger.With().Debug("no feasible txs from batch", ac.addr, log.Array("batch", nonceMarshaller(nonce2TXs)))
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

func getNonceOffset(start, end uint64) int {
	if end < start {
		return -1
	}
	offset64 := end - start
	// check overflow
	offset := int(offset64)
	if offset64 != uint64(offset) {
		return -1
	}
	return offset
}

func (ac *accountCache) addToExistingNonce(logger log.Log, ntx *txtypes.NanoTX) error {
	idx := getNonceOffset(ac.startNonce, ntx.Nonce.Counter)
	if idx < 0 {
		return errBadNonce
	}

	nonceTXs := ac.txsByNonce[idx]
	balance := nonceTXs.postBalance + nonceTXs.maxSpending()
	return ac.accept(logger, ntx, balance, nil)
}

// adding a tx to the account cache. possible outcomes:
// - nonce is smaller than the next nonce in state: reject from cache
// - nonce is higher than the next nonce in the account cache (i.e. nonce gap):
//   reject from cache for now but will retrieve it from DB when the nonce gap is closed
// - nonce already exists in the cache:
//   if it is better than the best candidate in that nonce group, swap
func (ac *accountCache) add(logger log.Log, tp txProvider, tx *types.Transaction, received time.Time, blockSeed []byte) error {
	if tx.Nonce.Counter < ac.startNonce {
		logger.With().Debug("nonce too small",
			ac.addr,
			tx.ID,
			log.Uint64("next_nonce", ac.startNonce),
			log.Uint64("tx_nonce", tx.Nonce.Counter))
		return errBadNonce
	}

	next := ac.nextNonce()
	if tx.Nonce.Counter > next {
		logger.With().Debug("nonce too large. will be loaded later",
			tx.Principal,
			tx.ID,
			log.Uint64("next_nonce", ac.startNonce),
			log.Uint64("tx_nonce", tx.Nonce.Counter))
		ac.moreInDB = true
		return errNonceTooBig
	}

	ntx := txtypes.NewNanoTX(&types.MeshTransaction{
		Transaction: *tx,
		Received:    received,
		LayerID:     types.LayerID{},
		BlockID:     types.EmptyBlockID,
	})

	if ntx.Nonce.Counter < next {
		return ac.addToExistingNonce(logger, ntx)
	}

	// transaction uses the next nonce
	if err := ac.accept(logger, ntx, ac.availBalance(), blockSeed); err != nil {
		if errors.Is(err, errTooManyNonce) {
			ac.moreInDB = true
			return nil
		}
		return err
	}

	// adding a new nonce can bridge the nonce gap in db
	// check DB for txs with higher nonce
	if ac.moreInDB {
		if err := ac.addPendingFromNonce(logger, tp, ac.nextNonce(), types.LayerID{}); err != nil {
			return err
		}
	}
	return nil
}

func (ac *accountCache) addPendingFromNonce(logger log.Log, tp txProvider, nonce uint64, applied types.LayerID) error {
	mtxs, err := tp.GetAcctPendingFromNonce(ac.addr, nonce)
	if err != nil {
		logger.With().Error("failed to get more pending txs from db", ac.addr, log.Err(err))
		return err
	}

	if len(mtxs) == 0 {
		ac.moreInDB = false
		return nil
	}

	if applied != (types.LayerID{}) {
		// we just applied a layer, need to update layer/block for the pending txs
		for i, mtx := range mtxs {
			nextLayer, nextBlock, err := tp.SetNextLayerBlock(mtx.ID, applied)
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
				logger.With().Debug("next layer found", ac.addr, mtx.ID, nextLayer)
			}
		}
	}

	byPrincipal := groupTXsByPrincipal(logger, mtxs)
	if _, ok := byPrincipal[ac.addr]; !ok {
		logger.With().Panic("no txs for account after grouping", ac.addr)
	}
	return ac.addBatch(logger, byPrincipal[ac.addr], nil)
}

// find the first nonce without a layer.
// a nonce with a valid layer indicates that it's already packed in a proposal/block.
func (ac *accountCache) getMempoolOffset() int {
	for i, nonceTXs := range ac.txsByNonce {
		if nonceTXs.layer() == (types.LayerID{}) {
			return i
		}
	}
	return -1
}

func (ac *accountCache) getMempool(logger log.Log) []*txtypes.NanoTX {
	bests := make([]*txtypes.NanoTX, 0, maxTXsPerAcct)
	offset := ac.getMempoolOffset()
	if offset < 0 {
		return nil
	}
	for _, nonceTXs := range ac.txsByNonce[offset:] {
		if nonceTXs.layer() != (types.LayerID{}) {
			logger.With().Warning("some proposals/blocks packed txs out of order",
				nonceTXs.id(),
				nonceTXs.layer(),
				nonceTXs.block(),
				log.Uint64("nonce", nonceTXs.nonce()))
		}
		bests = append(bests, nonceTXs.best)
	}
	return bests
}

func (ac *accountCache) applyLayer(
	logger log.Log,
	newNonce, newBalance uint64,
	tp txProvider,
	lid types.LayerID,
	appliedByNonce map[uint64]types.TransactionWithResult,
) error {
	nextNonce := ac.startNonce
	for {
		if _, ok := appliedByNonce[nextNonce]; !ok {
			break
		}
		nextNonce++
	}

	numApplied := nextNonce - ac.startNonce
	if numApplied != uint64(len(appliedByNonce)) {
		logger.With().Error("account was not applied in nonce order",
			ac.addr,
			log.Array("state_applied", nonceMarshaller(appliedByNonce)),
			log.Uint64("cache_start", ac.startNonce),
			log.Uint64("cache_end", nextNonce-1))
		return errNonceNotInOrder
	}

	if nextNonce != newNonce {
		logger.With().Error("unexpected next nonce",
			ac.addr,
			log.Uint64("cache_nonce", nextNonce),
			log.Uint64("state_nonce", newNonce))
		return errBadNonceInCache
	}

	offset := getNonceOffset(ac.startNonce, nextNonce)
	if offset < 0 {
		return errBadNonce
	}
	if offset > len(ac.txsByNonce) {
		// some applied nonce are not in cache
		logger.With().Warning("applied nonce not in cache",
			ac.addr,
			log.Uint64("cache_nonce", ac.nextNonce()-1),
			log.Array("applied_nonce", nonceMarshaller(appliedByNonce)))
	} else if offset > 1 && newBalance < ac.txsByNonce[offset-1].postBalance {
		logger.With().Error("unexpected conservative balance",
			ac.addr,
			log.Uint64("nonce", nextNonce),
			log.Uint64("balance", newBalance),
			log.Uint64("projected", ac.txsByNonce[offset-1].postBalance))
		return errBadBalanceInCache
	}

	if err := tp.ApplyLayer(appliedByNonce); err != nil {
		logger.With().Error("failed to set txs discarded for applied layer", lid, log.Err(err))
		return err
	}

	// txs that were rejected from cache due to nonce too low are discarded here
	if err := tp.DiscardNonceBelow(ac.addr, ac.startNonce); err != nil {
		logger.With().Error("failed to discard txs with lower nonce",
			ac.addr,
			log.Uint64("nonce", ac.startNonce))
		return err
	}

	return nil
}

// NOTE: this is the only point in time when we reconsider those previously rejected txs,
// because applying a layer changes the conservative balance in the cache.
func (ac *accountCache) resetAfterApply(logger log.Log, tp txProvider, nextNonce, newBalance uint64, applied types.LayerID) error {
	ac.removeFromOffset(0)
	ac.txsByNonce = make([]*sameNonceTXs, 0, maxTXsPerAcct)
	ac.startNonce = nextNonce
	ac.startBalance = newBalance
	return ac.addPendingFromNonce(logger, tp, ac.startNonce, applied)
}

func (ac *accountCache) removeFromOffset(offset int) {
	for _, nonceTXs := range ac.txsByNonce[offset:] {
		delete(ac.cachedTXs, nonceTXs.id())
	}
	ac.txsByNonce = ac.txsByNonce[:offset]
}

func (ac *accountCache) shouldEvict() bool {
	return len(ac.txsByNonce) == 0 && !ac.moreInDB
}

type stateFunc func(address.Address) (uint64, uint64)

type cache struct {
	logger log.Log
	tp     txProvider
	stateF stateFunc

	mu        sync.Mutex
	pending   map[address.Address]*accountCache
	cachedTXs map[types.TransactionID]*txtypes.NanoTX // shared with accountCache instances
}

func newCache(tp txProvider, s stateFunc, logger log.Log) *cache {
	return &cache{
		logger:    logger,
		tp:        tp,
		stateF:    s,
		pending:   make(map[address.Address]*accountCache),
		cachedTXs: make(map[types.TransactionID]*txtypes.NanoTX),
	}
}

func groupTXsByPrincipal(logger log.Log, mtxs []*types.MeshTransaction) map[address.Address]map[uint64][]*txtypes.NanoTX {
	byPrincipal := make(map[address.Address]map[uint64][]*txtypes.NanoTX)
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

// BuildFromScratch builds the cache from database.
func (c *cache) BuildFromScratch() error {
	mtxs, err := c.tp.GetAllPending()
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

	c.pending = make(map[address.Address]*accountCache)
	toCleanup := make(map[address.Address]struct{})
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

func (c *cache) createAcctIfNotPresent(addr address.Address) {
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
			txsByNonce:   make([]*sameNonceTXs, 0, maxTXsPerAcct),
			cachedTXs:    c.cachedTXs,
		}
	}
}

func (c *cache) MoreInDB(addr address.Address) bool {
	acct, ok := c.pending[addr]
	if !ok {
		return false
	}
	return acct.moreInDB
}

func (c *cache) cleanupAccounts(accounts map[address.Address]struct{}) {
	for addr := range accounts {
		if _, ok := c.pending[addr]; ok && c.pending[addr].shouldEvict() {
			delete(c.pending, addr)
		}
	}
}

func (c *cache) Add(tx *types.Transaction, received time.Time, blockSeed []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	principal := tx.Principal
	c.createAcctIfNotPresent(principal)
	defer c.cleanupAccounts(map[address.Address]struct{}{principal: {}})
	if err := c.pending[principal].add(c.logger, c.tp, tx, received, blockSeed); err != nil {
		return err
	}
	return nil
}

// Get gets a transaction from the cache.
func (c *cache) Get(tid types.TransactionID) *txtypes.NanoTX {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cachedTXs[tid]
}

// HasTx returns true if transaction exists in the cache.
func (c *cache) HasTx(tid types.TransactionID) (bool, error) {
	if c.has(tid) {
		return true, nil
	}

	has, err := c.tp.Has(tid)
	if err != nil {
		return false, fmt.Errorf("has tx: %w", err)
	}
	return has, nil
}

func (c *cache) has(tid types.TransactionID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cachedTXs[tid] != nil
}

// LinkTXsWithProposal associates the transactions to a proposal.
func (c *cache) LinkTXsWithProposal(lid types.LayerID, pid types.ProposalID, tids []types.TransactionID) error {
	if len(tids) == 0 {
		return nil
	}
	if err := c.tp.AddToProposal(lid, pid, tids); err != nil {
		c.logger.With().Error("failed to link txs to proposal in db", log.Err(err))
		return err
	}
	return c.updateLayer(lid, types.EmptyBlockID, tids)
}

// LinkTXsWithBlock associates the transactions to a block.
func (c *cache) LinkTXsWithBlock(lid types.LayerID, bid types.BlockID, tids []types.TransactionID) error {
	if len(tids) == 0 {
		return nil
	}
	if err := c.tp.AddToBlock(lid, bid, tids); err != nil {
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
func (c *cache) ApplyLayer(lid types.LayerID, bid types.BlockID, results []types.TransactionWithResult) ([]error, []error) {
	if err := c.CheckApplyOrder(lid); err != nil {
		return nil, []error{err}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	toCleanup := make(map[address.Address]struct{})
	for _, rst := range results {
		toCleanup[rst.Principal] = struct{}{}
	}
	defer c.cleanupAccounts(toCleanup)

	byPrincipal := make(map[address.Address]map[uint64]types.TransactionWithResult)
	for _, rst := range results {
		principal := rst.Principal
		if _, ok := byPrincipal[principal]; !ok {
			byPrincipal[principal] = make(map[uint64]types.TransactionWithResult)
		}
		if _, ok := byPrincipal[principal][rst.Nonce.Counter]; ok {
			return nil, []error{errDupNonceApplied}
		}
		byPrincipal[principal][rst.Nonce.Counter] = rst

		events.ReportResult(rst)
	}

	errsApply := make([]error, 0, len(byPrincipal))
	errsReset := make([]error, 0, len(byPrincipal))
	logger := c.logger.WithFields(lid, bid)
	for principal, appliedByNonce := range byPrincipal {
		c.createAcctIfNotPresent(principal)
		nextNonce, balance := c.stateF(principal)
		logger.With().Debug("new account nonce/balance",
			principal,
			log.Uint64("nonce", nextNonce),
			log.Uint64("balance", balance))
		if err := c.pending[principal].applyLayer(logger, nextNonce, balance, c.tp, lid, appliedByNonce); err != nil {
			logger.With().Warning("failed to apply layer to principal", principal, log.Err(err))
			errsApply = append(errsApply, err)
		}
		if err := c.pending[principal].resetAfterApply(logger, c.tp, nextNonce, balance, lid); err != nil {
			logger.With().Error("failed to reset cache for principal", principal, log.Err(err))
			errsReset = append(errsReset, err)
		}
	}
	return errsApply, errsReset
}

func (c *cache) RevertToLayer(revertTo types.LayerID) error {
	if err := c.tp.UndoLayers(revertTo.Add(1)); err != nil {
		return err
	}

	if err := c.BuildFromScratch(); err != nil {
		c.logger.With().Error("failed to build from scratch after revert", log.Err(err))
		return err
	}
	return nil
}

// GetProjection returns the projected nonce and balance for an account, including
// pending transactions that are paced in proposals/blocks but not yet applied to the state.
func (c *cache) GetProjection(addr address.Address) (uint64, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.pending[addr]; !ok {
		return c.stateF(addr)
	}
	return c.pending[addr].nextNonce(), c.pending[addr].availBalance()
}

// GetMempool returns all the transactions that eligible for a proposal/block.
func (c *cache) GetMempool() map[address.Address][]*txtypes.NanoTX {
	c.mu.Lock()
	defer c.mu.Unlock()

	all := make(map[address.Address][]*txtypes.NanoTX)
	for addr, accCache := range c.pending {
		txs := accCache.getMempool(c.logger)
		if len(txs) > 0 {
			all[addr] = txs
		}
	}
	return all
}

// AddToDB adds a transaction to the database.
func (c *cache) AddToDB(tx *types.Transaction, received time.Time) error {
	return c.tp.Add(tx, received)
}

// GetMeshTransaction retrieves a tx by its id.
func (c *cache) GetMeshTransaction(tid types.TransactionID) (*types.MeshTransaction, error) {
	return c.tp.Get(tid)
}

// GetMeshTransactions retrieves a list of txs by their id's.
func (c *cache) GetMeshTransactions(ids []types.TransactionID) ([]*types.MeshTransaction, map[types.TransactionID]struct{}) {
	missing := make(map[types.TransactionID]struct{})
	mtxs := make([]*types.MeshTransaction, 0, len(ids))
	for _, tid := range ids {
		var (
			mtx *types.MeshTransaction
			err error
		)
		if mtx, err = c.GetMeshTransaction(tid); err != nil {
			c.logger.With().Warning("could not get tx", tid, log.Err(err))
			missing[tid] = struct{}{}
		} else {
			mtxs = append(mtxs, mtx)
		}
	}
	return mtxs, missing
}

// GetTransactionsByAddress retrieves txs for a single address in between layers [from, to].
// Guarantees that transaction will appear exactly once, even if origin and recipient is the same, and in insertion order.
func (c *cache) GetTransactionsByAddress(from, to types.LayerID, address address.Address) ([]*types.MeshTransaction, error) {
	return c.tp.GetByAddress(from, to, address)
}

// CheckApplyOrder returns an error if layers were not applied in order.
func (c *cache) CheckApplyOrder(toApply types.LayerID) error {
	lastApplied, err := c.tp.LastAppliedLayer()
	if err != nil {
		c.logger.With().Error("failed to get last applied layer", log.Err(err))
		return fmt.Errorf("cache get last applied %w", err)
	}
	if toApply != lastApplied.Add(1) {
		c.logger.With().Error("layer not applied in order",
			log.Stringer("expected", lastApplied.Add(1)),
			log.Stringer("to_apply", toApply))
		return errLayerNotInOrder
	}
	return nil
}

// GetMeshHash gets the aggregated layer hash at the specified layer.
func (c *cache) GetMeshHash(lid types.LayerID) (types.Hash32, error) {
	return c.tp.GetMeshHash(lid)
}
