package sync

import (
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

type ForBlockInView func(view map[types.BlockID]struct{}, layer types.LayerID, blockHandler func(block *types.Block) (bool, error)) error

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

type Syncer struct {
	Configuration
	log.Log
	*mesh.Mesh
	EligibilityValidator
	*workerInfra
	poetDb            PoetDb
	txpool            TxMemPool
	atxpool           AtxMemPool
	lValidator        LayerValidator
	currentLayer      types.LayerID
	SyncLock          uint32
	startLock         uint32
	forceSync         chan bool
	clock             timesync.LayerTimer
	exit              chan struct{}
	currentLayerMutex sync.RWMutex
	syncRoutineWg     sync.WaitGroup

	//todo fetch server
	blockQueue *validationQueue
	txQueue    *txQueue
	atxQueue   *atxQueue
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
	s.blockQueue.done()
	s.MessageServer.Close()
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

func (s *Syncer) IsSynced() bool {
	s.Log.Info("latest: %v, maxSynced %v", s.LatestLayer(), s.lastTickedLayer())
	return s.LatestLayer()+1 >= s.lastTickedLayer()
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
func NewSync(srv service.Service, layers *mesh.Mesh, txpool TxMemPool, atxpool AtxMemPool, sv TxValidator, bv BlockValidator, poetdb PoetDb, conf Configuration, clock *timesync.Ticker, logger log.Log) *Syncer {

	srvr := &workerInfra{
		RequestTimeout: conf.RequestTimeout,
		MessageServer:  server.NewMsgServer(srv.(server.Service), syncProtocol, conf.RequestTimeout, make(chan service.DirectMessage, p2pconf.ConfigValues.BufferSize), logger.WithName("srv")),
		Peers:          p2p.NewPeers(srv, logger.WithName("peers")),
	}

	s := &Syncer{
		EligibilityValidator: bv,
		Configuration:        conf,
		Log:                  logger,
		Mesh:                 layers,
		workerInfra:          srvr,
		lValidator:           layers,
		SyncLock:             0,
		poetDb:               poetdb,
		txpool:               txpool,
		atxpool:              atxpool,
		startLock:            0,
		currentLayer:         clock.GetCurrentLayer(),
		forceSync:            make(chan bool),
		clock:                clock.Subscribe(),
		exit:                 make(chan struct{}),
	}

	s.blockQueue = NewValidationQueue(srvr, s.Configuration, s, logger.WithName("validQ"))
	s.txQueue = NewTxQueue(layers, srvr, txpool, sv, logger.WithName("txFetchQueue"))
	s.atxQueue = NewAtxQueue(layers, srvr, atxpool, s.FetchPoetProof, logger.WithName("atxFetchQueue"))

	srvr.RegisterBytesMsgHandler(LAYER_HASH, newLayerHashRequestHandler(layers, logger))
	srvr.RegisterBytesMsgHandler(MINI_BLOCK, newBlockRequestHandler(layers, logger))
	srvr.RegisterBytesMsgHandler(LAYER_IDS, newLayerBlockIdsRequestHandler(layers, logger))
	srvr.RegisterBytesMsgHandler(TX, newTxsRequestHandler(s, logger))
	srvr.RegisterBytesMsgHandler(ATX, newATxsRequestHandler(s, logger))
	srvr.RegisterBytesMsgHandler(POET, newPoetRequestHandler(s, logger))

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

	currentSyncLayer := s.lValidator.ValidatedLayer() + 1
	if s.lastTickedLayer() == 1 { // skip validation for first layer
		s.Info("Not syncing in layer 1")
		return
	}
	if s.IsSynced() {
		lyr, err := s.GetLayer(types.LayerID(currentSyncLayer))
		if err != nil {
			s.With().Error("failed getting layer even though IsSynced is true", log.Err(err))
			return
		}
		s.lValidator.ValidateLayer(lyr) // wait for layer validation
		return
	}
	for ; currentSyncLayer <= s.lastTickedLayer(); currentSyncLayer++ {
		lastTickedLayer := s.lastTickedLayer()
		if s.IsSynced() && currentSyncLayer == lastTickedLayer {
			continue
		}

		s.Info("syncing layer %v current consensus layer is %d", currentSyncLayer, lastTickedLayer)

		lyr, err := s.getLayerFromNeighbors(currentSyncLayer)
		if err != nil {
			s.Info("could not get layer %v from neighbors %v", currentSyncLayer, err)
			return
		}
		if currentSyncLayer < lastTickedLayer {
			s.lValidator.ValidateLayer(lyr) // wait for layer validation
		}
	}
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

	blocksArr, err := s.syncLayer(currenSyncLayer, blockIds)
	if len(blocksArr) == 0 || err != nil {
		return nil, fmt.Errorf("could get blocks for layer  %v", currenSyncLayer)
	}

	return types.NewExistingLayer(types.LayerID(currenSyncLayer), blocksArr), nil
}

func (s *Syncer) syncLayer(layerID types.LayerID, blockIds []types.BlockID) ([]*types.Block, error) {
	ch := make(chan bool, 1)
	foo := func(res bool) error {
		s.Info("layer %v done", layerID)
		ch <- res
		return nil
	}

	if res, err := s.blockQueue.addDependencies(layerID, blockIds, foo); res == false {
		return s.LayerBlocks(layerID)
	} else if err != nil {
		return nil, errors.New(fmt.Sprintf("failed adding layer %v blocks to queue", layerID))
	}

	s.Info("layer %v wait for blocks", layerID)
	if result := <-ch; !result {
		return nil, fmt.Errorf("could get blocks for layer  %v", layerID)
	}

	return s.LayerBlocks(layerID)
}

func (s *Syncer) blockSyntacticValidation(block *types.Block) ([]*types.AddressableSignedTransaction, []*types.ActivationTx, error) {

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
	if valid := s.validateBlockView(block); valid == false {
		return nil, nil, errors.New(fmt.Sprintf("block %v not syntacticly valid", block.ID()))
	}

	//validate block's votes
	if valid := validateVotes(block, s.ForBlockInView, s.Hdist); valid == false {
		return nil, nil, errors.New(fmt.Sprintf("validate votes failed for block %v", block.ID()))
	}

	return txs, atxs, nil
}

func (s *Syncer) validateBlockView(blk *types.Block) bool {
	ch := make(chan bool, 1)
	defer close(ch)
	foo := func(res bool) error {
		ch <- res
		return nil
	}
	if res, err := s.blockQueue.addDependencies(blk.ID(), blk.ViewEdges, foo); res == false {
		return true
	} else if err != nil {
		s.Error(fmt.Sprintf("block %v not syntactically valid ", blk.ID()))
		return false
	}

	return <-ch
}

func validateVotes(blk *types.Block, forBlockfunc ForBlockInView, depth int) bool {
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
	lowestLayer := blk.LayerIndex - types.LayerID(depth)
	if blk.LayerIndex < types.LayerID(depth) {
		lowestLayer = 0
	}
	err := forBlockfunc(view, lowestLayer, traverse)
	if err == nil && len(vote) > 0 {
		err = fmt.Errorf("voting on blocks out of view (or out of Hdist), %v", vote)
	}
	return err == nil
}

func (s *Syncer) DataAvailabilty(blk *types.Block) ([]*types.AddressableSignedTransaction, []*types.ActivationTx, error) {

	txres, txerr := s.txQueue.HandleTxs(blk.TxIds)
	if txerr != nil {
		s.Warning("failed fetching block %v transactions %v", blk.ID(), txerr)
		return nil, nil, txerr
	}

	atxres, atxerr := s.atxQueue.HandleAtxs(blk.AtxIds)
	if atxerr != nil {
		s.Warning("failed fetching block %v activation transactions %v", blk.ID(), atxerr)
		return nil, nil, atxerr
	}

	s.Info("fetched all block %v data  %v txs %v atxs", blk.ID(), len(blk.TxIds), len(blk.AtxIds))
	return txres, atxres, nil
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

func fetchWithFactory(wrk worker) chan interface{} {
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
		out := <-fetchWithFactory(NewNeighborhoodWorker(s, 1, PoetReqFactory(poetProofRef)))
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
