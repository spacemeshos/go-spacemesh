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
	GetLayerHash(types.LayerID) types.Hash32
	GetAggregatedLayerHash(types.LayerID) types.Hash32
	LayerBlockIds(types.LayerID) ([]types.BlockID, error)
	GetLayerInputVectorByID(id types.LayerID) ([]types.BlockID, error)
	SaveLayerInputVectorByID(ctx context.Context, id types.LayerID, blks []types.BlockID) error
	ProcessedLayer() types.LayerID
}

type atxIDsDB interface {
	GetEpochAtxs(epochID types.EpochID) ([]types.ATXID, error)
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
	PeerCount() uint64
	SendRequest(ctx context.Context, msgType server.MessageType, payload []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte), errorHandler func(err error)) error
	Close()
}

// ErrZeroLayer is the error returned when an empty hash is received when polling for layer
var ErrZeroLayer = errors.New("zero layer")

// ErrNoPeers is returned when node has no peers.
var ErrNoPeers = errors.New("no peers")

// ErrInternal is returned from the peer when the peer encounters an internal error
var ErrInternal = errors.New("unspecified error returned by peer")

// ErrBlockNotFetched is returned when at least one block is not fetched successfully
var ErrBlockNotFetched = errors.New("block not fetched")

// peerResult captures the response from each peer.
type peerResult struct {
	data *layerBlocks
	err  error
}

// layerResult captures expected content of a layer across peers
type layerResult struct {
	layerID     types.LayerID
	blocks      map[types.BlockID]struct{}
	inputVector []types.BlockID
	responses   map[peers.Peer]*peerResult
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
	layerBlocksRes map[types.LayerID]*layerResult
	layerBlocksChs map[types.LayerID][]chan LayerPromiseResult
	poetProofs     poetDB
	atxs           atxHandler
	blockHandler   blockHandler
	txs            TxProcessor
	layerDB        layerDB
	atxIds         atxIDsDB
	tbDB           tortoiseBeaconDB
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
		layerBlocksRes: make(map[types.LayerID]*layerResult),
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

	srv.RegisterBytesMsgHandler(server.LayerBlocksMsg, l.layerBlocksReqReceiver)
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
func (l *Logic) AddDBs(blockDB, AtxDB, TxDB, poetDB, tbDB database.Getter) {
	l.fetcher.AddDB(fetch.BlockDB, blockDB)
	l.fetcher.AddDB(fetch.ATXDB, AtxDB)
	l.fetcher.AddDB(fetch.TXDB, TxDB)
	l.fetcher.AddDB(fetch.POETDB, poetDB)
	l.fetcher.AddDB(fetch.TBDB, tbDB)
}

// epochATXsReqReceiver returns the ATXs for the specified epoch.
func (l *Logic) epochATXsReqReceiver(ctx context.Context, msg []byte) ([]byte, error) {
	epoch := types.EpochID(util.BytesToUint32(msg))
	atxs, err := l.atxIds.GetEpochAtxs(epoch)
	if err != nil {
		return nil, err
	}
	l.log.WithContext(ctx).With().Debug("responded to epoch atxs request",
		epoch,
		log.Int("count", len(atxs)))
	bts, err := types.InterfaceToBytes(atxs)
	if err != nil {
		l.log.WithContext(ctx).With().Panic("failed to serialize epoch atxs", epoch, log.Err(err))
	}
	return bts, err
}

// layerBlocksReqReceiver returns the block IDs for the specified layer hash,
// it also returns the validation vector for this data and the latest blocks received in gossip
func (l *Logic) layerBlocksReqReceiver(ctx context.Context, req []byte) ([]byte, error) {
	lyrID := types.BytesToLayerID(req)
	b := &layerBlocks{
		ProcessedLayer: l.layerDB.ProcessedLayer(),
		Hash:           l.layerDB.GetLayerHash(lyrID),
		AggregatedHash: l.layerDB.GetAggregatedLayerHash(lyrID),
	}
	var err error
	b.Blocks, err = l.layerDB.LayerBlockIds(lyrID)
	if err != nil {
		if err != database.ErrNotFound {
			l.log.WithContext(ctx).With().Debug("failed to get layer content", lyrID, log.Err(err))
			return nil, ErrInternal
		}
	} else {
		if b.InputVector, err = l.layerDB.GetLayerInputVectorByID(lyrID); err != nil {
			// best effort with input vector
			l.log.WithContext(ctx).With().Debug("failed to get input vector for layer", lyrID, log.Err(err))
		}
	}

	out, err := types.InterfaceToBytes(b)
	if err != nil {
		l.log.WithContext(ctx).With().Panic("failed to serialize layer blocks response", log.Err(err))
	}

	return out, nil
}

// tortoiseBeaconReqReceiver returns the tortoise beacon for the given layer ID
func (l *Logic) tortoiseBeaconReqReceiver(ctx context.Context, data []byte) ([]byte, error) {
	epoch := types.EpochID(util.BytesToUint32(data))
	l.log.WithContext(ctx).With().Debug("got tortoise beacon request", epoch)

	beacon, err := l.tbDB.GetTortoiseBeacon(epoch)
	if errors.Is(err, database.ErrNotFound) {
		l.log.WithContext(ctx).With().Warning("tortoise beacon not found in DB", epoch)
		return nil, err
	}

	if err != nil {
		l.log.WithContext(ctx).With().Error("failed to get tortoise beacon", epoch, log.Err(err))
		return nil, err
	}

	l.log.WithContext(ctx).With().Debug("replying to tortoise beacon request", epoch, log.String("beacon", beacon.ShortString()))
	return beacon.Bytes(), nil
}

// initLayerPolling returns false if there is an ongoing polling of the given layer content,
// otherwise it initializes the polling and returns true
func (l *Logic) initLayerPolling(layerID types.LayerID, ch chan LayerPromiseResult) bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.layerBlocksChs[layerID] = append(l.layerBlocksChs[layerID], ch)
	if _, ok := l.layerBlocksRes[layerID]; ok {
		// polling of block IDs of this layerID is ongoing, just wait for the polling to finish and get notified
		return false
	}
	l.layerBlocksRes[layerID] = &layerResult{
		layerID:   layerID,
		blocks:    make(map[types.BlockID]struct{}),
		responses: make(map[peers.Peer]*peerResult),
	}
	return true
}

// PollLayerContent polls peers for the content of a given layer ID.
// it returns a channel for the caller to be notified when responses are received from all peers.
func (l *Logic) PollLayerContent(ctx context.Context, layerID types.LayerID) chan LayerPromiseResult {
	resChannel := make(chan LayerPromiseResult, 1)

	remotePeers := l.net.GetPeers()
	numPeers := len(remotePeers)
	if numPeers == 0 {
		resChannel <- LayerPromiseResult{Layer: layerID, Err: ErrNoPeers}
		return resChannel
	}

	if !l.initLayerPolling(layerID, resChannel) {
		return resChannel
	}

	// send a request to the first peer of the list to get blocks data.
	// todo: think if we should aggregate or ask from multiple peers to have some redundancy in requests
	for _, p := range remotePeers {
		peer := p
		receiveForPeerFunc := func(data []byte) {
			l.receiveLayerContent(ctx, layerID, peer, numPeers, data, nil)
		}
		errFunc := func(err error) {
			l.receiveLayerContent(ctx, layerID, peer, numPeers, nil, err)
		}
		err := l.net.SendRequest(ctx, server.LayerBlocksMsg, layerID.Bytes(), peer, receiveForPeerFunc, errFunc)
		if err != nil {
			l.receiveLayerContent(ctx, layerID, peer, numPeers, nil, err)
		}
	}
	return resChannel
}

// fetchLayerBlocks fetches the content of the block IDs in the specified layerBlocks
func (l *Logic) fetchLayerBlocks(ctx context.Context, layerID types.LayerID, blocks *layerBlocks) error {
	logger := l.log.WithContext(ctx).WithFields(layerID, log.Int("num_blocks", len(blocks.Blocks)))
	l.mutex.Lock()
	lyrResult := l.layerBlocksRes[layerID]
	var toFetch []types.BlockID
	for _, blkID := range blocks.Blocks {
		if _, ok := lyrResult.blocks[blkID]; !ok {
			// not yet fetched
			lyrResult.blocks[blkID] = struct{}{}
			toFetch = append(toFetch, blkID)
		}
	}
	// save the largest input vector from peers
	if len(blocks.InputVector) > len(lyrResult.inputVector) {
		lyrResult.inputVector = blocks.InputVector
	}
	l.mutex.Unlock()

	logger.With().Debug("fetching new blocks", log.Int("to_fetch", len(toFetch)))
	if err := l.GetBlocks(ctx, toFetch); err != nil {
		// fail sync for the entire layer
		return err
	}
	return nil
}

func extractPeerResult(logger log.Log, layerID types.LayerID, data []byte, peerErr error) (result peerResult) {
	result.err = peerErr
	if peerErr != nil {
		logger.With().Debug("received peer error for layer content", layerID, log.Err(peerErr))
		return
	}

	var blocks layerBlocks
	if err := types.BytesToInterface(data, &blocks); err != nil {
		logger.With().Debug("error converting bytes to layerBlocks", log.Err(err))
		result.err = err
		return
	}

	result.data = &blocks
	if len(blocks.Blocks) == 0 {
		result.err = ErrZeroLayer
	}
	// TODO check layer hash to be consistent with the content. if not, blacklist the peer
	return
}

// receiveLayerContent is called when response of block IDs for a layer hash is received from remote peer.
// if enough responses are received, it notifies the channels waiting for the layer blocks result.
func (l *Logic) receiveLayerContent(ctx context.Context, layerID types.LayerID, peer peers.Peer, expectedResults int, data []byte, peerErr error) {
	l.log.WithContext(ctx).With().Debug("received layer content from peer", log.String("peer", peer.String()))
	peerRes := extractPeerResult(l.log.WithContext(ctx), layerID, data, peerErr)
	if peerRes.err == nil {
		if err := l.fetchLayerBlocks(ctx, layerID, peerRes.data); err != nil {
			peerRes.err = ErrBlockNotFetched
		}
	}

	l.mutex.Lock()
	defer l.mutex.Unlock()

	// check if we have all responses from peers
	result := l.layerBlocksRes[layerID]
	result.responses[peer] = &peerRes
	if len(result.responses) < expectedResults {
		l.log.WithContext(ctx).With().Debug("not yet received layer blocks from all peers",
			log.Int("received", len(result.responses)),
			log.Int("expected", expectedResults))
		return
	}

	// save the input vector
	if len(result.inputVector) > 0 {
		l.layerDB.SaveLayerInputVectorByID(ctx, layerID, result.inputVector)
	}

	// make a copy of data and channels to avoid holding a lock while notifying
	go notifyLayerBlocksResult(layerID, l.layerDB, l.layerBlocksChs[layerID], result, l.log.WithContext(ctx).WithFields(layerID))
	delete(l.layerBlocksChs, layerID)
	delete(l.layerBlocksRes, layerID)
}

// notifyLayerBlocksResult determines the final result for the layer, and notifies subscribed channels when
// all blocks are fetched for a given layer.
// it deliberately doesn't hold any lock while notifying channels.
func notifyLayerBlocksResult(layerID types.LayerID, lyrDB layerDB, channels []chan LayerPromiseResult, lyrResult *layerResult, logger log.Log) {
	var result *LayerPromiseResult
	hasZeroBlockHash := false
	var firstErr error
	for _, res := range lyrResult.responses {
		if res.err == nil && res.data != nil {
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
	logger.With().Debug("notifying layer blocks result", log.String("blocks", fmt.Sprintf("%+v", *result)))
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
	if l.net.PeerCount() == 0 {
		return errors.New("no peers")
	}
	err := l.net.SendRequest(ctx, server.AtxIDsMsg, id.ToBytes(), fetch.GetRandomPeer(l.net.GetPeers()), receiveForPeerFunc, errFunc)
	if err != nil {
		return err
	}
	l.log.WithContext(ctx).With().Debug("waiting for epoch atx response", id)
	res := <-resCh
	if res.Error != nil {
		return res.Error
	}
	return l.GetAtxs(ctx, res.Atxs)
}

// getAtxResults is called when an ATX result is received
func (l *Logic) getAtxResults(ctx context.Context, hash types.Hash32, data []byte) error {
	l.log.WithContext(ctx).With().Debug("got response for ATX",
		log.String("hash", hash.ShortString()),
		log.Int("dataSize", len(data)))
	return l.atxs.HandleAtxData(ctx, data, l)
}

func (l *Logic) getTxResult(ctx context.Context, hash types.Hash32, data []byte) error {
	l.log.WithContext(ctx).With().Debug("got response for TX",
		log.String("hash", hash.ShortString()),
		log.Int("dataSize", len(data)))
	return l.txs.HandleTxSyncData(data)
}

// getPoetResult is handler function to poet proof fetch result
func (l *Logic) getPoetResult(ctx context.Context, hash types.Hash32, data []byte) error {
	l.log.WithContext(ctx).Debug("got poet ref",
		log.String("hash", hash.ShortString()),
		log.Int("dataSize", len(data)))
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
	// TODO: atx Id is currently only the header bytes - should we change it?
	results := l.fetcher.GetHashes(hashes, fetch.ATXDB, false)
	for hash, resC := range results {
		res := <-resC
		if res.Err != nil {
			l.log.WithContext(ctx).With().Error("cannot fetch atx",
				log.String("hash", hash.ShortString()),
				log.Err(res.Err))
			return res.Err
		}
		if !res.IsLocal {
			err := l.getAtxResults(ctx, res.Hash, res.Data)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// GetBlocks gets the data for given block ids and validates the blocks. returns an error if a single atx failed to be
// fetched or validated
func (l *Logic) GetBlocks(ctx context.Context, IDs []types.BlockID) error {
	l.log.WithContext(ctx).With().Debug("requesting blocks from peer", log.Int("numBlocks", len(IDs)))
	hashes := make([]types.Hash32, 0, len(IDs))
	for _, blockID := range IDs {
		hashes = append(hashes, blockID.AsHash32())
	}
	results := l.fetcher.GetHashes(hashes, fetch.BlockDB, false)
	for hash, resC := range results {
		res, open := <-resC
		if !open {
			l.log.WithContext(ctx).With().Info("block res channel closed", log.String("hash", hash.ShortString()))
			continue
		}
		if res.Err != nil {
			l.log.WithContext(ctx).With().Error("cannot find block", log.String("hash", hash.String()), log.Err(res.Err))
			return res.Err
		}
		if !res.IsLocal {
			if err := l.blockReceiveFunc(ctx, res.Data); err != nil {
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
			l.log.WithContext(ctx).With().Error("cannot find tx", log.String("hash", hash.String()), log.Err(res.Err))
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
	l.log.WithContext(ctx).With().Debug("getting poet proof", log.String("hash", id.ShortString()))
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
				l.log.WithContext(ctx).With().Info("empty tortoise beacon response (peer does not have it)",
					id,
					log.String("peer", peer.String()))
				return
			}

			if len(data) != types.Hash32Length {
				l.log.WithContext(ctx).With().Warning("tortoise beacon response contains bad data, ignoring",
					log.String("data", util.Bytes2Hex(data)))
				return
			}

			l.log.WithContext(ctx).With().Info("tortoise beacon response from peer",
				log.String("peer", peer.String()),
				log.String("beacon", types.BytesToHash(data).ShortString()))
			resCh <- data
		}
	}

	makeErrFunc := func(peer fmt.Stringer) func(error) {
		return func(err error) {
			l.log.WithContext(ctx).With().Warning("error in tortoise beacon response",
				log.String("peer", peer.String()),
				log.Err(err))
		}
	}

	l.log.WithContext(ctx).With().Info("requesting tortoise beacon from all peers", id)

	for _, p := range remotePeers {
		go func(peer peers.Peer) {
			select {
			case <-cancelCtx.Done():
				return
			default:
				l.log.WithContext(ctx).With().Debug("requesting tortoise beacon from peer",
					id,
					log.String("peer", peer.String()))
				err := l.net.SendRequest(cancelCtx, server.TortoiseBeaconMsg, id.ToBytes(), peer, makeReceiveFunc(peer), makeErrFunc(peer))
				if err != nil {
					l.log.WithContext(ctx).Warning("failed to send tortoise beacon request",
						log.String("peer", peer.String()),
						log.Err(err))
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
		l.log.WithContext(ctx).With().Debug("receiving tortoise beacon canceled", id)
		return nil

	case <-timer.C:
		l.log.WithContext(ctx).With().Debug("receiving tortoise beacon timed out", id, log.String("timeout", timeout.String()))
		return nil

	case res := <-resCh:
		resHash := types.BytesToHash(res)
		l.log.WithContext(ctx).With().Info("received tortoise beacon",
			id,
			log.String("beacon", resHash.ShortString()))
		return l.tbDB.SetTortoiseBeacon(id, resHash)
	}
}
