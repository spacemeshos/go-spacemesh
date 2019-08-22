package sync

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	p2pconf "github.com/spacemeshos/go-spacemesh/p2p/config"

	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/types"
	"sync"
	"sync/atomic"
	"time"
)

type TxMemPool interface {
	Get(id types.TransactionId) (types.AddressableSignedTransaction, error)
	PopItems(size int) []types.AddressableSignedTransaction
	Put(id types.TransactionId, item *types.AddressableSignedTransaction)
	Invalidate(id types.TransactionId)
}

type AtxMemPool interface {
	Get(id types.AtxId) (types.ActivationTx, error)
	PopItems(size int) []types.ActivationTx
	Put(id types.AtxId, item *types.ActivationTx)
	Invalidate(id types.AtxId)
}

type PoetDb interface {
	HasProof(proofRef []byte) bool
	ValidateAndStore(proofMessage *types.PoetProofMessage) error
	GetProofMessage(proofRef []byte) ([]byte, error)
}

type BlockValidator interface {
	BlockSignedAndEligible(block *types.Block) (bool, error)
}

type EligibilityValidator interface {
	BlockSignedAndEligible(block *types.Block) (bool, error)
}

type TxValidator interface {
	GetValidAddressableTx(tx *types.SerializableSignedTransaction) (*types.AddressableSignedTransaction, error)
}

type clockSubscriber interface {
	Subscribe() timesync.LayerTimer
	Unsubscribe(timer timesync.LayerTimer)
}

type Configuration struct {
	LayersPerEpoch uint16
	Concurrency    int //number of workers for sync method
	LayerSize      int
	RequestTimeout time.Duration
	Hdist          int
}

type LayerValidator interface {
	ValidatedLayer() types.LayerID
	ValidateLayer(lyr *types.Layer)
}

type LayerProvider interface {
	GetLayer(index types.LayerID) (*types.Layer, error)
}

type Syncer struct {
	p2p.Peers
	*mesh.Mesh
	BlockValidator
	Configuration
	log.Log
	*server.MessageServer

	lValidator        LayerValidator
	lProvider         LayerProvider
	txValidator       TxValidator
	poetDb            PoetDb
	txpool            TxMemPool
	atxpool           AtxMemPool
	currentLayer      types.LayerID
	SyncLock          uint32
	startLock         uint32
	forceSync         chan bool
	clockSub          clockSubscriber
	clock             timesync.LayerTimer
	exit              chan struct{}
	currentLayerMutex sync.RWMutex
	syncRoutineWg     sync.WaitGroup
	p2pSynced         bool // true if we are p2p-synced, false otherwise
	p2pLock           sync.RWMutex
}

func (s *Syncer) ForceSync() {
	s.forceSync <- true
}

func (s *Syncer) Close() {
	s.Info("Closing syncer")
	close(s.exit)
	close(s.forceSync)
	// TODO: broadly implement a better mechanism for shutdown
	time.Sleep(5 * time.Millisecond) // "ensures" no more sync routines can be created, ok for now
	s.syncRoutineWg.Wait()           // must be called after we ensure no more sync routines can be created
	s.MessageServer.Close()
	s.Peers.Close()
}

const (
	IDLE         uint32             = 0
	RUNNING      uint32             = 1
	MINI_BLOCK   server.MessageType = 1
	LAYER_HASH   server.MessageType = 2
	LAYER_IDS    server.MessageType = 3
	TX           server.MessageType = 4
	ATX          server.MessageType = 5
	POET         server.MessageType = 6
	syncProtocol                    = "/sync/1.0/"
)

func (s *Syncer) WeaklySynced() bool {
	// equivalent to s.LatestLayer() >= s.lastTickedLayer()-1
	// means we have data from the previous layer
	return s.LatestLayer()+1 >= s.lastTickedLayer()
}

func (s *Syncer) getP2pSynced() bool {
	s.p2pLock.RLock()
	b := s.p2pSynced
	s.p2pLock.RUnlock()

	return b
}

func (s *Syncer) setP2pSynced(b bool) {
	s.p2pLock.Lock()
	s.p2pSynced = b
	s.p2pLock.Unlock()
}

func (s *Syncer) IsSynced() bool {
	s.Log.Info("latest: %v, maxSynced %v", s.LatestLayer(), s.lastTickedLayer())
	return s.WeaklySynced() && s.getP2pSynced()
}

func (s *Syncer) Start() {
	if atomic.CompareAndSwapUint32(&s.startLock, 0, 1) {
		go s.run()
		s.forceSync <- true
		return
	}
}

func (s *Syncer) getSyncRoutine() func() {
	return func() {
		if atomic.CompareAndSwapUint32(&s.SyncLock, IDLE, RUNNING) {
			s.syncRoutineWg.Add(1)
			s.Synchronise()
			atomic.StoreUint32(&s.SyncLock, IDLE)
		}
	}
}

//fires a sync every sm.syncInterval or on force space from outside
func (s *Syncer) run() {
	syncRoutine := s.getSyncRoutine()
	for {
		select {
		case <-s.exit:
			s.Debug("Work stopped")
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
func NewSync(srv service.Service, layers *mesh.Mesh, txpool TxMemPool, atxpool AtxMemPool, tv TxValidator, bv BlockValidator, poetdb PoetDb, conf Configuration, clockSub clockSubscriber, currentLayer types.LayerID, logger log.Log) *Syncer {
	s := &Syncer{
		BlockValidator: bv,
		Configuration:  conf,
		Log:            logger,
		Mesh:           layers,
		Peers:          p2p.NewPeers(srv, logger.WithName("peers")),
		MessageServer:  server.NewMsgServer(srv.(server.Service), syncProtocol, conf.RequestTimeout, make(chan service.DirectMessage, p2pconf.ConfigValues.BufferSize), logger.WithName("srv")),
		lValidator:     layers,
		lProvider:      layers,
		txValidator:    tv,
		SyncLock:       0,
		txpool:         txpool,
		atxpool:        atxpool,
		poetDb:         poetdb,
		startLock:      0,
		currentLayer:   currentLayer,
		forceSync:      make(chan bool),
		clockSub:       clockSub,
		clock:          clockSub.Subscribe(),
		exit:           make(chan struct{}),
		p2pSynced:      false,
	}

	s.RegisterBytesMsgHandler(LAYER_HASH, newLayerHashRequestHandler(layers, logger))
	s.RegisterBytesMsgHandler(MINI_BLOCK, newBlockRequestHandler(layers, logger))
	s.RegisterBytesMsgHandler(LAYER_IDS, newLayerBlockIdsRequestHandler(layers, logger))
	s.RegisterBytesMsgHandler(TX, newTxsRequestHandler(s, logger))
	s.RegisterBytesMsgHandler(ATX, newATxsRequestHandler(s, logger))
	s.RegisterBytesMsgHandler(POET, newPoetRequestHandler(s, logger))

	return s
}

func (s *Syncer) lastTickedLayer() types.LayerID {
	s.currentLayerMutex.RLock()
	curr := s.currentLayer
	s.currentLayerMutex.RUnlock()
	return curr
}

func (s *Syncer) Synchronise() {
	defer s.syncRoutineWg.Done()

	if s.lastTickedLayer() <= 1 { // skip validation for first layer
		s.With().Info("Not syncing in layer <= 1", log.LayerId(uint64(s.lastTickedLayer())))
		s.setP2pSynced(true) // fully-synced, make sure we listen to p2p
		return
	}

	currentSyncLayer := s.lValidator.ValidatedLayer() + 1
	if currentSyncLayer == s.lastTickedLayer() { // only validate if current < lastTicked
		s.With().Info("Already synced for layer", log.Uint64("current_sync_layer", uint64(currentSyncLayer)))
		return
	}

	if s.WeaklySynced() { // we have all the data of the prev layers so we can simply validate
		s.With().Info("Node is synced. Going to validate layer", log.LayerId(uint64(currentSyncLayer)))

		lyr, err := s.lProvider.GetLayer(currentSyncLayer)
		if err != nil {
			s.Panic("failed getting layer even though we are weakly-synced currentLayer=%v lastTicked=%v err=%v ", currentSyncLayer, s.lastTickedLayer(), err)
			return
		}
		s.lValidator.ValidateLayer(lyr) // wait for layer validation
		return
	}

	// node is not synced
	s.Info("Node is out of sync setting p2p-synced to false and starting sync")
	s.setP2pSynced(false) // don't listen to p2p while not synced

	// first, bring all the data of the prev layers
	// Note: lastTicked() is not constant but updates as ticks are received
	for ; currentSyncLayer < s.lastTickedLayer(); currentSyncLayer++ {
		s.With().Info("syncing layer", log.Uint64("current_sync_layer", uint64(currentSyncLayer)), log.Uint64("last_ticked_layer", uint64(s.lastTickedLayer())))
		lyr, err := s.getLayerFromNeighbors(currentSyncLayer)
		if err != nil {
			s.Info("could not get layer %v from neighbors %v", currentSyncLayer, err)
			return
		}

		s.lValidator.ValidateLayer(lyr) // wait for layer validation
	}

	// Now we are somewhere in the layer (begin, middle, end)
	// fetch what you can from the neighbors
	_, err := s.getLayerFromNeighbors(currentSyncLayer)
	if err != nil {
		s.With().Info("could not get last ticked layer from neighbors", log.LayerId(uint64(currentSyncLayer)), log.Err(err))
		return
	}

	// wait for two ticks to ensure we are fully synced before we open p2p or validate the current layer
	err = s.p2pSyncForOneFullLayer(currentSyncLayer)
	if err != nil {
		s.With().Error("Fatal: failed getting layer even though we listened to p2p", log.LayerId(uint64(currentSyncLayer)), log.Err(err))
	}
}

// Waits two ticks (while weakly-synced) in order to ensure that we listened to p2p for one full layer
// after that we are assumed to have all the data required for validation so we can validate and open p2p
// opening p2p in weakly-synced transition us to fully-synced
func (s *Syncer) p2pSyncForOneFullLayer(currentSyncLayer types.LayerID) error {
	// subscribe and wait for two ticks
	ch := s.clockSub.Subscribe()
	<-ch
	<-ch
	s.clockSub.Unsubscribe(ch) // unsub, we won't be listening on this ch anymore

	// assumed to be weakly synced here
	// just get the layers and validate

	// get & validate first tick
	lyr, err := s.lProvider.GetLayer(currentSyncLayer)
	if err != nil {
		return err
	}
	s.lValidator.ValidateLayer(lyr)

	// get & validate second tick
	currentSyncLayer++
	lyr, err = s.lProvider.GetLayer(currentSyncLayer)
	if err != nil {
		return err
	}
	s.lValidator.ValidateLayer(lyr)
	s.Info("Done waiting for ticks and validation. setting p2p true")

	// fully-synced - set p2p-synced to true
	s.setP2pSynced(true)

	return nil
}

func (s *Syncer) getLayerFromNeighbors(currenSyncLayer types.LayerID) (*types.Layer, error) {

	//fetch layer hash from each peer
	m, err := s.fetchLayerHashes(currenSyncLayer)
	if err != nil {
		return nil, err
	}

	//fetch ids for each hash
	blockIds, err := s.fetchLayerBlockIds(m, currenSyncLayer)
	if err != nil {
		return nil, err
	}

	blocksArr := s.GetFullBlocks(blockIds)
	if len(blocksArr) == 0 {
		return nil, fmt.Errorf("could not any blocks  for layer  %v", currenSyncLayer)
	}

	return types.NewExistingLayer(types.LayerID(currenSyncLayer), blocksArr), nil
}

func (s *Syncer) GetFullBlocks(blockIds []types.BlockID) []*types.Block {
	output := s.fetchWithFactory(NewBlockWorker(s, s.Concurrency, BlockReqFactory(), blockSliceToChan(blockIds)))
	blocksArr := make([]*types.Block, 0, len(blockIds))
	for out := range output {
		//ignore blocks that we could not fetch

		block, ok := out.(*types.Block)
		if block != nil && ok {
			txs, atxs, err := s.BlockSyntacticValidation(block)
			if err != nil {
				s.Error("failed to validate block %v %v", block.ID(), err)
				continue
			}

			if err := s.AddBlockWithTxs(block, txs, atxs); err != nil {
				s.Error("failed to add block %v to database %v", block.ID(), err)
				continue
			}

			s.Info("added block %v to layer %v", block.ID(), block.Layer())
			blocksArr = append(blocksArr, block)
		}
	}

	s.Info("done getting full blocks")
	return blocksArr
}

func (s *Syncer) BlockSyntacticValidation(block *types.Block) ([]*types.AddressableSignedTransaction, []*types.ActivationTx, error) {

	//block eligibility
	if eligable, err := s.BlockSignedAndEligible(block); err != nil || !eligable {
		return nil, nil, errors.New(fmt.Sprintf("block %v eligablety check failed %v", block.ID(), err))
	}

	//data availability
	txs, atxs, err := s.DataAvailabilty(block)
	if err != nil {
		return nil, nil, errors.New(fmt.Sprintf("data availabilty failed for block %v", block.ID()))
	}

	//validate block's view
	if valid := s.ValidateView(block); valid == false {
		return nil, nil, errors.New(fmt.Sprintf("block %v not syntacticly valid", block.ID()))
	}

	//validate block's votes
	if valid := s.validateVotes(block); valid == false {
		return nil, nil, errors.New(fmt.Sprintf("validate votes failed for block %v", block.ID()))
	}

	return txs, atxs, nil
}

func (s *Syncer) ValidateView(blk *types.Block) bool {
	vq := NewValidationQueue(s.Log.WithName("validQ"))
	if err := vq.traverse(s, &blk.BlockHeader); err != nil {
		s.Warning("could not validate %v view %v", blk.ID(), err)
		return false
	}
	return true
}

func (s *Syncer) validateVotes(blk *types.Block) bool {
	view := map[types.BlockID]struct{}{}
	for _, blk := range blk.ViewEdges {
		view[blk] = struct{}{}
	}

	vote := map[types.BlockID]struct{}{}
	for _, blk := range blk.BlockVotes {
		vote[blk] = struct{}{}
	}

	traverse := func(b *types.Block) (stop bool, err error) {
		if _, ok := vote[b.ID()]; ok {
			delete(vote, b.ID())
		}
		return len(vote) == 0, nil
	}

	// traverse only through the last Hdist layers
	lowestLayer := blk.LayerIndex - types.LayerID(s.Hdist)
	if blk.LayerIndex < types.LayerID(s.Hdist) {
		lowestLayer = 0
	}
	err := s.ForBlockInView(view, lowestLayer, traverse)
	if err == nil && len(vote) > 0 {
		err = fmt.Errorf("voting on blocks out of view (or out of Hdist), %v", vote)
	}
	s.Log.With().Info("validateVotes done", log.Err(err))

	return err == nil
}

func (s *Syncer) DataAvailabilty(blk *types.Block) ([]*types.AddressableSignedTransaction, []*types.ActivationTx, error) {
	var txs []*types.AddressableSignedTransaction
	var txerr error
	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		//sync Transactions
		txs, txerr = s.syncTxs(blk.TxIds)
		for _, tx := range txs {
			id := types.GetTransactionId(tx.SerializableSignedTransaction)
			s.txpool.Put(id, tx)
		}
		wg.Done()
	}()

	var atxs []*types.ActivationTx
	var atxerr error

	//sync ATxs
	go func() {
		atxs, atxerr = s.syncAtxs(blk.AtxIds)
		for _, atx := range atxs {
			s.atxpool.Put(atx.Id(), atx)
		}
		wg.Done()
	}()

	wg.Wait()

	if txerr != nil {
		s.Warning("failed fetching block %v transactions %v", blk.ID(), txerr)
		return txs, atxs, txerr
	}

	if atxerr != nil {
		s.Warning("failed fetching block %v activation transactions %v", blk.ID(), atxerr)
		return txs, atxs, atxerr
	}

	s.Info("fetched all block data %v %v txs %v atxs", blk.ID())
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

	idSet := make(map[types.BlockID]struct{}, s.LayerSize)
	ids := make([]types.BlockID, 0, s.LayerSize)

	//unify results
	for out := range output {
		if out != nil {
			//filter double ids
			for _, bid := range out.([]types.BlockID) {
				if _, exists := idSet[bid]; !exists {
					idSet[bid] = struct{}{}
					ids = append(ids, bid)
				}
			}
		}
	}

	if len(ids) == 0 {
		return nil, errors.New("could not get layer ids from any peer")
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
		pair, ok := out.(*peerHashPair)
		if pair != nil && ok { //do nothing on close channel
			m[string(pair.hash)] = pair.peer
		}
	}
	if len(m) == 0 {
		return nil, errors.New("could not get layer hashes from any peer")
	}
	return m, nil
}

//returns txs out of txids that are not in the local database
func (s *Syncer) syncTxs(txids []types.TransactionId) ([]*types.AddressableSignedTransaction, error) {

	//look in pool
	unprocessedTxs := make(map[types.TransactionId]*types.AddressableSignedTransaction)
	missing := make([]types.TransactionId, 0)
	for _, t := range txids {
		if tx, err := s.txpool.Get(t); err == nil {
			s.Debug("found tx, %v in tx pool", hex.EncodeToString(t[:]))
			unprocessedTxs[t] = &tx
		} else {
			missing = append(missing, t)
		}
	}

	//look in db
	dbTxs, missinDB := s.GetTransactions(missing)

	//map and sort txs
	if len(missinDB) > 0 {
		for out := range s.fetchWithFactory(NewNeighborhoodWorker(s, 1, TxReqFactory(missinDB))) {
			ntxs := out.([]types.SerializableSignedTransaction)
			for _, tx := range ntxs {
				tmp := tx
				ast, err := s.txValidator.GetValidAddressableTx(&tmp)
				if err != nil {
					id := types.GetTransactionId(&tmp)
					s.Warning("tx %v not valid %v", hex.EncodeToString(id[:]), err)
					continue
				}
				unprocessedTxs[types.GetTransactionId(&tmp)] = ast
			}
		}
	}

	txs := make([]*types.AddressableSignedTransaction, 0, len(txids))
	for _, id := range txids {
		if tx, ok := unprocessedTxs[id]; ok {
			txs = append(txs, tx)
		} else if _, ok := dbTxs[id]; ok {
			continue
		} else {
			return nil, errors.New(fmt.Sprintf("could not fetch tx %v", hex.EncodeToString(id[:])))
		}
	}
	return txs, nil
}

//returns atxs out of txids that are not in the local database
func (s *Syncer) syncAtxs(atxIds []types.AtxId) ([]*types.ActivationTx, error) {

	//look in pool
	unprocessedAtxs := make(map[types.AtxId]*types.ActivationTx, len(atxIds))
	missingInPool := make([]types.AtxId, 0, len(atxIds))
	for _, t := range atxIds {
		id := t
		if x, err := s.atxpool.Get(id); err == nil {
			atx := x
			if atx.Nipst == nil {
				s.Warning("atx %v nipst not found ", id.ShortId())
				missingInPool = append(missingInPool, id)
				continue
			}

			s.Debug("found atx, %v in atx pool", id.ShortId())
			unprocessedAtxs[id] = &atx
		} else {
			s.Debug("atx %v not in atx pool", id.ShortId())
			missingInPool = append(missingInPool, id)
		}
	}

	//look in db
	dbAtxs, missingInDb := s.GetATXs(missingInPool)

	//map and sort atxs
	if len(missingInDb) > 0 {
		output := s.fetchWithFactory(NewNeighborhoodWorker(s, 1, ATxReqFactory(missingInDb)))
		for out := range output {
			atxs := out.([]types.ActivationTx)
			for _, atx := range atxs {
				if err := s.FetchPoetProof(atx.GetPoetProofRef()); err != nil {
					s.Error("received atx (%v) with syntactically invalid or missing PoET proof (%x): %v",
						atx.ShortId(), atx.GetShortPoetProofRef(), err)
					continue
				}

				if err := s.SyntacticallyValidateAtx(&atx); err != nil {
					s.Error("received an invalid atx (%v): %v", atx.ShortId(), err)
					continue
				}

				tmp := atx
				unprocessedAtxs[atx.Id()] = &tmp
			}
		}
	}

	atxs := make([]*types.ActivationTx, 0, len(atxIds))
	for _, id := range atxIds {
		if tx, ok := unprocessedAtxs[id]; ok {
			atxs = append(atxs, tx)
		} else if _, ok := dbAtxs[id]; ok {
			continue
		} else {
			return nil, errors.New(fmt.Sprintf("could not fetch atx %v", id.ShortId()))
		}
	}
	return atxs, nil
}

func (s *Syncer) fetchWithFactory(wrk worker) chan interface{} {
	// each worker goroutine tries to fetch a block iteratively from each peer
	go wrk.Work()
	for i := 0; int32(i) < *wrk.workCount-1; i++ {
		cloneWrk := wrk.Clone()
		go cloneWrk.Work()
	}

	return wrk.output
}

func (s *Syncer) FetchPoetProof(poetProofRef []byte) error {
	if !s.poetDb.HasProof(poetProofRef) {
		out := <-s.fetchWithFactory(NewNeighborhoodWorker(s, 1, PoetReqFactory(poetProofRef)))
		if out == nil {
			return fmt.Errorf("could not find PoET proof with any neighbor")
		}
		proofMessage := out.(types.PoetProofMessage)
		err := s.poetDb.ValidateAndStore(&proofMessage)
		if err != nil {
			return err
		}
	}
	return nil
}
