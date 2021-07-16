// Package layerfetcher fetches layers from remote peers
package layerfetcher

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

// atxHandler defines handling function for incoming ATXs
type atxHandler interface {
	HandleAtxData(ctx context.Context, data []byte, syncer service.Fetcher) error
}

// blockHandler defines handling function for blocks
type blockHandler interface {
	HandleBlockData(ctx context.Context, date []byte, fetcher service.Fetcher) error
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

// poetDB is an interface to reading and storing poet proofs
type poetDB interface {
	HasProof(proofRef []byte) bool
	ValidateAndStore(proofMessage *types.PoetProofMessage) error
	ValidateAndStoreMsg(data []byte) error
}

// tortoiseBeaconDB is an interface for tortoise beacon database
type tortoiseBeaconDB interface {
	GetTortoiseBeacon(epochID types.EpochID) (types.Hash32, error)
	SetTortoiseBeacon(epochID types.EpochID, beacon types.Hash32) error
}

// network defines network capabilities used
type network interface {
	GetPeers() []peers.Peer
	SendRequest(ctx context.Context, msgType server.MessageType, payload []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte), timeoutHandler func(err error)) error
	Close()
}

var emptyHash = types.Hash32{}

// ErrZeroLayer is the error returned when an empty hash is received when polling for layer
var ErrZeroLayer = errors.New("zero layer")

// ErrNoPeers is returned when node has no peers.
var ErrNoPeers = errors.New("no peers")

// ErrTooManyPeerErrors is returned when too many (> 1/2) peers return error
var ErrTooManyPeerErrors = errors.New("too many peers returned error")

// ErrBeaconNotReceived is returned when no valid beacon was received.
var ErrBeaconNotReceived = errors.New("no peer sent a valid beacon")

// peerResult captures the response from each peer.
type peerResult struct {
	data []byte
	err  error
}

// LayerHashResult is the result of fetching hashes for each layer from peers.
type LayerHashResult struct {
	Hashes map[types.Hash32][]peers.Peer
	Err    error
}

// LayerPromiseResult is the result of trying to fetch data for an entire layer.
type LayerPromiseResult struct {
	Err   error
	Layer types.LayerID
}

// Logic is the struct containing components needed to follow layer fetching logic
type Logic struct {
	log            log.Log
	fetcher        fetch.Fetcher
	net            network
	mutex          sync.Mutex
	layerHashesRes map[types.LayerID]map[peers.Peer]*peerResult
	layerHashesChs map[types.LayerID][]chan LayerHashResult
	layerBlocksRes map[types.LayerID]map[peers.Peer]*peerResult
	layerBlocksChs map[types.LayerID][]chan LayerPromiseResult
	poetProofs     poetDB
	atxs           atxHandler
	blockHandler   blockHandler
	txs            TxProcessor
	layerDB        layerDB
	atxIds         atxIDsDB
	tbDB           tortoiseBeaconDB
	gossipBlocks   gossipBlocks
	goldenATXID    types.ATXID
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
func NewLogic(ctx context.Context, cfg Config, blocks blockHandler, atxs atxHandler, poet poetDB, atxIDs atxIDsDB, txs TxProcessor, network service.Service, fetcher fetch.Fetcher, layers layerDB, tortoiseBeacons tortoiseBeaconDB, log log.Log) *Logic {
	srv := fetch.NewMessageNetwork(ctx, cfg.RequestTimeout, network, layersProtocol, log)
	l := &Logic{
		log:            log,
		fetcher:        fetcher,
		net:            srv,
		layerHashesRes: make(map[types.LayerID]map[peers.Peer]*peerResult),
		layerHashesChs: make(map[types.LayerID][]chan LayerHashResult),
		layerBlocksRes: make(map[types.LayerID]map[peers.Peer]*peerResult),
		layerBlocksChs: make(map[types.LayerID][]chan LayerPromiseResult),
		poetProofs:     poet,
		atxs:           atxs,
		layerDB:        layers,
		tbDB:           tortoiseBeacons,
		blockHandler:   blocks,
		atxIds:         atxIDs,
		txs:            txs,
		goldenATXID:    cfg.GoldenATXID,
	}

	srv.RegisterBytesMsgHandler(server.LayerHashMsg, l.layerHashReqReceiver)
	srv.RegisterBytesMsgHandler(server.LayerBlocksMsg, l.layerHashBlocksReqReceiver)
	srv.RegisterBytesMsgHandler(server.AtxIDsMsg, l.epochATXsReqReceiver)
	srv.RegisterBytesMsgHandler(server.TortoiseBeaconMsg, l.tortoiseBeaconReqReceiver)

	return l
}

const (
	layersProtocol = "/layers/2.0/"
)

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
func (l *Logic) AddDBs(blockDB, AtxDB, TxDB, poetDB, IvDB, tbDB database.Store) {
	l.fetcher.AddDB(fetch.BlockDB, blockDB)
	l.fetcher.AddDB(fetch.ATXDB, AtxDB)
	l.fetcher.AddDB(fetch.TXDB, TxDB)
	l.fetcher.AddDB(fetch.POETDB, poetDB)
	l.fetcher.AddDB(fetch.IVDB, IvDB)
	l.fetcher.AddDB(fetch.TBDB, tbDB)
}

// layerHashReqReceiver returns the layer hash for the specified layer.
func (l *Logic) layerHashReqReceiver(ctx context.Context, msg []byte) []byte {
	lyr := types.NewLayerID(util.BytesToUint32(msg))
	h := l.layerDB.GetLayerHash(lyr)
	l.log.WithContext(ctx).Debug("got layer hash request %v, responding with %v", lyr, h.Hex())
	return h.Bytes()
}

// epochATXsReqReceiver returns the ATXs for the specified epoch.
func (l *Logic) epochATXsReqReceiver(ctx context.Context, msg []byte) []byte {
	lyr := types.EpochID(util.BytesToUint32(msg))
	l.log.WithContext(ctx).With().Debug("got epoch atxs request for layer", lyr)
	atxs := l.atxIds.GetEpochAtxs(lyr)
	bts, err := types.InterfaceToBytes(atxs)
	if err != nil {
		l.log.WithContext(ctx).Warning("cannot find epoch atxs for epoch %v", lyr)
	}
	return bts
}

// layerHashBlocksReqReceiver returns the block IDs for the specified layer hash,
// it also returns the validation vector for this data and the latest blocks received in gossip
func (l *Logic) layerHashBlocksReqReceiver(ctx context.Context, msg []byte) []byte {
	h := types.BytesToHash(msg)

	blocks := l.layerDB.GetLayerHashBlocks(h)
	vector, err := l.layerDB.GetLayerInputVector(h)
	// best effort with input vector
	if err != nil {
		// TODO: We need to diff empty set and no results in sync somehow.
		l.log.WithContext(ctx).Debug("didn't have input vector for layer ")
	}
	// latest := l.gossipBlocks.Get() todo: implement this
	b := layerBlocks{
		blocks,
		nil,
		vector,
	}

	out, err := types.InterfaceToBytes(b)
	if err != nil {
		l.log.WithContext(ctx).Error("cannot serialize response")
	}

	return out
}

// tortoiseBeaconReqReceiver returns the tortoise beacon for the given layer ID
func (l *Logic) tortoiseBeaconReqReceiver(ctx context.Context, msg []byte) []byte {
	epoch := types.EpochID(util.BytesToUint32(msg))
	l.log.WithContext(ctx).Debug("got tortoise beacon request for epoch %v", epoch)

	beacon, err := l.tbDB.GetTortoiseBeacon(epoch)
	if errors.Is(err, database.ErrNotFound) {
		l.log.WithContext(ctx).Debug("still no tortoise beacon for epoch %v", epoch)
		return []byte{}
	}

	if err != nil {
		l.log.WithContext(ctx).Error("cannot get tortoise beacon for epoch %v", epoch)
		return []byte{}
	}

	l.log.WithContext(ctx).Debug("replying to tortoise beacon request for epoch %v: %v", epoch, beacon.String())
	return beacon.Bytes()
}

// PollLayerHash polls peers on the layer hash. it returns a channel for the caller to be notified when
// responses are received from all peers.
func (l *Logic) PollLayerHash(ctx context.Context, layerID types.LayerID) chan LayerHashResult {
	l.log.WithContext(ctx).Info("polling layer hash for layer %v", layerID)
	resChannel := make(chan LayerHashResult, 1)

	remotePeers := l.net.GetPeers()
	numPeers := len(remotePeers)
	if numPeers == 0 {
		resChannel <- LayerHashResult{Hashes: nil, Err: ErrNoPeers}
		return resChannel
	}

	l.mutex.Lock()
	l.layerHashesChs[layerID] = append(l.layerHashesChs[layerID], resChannel)
	if _, ok := l.layerHashesRes[layerID]; ok {
		// polling hashes of layerID is on-going, just wait for the polling to finish and get notified
		l.mutex.Unlock()
		return resChannel
	}
	l.layerHashesRes[layerID] = make(map[peers.Peer]*peerResult)
	l.mutex.Unlock()

	// request layers from all peers since different peers can have different layer hashes
	for _, p := range remotePeers {
		// build custom receiver for each peer so that receiver will know which peer the data came from
		// so that it could request relevant block ids from the same peer
		peer := p
		receiveForPeerFunc := func(b []byte) {
			l.receiveLayerHash(ctx, layerID, peer, numPeers, b, nil)
		}
		timeoutFunc := func(err error) {
			l.receiveLayerHash(ctx, layerID, peer, numPeers, nil, err)
		}
		err := l.net.SendRequest(ctx, server.LayerHashMsg, layerID.Bytes(), p, receiveForPeerFunc, timeoutFunc)
		if err != nil {
			l.receiveLayerHash(ctx, layerID, peer, numPeers, nil, err)
		}
	}
	return resChannel
}

// receiveLayerHash is called when a layer hash response is received from a remote peer.
// if enough responses are received, it notifies the channels waiting for the layer hash result.
func (l *Logic) receiveLayerHash(ctx context.Context, layerID types.LayerID, peer peers.Peer, numPeers int, data []byte, err error) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if err == nil {
		hash := types.BytesToHash(data)
		l.layerHashesRes[layerID][peer] = &peerResult{data: data, err: nil}
		l.log.WithContext(ctx).Debug("received data %v for layer %v from peer %v", hash.Hex(), layerID, peer.String())
	} else {
		l.layerHashesRes[layerID][peer] = &peerResult{data: nil, err: err}
		l.log.WithContext(ctx).Debug("received error for layer %v from peer %v err: %v", layerID, peer.String(), err)
	}

	// check if we have all responses from peers
	result := l.layerHashesRes[layerID]
	if len(result) < numPeers {
		l.log.WithContext(ctx).Debug("received %v results, not from all numPeers yet (%v)", len(result), numPeers)
		return
	}

	// make a copy of data and channels to avoid holding a lock while notifying
	go notifyLayerHashResult(layerID, l.layerHashesChs[layerID], result, numPeers, l.log.WithContext(ctx))
	delete(l.layerHashesChs, layerID)
	delete(l.layerHashesRes, layerID)
}

// notifyLayerHashResult aggregates all layer hash responses from peers and notify subscribed channels of the result.
// if there are too many errors from peers, it reports error instead.
// it deliberately doesn't hold any lock while notifying channels
func notifyLayerHashResult(layerID types.LayerID, channels []chan LayerHashResult, peerResult map[peers.Peer]*peerResult, numPeers int, logger log.Log) {
	logger.Debug("got hashes for layer %v, now aggregating", layerID)
	numErrors := 0
	// aggregate the peerResult by data
	hashes := make(map[types.Hash32][]peers.Peer)
	for peer, res := range peerResult {
		if res.err != nil {
			numErrors++
		} else {
			hash := types.BytesToHash(res.data)
			hashes[hash] = append(hashes[hash], peer)
		}
	}
	result := LayerHashResult{Hashes: hashes, Err: nil}

	// if more than half the peers returned an error, fail the sync of the entire layer
	// todo: think whether we should panic here
	if numErrors > numPeers/2 {
		logger.With().Error("too many errors from peers",
			layerID,
			log.Int("numPeers", numPeers),
			log.Int("numErrors", numErrors))
		result.Hashes = nil
		result.Err = ErrTooManyPeerErrors
	}
	for _, ch := range channels {
		ch <- result
	}
}

// PollLayerBlocks polls peers on blocks for given layer hashes. for each layer hash, it requests the blocks from a
// peer that reported the layer hash previously. it returns a channel for the caller to be notified when responses
// are received from all peers.
func (l *Logic) PollLayerBlocks(ctx context.Context, layerID types.LayerID, hashes map[types.Hash32][]peers.Peer) chan LayerPromiseResult {
	resChannel := make(chan LayerPromiseResult, 1)
	l.mutex.Lock()
	l.layerBlocksChs[layerID] = append(l.layerBlocksChs[layerID], resChannel)
	if _, ok := l.layerBlocksRes[layerID]; ok {
		// polling of block hashes of  is on-going, just wait for the polling to finish and get notified
		l.mutex.Unlock()
		return resChannel
	}
	l.layerBlocksRes[layerID] = make(map[peers.Peer]*peerResult)
	l.mutex.Unlock()

	// send a request to the first peer of the list to get blocks data.
	// todo: think if we should aggregate or ask from multiple peers to have some redundancy in requests
	numHashes := len(hashes)
	for hash, remotePeers := range hashes {
		peer := remotePeers[0]
		if hash == emptyHash {
			l.receiveBlockHashes(ctx, layerID, peer, numHashes, nil, ErrZeroLayer)
			continue
		}
		receiveForPeerFunc := func(data []byte) {
			l.receiveBlockHashes(ctx, layerID, peer, numHashes, data, nil)
		}
		errFunc := func(err error) {
			l.receiveBlockHashes(ctx, layerID, peer, numHashes, nil, err)
		}
		err := l.net.SendRequest(ctx, server.LayerBlocksMsg, hash.Bytes(), peer, receiveForPeerFunc, errFunc)
		if err != nil {
			l.receiveBlockHashes(ctx, layerID, peer, numHashes, nil, err)
		}
	}
	return resChannel
}

// fetchLayerBlocks fetches the content of the block IDs in the specified layerBlocks
func (l *Logic) fetchLayerBlocks(ctx context.Context, layerID types.LayerID, blocks *layerBlocks) {
	if err := l.GetBlocks(ctx, blocks.Blocks); err != nil {
		// if there is an error this means that the entire layerID cannot be validated and therefore sync should fail
		// TODO: fail sync?
		l.log.WithContext(ctx).With().Debug("error fetching layer blocks", layerID, log.Err(err))
	}

	if err := l.GetInputVector(ctx, layerID); err != nil {
		l.log.WithContext(ctx).With().Debug("error getting input vector", layerID, log.Err(err))
	}
}

// receiveBlockHashes is called when response of block IDs for a layer hash is received from remote peer.
// if enough responses are received, it notifies the channels waiting for the layer blocks result.
func (l *Logic) receiveBlockHashes(ctx context.Context, layerID types.LayerID, peer peers.Peer, expectedResults int, data []byte, extErr error) {
	pRes := &peerResult{err: extErr}
	if extErr != nil {
		if !errors.Is(extErr, ErrZeroLayer) || !layerID.GetEpoch().IsGenesis() {
			l.log.WithContext(ctx).With().Error("received error from peer for block hashes", log.Err(extErr), layerID)
		}
	} else if data != nil {
		var blocks layerBlocks
		convertErr := types.BytesToInterface(data, &blocks)
		if convertErr != nil {
			l.log.WithContext(ctx).Error("error converting bytes to layerBlocks", convertErr)
			pRes.err = convertErr
		} else {
			pRes.data = data
			l.fetchLayerBlocks(ctx, layerID, &blocks)
		}
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()

	l.layerBlocksRes[layerID][peer] = pRes

	// check if we have all responses from peers
	result := l.layerBlocksRes[layerID]
	if len(result) < expectedResults {
		l.log.WithContext(ctx).Debug("received %v results, not from all peers yet (%v)", len(result), expectedResults)
		return
	}

	// make a copy of data and channels to avoid holding a lock while notifying
	go notifyLayerBlocksResult(layerID, l.layerBlocksChs[layerID], result, l.log.WithContext(ctx))
	delete(l.layerBlocksChs, layerID)
	delete(l.layerBlocksRes, layerID)
}

// notifyLayerBlocksResult determines the final result for the layer, and notifies subscribed channels when
// all blocks are fetched for a given layer.
// it deliberately doesn't hold any lock while notifying channels.
func notifyLayerBlocksResult(layerID types.LayerID, channels []chan LayerPromiseResult, peerResult map[peers.Peer]*peerResult, logger log.Log) {
	var result *LayerPromiseResult
	hasZeroBlockHash := false
	var firstErr error
	for _, res := range peerResult {
		if res.data != nil {
			// at least one layer hash contains blocks. not a zero block layer
			result = &LayerPromiseResult{Layer: layerID, Err: nil}
			break
		}
		if res.err == ErrZeroLayer {
			hasZeroBlockHash = true
		} else if firstErr == nil {
			firstErr = res.err
		}
	}
	if result == nil { // no block data available
		result = &LayerPromiseResult{Layer: layerID, Err: nil}
		if hasZeroBlockHash {
			// all other non-empty layer hashes returned errors. use the best information we've got
			result.Err = ErrZeroLayer
		} else {
			// no usable result. just return the first error we received
			result.Err = firstErr
		}
	}
	logger.Debug("notifying layer blocks result %v", result)
	for _, ch := range channels {
		ch <- *result
	}
}

type epochAtxRes struct {
	Error error
	Atxs  []types.ATXID
}

// GetEpochATXs fetches all atxs received by peer for given layer
func (l *Logic) GetEpochATXs(ctx context.Context, id types.EpochID) error {
	resCh := make(chan epochAtxRes, 1)

	// build receiver function
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
	err := l.net.SendRequest(ctx, server.AtxIDsMsg, id.ToBytes(), fetch.GetRandomPeer(l.net.GetPeers()), receiveForPeerFunc, errFunc)
	if err != nil {
		return err
	}
	l.log.WithContext(ctx).Debug("waiting for epoch atx response")
	res := <-resCh
	if res.Error != nil {
		return res.Error
	}
	return l.GetAtxs(ctx, res.Atxs)
}

// getAtxResults is called when an ATX result is received
func (l *Logic) getAtxResults(ctx context.Context, hash types.Hash32, data []byte) error {
	l.log.WithContext(ctx).Info("got response for ATX %v, len: %v", hash.ShortString(), len(data))
	return l.atxs.HandleAtxData(ctx, data, l)
}

func (l *Logic) getTxResult(ctx context.Context, hash types.Hash32, data []byte) error {
	l.log.WithContext(ctx).Info("got response for TX %v, len: %v", hash.ShortString(), len(data))
	return l.txs.HandleTxSyncData(data)
}

// getPoetResult is handler function to poet proof fetch result
func (l *Logic) getPoetResult(ctx context.Context, hash types.Hash32, data []byte) error {
	l.log.WithContext(ctx).Info("got poet ref %v, size %v", hash.ShortString(), len(data))
	return l.poetProofs.ValidateAndStoreMsg(data)
}

// blockReceiveFunc handles blocks received via fetch
func (l *Logic) blockReceiveFunc(ctx context.Context, data []byte) error {
	return l.blockHandler.HandleBlockData(ctx, data, l)
}

// IsSynced indicates if this node is synced
func (l *Logic) IsSynced(context.Context) bool {
	// todo: add this logic
	return true
}

// ListenToGossip indicates if node is currently accepting packets from gossip
func (l *Logic) ListenToGossip() bool {
	// todo: add this logic
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
func (l *Logic) FetchAtx(ctx context.Context, id types.ATXID) error {
	f := Future{l.fetcher.GetHash(id.Hash32(), fetch.ATXDB, false), nil}
	if f.Result().Err != nil {
		return f.Result().Err
	}
	if !f.Result().IsLocal {
		return l.getAtxResults(ctx, f.Result().Hash, f.Result().Data)
	}
	return nil
}

// FetchBlock gets data for a single block id and validates it
func (l *Logic) FetchBlock(ctx context.Context, id types.BlockID) error {
	res, open := <-l.fetcher.GetHash(id.AsHash32(), fetch.BlockDB, false)
	if !open {
		return fmt.Errorf("stopped on call for id %v", id.String())
	}
	if res.Err != nil {
		return res.Err
	}
	if !res.IsLocal {
		return l.blockHandler.HandleBlockData(ctx, res.Data, l)
	}
	return res.Err
}

// GetAtxs gets the data for given atx ids IDs and validates them. returns an error if at least one ATX cannot be fetched
func (l *Logic) GetAtxs(ctx context.Context, IDs []types.ATXID) error {
	hashes := make([]types.Hash32, 0, len(IDs))
	for _, atxID := range IDs {
		hashes = append(hashes, atxID.Hash32())
	}
	// todo: atx Id is currently only the header bytes - should we change it?
	results := l.fetcher.GetHashes(hashes, fetch.ATXDB, false)
	for hash, resC := range results {
		res := <-resC
		if res.Err != nil {
			l.log.WithContext(ctx).Error("cannot fetch atx %v err %v", hash.ShortString(), res.Err)
			return res.Err
		}
		if !res.IsLocal {
			err := l.getAtxResults(nil, hash, res.Data)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// GetBlocks gets the data for given block ids and validates the blocks. returns an error if a single atx failed to be fetched
// or validated
func (l *Logic) GetBlocks(ctx context.Context, IDs []types.BlockID) error {
	l.log.WithContext(ctx).Debug("requesting %v blocks from peer", len(IDs))
	hashes := make([]types.Hash32, 0, len(IDs))
	for _, blockID := range IDs {
		hashes = append(hashes, blockID.AsHash32())
	}
	results := l.fetcher.GetHashes(hashes, fetch.BlockDB, false)
	for hash, resC := range results {
		res, open := <-resC
		if !open {
			l.log.WithContext(ctx).Info("res channel for %v is closed", hash.ShortString())
			continue
		}
		if res.Err != nil {
			l.log.WithContext(ctx).Error("cannot find block %v err %v", hash.String(), res.Err)
			continue
		}
		if !res.IsLocal {
			err := l.blockReceiveFunc(ctx, res.Data)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// GetTxs fetches the txs provided as IDs and validates them, returns an error if one TX failed to be fetched
func (l *Logic) GetTxs(ctx context.Context, IDs []types.TransactionID) error {
	hashes := make([]types.Hash32, 0, len(IDs))
	for _, atxID := range IDs {
		hashes = append(hashes, atxID.Hash32())
	}
	results := l.fetcher.GetHashes(hashes, fetch.TXDB, false)
	for hash, resC := range results {
		res := <-resC
		if res.Err != nil {
			l.log.WithContext(ctx).Error("cannot find tx %v err %v", hash.String(), res.Err)
			continue
		}
		if !res.IsLocal {
			err := l.getTxResult(ctx, hash, res.Data)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// GetPoetProof gets poet proof from remote peer
func (l *Logic) GetPoetProof(ctx context.Context, id types.Hash32) error {
	l.log.WithContext(ctx).Debug("getting proof %v", id.ShortString())
	res := <-l.fetcher.GetHash(id, fetch.POETDB, false)
	if res.Err != nil {
		return res.Err
	}
	// if result is local we don't need to process it again
	if !res.IsLocal {
		return l.getPoetResult(ctx, res.Hash, res.Data)
	}
	return nil
}

// GetInputVector gets input vector data from remote peer
func (l *Logic) GetInputVector(ctx context.Context, id types.LayerID) error {
	l.log.WithContext(ctx).With().Debug("getting inputvector for layer", id, types.CalcHash32(id.Bytes()))
	res := <-l.fetcher.GetHash(types.CalcHash32(id.Bytes()), fetch.IVDB, false)
	l.log.WithContext(ctx).Debug("done getting inputvector")
	if res.Err != nil {
		return res.Err
	}
	// if result is local we don't need to process it again
	if !res.IsLocal {
		l.log.WithContext(ctx).With().Debug("SaveLayerHashInputVector: Saving input vector", id, res.Hash)
		return l.layerDB.SaveLayerHashInputVector(res.Hash, res.Data)
	}
	return nil
}

// GetTortoiseBeacon gets tortoise beacon data from remote peer
func (l *Logic) GetTortoiseBeacon(ctx context.Context, id types.EpochID) error {
	remotePeers := l.net.GetPeers()
	if len(remotePeers) == 0 {
		return ErrNoPeers
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	resCh := make(chan []byte, len(remotePeers))

	// build receiver function
	makeReceiveFunc := func(peer fmt.Stringer) func([]byte) {
		return func(data []byte) {
			if len(data) == 0 {
				l.log.WithContext(ctx).Info("received empty tortoise beacon (peer does not have it): %v", util.Bytes2Hex(data))
				return
			}

			if len(data) != types.Hash32Length {
				l.log.WithContext(ctx).Warning("received tortoise beacon response contains either empty or bad data, ignoring: %v", util.Bytes2Hex(data))
				return
			}

			l.log.WithContext(ctx).Info("peer %v responded to tortoise beacon request with beacon %v", peer.String(), util.Bytes2Hex(data))
			resCh <- data
		}
	}

	makeErrFunc := func(peer fmt.Stringer) func(error) {
		return func(err error) {
			l.log.WithContext(ctx).Warning("peer %v responded to tortoise beacon request with error %v", peer.String(), err)
		}
	}

	l.log.WithContext(ctx).Info("requesting tortoise beacon from all peers for epoch %v", id)

	for _, p := range remotePeers {
		go func(peer peers.Peer) {
			select {
			case <-cancelCtx.Done():
				return
			default:
				l.log.WithContext(ctx).Info("requesting tortoise beacon from for epoch %v, peer: %v", id, peer.String())

				err := l.net.SendRequest(cancelCtx, server.TortoiseBeaconMsg, id.ToBytes(), peer, makeReceiveFunc(peer), makeErrFunc(peer))
				if err != nil {
					l.log.WithContext(ctx).Warning("failed to send  tortoise beacon request to peer %v: %v", peer.String(), err)
				}
			}
		}(p)
	}

	l.log.WithContext(ctx).Info("waiting for tortoise beacon response")

	const timeout = 10 * time.Second // TODO(nkryuchkov): define in config or globally
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-cancelCtx.Done():
		l.log.WithContext(ctx).Debug("receiving tortoise beacon for epoch canceled", id)
		return nil

	case <-timer.C:
		l.log.WithContext(ctx).Debug("receiving tortoise beacon for epoch timed out after %v", timeout)
		return nil

	case res := <-resCh:
		resHash := types.BytesToHash(res)
		l.log.WithContext(ctx).Info("received tortoise beacon for epoch %v: %v", id, resHash.String())

		return l.tbDB.SetTortoiseBeacon(id, resHash)
	}
}
