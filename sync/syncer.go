package sync

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/address"
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

type TxSigValidator interface {
	ValidateTransactionSignature(tx *types.SerializableSignedTransaction) (address.Address, error)
}

type blockValidator struct {
	EligibilityValidator
}

func NewBlockValidator(bev EligibilityValidator) BlockValidator {
	return &blockValidator{bev}
}

type Configuration struct {
	LayersPerEpoch uint16
	Concurrency    int //number of workers for sync method
	LayerSize      int
	RequestTimeout time.Duration
}

type LayerValidator interface {
	ValidatedLayer() types.LayerID
	ValidateLayer(lyr *types.Layer)
}

type Syncer struct {
	p2p.Peers
	*mesh.Mesh
	BlockValidator
	Configuration
	log.Log
	*server.MessageServer

	lValidator        LayerValidator
	sigValidator      TxSigValidator
	poetDb            PoetDb
	txpool            TxMemPool
	atxpool           AtxMemPool
	currentLayer      types.LayerID
	SyncLock          uint32
	startLock         uint32
	forceSync         chan bool
	clock             timesync.LayerTimer
	exit              chan struct{}
	currentLayerMutex sync.RWMutex

	//todo fetch server
	blockQueue *validationQueue
	txQueue    *txQueue
	atxQueue   *atxQueue
}

func (s *Syncer) ForceSync() {
	s.forceSync <- true
}

func (s *Syncer) Close() {
	close(s.exit)
	close(s.forceSync)
	s.blockQueue.done()
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

func (s *Syncer) IsSynced() bool {
	s.Log.Info("latest: %v, maxSynced %v", s.LatestLayer(), s.lastTickedLayer())
	return s.LatestLayer()+1 >= s.lastTickedLayer()
}

func (s *Syncer) Start() {
	if atomic.CompareAndSwapUint32(&s.startLock, 0, 1) {
		go s.run()
		go s.blockQueue.traverse(s)
		s.forceSync <- true
		return
	}
}

func (s *Syncer) getSyncRoutine() func() {
	return func() {
		if atomic.CompareAndSwapUint32(&s.SyncLock, IDLE, RUNNING) {
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
func NewSync(srv service.Service, layers *mesh.Mesh, txpool TxMemPool, atxpool AtxMemPool, sv TxSigValidator, bv BlockValidator, poetdb PoetDb, conf Configuration, clock *timesync.Ticker, logger log.Log) *Syncer {
	s := &Syncer{
		BlockValidator: bv,
		Configuration:  conf,
		Log:            logger,
		Mesh:           layers,
		Peers:          p2p.NewPeers(srv, logger.WithName("peers")),
		MessageServer:  server.NewMsgServer(srv.(server.Service), syncProtocol, conf.RequestTimeout, make(chan service.DirectMessage, p2pconf.ConfigValues.BufferSize), logger.WithName("srv")),
		lValidator:     layers,
		sigValidator:   sv,
		SyncLock:       0,
		txpool:         txpool,
		atxpool:        atxpool,
		poetDb:         poetdb,
		startLock:      0,
		currentLayer:   clock.GetCurrentLayer(),
		forceSync:      make(chan bool),
		clock:          clock.Subscribe(),
		exit:           make(chan struct{}),
	}

	//todo change get block to check local
	s.blockQueue = NewValidationQueue(s.DataAvailabiltyV2, s.AddBlockWithTxs, s.GetBlock, s.Log.WithName("validQ"))
	s.txQueue = NewTxQueue(s)
	s.atxQueue = NewAtxQueue(s)

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
	for currentSyncLayer := s.lValidator.ValidatedLayer() + 1; currentSyncLayer <= s.lastTickedLayer(); currentSyncLayer++ {
		lastTickedLayer := s.lastTickedLayer()
		if s.IsSynced() && currentSyncLayer == lastTickedLayer {
			continue
		}

		s.Info("syncing layer %v current consensus layer is %d", currentSyncLayer, lastTickedLayer)

		lyr, err := s.GetLayer(types.LayerID(currentSyncLayer))
		if err != nil {
			s.Info("layer %v is not in the database", currentSyncLayer)
			if lyr, err = s.getLayerFromNeighbors(currentSyncLayer); err != nil {
				s.Info("could not get layer %v from neighbors %v", currentSyncLayer, err)
				return
			}
		}

		s.lValidator.ValidateLayer(lyr) // wait for layer validation
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
	s.blockQueue.addDependencies(layerID, blockIds, foo)
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
	txs, atxs, err := s.DataAvailabiltyV2(block)
	if err != nil {
		return nil, nil, errors.New(fmt.Sprintf("data availabilty failed for block %v", block.ID()))
	}

	//validate blocks view
	if valid := s.validateBlockView(block); valid == false {
		return nil, nil, errors.New(fmt.Sprintf("block %v not syntacticly valid", block.ID()))
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

	s.Info("fetched all block %v data  %v txs %v atxs", blk.ID(), len(blk.TxIds), len(blk.AtxIds))
	return txs, atxs, nil
}

func (s *Syncer) DataAvailabiltyV2(blk *types.Block) ([]*types.AddressableSignedTransaction, []*types.ActivationTx, error) {

	txres, txerr := s.txQueue.Handle(blk.TxIds)
	if txerr != nil {
		s.Warning("failed fetching block %v transactions %v", blk.ID(), txerr)
		return nil, nil, txerr
	}

	atxres, atxerr := s.atxQueue.Handle(blk.AtxIds)
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

func (s *Syncer) validateAndBuildTx(x *types.SerializableSignedTransaction) (*types.AddressableSignedTransaction, error) {
	addr, err := s.sigValidator.ValidateTransactionSignature(x)
	if err != nil {
		return nil, err
	}

	return &types.AddressableSignedTransaction{SerializableSignedTransaction: x, Address: addr}, nil
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
				ast, err := s.validateAndBuildTx(&tmp)
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
			s.Warning("atx %v not in atx pool", id.ShortId())
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
