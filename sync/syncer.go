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
}

type TxValidator interface {
	TxValid(tx types.SerializableTransaction) bool
}

type Configuration struct {
	Concurrency    int //number of workers for sync method
	LayerSize      int
	RequestTimeout time.Duration
}

type PoetDb interface {
	HasProof(proofRef []byte) bool
	ValidateAndStore(proofMessage *types.PoetProofMessage) error
	GetProofMessage(proofRef []byte) ([]byte, error)
}

type Syncer struct {
	p2p.Peers
	*mesh.Mesh
	BlockValidator //todo should not be here
	TxValidator
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
	poetDb            PoetDb
}

func (s *Syncer) ForceSync() {
	s.forceSync <- true
}

func (s *Syncer) Close() {
	close(s.exit)
	close(s.forceSync)
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
	s.Log.Info("latest: %v, maxSynced %v", s.LatestLayer(), s.maxSyncLayer())
	return s.LatestLayer()+1 >= s.maxSyncLayer()
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
func NewSync(srv service.Service, layers *mesh.Mesh, txpool MemPool, atxpool MemPool, bv BlockValidator, tv TxValidator, poetDb PoetDb, conf Configuration, clock timesync.LayerTimer, currentLayer types.LayerID, logger log.Log) *Syncer {
	s := &Syncer{
		BlockValidator: bv,
		TxValidator:    tv,
		Configuration:  conf,
		Log:            logger,
		Mesh:           layers,
		Peers:          p2p.NewPeers(srv, logger.WithName("peers")),
		MessageServer:  server.NewMsgServer(srv.(server.Service), syncProtocol, conf.RequestTimeout, make(chan service.DirectMessage, config.ConfigValues.BufferSize), logger.WithName("srv")),
		SyncLock:       0,
		txpool:         txpool,
		atxpool:        atxpool,
		startLock:      0,
		currentLayer:   currentLayer,
		forceSync:      make(chan bool),
		clock:          clock,
		exit:           make(chan struct{}),
		poetDb:         poetDb,
	}

	s.RegisterBytesMsgHandler(LAYER_HASH, newLayerHashRequestHandler(layers, logger))
	s.RegisterBytesMsgHandler(MINI_BLOCK, newMiniBlockRequestHandler(layers, logger))
	s.RegisterBytesMsgHandler(LAYER_IDS, newLayerBlockIdsRequestHandler(layers, logger))
	s.RegisterBytesMsgHandler(TX, newTxsRequestHandler(s, logger))
	s.RegisterBytesMsgHandler(ATX, newATxsRequestHandler(s, logger))
	s.RegisterBytesMsgHandler(POET, newPoetRequestHandler(s, logger))
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

	blocksArr, err := s.fetchFullBlocks(blockIds)
	if err != nil {
		s.Error("could not get layer blocks %v", currenSyncLayer, err)
		return nil, err
	}

	return types.NewExistingLayer(types.LayerID(currenSyncLayer), blocksArr), nil
}

func (s *Syncer) fetchFullBlocks(blockIds []types.BlockID) ([]*types.Block, error) {
	output := s.fetchWithFactory(BlockReqFactory(blockIds), s.Concurrency)
	blocksArr := make([]*types.Block, 0, len(blockIds))
	for out := range output {
		block := out.(*types.Block)

		eligible, err := s.BlockEligible(&block.BlockHeader)
		if err != nil {
			s.Warning(fmt.Sprintf("failed checking eligiblety %v", block.ID()), err)
			continue
		}
		if !eligible {
			s.Warning(fmt.Sprintf("block %v not eligible", block.ID()), err)
			continue
		}

		if err := s.syncMissingContent(block); err != nil {
			s.Error(fmt.Sprintf("failed derefrencing block data %v", block.ID()), err)
			continue
		}

		s.Info("added block to layer %v", block.Layer())
		blocksArr = append(blocksArr, block)

	}

	return blocksArr, nil
}

func (s *Syncer) syncMissingContent(blk *types.Block) error {
	var txs []*types.SerializableTransaction
	var txerr error
	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		//sync Transactions
		txs, txerr = s.Txs(blk)
		wg.Done()
	}()

	var atxs []*types.ActivationTx
	//var associated *types.ActivationTx
	var atxerr error

	go func() {
		//sync ATxs
		atxs, _, atxerr = s.ATXs(blk)
		wg.Done()
	}()

	wg.Wait()

	if txerr != nil {
		s.Warning(fmt.Sprintf("failed fetching block %v transactions %v", blk.ID(), txerr))
		return txerr
	}

	if atxerr != nil {
		s.Warning(fmt.Sprintf("failed fetching block %v activation transactions %v", blk.ID(), atxerr))
		return atxerr
	}

	s.Info(fmt.Sprintf("fetched all txs for block %v", blk.ID()))
	s.Info(fmt.Sprintf("fetched all atxs for block %v", blk.ID()))

	if err := s.AddBlockWithTxs(blk, txs, atxs); err != nil {
		s.Warning(fmt.Sprintf("failed fetching block %v activation transactions %v", blk.ID(), err))
		return err
	}

	s.Info(fmt.Sprintf("added block with txs to database %v", blk.ID()))
	return nil
}

func (s *Syncer) Txs(mb *types.Block) ([]*types.SerializableTransaction, error) {

	//look in pool
	txMap := make(map[types.TransactionId]*types.SerializableTransaction)
	missing := make([]types.TransactionId, 0)
	for _, t := range mb.TxIds {
		if tx := s.txpool.Get(t); tx != nil {
			s.Info("found tx, %v in tx pool", hex.EncodeToString(t[:]))
			txMap[t] = tx.(*types.SerializableTransaction)
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
				txMap[types.GetTransactionId(&tx)] = &tx
			}
		}
	}

	txs := make([]*types.SerializableTransaction, 0, len(mb.TxIds))
	for _, t := range mb.TxIds {
		if tx, ok := txMap[t]; ok {
			txs = append(txs, tx)
		} else if tx, ok := dbTxs[t]; ok {
			txs = append(txs, tx)
		} else {
			return nil, errors.New(fmt.Sprintf("could not fetch tx %v", hex.EncodeToString(t[:])))
		}
	}
	return txs, nil
}

func (s *Syncer) ATXs(mb *types.Block) (atxs []*types.ActivationTx, associated *types.ActivationTx, err error) {

	//look in pool
	atxMap := make(map[types.AtxId]*types.ActivationTx, len(atxs))
	missingInPool := make([]types.AtxId, 0, len(atxs))
	for _, t := range mb.AtxIds {
		id := t
		if x := s.atxpool.Get(id); x != nil {
			atx := x.(*types.ActivationTx)
			if atx.Nipst == nil {
				s.Warning("atx %v nipst not found ", hex.EncodeToString(id.Bytes()))
				missingInPool = append(missingInPool, id)
				continue
			}
			s.Info("found atx, %v in atx pool", hex.EncodeToString(id.Bytes()))
			atxMap[id] = atx
		} else {
			s.Warning("atx %v not in atx pool", hex.EncodeToString(id.Bytes()))
			missingInPool = append(missingInPool, id)
		}
	}

	//look in db
	dbAtxs, missing := s.GetATXs(missingInPool)

	fetchAssociated := false
	if mb.ATXID != *types.EmptyAtxId {
		if _, err := s.GetAtx(mb.ATXID); err != nil {
			fetchAssociated = true
			missing = append(missing, mb.ATXID)
		}
	}

	//map and sort atxs
	if len(missing) > 0 {
		output := s.fetchWithFactory(ATxReqFactory(missing), 1)
		for out := range output {
			ntxs := out.([]types.ActivationTx)
			for _, atx := range ntxs {
				err := s.FetchPoetProof(atx.GetPoetProofRef())
				if err != nil {
					if types.IsProcessingError(err) {
						s.Error("error while validating PoET proof: %v", err)
					} else {
						s.Warning("received ATX with invalid or missing PoET proof: %v", err)
					}
					return nil, nil, fmt.Errorf("a referenced PoET proof is missing or invalid: %v", err)
				}
				atxMap[atx.Id()] = &atx
			}
		}
	}

	if fetchAssociated {
		if _, ok := atxMap[mb.BlockHeader.ATXID]; !ok {
			return nil, nil, errors.New(fmt.Sprintf("could not fetch associated %v", hex.EncodeToString(mb.BlockHeader.ATXID.Bytes())))
		}
	}

	atxs = make([]*types.ActivationTx, 0, len(mb.AtxIds))
	for _, t := range mb.AtxIds {
		if tx, ok := atxMap[t]; ok {
			atxs = append(atxs, tx)
		} else if tx, ok := dbAtxs[t]; ok {
			atxs = append(atxs, tx)
		} else {
			return nil, nil, errors.New(fmt.Sprintf("could not fetch atx %v", hex.EncodeToString(t.Bytes())))
		}
	}
	return atxs, associated, nil
}

func (s *Syncer) FetchPoetProof(poetProofRef []byte) error {
	if !s.poetDb.HasProof(poetProofRef) {
		out := <-s.fetchWithFactory(PoetReqFactory(poetProofRef), 1)
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
	// each worker goroutine tries to fetch a block iteratively from each peer

	wrk := NewNeighborhoodWorker(s, workers, refac)
	go wrk.Work()

	for i := 0; i < workers-1; i++ {
		cloneWrk := wrk.Clone()
		go cloneWrk.Work()
	}

	return wrk.output
}
