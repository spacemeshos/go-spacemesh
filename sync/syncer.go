package sync

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/types"
	"sync"
	"sync/atomic"
	"time"
)

type MemPool interface {
	Get(id interface{}) interface{}
	PopItems(size int) interface{}
	Put(id interface{}, item interface{})
	Invalidate(id interface{})
}

type BlockValidator interface {
	BlockEligible(block *types.BlockHeader) (bool, error)
	ValidateTx(tx *types.SerializableTransaction) (bool, error)
	ValidateAtx(atx *types.ActivationTx) error
}

type EligibilityValidator interface {
	BlockEligible(block *types.BlockHeader) (bool, error)
}

type TxValidator interface {
	ValidateTx(tx *types.SerializableTransaction) (bool, error)
}

type AtxValidator interface {
	ValidateAtx(atx *types.ActivationTx) error
}

type blockValidator struct {
	EligibilityValidator
	TxValidator
	AtxValidator
}

func NewBlockValidator(bev EligibilityValidator, txv TxValidator, atxv AtxValidator) BlockValidator {
	return &blockValidator{bev, txv, atxv}
}

type Configuration struct {
	Concurrency    int //number of workers for sync method
	LayerSize      int
	RequestTimeout time.Duration
}

type Syncer struct {
	p2p.Peers
	*mesh.Mesh
	BlockValidator
	Configuration
	log.Log
	*server.MessageServer
	txpool            MemPool
	atxpool           MemPool
	currentLayer      types.LayerID
	SyncLock          uint32
	startLock         uint32
	forceSync         chan bool
	clock             timesync.LayerTimer
	exit              chan struct{}
	currentLayerMutex sync.RWMutex
}

func (s *Syncer) ForceSync() {
	s.forceSync <- true
}

func (s *Syncer) Close() {
	s.Info("syncer closing, waiting for gorutines")
	close(s.exit)
	close(s.forceSync)
	s.MessageServer.Close()
	s.Peers.Close()
	s.Info("syncer closed")
}

const (
	IDLE         uint32             = 0
	RUNNING      uint32             = 1
	MINI_BLOCK   server.MessageType = 1
	LAYER_HASH   server.MessageType = 2
	LAYER_IDS    server.MessageType = 3
	TX           server.MessageType = 4
	ATX          server.MessageType = 5
	syncProtocol                    = "/sync/1.0/"
)

func (s *Syncer) IsSynced() bool {
	return s.ValidatedLayer()+1 == s.maxSyncLayer()
}

func (s *Syncer) Start() {
	if atomic.CompareAndSwapUint32(&s.startLock, 0, 1) {
		go s.run()
		s.forceSync <- true
		return
	}
}

//fires a sync every sm.syncInterval or on force space from outside
func (s *Syncer) run() {
	syncRoutine := func() {
		if atomic.CompareAndSwapUint32(&s.SyncLock, IDLE, RUNNING) {
			s.Synchronise()
			atomic.StoreUint32(&s.SyncLock, IDLE)
		}
	}
	for {
		select {
		case <-s.exit:
			s.Debug("Work stoped")
			return
		case <-s.forceSync:
			go syncRoutine()
		case layer := <-s.clock:
			s.currentLayerMutex.Lock()
			s.currentLayer = layer
			s.currentLayerMutex.Unlock()
			s.Debug("sync got tick for layer %v", layer)
			go syncRoutine()
		}
	}
}

//fires a sync every sm.syncInterval or on force space from outside
func NewSync(srv service.Service, layers *mesh.Mesh, txpool MemPool, atxpool MemPool, bv BlockValidator, conf Configuration, clock timesync.LayerTimer, logger log.Log) *Syncer {
	s := &Syncer{
		BlockValidator: bv,
		Configuration:  conf,
		Log:            logger,
		Mesh:           layers,
		Peers:          p2p.NewPeers(srv, logger.WithName("peers")),
		MessageServer:  server.NewMsgServer(srv.(server.Service), syncProtocol, conf.RequestTimeout, make(chan service.DirectMessage, config.ConfigValues.BufferSize), logger.WithName("srv")),
		SyncLock:       0,
		txpool:         txpool,
		atxpool:        atxpool,
		startLock:      0,
		forceSync:      make(chan bool),
		clock:          clock,
		exit:           make(chan struct{}),
	}

	s.RegisterBytesMsgHandler(LAYER_HASH, newLayerHashRequestHandler(layers, logger))
	s.RegisterBytesMsgHandler(MINI_BLOCK, newMiniBlockRequestHandler(layers, logger))
	s.RegisterBytesMsgHandler(LAYER_IDS, newLayerBlockIdsRequestHandler(layers, logger))
	s.RegisterBytesMsgHandler(TX, newTxsRequestHandler(s, logger))
	s.RegisterBytesMsgHandler(ATX, newATxsRequestHandler(s, logger))
	return s
}

func (s *Syncer) maxSyncLayer() types.LayerID {
	defer s.currentLayerMutex.RUnlock()
	s.currentLayerMutex.RLock()
	return s.currentLayer
}

func (s *Syncer) Synchronise() {
	mu := sync.Mutex{}
	for currentSyncLayer := s.ValidatedLayer() + 1; currentSyncLayer < s.maxSyncLayer(); currentSyncLayer++ {
		s.Info("syncing layer %v to layer %v current consensus layer is %d", s.ValidatedLayer(), currentSyncLayer, s.currentLayer)
		lyr, err := s.GetLayer(types.LayerID(currentSyncLayer))
		if err != nil {
			s.Info("layer %v is not in the database", currentSyncLayer)
			if lyr, err = s.getLayerFromNeighbors(currentSyncLayer); err != nil {
				s.Info("could not get layer %v from neighbors %v", currentSyncLayer, err)
				return
			}
		}

		mu.Lock()
		go func() {
			s.ValidateLayer(lyr) //run one at a time
			mu.Unlock()
		}()
	}
}

func (s *Syncer) getLayerFromNeighbors(currenSyncLayer types.LayerID) (*types.Layer, error) {

	//fetch layer hash from each peer
	m, err := s.fetchLayerHashes(currenSyncLayer)

	if err != nil {
		s.Error("could not get LayerHashes for layer: %v", currenSyncLayer)
		return nil, err
	}

	//fetch ids for each hash
	blockIds, err := s.fetchLayerBlockIds(m, currenSyncLayer)
	if err != nil {
		s.Error("could not get layer block ids %v", currenSyncLayer, err)
		return nil, err
	}

	blocksArr, err := s.FetchFullBlocks(blockIds)
	if err != nil {
		s.Error("could not get layer blocks %v", currenSyncLayer, err)
		return nil, err
	}

	return types.NewExistingLayer(types.LayerID(currenSyncLayer), blocksArr), nil
}

func (s *Syncer) FetchFullBlocks(blockIds []types.BlockID) ([]*types.Block, error) {
	output := s.fetchWithFactory(BlockReqFactory(blockSliceToChan(blockIds)), s.Concurrency)
	blocksArr := make([]*types.Block, 0, len(blockIds))
	for out := range output {
		block := out.(*types.Block)
		if err := s.SyncAndValidate(block); err != nil {
			s.Error(fmt.Sprintf("failed derefrencing block data %v %v", block.ID(), err))
			continue
		}

		s.Info("added block to layer %v", block.Layer())
		blocksArr = append(blocksArr, block)
	}

	return blocksArr, nil
}

func (s *Syncer) SyncAndValidate(blk *types.Block) error {

	//check block signature
	s.ValidateSignature(blk)

	//is identity active
	if eligable, err := s.IsIdentityActive(blk.MinerID.Key, blk.Layer()); err != nil || !eligable {
		return errors.New(fmt.Sprintf("block %v identity activation check failed ", blk.ID()))
	}

	//associated atx data availability
	if blk.ATXID != *types.EmptyAtxId {
		if _, err := s.syncAtxs([]types.AtxId{blk.ATXID}); err != nil {
			return errors.New(fmt.Sprintf("cannot fetch associated atx for block %v %v ", blk.ID(), err))
		}
	}

	//block eligibility
	if eligable, err := s.BlockEligible(&blk.BlockHeader); err != nil || !eligable {
		return errors.New(fmt.Sprintf("block %v eligablety check failed ", blk.ID()))
	}

	//data availability
	txs, atxs, err := s.syncMissingContent(blk)
	if err != nil {
		return errors.New(fmt.Sprintf("data availabilty failed for block %v", blk.ID()))
	}

	//validate blocks view
	if valid := s.ValidateView(blk); valid == false {
		return errors.New(fmt.Sprintf("could not validate for block %v view ", blk.ID()))
	}

	if err := s.AddBlockWithTxs(blk, txs, atxs); err != nil {
		s.Warning(fmt.Sprintf("failed fetching block %v activation transactions %v", blk.ID(), err))
		return err
	}

	s.Info(fmt.Sprintf("added block with txs to database %v", blk.ID()))

	return nil
}

func (s *Syncer) ValidateView(blk *types.Block) bool {
	var allBlocksInDb = true

	//check local database for all blocks in view
	for _, bid := range blk.BlockHeader.ViewEdges {
		if _, err := s.GetBlock(bid); err != nil {
			allBlocksInDb = false
			break
		}
	}

	//crawl mesh and check local block syntactic validity return true
	if !allBlocksInDb {
		if bks := s.crwal(NewValidationQueue(blk)); len(bks) > 0 {
			s.Warning(fmt.Sprintf("could not validate %v blocks in block %v view ", len(bks) > 0, blk.ID()))
			return false
		}
	}

	return true
}

func (s *Syncer) syncMissingContent(blk *types.Block) ([]*types.SerializableTransaction, []*types.ActivationTx, error) {
	var txs []*types.SerializableTransaction
	var txerr error
	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		//sync Transactions
		txs, txerr = s.syncTxs(blk.TxIds)
		wg.Done()
	}()

	var atxs []*types.ActivationTx
	var atxerr error

	//sync ATxs
	go func() {
		atxs, atxerr = s.syncAtxs(blk.AtxIds)
		wg.Done()
	}()

	wg.Wait()

	if txerr != nil {
		s.Warning(fmt.Sprintf("failed fetching block %v transactions %v", blk.ID(), txerr))
		return txs, atxs, txerr
	}

	if atxerr != nil {
		s.Warning(fmt.Sprintf("failed fetching block %v activation transactions %v", blk.ID(), atxerr))
		return txs, atxs, atxerr
	}

	s.Info(fmt.Sprintf("fetched all txs for block %v", blk.ID()))
	s.Info(fmt.Sprintf("fetched all atxs for block %v", blk.ID()))

	return txs, atxs, nil
}

func (s *Syncer) fetchLayerBlockIds(m map[string]p2p.Peer, lyr types.LayerID) ([]types.BlockID, error) {
	//send request to different users according to returned hashes
	v := make([]p2p.Peer, 0, len(m))
	for _, value := range m {
		v = append(v, value)
	}

	wrk, output := NewPeersWorker(s, v, &sync.Once{}, LayerIdsReqFactory(lyr))
	go wrk.Work()

	out := <-output
	if out == nil {
		return nil, errors.New("could not get layer ids from any peer")
	}

	idSet := make(map[types.BlockID]struct{}, s.LayerSize)
	ids := make([]types.BlockID, 0, s.LayerSize)

	//filter double ids
	for _, bid := range out.([]types.BlockID) {
		if _, exists := idSet[bid]; !exists {
			idSet[bid] = struct{}{}
			ids = append(ids, bid)
		}
	}

	return ids, nil
}

type peerHashPair struct {
	peer p2p.Peer
	hash []byte
}

func (s *Syncer) fetchLayerHashes(lyr types.LayerID) (map[string]p2p.Peer, error) {
	// get layer hash from each peer
	wrk, output := NewPeersWorker(s, s.GetPeers(), &sync.Once{}, HashReqFactory(lyr))
	go wrk.Work()
	m := make(map[string]p2p.Peer)
	for out := range output {
		pair := out.(*peerHashPair)
		if pair == nil { //do nothing on close channel
			continue
		}
		m[string(pair.hash)] = pair.peer
	}
	if len(m) == 0 {
		return nil, errors.New("could not get layer hashes from any peer")
	}
	return m, nil
}

func (s *Syncer) fetchWithFactory(refac RequestFactory, workers int) chan interface{} {
	// each worker goroutine tries to fetch data iteratively from each peer

	wrk := NewNeighborhoodWorker(s, workers, refac)
	go wrk.Work()

	for i := 0; i < workers-1; i++ {
		cloneWrk := wrk.Clone()
		go cloneWrk.Work()
	}

	return wrk.output
}

func (s *Syncer) syncTxs(txids []types.TransactionId) ([]*types.SerializableTransaction, error) {

	//look in pool
	poolTxs := make(map[types.TransactionId]*types.SerializableTransaction)
	missing := make([]types.TransactionId, 0)
	for _, t := range txids {
		if tx := s.txpool.Get(t); tx != nil {
			s.Info("found tx, %v in tx pool", hex.EncodeToString(t[:]))
			poolTxs[t] = tx.(*types.SerializableTransaction)
		} else {
			missing = append(missing, t)
		}
	}

	//look in db
	dbTxs, missinDB := s.GetTransactions(missing)

	//map and sort txs
	if len(missing) > 0 {
		for out := range s.fetchWithFactory(TxReqFactory(missinDB), 1) {
			ntxs := out.([]types.SerializableTransaction)
			for _, tx := range ntxs {
				if valid, err := s.ValidateTx(&tx); !valid || err != nil {
					id := types.GetTransactionId(&tx)
					s.Warning("tx %v not valid %v", hex.EncodeToString(id[:]), err)
					continue
				}
				poolTxs[types.GetTransactionId(&tx)] = &tx
			}
		}
	}

	txs := make([]*types.SerializableTransaction, 0, len(txids))
	for _, id := range txids {
		if tx, ok := poolTxs[id]; ok {
			txs = append(txs, tx)
		} else if tx, ok := dbTxs[id]; ok {
			txs = append(txs, tx)
		} else {
			return nil, errors.New(fmt.Sprintf("could not fetch tx %v", hex.EncodeToString(id[:])))
		}
	}
	return txs, nil
}

func (s *Syncer) syncAtxs(atxIds []types.AtxId) (atxs []*types.ActivationTx, err error) {

	//look in pool
	poolAtxs := make(map[types.AtxId]*types.ActivationTx, len(atxs))
	missingInPool := make([]types.AtxId, 0, len(atxs))
	for _, t := range atxIds {
		id := t
		if x := s.atxpool.Get(id); x != nil {
			atx := x.(*types.ActivationTx)
			if atx.Nipst == nil {
				s.Warning("atx %v nipst not found ", id.ShortId())
				missingInPool = append(missingInPool, id)
				continue
			}
			s.Info("found atx, %v in atx pool", id.ShortId())
			poolAtxs[id] = atx
		} else {
			s.Warning("atx %v not in atx pool", id.ShortId())
			missingInPool = append(missingInPool, id)
		}
	}

	//look in db
	dbAtxs, missing := s.GetATXs(missingInPool)

	//map and sort atxs
	if len(missing) > 0 {
		output := s.fetchWithFactory(ATxReqFactory(missing), 1)
		for out := range output {
			atxs := out.([]types.ActivationTx)
			for _, atx := range atxs {
				if err := s.ValidateAtx(&atx); err != nil {
					s.Warning("atx %v not valid %v", atx.ShortId(), err)
					continue
				}
				poolAtxs[atx.Id()] = &atx
			}
		}
	}

	atxs = make([]*types.ActivationTx, 0, len(atxIds))
	for _, id := range atxIds {
		if tx, ok := poolAtxs[id]; ok {
			atxs = append(atxs, tx)
		} else if tx, ok := dbAtxs[id]; ok {
			atxs = append(atxs, tx)
		} else {
			return nil, errors.New(fmt.Sprintf("could not fetch atx %v", id.ShortId()))
		}
	}
	return atxs, nil
}

func (s *Syncer) crwal(vq *validationQueue) []types.BlockID {
	ch := make(chan types.BlockID, 10)
	output := s.fetchWithFactory(BlockReqFactory(ch), 4)
	go func() {
		for out := range output {
			block := out.(*types.Block)
			if err := s.SyncAndValidate(block); err != nil {
				s.Error(fmt.Sprintf("failed derefrencing block data %v %v", block.ID(), err))
				continue
			}
			if vq.updateDependencies(&block.BlockHeader, s.GetBlock) {

				//remove this block from all dependencies lists
				for _, b := range vq.reverseDepMap[block.ID()] {

					//we can update recursively all dependencies or just wait for the queue to finish processing
					delete(vq.depMap[b], block.ID())
				}

				//remove this block from reverse dependency map
				delete(vq.reverseDepMap, block.ID())
			}
		}
	}()

	for {
		if vq.Size() == 0 {
			s.Info("validation queue empty ")
			break
		}

		b := vq.PopItem()
		if b == nil {
			close(ch)
			s.Info(fmt.Sprintf("mesh crewler queue is empty"))
		}
		blk := b.(types.BlockID)
		ch <- blk

	}
	return vq.getMissingBlocks()
}

func (s *Syncer) ValidateSignature(lyr *types.Block) bool {
	return true
}
