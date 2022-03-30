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
	maxTXsPerAcct = 100
)

var (
	errBadNonce            = errors.New("bad nonce")
	errNonceTooBig         = errors.New("nonce too big")
	errInsufficientBalance = errors.New("insufficient balance")
	errNotInCache          = errors.New("not in cache")
	errNotInAcctCache      = errors.New("not in account cache")
	errNonceAppliedOOO     = errors.New("nonce applied out of order")
	errBadNonceInCache     = errors.New("cache contains incorrect nonce")
	errBadBalanceInCache   = errors.New("cache contains incorrect balance")
	errDupNonceApplied     = errors.New("multiple txs applied for the same nonce")
)

type sameNonceTXs struct {
	best        *types.NanoTX
	postBalance uint64
	all         map[types.TransactionID]*types.NanoTX
}

func (s *sameNonceTXs) layer() types.LayerID {
	return s.best.Layer
}

func (s *sameNonceTXs) txs() []*types.NanoTX {
	ntxs := make([]*types.NanoTX, 0, len(s.all))
	for _, ntx := range s.all {
		ntxs = append(ntxs, ntx)
	}
	return ntxs
}

func (s *sameNonceTXs) resetLayerForNonce(logger log.Log, tp txProvider, lastApplied types.LayerID) error {
	for tid, ntx := range s.all {
		nextLayer, nextBlock, err := tp.setNextLayerBlock(tid, lastApplied)
		if err != nil {
			logger.With().Error("failed to reset layer for tx",
				tid,
				log.Uint64("nonce", ntx.Nonce),
				log.Named("applied", lastApplied))
			return err
		}
		ntx.Layer = nextLayer
		ntx.Block = nextBlock
	}
	return nil
}

type accountCache struct {
	addr         types.Address
	txsByNonce   []*sameNonceTXs
	cachedTXs    map[types.TransactionID]*types.NanoTX // shared with the cache instance
	startNonce   uint64
	availBalance uint64
	hasGapInDB   bool // set to true when there is a nonce gap
}

func (ac *accountCache) nextNonce() uint64 {
	return ac.startNonce + uint64(len(ac.txsByNonce))
}

func (ac *accountCache) accept(ntx *types.NanoTX, balance uint64) error {
	offset := int(ntx.Nonce - ac.startNonce)
	if offset > len(ac.txsByNonce) {
		return errBadNonce
	}

	ac.cachedTXs[ntx.Tid] = ntx
	if offset == len(ac.txsByNonce) {
		ac.txsByNonce = append(ac.txsByNonce, &sameNonceTXs{})
	}
	nonceTXs := ac.txsByNonce[offset]
	nonceTXs.all[ntx.Tid] = ntx
	// find the best tx among competing txs
	if ntx.Better(nonceTXs.best) {
		nonceTXs.best = ntx
		nonceTXs.postBalance = balance - ntx.MaxSpending()
	}
	return nil
}

func (ac *accountCache) discard(logger log.Log, tp txProvider, txs []*types.NanoTX, filter func(uint64) bool) error {
	for _, ntx := range txs {
		if filter == nil || filter(ntx.Nonce) {
			delete(ac.cachedTXs, ntx.Tid)
			logger.With().Debug("discard tx", ntx.Principal, ntx.Tid, log.Uint64("nonce", ntx.Nonce))
			if err := tp.discard4Ever(ntx.Tid); err != nil {
				logger.With().Error("failed to discard tx",
					ntx.Tid,
					log.Uint64("nonce", ntx.Nonce),
					log.Err(err))
				return err
			}
		}
	}
	return nil
}

func (ac *accountCache) discardOldNonce(logger log.Log, tp txProvider, nonce2TXs map[uint64][]*types.NanoTX) error {
	var all []*types.NanoTX
	for _, txs := range nonce2TXs {
		all = append(all, txs...)
	}
	filter := func(nonce uint64) bool {
		return nonce < ac.nextNonce()
	}
	return ac.discard(logger, tp, all, filter)
}

func nonceMarshaller(allNonce []uint64) log.ArrayMarshaler {
	return log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
		sort.Slice(allNonce, func(i, j int) bool { return allNonce[i] < allNonce[j] })
		for _, nonce := range allNonce {
			encoder.AppendUint64(nonce)
		}
		return nil
	})
}

func (ac *accountCache) addBatch(logger log.Log, tp txProvider, nonce2TXs map[uint64][]*types.NanoTX) error {
	var (
		oldSize   = len(ac.cachedTXs)
		oldNonce  = ac.nextNonce()
		nextNonce = oldNonce
		balance   = ac.availBalance
	)
	for len(nonce2TXs) > 0 {
		if _, ok := nonce2TXs[nextNonce]; !ok {
			allNonce := make([]uint64, 0, len(nonce2TXs))
			for nonce := range nonce2TXs {
				allNonce = append(allNonce, nonce)
			}
			logger.Debug("batch does not contain the next nonce",
				ac.addr,
				log.Uint64("expected_nonce", nextNonce),
				log.Array("batch_nonce", nonceMarshaller(allNonce)))
			ac.hasGapInDB = true
			break
		}

		for _, ntx := range nonce2TXs[nextNonce] {
			if err := ac.accept(ntx, balance); err != nil {
				return err
			}
		}
		delete(nonce2TXs, nextNonce)
		nextNonce++
		balance = ac.txsByNonce[len(ac.txsByNonce)-1].postBalance
	}

	if err := ac.discardOldNonce(logger, tp, nonce2TXs); err != nil {
		return err
	}

	if !ac.hasGapInDB && len(nonce2TXs) > 0 {
		for nonce, txs := range nonce2TXs {
			for _, ntx := range txs {
				logger.With().Error("unprocessed tx in batch",
					ntx.Tid,
					log.Named("intended", ac.addr),
					log.Named("actual", ntx.Principal),
					log.Uint64("nonce", nonce))
			}
		}
	}

	logger.With().Debug("added batch to account pool",
		ac.addr,
		log.Uint64("from_nonce", oldNonce),
		log.Uint64("to_nonce", nextNonce),
		log.Int("num_txs", len(ac.cachedTXs)-oldSize))
	return nil
}

func (ac *accountCache) addToExistingNonce(logger log.Log, tp txProvider, ntx *types.NanoTX) error {
	if ntx.Nonce < ac.startNonce {
		return errBadNonce
	}
	offset64 := ntx.Nonce - ac.startNonce
	offset := int(offset64)
	if offset64 != uint64(offset) {
		return errBadNonce
	}
	nonceTXs := ac.txsByNonce[offset]

	balance := nonceTXs.postBalance + nonceTXs.best.MaxSpending()
	if balance < ntx.MaxSpending() {
		logger.With().Debug("insufficient balance, discarding tx",
			ntx.Tid,
			ntx.Principal,
			log.Uint64("nonce", ntx.Nonce),
			log.Uint64("cons_balance", balance),
			log.Uint64("cons_spending", ntx.MaxSpending()))
		// TODO: do better
		// this is too harsh. conservative balance does not factor into incoming fund.
		// a transaction can still be valid.
		return ac.discard(logger, tp, []*types.NanoTX{ntx}, nil)
		// return tp.Discard(ntx.Tid)
	}

	if err := ac.accept(ntx, balance); err != nil {
		return err
	}

	// update the posterior balance for the higher nonce nonceTXs
	newBalance := nonceTXs.postBalance
	lastOffset := offset
	for i := offset + 1; i < len(ac.txsByNonce); i++ {
		nonceTXs = ac.txsByNonce[i]
		if nonceTXs.best.MaxSpending() <= newBalance {
			lastOffset = i
			nonceTXs.postBalance = newBalance - nonceTXs.best.MaxSpending()
		} else {
			// TODO: do better here
			// this is too harsh. conservative balance does not factor into incoming fund.
			// a transaction can still be valid.
			if err := ac.discard(logger, tp, nonceTXs.txs(), nil); err != nil {
				return err
			}
		}
	}
	if lastOffset < len(ac.txsByNonce)-1 {
		ac.txsByNonce = ac.txsByNonce[:lastOffset+1]
	}
	return nil
}

// adding a tx to the account cache. potential outcomes:
// - nonce is smaller than the next nonce in state: discard
// - nonce is higher than the next nonce in the account cache (i.e. gap):
//   reject from cache for now but will retrieve it from DB when the nonce gap is closed
// - nonce already exists in the cache:
//   add the tx to the nonce group and potentially become the best candidate in that nonce group
func (ac *accountCache) add(logger log.Log, tp txProvider, tx *types.Transaction, received time.Time) error {
	ntx := types.NewNanoTX(&types.MeshTransaction{
		Transaction: *tx,
		Received:    received,
		LayerID:     types.LayerID{},
		BlockID:     types.EmptyBlockID,
	})

	if tx.AccountNonce < ac.startNonce {
		logger.With().Debug("nonce too small",
			ac.addr,
			tx.ID(),
			log.Uint64("next_nonce", ac.startNonce),
			log.Uint64("tx_nonce", tx.AccountNonce))
		if err := ac.discard(logger, tp, []*types.NanoTX{ntx}, nil); err != nil {
			logger.With().Error("failed to discard bad-nonce tx", tx.ID(), log.Err(err))
			return err
		}
		return errBadNonce
	}

	next := ac.nextNonce()
	if ntx.Nonce > next {
		logger.With().Debug("nonce too large. will be loaded later",
			ntx.Principal,
			ntx.Tid,
			log.Uint64("next_nonce", ac.startNonce),
			log.Uint64("tx_nonce", ntx.Nonce))
		ac.hasGapInDB = true
		return errNonceTooBig
	}

	if ntx.Nonce < next {
		return ac.addToExistingNonce(logger, tp, ntx)
	}

	// tx uses the next nonce
	if ac.availBalance < ntx.MaxSpending() {
		// TODO: do better here
		// this is too harsh. conservative balance does not factor into incoming fund.
		// a transaction can still be valid.
		if err := ac.discard(logger, tp, []*types.NanoTX{ntx}, nil); err != nil {
			logger.With().Error("failed to discard insufficient balance tx", tx.ID(), log.Err(err))
			return err
		}
		return errInsufficientBalance
	}

	if err := ac.accept(ntx, ac.availBalance); err != nil {
		return err
	}

	if !ac.hasGapInDB {
		return nil
	}

	// check DB for more pending nonceTXs with higher nonce
	byNonce, err := ac.getPendingByNonce(logger, tp)
	if err != nil {
		return err
	}
	if err = ac.addBatch(logger, tp, byNonce); err != nil {
		return err
	}
	ac.hasGapInDB = false
	return nil
}

func (ac *accountCache) getPendingByNonce(logger log.Log, tp txProvider) (map[uint64][]*types.NanoTX, error) {
	mtxs, err := tp.getAcctPending(ac.addr)
	if err != nil {
		logger.With().Error("failed to get more pending nonceTXs from db", ac.addr, log.Err(err))
		return nil, err
	}
	byNonce := make(map[uint64][]*types.NanoTX)
	for _, mtx := range mtxs {
		if _, ok := byNonce[mtx.AccountNonce]; !ok {
			byNonce[mtx.AccountNonce] = make([]*types.NanoTX, 0, maxTXsPerAcct)
		}
		byNonce[mtx.AccountNonce] = append(byNonce[mtx.AccountNonce], types.NewNanoTX(mtx))
	}
	return byNonce, nil
}

// find the first nonce without a layer.
// a nonce with a valid layer indicates that it's already packed in a proposal/block.
func (ac *accountCache) findMempoolOffset() int {
	for i, nonceTXs := range ac.txsByNonce {
		if nonceTXs.layer() == (types.LayerID{}) {
			return i
		}
	}
	return -1
}

func (ac *accountCache) getMempool(logger log.Log) ([]*types.NanoTX, error) {
	bests := make([]*types.NanoTX, 0, maxTXsPerAcct)
	offset := ac.findMempoolOffset()
	if offset < 0 {
		return nil, nil
	}
	for _, nonceTXs := range ac.txsByNonce[offset:] {
		if nonceTXs.layer() != (types.LayerID{}) {
			logger.With().Error("some proposals/blocks packed nonceTXs out of nonce order",
				nonceTXs.best.Tid,
				nonceTXs.best.Layer,
				nonceTXs.best.Block,
				log.Uint64("nonce", nonceTXs.best.Nonce))
		}
		bests = append(bests, nonceTXs.best)
	}
	return bests, nil
}

// TODO: check that applyLayer is called in order
// add last applied layer to SVM?
func (ac *accountCache) applyLayer(
	logger log.Log,
	newNonce, newBalance uint64,
	tp txProvider,
	lid types.LayerID,
	bid types.BlockID,
	appliedByNonce map[uint64]*types.NanoTX,
) error {
	nextNonce := ac.startNonce
	var toApply, toDiscard []types.TransactionID
	for len(appliedByNonce) > 0 {
		offset := nextNonce - ac.startNonce
		if _, ok := appliedByNonce[nextNonce]; !ok {
			allNonce := make([]uint64, 0, len(appliedByNonce))
			for nonce := range appliedByNonce {
				allNonce = append(allNonce, nonce)
			}
			logger.Error("account was not applied in nonce order",
				ac.addr,
				log.Uint64("expected_nonce", nextNonce),
				log.Array("applied_nonce", nonceMarshaller(allNonce)))
			return errNonceAppliedOOO
		}

		nonceTXs := ac.txsByNonce[offset]
		applied := appliedByNonce[nextNonce]
		if _, ok := nonceTXs.all[applied.Tid]; !ok {
			logger.With().Error("applied tx not in account cache", ac.addr, applied.Tid)
			return errNotInAcctCache
		}
		delete(nonceTXs.all, applied.Tid)
		for tid := range nonceTXs.all {
			delete(ac.cachedTXs, tid)
			if tid == applied.Tid {
				toApply = append(toApply, tid)
			} else {
				toDiscard = append(toDiscard, tid)
			}
		}

		delete(appliedByNonce, nextNonce)
		nextNonce++
	}

	if nextNonce != newNonce {
		logger.With().Error("unexpected next nonce",
			ac.addr,
			log.Uint64("next_nonce", nextNonce),
			log.Uint64("state_nonce", newNonce))
		return errBadNonceInCache
	}

	if err := tp.applyLayer(lid, bid, toApply, toDiscard); err != nil {
		logger.With().Error("failed to set txs discarded for applied layer", lid, log.Err(err))
		return err
	}

	return ac.resetAfterApply(logger, tp, newNonce, newBalance, lid)
}

func (ac *accountCache) resetAfterApply(logger log.Log, tp txProvider, nextNonce, newBalance uint64, applied types.LayerID) error {
	offset := nextNonce - ac.startNonce
	ac.txsByNonce = ac.txsByNonce[offset:]
	ac.startNonce = nextNonce

	if len(ac.txsByNonce) > 0 && ac.txsByNonce[0].postBalance < newBalance {
		logger.With().Error("unexpected conservative balance",
			log.Uint64("balance", newBalance),
			log.Uint64("projected", ac.txsByNonce[0].postBalance))
		return errBadBalanceInCache
	}

	balance := newBalance
	for _, nonceTXs := range ac.txsByNonce {
		if balance < nonceTXs.best.MaxSpending() {
			logger.With().Error("unexpected tx spending",
				nonceTXs.best.Tid,
				log.Uint64("nonce", nonceTXs.best.Nonce),
				log.Uint64("balance", balance),
				log.Uint64("spending", nonceTXs.best.MaxSpending()))
			return errBadBalanceInCache
		}

		balance -= nonceTXs.best.MaxSpending()
		nonceTXs.postBalance = balance
		if nonceTXs.layer().After(applied) {
			continue
		}
		if err := nonceTXs.resetLayerForNonce(logger, tp, applied); err != nil {
			return err
		}
	}
	ac.availBalance = balance
	return nil
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

func groupTXsByPrincipal(mtxs []*types.MeshTransaction) map[types.Address]map[uint64][]*types.NanoTX {
	byPrincipal := make(map[types.Address]map[uint64][]*types.NanoTX)
	for _, mtx := range mtxs {
		principal := mtx.Origin()
		if _, ok := byPrincipal[principal]; !ok {
			byPrincipal[principal] = make(map[uint64][]*types.NanoTX)
		}
		if _, ok := byPrincipal[principal][mtx.AccountNonce]; !ok {
			byPrincipal[principal][mtx.AccountNonce] = make([]*types.NanoTX, 0, maxTXsPerAcct)
		}
		byPrincipal[principal][mtx.AccountNonce] = append(byPrincipal[principal][mtx.AccountNonce], types.NewNanoTX(mtx))
	}
	return byPrincipal
}

// BuildFromScratch builds the cache from database.
func (c *cache) BuildFromScratch() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pending = make(map[types.Address]*accountCache)
	mtxs, err := c.tp.getAllPending()
	if err != nil {
		return err
	}

	byPrincipal := groupTXsByPrincipal(mtxs)
	for principal, nonce2TXs := range byPrincipal {
		c.createAcctIfNotPresent(principal)
		if err = c.pending[principal].addBatch(c.logger, c.tp, nonce2TXs); err != nil {
			return err
		}
		if c.pending[principal].shouldEvict() {
			// this should not happen
			c.logger.With().Warning("account has pending nonceTXs but none feasible",
				principal, log.Int("num_nonce", len(nonce2TXs)))
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
			availBalance: balance,
			txsByNonce:   make([]*sameNonceTXs, 0, maxTXsPerAcct),
			cachedTXs:    c.cachedTXs,
		}
	}
}

// Add adds a tx to the cache.
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
// If a transaction is included in multiple proposals/blocks in different layers, the lowest layer is retained.
func (c *cache) AddToLayer(lid types.LayerID, tids []types.TransactionID) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, tid := range tids {
		if _, ok := c.cachedTXs[tid]; !ok {
			// TODO: get from db. if not in db, fetch it form peers
			c.logger.With().Warning("tx not in cache", tid)
			return errNotInCache
		}
		c.cachedTXs[tid].UpdateLayerMaybe(lid)
	}
	return nil
}

// ApplyLayer retires the applied transactions from the cache and updates the balances.
func (c *cache) ApplyLayer(lid types.LayerID, bid types.BlockID, tids []types.TransactionID) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	byPrincipal := make(map[types.Address]map[uint64]*types.NanoTX)
	for _, tid := range tids {
		if _, ok := c.cachedTXs[tid]; !ok {
			// TODO: get from db. if not in db, fetch it form peers
			c.logger.With().Warning("tx not in cache", tid)
			return errNotInCache
		}
		ntx := c.cachedTXs[tid]
		principal := ntx.Principal
		if _, ok := byPrincipal[principal]; !ok {
			byPrincipal[principal] = make(map[uint64]*types.NanoTX)
		}
		if _, ok := byPrincipal[principal][ntx.Nonce]; ok {
			return errDupNonceApplied
		}
		byPrincipal[principal][ntx.Nonce] = ntx
	}

	for principal, appliedTXs := range byPrincipal {
		nextNonce := c.state.GetNonce(principal)
		balance := c.state.GetBalance(principal)
		err := c.pending[principal].applyLayer(c.logger, nextNonce, balance, c.tp, lid, bid, appliedTXs)
		if err != nil {
			return err
		}

		if c.pending[principal].shouldEvict() {
			delete(c.pending, principal)
		}
	}
	return nil
}

func (c *cache) GetProjection(addr types.Address) (uint64, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.pending[addr]; !ok {
		return c.state.GetNonce(addr), c.state.GetBalance(addr)
	}
	return c.pending[addr].nextNonce(), c.pending[addr].availBalance
}

func (c *cache) getMempool() (map[types.Address][]*types.NanoTX, error) {
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
