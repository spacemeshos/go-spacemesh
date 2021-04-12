// Package layerfetcher fetches layers from remote peers
package layerfetcher

import (
	"errors"
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	p2ppeers "github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

// atxHandler defines handling function for incoming ATXs
type atxHandler interface {
	HandleAtxData(data []byte, syncer service.Fetcher) error
}

// blockHandler defines handling function for blocks
type blockHandler interface {
	HandleBlockData(date []byte, fetcher service.Fetcher) error
}

// TxProcessor is an interface for handling TX data received in sync
type TxProcessor interface {
	HandleTxSyncData(data []byte) error
}

// layerDB is an interface that returns layer data and blocks
type layerDB interface {
	GetLayerHash(ID types.LayerID) types.Hash32
	GetLayerHashBlocks(hash types.Hash32) []types.BlockID
	GetLayerInputVector(hash types.Hash32) ([]types.BlockID, error)
	SaveLayerHashInputVector(id types.Hash32, data []byte) error
}

type atxIDsDB interface {
	GetEpochAtxs(epochID types.EpochID) (atxs []types.ATXID)
}

// gossipBlocks returns gossip block received byu this node in the last x time frame
type gossipBlocks interface {
	Get() []types.BlockID
}

// poetDb is an interface to reading and storing poet proofs
type poetDb interface {
	HasProof(proofRef []byte) bool
	ValidateAndStore(proofMessage *types.PoetProofMessage) error
	ValidateAndStoreMsg(data []byte) error
}

// network defines network capabilities used
type network interface {
	GetPeers() []p2ppeers.Peer
	SendRequest(msgType server.MessageType, payload []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte), timeoutHandler func(err error)) error
	Close()
}

var emptyHash = types.Hash32{}

// ErrZeroLayer is the error returned when an empty hash is received when polling for layer
var ErrZeroLayer = errors.New("zero layer")

// Logic is the struct containing components needed to follow layer fetching logic
type Logic struct {
	log                  log.Log
	fetcher              fetch.Fetcher
	net                  network
	layerHashResults     map[types.LayerID]map[p2ppeers.Peer]*types.Hash32
	blockHashResults     map[types.LayerID][]bool //list of results from peers - true if blocks were received
	layerResultsChannels map[types.LayerID][]chan LayerPromiseResult
	poetProofs           poetDb
	atxs                 atxHandler
	blockHandler         blockHandler
	txs                  TxProcessor
	layerDB              layerDB
	atxIds               atxIDsDB
	gossipBlocks         gossipBlocks
	layerResM            sync.RWMutex
	layerHashResM        sync.RWMutex
	blockHashResM        sync.RWMutex
	goldenATXID          types.ATXID
}

// Config defines configuration for layer fetching logic
type Config struct {
	RequestTimeout int
	GoldenATXID    types.ATXID
}

// DefaultConfig returns default configuration for layer fetching logic
func DefaultConfig() Config {
	return Config{RequestTimeout: 10}
}

// NewLogic creates a new instance of layer fetching logic
func NewLogic(cfg Config, blocks blockHandler, atxs atxHandler, poet poetDb, atxIDs atxIDsDB, txs TxProcessor, network service.Service, fetcher fetch.Fetcher, layers layerDB, log log.Log) *Logic {

	srv := fetch.NewMessageNetwork(cfg.RequestTimeout, network, layersProtocol, log)
	l := &Logic{
		log:                  log,
		fetcher:              fetcher,
		net:                  srv,
		layerHashResults:     make(map[types.LayerID]map[p2ppeers.Peer]*types.Hash32),
		blockHashResults:     make(map[types.LayerID][]bool),
		layerResultsChannels: make(map[types.LayerID][]chan LayerPromiseResult),
		poetProofs:           poet,
		atxs:                 atxs,
		layerDB:              layers,
		blockHandler:         blocks,
		atxIds:               atxIDs,
		txs:                  txs,
		layerResM:            sync.RWMutex{},
		goldenATXID:          cfg.GoldenATXID,
	}

	srv.RegisterBytesMsgHandler(LayerHashMsg, l.LayerHashReqReceiver)
	srv.RegisterBytesMsgHandler(LayerBlocksMsg, l.LayerHashBlocksReceiver)
	srv.RegisterBytesMsgHandler(atxIDsMsg, l.epochATXsReceiver)

	return l
}

// DB hints per DB
const (
	BlockDB fetch.Hint = "blocksDB"
	ATXDB   fetch.Hint = "ATXDB"
	TXDB    fetch.Hint = "TXDB"
	POETDB  fetch.Hint = "POETDB"
	IVDB    fetch.Hint = "IVDB"

	layersProtocol = "/layers/2.0/"

	LayerBlocksMsg = 1
	LayerHashMsg   = 2
	atxIDsMsg      = 3
)

// FetchFlow is the main syncing flow TBD
func (l *Logic) FetchFlow() {

}

// Start starts layerFetcher logic and fetch component
func (l *Logic) Start() {
	l.fetcher.Start()
}

// Close closes all running workers
func (l *Logic) Close() {
	l.net.Close()
	l.fetcher.Stop()
}

// AddDBs adds dbs that will be queried when sync requests are received. these databases will be exposed to external callers
func (l *Logic) AddDBs(blockDB, AtxDB, TxDB, poetDB, IvDB database.Store) {
	l.fetcher.AddDB(BlockDB, blockDB)
	l.fetcher.AddDB(ATXDB, AtxDB)
	l.fetcher.AddDB(TXDB, TxDB)
	l.fetcher.AddDB(POETDB, poetDB)
	l.fetcher.AddDB(IVDB, IvDB)
}

// LayerPromiseResult is the result of trying to fetch an entire layer- if this fails the error will be added to result
type LayerPromiseResult struct {
	Err   error
	Layer types.LayerID
}

// LayerHashReqReceiver returns the layer hash for the given layer ID
func (l *Logic) LayerHashReqReceiver(msg []byte) []byte {
	lyr := types.LayerID(util.BytesToUint64(msg))
	h := l.layerDB.GetLayerHash(lyr)
	l.log.Info("got layer hash request %v, responding with %v", lyr, h.Hex())
	return h.Bytes()
}

// epochATXsReceiver returns the layer hash for the given layer ID
func (l *Logic) epochATXsReceiver(msg []byte) []byte {
	l.log.Info("got epoch atxs request ")
	lyr := types.EpochID(util.BytesToUint64(msg))
	atxs := l.atxIds.GetEpochAtxs(lyr)
	bts, err := types.InterfaceToBytes(atxs)
	if err != nil {
		l.log.Warning("cannot find epoch atxs for epoch %v", lyr)
	}
	return bts
}

// LayerHashBlocksReceiver returns the block IDs for the specified layer hash,
// it also returns the validation vector for this hash and latest blocks received in gossip
func (l *Logic) LayerHashBlocksReceiver(msg []byte) []byte {
	h := types.BytesToHash(msg)

	blocks := l.layerDB.GetLayerHashBlocks(h)
	vector, err := l.layerDB.GetLayerInputVector(h)
	if err != nil {
		// TODO: We need to diff empty set and no results in sync somehow.
		l.log.Error("didn't have input vector for layer ")
	}
	//latest := l.gossipBlocks.Get() todo: implement this
	b := layerBlocks{
		blocks,
		nil,
		vector,
	}

	out, err := types.InterfaceToBytes(b)
	if err != nil {
		l.log.Error("cannot serialize response")
	}

	return out
}

// PollLayer performs the following
// send layer number to all parties
// receive layer hash from all peers
// get layer hash from corresponding peer
// fetch block ids from all peers
// fetch ATXs and Txs per block
func (l *Logic) PollLayer(layer types.LayerID) chan LayerPromiseResult {
	l.log.Info("polling for layer %v", layer)
	result := make(chan LayerPromiseResult, 1)

	l.layerResM.Lock()
	l.layerResultsChannels[layer] = append(l.layerResultsChannels[layer], result)
	l.layerResM.Unlock()

	peers := l.net.GetPeers()
	// request layers from all peers since different peers can have different layer structures (in extreme cases)
	// we ask for all blocks so that we know
	for _, p := range peers {
		// build custom receiver for each peer so that receiver will know which peer the hash came from
		// so that it could request relevant block ids from the same peer
		peer := p
		receiveForPeerFunc := func(b []byte) {
			l.receiveLayerHash(layer, peer, len(peers), b, nil)
		}

		timeoutFunc := func(err error) {
			l.receiveLayerHash(layer, peer, len(peers), nil, err)
		}
		l.log.Debug("asking for message type %v from peer %v", LayerHashMsg, p.String())
		err := l.net.SendRequest(LayerHashMsg, layer.Bytes(), peer, receiveForPeerFunc, timeoutFunc)
		if err != nil {
			l.receiveLayerHash(layer, peer, len(peers), nil, err)
		}
	}
	return result
}

// receiver function for block hash result per layer, this function aggregates all responses from all peers
// and then unifies them. it also fails if a threshold of failed calls to peers have been reached
func (l *Logic) receiveLayerHash(id types.LayerID, p p2ppeers.Peer, peers int, data []byte, err error) {
	// log result for peer
	l.layerHashResM.Lock()
	if _, ok := l.layerHashResults[id]; !ok {
		l.layerHashResults[id] = make(map[p2ppeers.Peer]*types.Hash32)
	}
	// if no error from peer, try to parse data
	if err == nil {
		hash := types.BytesToHash(data)
		l.layerHashResults[id][p] = &hash
		l.log.Info("received hash %v for layer id %v from peer %v", l.layerHashResults[id][p].Hex(), id, p.String())
	} else {
		l.layerHashResults[id][p] = nil
		l.log.Info("received zero hash for layer id %v from peer %v", id, p.String())
	}
	// not enough results
	if len(l.layerHashResults[id]) < peers {
		l.log.Debug("received %v results, not from all peers yet (%v)", len(l.layerHashResults[id]), peers)
		l.layerHashResM.Unlock()
		return
	}
	l.log.Info("got hashes for layer, now aggregating")
	l.layerHashResM.Unlock()
	errors := 0
	//aggregate hashes so that same hash will not be requested several times
	hashes := make(map[types.Hash32][]p2ppeers.Peer)
	l.layerHashResM.RLock()
	for peer, hash := range l.layerHashResults[id] {
		//count zero hashes - mark errors.
		if hash == nil {
			errors++
		} else {
			// if there is a new hash - query for it
			if _, ok := hashes[*hash]; !ok {
				hashes[*hash] = append(hashes[*hash], peer)
			}
		}
	}
	l.layerHashResM.RUnlock()

	//delete the receiver since we got all the needed messages
	l.layerHashResM.Lock()
	delete(l.layerHashResults, id)
	l.layerHashResM.Unlock()

	// if more than half the peers returned an error, fail the sync of the entire layer
	// todo: think whether we should panic here
	if errors > peers/2 {
		l.log.Error("cannot sync layer %v", id)
		l.notifyLayerPromiseResult(id, 0, fmt.Errorf("too many peers returned error"))
		return
	}

	l.log.Info("got hashes %v for layer, now querying peers", len(hashes))

	// send a request to get blocks from a single peer if multiple peers declare same hash per layer
	// if the peers fails to respond request will be sen to next peer in line
	//todo: think if we should aggregate or ask from multiple peers to have some redundancy in requests
	for hash, peer := range hashes {
		if hash == emptyHash {
			l.receiveBlockHashes(id, nil, len(hashes), ErrZeroLayer)
			continue
		}
		//build receiver function
		receiveForPeerFunc := func(data []byte) {
			l.receiveBlockHashes(id, data, len(hashes), nil)
		}
		remainingPeers := 0
		errFunc := func(err error) {
			l.receiveBlockHashes(id, nil, len(hashes), err)
		}
		l.log.Debug("asking for message type %v from peer %v", LayerHashMsg, p.String())
		err := l.net.SendRequest(LayerBlocksMsg, hash.Bytes(), peer[remainingPeers], receiveForPeerFunc, errFunc)
		if err != nil {
			l.receiveBlockHashes(id, nil, len(hashes), err)
		}
	}

}

// notifyLayerPromiseResult notifies that a layer result has been received or wasn't received
func (l *Logic) notifyLayerPromiseResult(id types.LayerID, expectedResults int, err error) {

	// count number of results - only after all results were received we can notify the caller
	l.blockHashResM.Lock()
	// put false if no blocks
	l.blockHashResults[id] = append(l.blockHashResults[id], err == ErrZeroLayer)

	// we are done with no block iteration errors
	if len(l.blockHashResults[id]) < expectedResults {
		l.blockHashResM.Unlock()
		return
	}
	isZero := false
	for _, hadBlocks := range l.blockHashResults[id] {
		if hadBlocks {
			isZero = false
			break
		}
	}
	delete(l.blockHashResults, id)
	l.blockHashResM.Unlock()

	var er = err
	if isZero {
		er = ErrZeroLayer
	}
	res := LayerPromiseResult{
		er,
		id,
	}
	l.layerResM.Lock()
	for _, ch := range l.layerResultsChannels[id] {
		l.log.Info("writing res for layer %v err %v", id, err)
		ch <- res
	}
	delete(l.layerResultsChannels, id)
	l.layerResM.Unlock()
	l.log.Info("writing error for layer %v done %v", id, err)
}

// receiveBlockHashes is called when receiving block hashes for specified layer layer from remote peer
func (l *Logic) receiveBlockHashes(layer types.LayerID, data []byte, expectedResults int, extErr error) {
	//if we failed getting layer data - notify
	if extErr != nil {
		l.log.Error("received error for layer id %v", extErr)
		l.notifyLayerPromiseResult(layer, expectedResults, extErr)
		return
	}

	if data != nil {
		var blocks layerBlocks
		err := types.BytesToInterface(data, &blocks)
		if err != nil {
			l.log.Error("received error for layer id %v", err)
			l.notifyLayerPromiseResult(layer, expectedResults, err)
			return
		}

		// fetch all blocks
		retErr := l.GetBlocks(blocks.Blocks)
		// if there is an error this means that the entire layer cannot be validated and therefore sync should fail
		if retErr != nil {
			l.log.With().Error("received error for layer id", log.Err(retErr))
		}

		ivErr := l.GetInputVector(layer)
		if ivErr != nil {
			l.log.With().Error("received error for inputvecor of layer", log.Err(ivErr))
		}
	}

	// here we need to update layer hash
	l.notifyLayerPromiseResult(layer, expectedResults, nil)
}

type epochAtxRes struct {
	Error error
	Atxs  []types.ATXID
}

// GetEpochATXs fetches all atxs received by peer for given layer
func (l *Logic) GetEpochATXs(id types.EpochID) error {
	resCh := make(chan epochAtxRes, 1)

	//build receiver function
	receiveForPeerFunc := func(data []byte) {
		var atxsIDs []types.ATXID
		err := types.BytesToInterface(data, &atxsIDs)
		resCh <- epochAtxRes{
			Error: err,
			Atxs:  atxsIDs,
		}
	}
	errFunc := func(err error) {
		resCh <- epochAtxRes{
			Error: err,
			Atxs:  nil,
		}
	}
	err := l.net.SendRequest(atxIDsMsg, id.ToBytes(), fetch.GetRandomPeer(l.net.GetPeers()), receiveForPeerFunc, errFunc)
	if err != nil {
		return err
	}
	l.log.Info("waiting for epoch atx response")
	res := <-resCh
	if res.Error != nil {
		return res.Error
	}
	return l.GetAtxs(res.Atxs)
}

// getAtxResults is called when an ATX result is received
func (l *Logic) getAtxResults(hash types.Hash32, data []byte) error {
	return l.atxs.HandleAtxData(data, l)
}

func (l *Logic) getTxResult(hash types.Hash32, data []byte) error {
	l.log.Info("got response for hash %v, len: %v", hash.ShortString(), len(data))
	return l.txs.HandleTxSyncData(data)
}

// getPoetResult is handler function to poet proof fetch result
func (l *Logic) getPoetResult(hash types.Hash32, data []byte) error {
	l.log.Info("got poet ref %v, size %v", hash.ShortString(), len(data))
	return l.poetProofs.ValidateAndStoreMsg(data)
}

// blockReceiveFunc handles blocks received via fetch
func (l *Logic) blockReceiveFunc(data []byte) error {
	return l.blockHandler.HandleBlockData(data, l)
}

// IsSynced indocates if this node is synced
func (l *Logic) IsSynced() bool {
	//todo: add this logic
	return true
}

// ListenToGossip indicates if node is currently accepting packets from gossip
func (l *Logic) ListenToGossip() bool {
	//todo: add this logic
	return true
}

// Future is a preparation for using actual futures in the code, this will allow to truly execute
// asynchronous reads and receive result only when needed
type Future struct {
	res chan fetch.HashDataPromiseResult
	ret *fetch.HashDataPromiseResult
}

// Result actually evaluates the result of the fetch task
func (f *Future) Result() fetch.HashDataPromiseResult {
	if f.ret == nil {
		ret := <-f.res
		f.ret = &ret
	}
	return *f.ret
}

// FetchAtx returns error if ATX was not found
func (l *Logic) FetchAtx(id types.ATXID) error {
	f := Future{l.fetcher.GetHash(id.Hash32(), ATXDB, false), nil}
	if f.Result().Err != nil {
		return f.Result().Err
	}
	if !f.Result().IsLocal {
		return l.getAtxResults(f.Result().Hash, f.Result().Data)
	}
	return nil
}

// FetchBlock gets data for a single block id and validates it
func (l *Logic) FetchBlock(id types.BlockID) error {
	res, open := <-l.fetcher.GetHash(id.AsHash32(), BlockDB, false)
	if !open {
		return fmt.Errorf("stopped on call for id %v", id.String())
	}
	if res.Err != nil {
		return res.Err
	}
	if !res.IsLocal {
		return l.blockHandler.HandleBlockData(res.Data, l)
	}
	return res.Err
}

// GetAtxs gets the data for given atx ids IDs and validates them. returns an error if at least one ATX cannot be fetched
func (l *Logic) GetAtxs(IDs []types.ATXID) error {
	hashes := make([]types.Hash32, 0, len(IDs))
	for _, atxID := range IDs {
		hashes = append(hashes, atxID.Hash32())
	}
	//todo: atx Id is currently only the header bytes - should we change it?
	results := l.fetcher.GetHashes(hashes, ATXDB, false)
	for hash, resC := range results {
		res := <-resC
		if res.Err != nil {
			l.log.Error("cannot fetch atx %v err %v", hash.ShortString(), res.Err)
			return res.Err
		}
		if !res.IsLocal {
			err := l.getAtxResults(hash, res.Data)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// GetBlocks gets the data for given block ids and validates the blocks. returns an error if a single atx failed to be fetched
// or validated
func (l *Logic) GetBlocks(IDs []types.BlockID) error {
	l.log.Info("requesting %v blocks from peer", len(IDs))
	hashes := make([]types.Hash32, 0, len(IDs))
	for _, blockID := range IDs {
		hashes = append(hashes, blockID.AsHash32())
	}
	results := l.fetcher.GetHashes(hashes, BlockDB, false)
	for hash, resC := range results {
		res, open := <-resC
		if !open {
			l.log.Info("res channel for %v is closed", hash.ShortString())
			continue
		}
		if res.Err != nil {
			l.log.Error("cannot find block %v err %v", hash.String(), res.Err)
			continue
		}
		if !res.IsLocal {
			err := l.blockReceiveFunc(res.Data)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// GetTxs fetches the txs provided as IDs and validates them, returns an error if one TX failed to be fetched
func (l *Logic) GetTxs(IDs []types.TransactionID) error {
	hashes := make([]types.Hash32, 0, len(IDs))
	for _, atxID := range IDs {
		hashes = append(hashes, atxID.Hash32())
	}
	results := l.fetcher.GetHashes(hashes, TXDB, false)
	for hash, resC := range results {
		res := <-resC
		if res.Err != nil {
			l.log.Error("cannot find tx %v err %v", hash.String(), res.Err)
			continue
		}
		if !res.IsLocal {
			err := l.getTxResult(hash, res.Data)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// GetPoetProof gets poet proof from remote peer
func (l *Logic) GetPoetProof(id types.Hash32) error {
	l.log.Info("getting proof %v", id.ShortString())
	res := <-l.fetcher.GetHash(id, POETDB, false)
	if res.Err != nil {
		return res.Err
	}
	// if result is local we don't need to process it again
	if !res.IsLocal {
		return l.getPoetResult(res.Hash, res.Data)
	}
	return nil
}

// GetInputVector gets input vector data from remote peer
func (l *Logic) GetInputVector(id types.LayerID) error {
	l.log.With().Info("getting inputvector for layer", id)
	res := <-l.fetcher.GetHash(types.CalcHash32(id.Bytes()), IVDB, false)
	if res.Err != nil {
		return res.Err
	}
	// if result is local we don't need to process it again
	if !res.IsLocal {
		return l.layerDB.SaveLayerHashInputVector(res.Hash, res.Data)
	}
	return nil
}
