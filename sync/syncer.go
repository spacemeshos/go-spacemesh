package sync

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
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

type PoetDb interface {
	HasProof(proofRef []byte) bool
	ValidateAndStore(proofMessage *types.PoetProofMessage) error
	GetProofMessage(proofRef []byte) ([]byte, error)
}

type BlockValidator interface {
	BlockEligible(block *types.BlockHeader) (bool, error)
}

type EligibilityValidator interface {
	BlockEligible(block *types.BlockHeader) (bool, error)
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
	sigValidator      TxSigValidator
	poetDb            PoetDb
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
func NewSync(srv service.Service, layers *mesh.Mesh, txpool MemPool, atxpool MemPool, sv TxSigValidator, bv BlockValidator, poetdb PoetDb, conf Configuration, clock timesync.LayerTimer, currentLayer types.LayerID, logger log.Log) *Syncer {
	s := &Syncer{
		BlockValidator: bv,
		Configuration:  conf,
		Log:            logger,
		Mesh:           layers,
		Peers:          p2p.NewPeers(srv, logger.WithName("peers")),
		MessageServer:  server.NewMsgServer(srv.(server.Service), syncProtocol, conf.RequestTimeout, make(chan service.DirectMessage, config.ConfigValues.BufferSize), logger.WithName("srv")),
		sigValidator:   sv,
		SyncLock:       0,
		txpool:         txpool,
		atxpool:        atxpool,
		poetDb:         poetdb,
		startLock:      0,
		currentLayer:   currentLayer,
		forceSync:      make(chan bool),
		clock:          clock,
		exit:           make(chan struct{}),
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
		go func(lyrToValidate types.Layer) {
			s.ValidateLayer(&lyrToValidate) //run one at a time
			mu.Unlock()
		}(*lyr)
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

	blocksArr := s.GetFullBlocks(blockIds)
	if len(blocksArr) == 0 {
		s.Error("could not any blocks  for layer  %v", currenSyncLayer)
		return nil, err
	}

	return types.NewExistingLayer(types.LayerID(currenSyncLayer), blocksArr), nil
}

func (s *Syncer) GetFullBlocks(blockIds []types.BlockID) []*types.Block {
	output := s.fetchWithFactory(NewBlockWorker(s, s.Concurrency, BlockReqFactory(), blockSliceToChan(blockIds)))
	blocksArr := make([]*types.Block, 0, len(blockIds))
	for out := range output {
		//ignore blocks that we could not fetch
		if out != nil {
			block := out.(*types.Block)
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
	if err := s.confirmBlockValidity(block); err != nil {
		s.Error("failed derefrencing block data %v %v", block.ID(), err)
		return nil, nil, errors.New(fmt.Sprintf("failed derefrencing block data %v %v", block.ID(), err))
	}

	//data availability
	txs, atxs, err := s.DataAvailabilty(block)
	if err != nil {
		return nil, nil, errors.New(fmt.Sprintf("data availabilty failed for block %v", block.ID()))
	}

	//validate blocks view
	if valid := s.ValidateView(block); valid == false {
		return nil, nil, errors.New(fmt.Sprintf("block %v not syntacticly valid", block.ID()))
	}

	return txs, atxs, nil
}

func (s *Syncer) confirmBlockValidity(blk *types.Block) error {
	s.Info("validate block %v ", blk.ID())

	//check block signature and is identity active
	pubKey, err := ed25519.ExtractPublicKey(blk.Bytes(), blk.Sig())
	if err != nil {
		return errors.New(fmt.Sprintf("could not extract block %v public key %v ", blk.ID(), err))
	}

	active, atxid, err := s.IsIdentityActive(signing.NewPublicKey(pubKey).String(), blk.Layer())
	if err != nil {
		return errors.New(fmt.Sprintf("error while checking IsIdentityActive for %v %v ", blk.ID(), err))
	}

	if !active {
		return errors.New(fmt.Sprintf("block %v identity activation check failed ", blk.ID()))
	}

	if atxid != blk.ATXID {
		return errors.New(fmt.Sprintf("wrong associated atx got %v expected %v ", blk.ATXID.ShortId(), atxid.ShortId()))
	}

	//block eligibility
	if eligable, err := s.BlockEligible(&blk.BlockHeader); err != nil || !eligable {
		return errors.New(fmt.Sprintf("block %v eligablety check failed ", blk.ID()))
	}

	return nil
}

func (s *Syncer) ValidateView(blk *types.Block) bool {
	vq := NewValidationQueue(s.Log.WithName("validQ"))
	if err := vq.traverse(s, &blk.BlockHeader); err != nil {
		s.Warning("could not validate %v view %v", blk.ID(), err)
		return false
	}
	return true
}

func (s *Syncer) DataAvailabilty(blk *types.Block) ([]*types.AddressableSignedTransaction, []*types.ActivationTx, error) {
	var txs []*types.AddressableSignedTransaction
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
		s.Warning("failed fetching block %v transactions %v", blk.ID(), txerr)
		return txs, atxs, txerr
	}

	if atxerr != nil {
		s.Warning("failed fetching block %v activation transactions %v", blk.ID(), atxerr)
		return txs, atxs, atxerr
	}

	s.Info("fetched all txs for block %v", blk.ID())
	s.Info("fetched all atxs for block %v", blk.ID())

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
		if tx := s.txpool.Get(t); tx != nil {
			s.Info("found tx, %v in tx pool", hex.EncodeToString(t[:]))
			unprocessedTxs[t] = tx.(*types.AddressableSignedTransaction)
		} else {
			missing = append(missing, t)
		}
	}

	//look in db
	dbTxs, missinDB := s.GetTransactions(missing)

	//map and sort txs
	if len(missing) > 0 {
		for out := range s.fetchWithFactory(NewNeighborhoodWorker(s, 1, TxReqFactory(missinDB))) {
			ntxs := out.([]types.SerializableSignedTransaction)
			for _, tx := range ntxs {
				ast, err := s.validateAndBuildTx(&tx)
				if err != nil {
					id := types.GetTransactionId(&tx)
					s.Warning("tx %v not valid %v", hex.EncodeToString(id[:]), err)
					continue
				}
				unprocessedTxs[types.GetTransactionId(&tx)] = ast
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
		if x := s.atxpool.Get(id); x != nil {
			atx := x.(*types.ActivationTx)
			if atx.Nipst == nil {
				s.Warning("atx %v nipst not found ", id.ShortId())
				missingInPool = append(missingInPool, id)
				continue
			}
			s.Info("found atx, %v in atx pool", id.ShortId())
			unprocessedAtxs[id] = atx
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
				if err := s.SyntacticallyValidateAtx(&atx); err != nil {
					s.Warning("atx %v not valid %v", atx.ShortId(), err)
					continue
				}
				unprocessedAtxs[atx.Id()] = &atx
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
