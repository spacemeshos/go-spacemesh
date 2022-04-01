package txs

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
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
)

type sameNonceTXs struct {
	best        *types.NanoTX
	postBalance uint64
}

func (s *sameNonceTXs) id() types.TransactionID {
	return s.best.Tid
}

func (s *sameNonceTXs) layer() types.LayerID {
	return s.best.Layer
}

func (s *sameNonceTXs) block() types.BlockID {
	return s.best.Block
}

func (s *sameNonceTXs) nonce() uint64 {
	return s.best.Nonce
}

func (s *sameNonceTXs) maxSpending() uint64 {
	return s.best.MaxSpending()
}

func resetLayerForNonce(logger log.Log, tp txProvider, addr types.Address, nonce uint64, lastApplied types.LayerID) ([]*types.NanoTX, error) {
	mtxs, err := tp.GetAcctPendingAtNonce(addr, nonce)
	if err != nil {
		logger.With().Error("failed to get tx at nonce", addr, log.Uint64("nonce", nonce))
		return nil, err
	}

	ntxs := make([]*types.NanoTX, 0, len(mtxs))
	for _, mtx := range mtxs {
		nextLayer, nextBlock, err := tp.SetNextLayerBlock(mtx.ID(), lastApplied)
		if err != nil {
			logger.With().Error("failed to reset layer",
				mtx.ID(),
				log.Uint64("nonce", nonce),
				log.Stringer("applied", lastApplied))
			return nil, err
		}
		mtx.LayerID = nextLayer
		mtx.BlockID = nextBlock
		ntxs = append(ntxs, types.NewNanoTX(mtx))
	}
	return ntxs, nil
}

type accountCache struct {
	addr         types.Address
	txsByNonce   []*sameNonceTXs
	cachedTXs    map[types.TransactionID]*types.NanoTX // shared with the cache instance
	startNonce   uint64
	startBalance uint64
	hasGapInDB   bool // set to true when there is a nonce gap
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

func (ac *accountCache) accept(logger log.Log, ntx *types.NanoTX, balance uint64) error {
	idx := getNonceOffset(ac.startNonce, ntx.Nonce)
	if idx < 0 {
		logger.With().Debug("bad nonce",
			log.Uint64("acct_nonce", ac.startNonce),
			log.Uint64("tx_nonce", ntx.Nonce))
		return errBadNonce
	}

	if balance < ntx.MaxSpending() {
		logger.With().Debug("insufficient balance",
			ntx.Tid,
			ntx.Principal,
			log.Uint64("nonce", ntx.Nonce),
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
		return nil
	}

	// tx for an existing nonce
	nonceTXs := ac.txsByNonce[idx]
	if !ntx.Better(nonceTXs.best) {
		return nil
	}

	delete(ac.cachedTXs, nonceTXs.best.Tid)
	ac.cachedTXs[ntx.Tid] = ntx
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
		ac.removeFromOffset(toRemove)
	}
	return nil
}

func nonceMarshaller(any interface{}) log.ArrayMarshaler {
	var allNonce []uint64
	nonce2Tid, ok := any.(map[uint64]types.TransactionID)
	if ok {
		allNonce = make([]uint64, 0, len(nonce2Tid))
		for nonce := range nonce2Tid {
			allNonce = append(allNonce, nonce)
		}
	} else if nonce2TXs, ok := any.(map[uint64][]*types.NanoTX); ok {
		allNonce = make([]uint64, 0, len(nonce2TXs))
		for nonce := range nonce2TXs {
			allNonce = append(allNonce, nonce)
		}
	}
	sort.Slice(allNonce, func(i, j int) bool { return allNonce[i] < allNonce[j] })
	return log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
		for _, nonce := range allNonce {
			encoder.AppendUint64(nonce)
		}
		return nil
	})
}

func (ac *accountCache) addBatch(logger log.Log, nonce2TXs map[uint64][]*types.NanoTX) error {
	var (
		oldNonce  = ac.nextNonce()
		nextNonce = oldNonce
		balance   = ac.availBalance()
	)
	for len(nonce2TXs) > 0 {
		if _, ok := nonce2TXs[nextNonce]; !ok {
			logger.Debug("batch does not contain the next nonce",
				ac.addr,
				log.Uint64("nonce", nextNonce),
				log.Array("batch", nonceMarshaller(nonce2TXs)))
			ac.hasGapInDB = true
			break
		}

		best := findBest(nonce2TXs[nextNonce], balance)
		if best == nil {
			ac.hasGapInDB = true
			break
		}
		if err := ac.accept(logger, best, balance); err != nil {
			return err
		}
		delete(nonce2TXs, nextNonce)
		nextNonce++
		balance = ac.availBalance()
	}

	logger.With().Debug("added batch to account pool",
		ac.addr,
		log.Uint64("from_nonce", oldNonce),
		log.Uint64("to_nonce", nextNonce-1))
	return nil
}

func findBest(ntxs []*types.NanoTX, balance uint64) *types.NanoTX {
	var best *types.NanoTX
	for _, ntx := range ntxs {
		if balance >= ntx.MaxSpending() &&
			(best == nil || ntx.Better(best)) {
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

func (ac *accountCache) addToExistingNonce(logger log.Log, ntx *types.NanoTX) error {
	idx := getNonceOffset(ac.startNonce, ntx.Nonce)
	if idx < 0 {
		return errBadNonce
	}

	nonceTXs := ac.txsByNonce[idx]
	balance := nonceTXs.postBalance + nonceTXs.maxSpending()
	return ac.accept(logger, ntx, balance)
}

// adding a tx to the account cache. possible outcomes:
// - nonce is smaller than the next nonce in state: reject from cache
// - nonce is higher than the next nonce in the account cache (i.e. nonce gap):
//   reject from cache for now but will retrieve it from DB when the nonce gap is closed
// - nonce already exists in the cache:
//   if it is better than the best candidate in that nonce group, swap
func (ac *accountCache) add(logger log.Log, tp txProvider, tx *types.Transaction, received time.Time) error {
	if tx.AccountNonce < ac.startNonce {
		logger.With().Debug("nonce too small",
			ac.addr,
			tx.ID(),
			log.Uint64("next_nonce", ac.startNonce),
			log.Uint64("tx_nonce", tx.AccountNonce))
		return errBadNonce
	}

	next := ac.nextNonce()
	if tx.AccountNonce > next {
		logger.With().Debug("nonce too large. will be loaded later",
			tx.Origin(),
			tx.ID(),
			log.Uint64("next_nonce", ac.startNonce),
			log.Uint64("tx_nonce", tx.AccountNonce))
		ac.hasGapInDB = true
		return errNonceTooBig
	}

	ntx := types.NewNanoTX(&types.MeshTransaction{
		Transaction: *tx,
		Received:    received,
		LayerID:     types.LayerID{},
		BlockID:     types.EmptyBlockID,
	})

	if ntx.Nonce < next {
		return ac.addToExistingNonce(logger, ntx)
	}

	// transaction uses the next nonce
	if err := ac.accept(logger, ntx, ac.availBalance()); err != nil {
		return err
	}

	// adding a new nonce can bridge the nonce gap in db
	// check DB for txs with higher nonce
	if ac.hasGapInDB {
		if err := ac.addPendingFromNonce(logger, tp, ac.nextNonce()); err != nil {
			return err
		}
		ac.hasGapInDB = false
	}
	return nil
}

func (ac *accountCache) addPendingFromNonce(logger log.Log, tp txProvider, nonce uint64) error {
	mtxs, err := tp.GetAcctPendingFromNonce(ac.addr, nonce)
	if err != nil {
		logger.With().Error("failed to get more pending txs from db", ac.addr, log.Err(err))
		return err
	}

	if len(mtxs) == 0 {
		return nil
	}

	byPrincipal := groupTXsByPrincipal(logger, mtxs)
	if _, ok := byPrincipal[ac.addr]; !ok {
		logger.With().Panic("no txs for account after grouping", ac.addr)
	}
	return ac.addBatch(logger, byPrincipal[ac.addr])
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

func (ac *accountCache) getMempool(logger log.Log) ([]*types.NanoTX, error) {
	bests := make([]*types.NanoTX, 0, maxTXsPerAcct)
	offset := ac.getMempoolOffset()
	if offset < 0 {
		return nil, nil
	}
	for _, nonceTXs := range ac.txsByNonce[offset:] {
		if nonceTXs.layer() != (types.LayerID{}) {
			logger.With().Error("some proposals/blocks packed txs out of order",
				nonceTXs.id(),
				nonceTXs.layer(),
				nonceTXs.block(),
				log.Uint64("nonce", nonceTXs.nonce()))
		}
		bests = append(bests, nonceTXs.best)
	}
	return bests, nil
}

// TODO: check that applyLayer is called in order
// maybe add last applied layer to SVM?
func (ac *accountCache) applyLayer(
	logger log.Log,
	newNonce, newBalance uint64,
	tp txProvider,
	lid types.LayerID,
	bid types.BlockID,
	appliedByNonce map[uint64]types.TransactionID,
) error {
	var (
		nextNonce = ac.startNonce
		hesNext   bool
		tid       types.TransactionID
	)
	for {
		if tid, hesNext = appliedByNonce[nextNonce]; !hesNext {
			break
		}
		delete(ac.cachedTXs, tid)
		nextNonce++
	}

	numApplied := nextNonce - ac.startNonce
	if numApplied != uint64(len(appliedByNonce)) {
		logger.Error("account was not applied in nonce order",
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

	if err := tp.ApplyLayer(lid, bid, ac.addr, appliedByNonce); err != nil {
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

	if err := ac.resetAfterApply(logger, tp, newNonce, newBalance, lid); err != nil {
		return err
	}

	return nil
}

// NOTE: this is the only point in time when we reconsider those previously rejected txs,
// because applying a layer changes the conservative balance in the cache.
func (ac *accountCache) resetAfterApply(logger log.Log, tp txProvider, nextNonce, newBalance uint64, applied types.LayerID) error {
	offset := getNonceOffset(ac.startNonce, nextNonce)
	if offset < 0 {
		return errBadNonce
	}
	ac.txsByNonce = ac.txsByNonce[offset:]
	ac.startNonce = nextNonce

	// TODO test the case where ac.txsByNonce is empty but DB has more
	// previously rejected txs with the next nonce
	if len(ac.txsByNonce) > 0 && ac.txsByNonce[0].postBalance < newBalance {
		logger.With().Error("unexpected conservative balance",
			log.Uint64("balance", newBalance),
			log.Uint64("projected", ac.txsByNonce[0].postBalance))
		return errBadBalanceInCache
	}

	toRemove := len(ac.txsByNonce)
	balance := newBalance
	for i, nonceTXs := range ac.txsByNonce {
		ntxs, err := resetLayerForNonce(logger, tp, ac.addr, nonceTXs.nonce(), applied)
		if err != nil {
			return err
		}
		newBest := findBest(ntxs, balance)
		if newBest == nil {
			toRemove = i
			break
		}
		if err = ac.accept(logger, newBest, balance); err != nil {
			return err
		}
		balance = ac.availBalance()
	}

	if toRemove < len(ac.txsByNonce) {
		ac.removeFromOffset(toRemove)
		return nil
	}

	// check if there are txs previously rejected due to insufficient balance
	return ac.addPendingFromNonce(logger, tp, ac.nextNonce())
}

func (ac *accountCache) removeFromOffset(offset int) {
	for _, nonceTXs := range ac.txsByNonce[offset:] {
		delete(ac.cachedTXs, nonceTXs.id())
	}
	ac.txsByNonce = ac.txsByNonce[:offset]
}

func (ac *accountCache) shouldEvict() bool {
	return len(ac.txsByNonce) == 0 && !ac.hasGapInDB
}

type cache struct {
	logger log.Log
	tp     txProvider
	state  svmState

	mu        sync.Mutex
	pending   map[types.Address]*accountCache
	cachedTXs map[types.TransactionID]*types.NanoTX // shared with accountCache instances
}

func newCache(tp txProvider, s svmState, logger log.Log) *cache {
	return &cache{
		logger:    logger,
		tp:        tp,
		state:     s,
		pending:   make(map[types.Address]*accountCache),
		cachedTXs: make(map[types.TransactionID]*types.NanoTX),
	}
}

func groupTXsByPrincipal(logger log.Log, mtxs []*types.MeshTransaction) map[types.Address]map[uint64][]*types.NanoTX {
	byPrincipal := make(map[types.Address]map[uint64][]*types.NanoTX)
	for _, mtx := range mtxs {
		principal := mtx.Origin()
		if _, ok := byPrincipal[principal]; !ok {
			byPrincipal[principal] = make(map[uint64][]*types.NanoTX)
		}
		if _, ok := byPrincipal[principal][mtx.AccountNonce]; !ok {
			byPrincipal[principal][mtx.AccountNonce] = make([]*types.NanoTX, 0, maxTXsPerNonce)
		}
		if len(byPrincipal[principal][mtx.AccountNonce]) < maxTXsPerNonce {
			byPrincipal[principal][mtx.AccountNonce] = append(byPrincipal[principal][mtx.AccountNonce], types.NewNanoTX(mtx))
		} else {
			logger.With().Warning("too many txs in same nonce. ignoring tx",
				mtx.ID(),
				principal,
				log.Uint64("nonce", mtx.AccountNonce))
		}
	}
	return byPrincipal
}

// BuildFromScratch builds the cache from database.
func (c *cache) BuildFromScratch() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pending = make(map[types.Address]*accountCache)
	mtxs, err := c.tp.GetAllPending()
	if err != nil {
		c.logger.Error("failed to get all pending txs", log.Err(err))
		return err
	}

	byPrincipal := groupTXsByPrincipal(c.logger, mtxs)
	for principal, nonce2TXs := range byPrincipal {
		c.createAcctIfNotPresent(principal)
		if err = c.pending[principal].addBatch(c.logger, nonce2TXs); err != nil {
			return err
		}
		if c.pending[principal].shouldEvict() {
			c.logger.With().Warning("account has pending txs but none feasible",
				principal,
				log.Array("batch", nonceMarshaller(nonce2TXs)))
			delete(c.pending, principal)
		}
	}
	return nil
}

func (c *cache) createAcctIfNotPresent(addr types.Address) {
	if _, ok := c.pending[addr]; !ok {
		nextNonce := c.state.GetNonce(addr)
		balance := c.state.GetBalance(addr)
		c.pending[addr] = &accountCache{
			addr:         addr,
			startNonce:   nextNonce,
			startBalance: balance,
			txsByNonce:   make([]*sameNonceTXs, 0, maxTXsPerAcct),
			cachedTXs:    c.cachedTXs,
		}
	}
}

// Add adds a transaction to the cache.
func (c *cache) Add(tx *types.Transaction, received time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	principal := tx.Origin()
	c.createAcctIfNotPresent(principal)
	if err := c.pending[principal].add(c.logger, c.tp, tx, received); err != nil {
		return err
	}
	return nil
}

// AddToLayer associates the transactions to a layer.
// A transaction is tagged with a layer when it's included in a proposal/block.
// If a transaction is included in multiple proposals/blocks in different layers,
// the lowest layer is retained.
func (c *cache) AddToLayer(lid types.LayerID, tids []types.TransactionID) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, tid := range tids {
		if _, ok := c.cachedTXs[tid]; !ok {
			// transaction is not considered best in its nonce group
			return nil
		}
		c.cachedTXs[tid].UpdateLayerMaybe(lid)
	}
	return nil
}

// ApplyLayer retires the applied transactions from the cache and updates the balances.
func (c *cache) ApplyLayer(lid types.LayerID, bid types.BlockID, txs []*types.Transaction) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	byPrincipal := make(map[types.Address]map[uint64]types.TransactionID)
	for _, tx := range txs {
		principal := tx.Origin()
		if _, ok := byPrincipal[principal]; !ok {
			byPrincipal[principal] = make(map[uint64]types.TransactionID)
		}
		if _, ok := byPrincipal[principal][tx.AccountNonce]; ok {
			return errDupNonceApplied
		}
		byPrincipal[principal][tx.AccountNonce] = tx.ID()
	}

	for principal, appliedByNonce := range byPrincipal {
		c.createAcctIfNotPresent(principal)
		nextNonce := c.state.GetNonce(principal)
		balance := c.state.GetBalance(principal)
		if err := c.pending[principal].applyLayer(c.logger, nextNonce, balance, c.tp, lid, bid, appliedByNonce); err != nil {
			return err
		}
		if c.pending[principal].shouldEvict() {
			delete(c.pending, principal)
		}
	}
	return nil
}

// GetProjection returns the projected nonce and balance for an account, including
// pending transactions that are paced in proposals/blocks but not yet applied to the state.
func (c *cache) GetProjection(addr types.Address) (uint64, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.pending[addr]; !ok {
		return c.state.GetNonce(addr), c.state.GetBalance(addr)
	}
	return c.pending[addr].nextNonce(), c.pending[addr].availBalance()
}

// GetMempool returns all the transactions that should be included in the next proposal.
func (c *cache) GetMempool() (map[types.Address][]*types.NanoTX, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	all := make(map[types.Address][]*types.NanoTX)
	for addr, accCache := range c.pending {
		txs, err := accCache.getMempool(c.logger)
		if err != nil {
			return nil, err
		}
		all[addr] = txs
	}
	return all, nil
}
