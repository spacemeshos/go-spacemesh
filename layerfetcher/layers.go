// Package layerfetcher fetches layers from remote peers
package layerfetcher

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/lp2p"
	"github.com/spacemeshos/go-spacemesh/lp2p/server"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./layers.go

// atxHandler defines handling function for incoming ATXs.
type atxHandler interface {
	HandleAtxData(context.Context, []byte) error
}

// blockHandler defines handling function for blocks.
type blockHandler interface {
	HandleBlockData(context.Context, []byte) error
}

// TxProcessor is an interface for handling TX data received in sync.
type TxProcessor interface {
	HandleTxSyncData(data []byte) error
}

// layerDB is an interface that returns layer data and blocks.
type layerDB interface {
	GetLayerHash(types.LayerID) types.Hash32
	GetAggregatedLayerHash(types.LayerID) types.Hash32
	LayerBlockIds(types.LayerID) ([]types.BlockID, error)
	GetLayerInputVectorByID(types.LayerID) ([]types.BlockID, error)
	SaveLayerInputVectorByID(context.Context, types.LayerID, []types.BlockID) error
	ProcessedLayer() types.LayerID
	SetZeroBlockLayer(types.LayerID) error
}

type atxIDsDB interface {
	GetEpochAtxs(epochID types.EpochID) ([]types.ATXID, error)
}

// poetDB is an interface to reading and storing poet proofs.
type poetDB interface {
	HasProof(proofRef []byte) bool
	ValidateAndStore(proofMessage *types.PoetProofMessage) error
	ValidateAndStoreMsg(data []byte) error
}

// network defines network capabilities used.
type network interface {
	GetPeers() []lp2p.Peer
	PeerCount() uint64
	Request(ctx context.Context, payload []byte, address lp2p.Peer, resHandler func(msg []byte), errorHandler func(err error))
	Close()
}

var (
	// ErrNoPeers is returned when node has no peers.
	ErrNoPeers = errors.New("no peers")
	// ErrInternal is returned from the peer when the peer encounters an internal error.
	ErrInternal = errors.New("unspecified error returned by peer")
	// ErrBlockNotFetched is returned when at least one block is not fetched successfully.
	ErrBlockNotFetched = errors.New("block not fetched")

	// errLayerNotProcessed is returned when requested layer was not yet processed.
	errLayerNotProcessed = errors.New("requested layer is not yet processed")
)

// peerResult captures the response from each peer.
type peerResult struct {
	data *layerBlocks
	err  error
}

// layerResult captures expected content of a layer across peers.
type layerResult struct {
	layerID     types.LayerID
	blocks      map[types.BlockID]struct{}
	inputVector []types.BlockID
	responses   map[lp2p.Peer]*peerResult
}

// LayerPromiseResult is the result of trying to fetch data for an entire layer.
type LayerPromiseResult struct {
	Err   error
	Layer types.LayerID
}

// Logic is the struct containing components needed to follow layer fetching logic.
type Logic struct {
	log                         log.Log
	fetcher                     fetch.Fetcher
	atxsrv, blocksrv, beaconsrv *server.Server
	host                        *lp2p.Host
	mutex                       sync.Mutex
	layerBlocksRes              map[types.LayerID]*layerResult
	layerBlocksChs              map[types.LayerID][]chan LayerPromiseResult
	poetProofs                  poetDB
	atxs                        atxHandler
	blockHandler                blockHandler
	txs                         TxProcessor
	layerDB                     layerDB
	atxIds                      atxIDsDB
	goldenATXID                 types.ATXID
}

// Config defines configuration for layer fetching logic.
type Config struct {
	RequestTimeout int
	GoldenATXID    types.ATXID
}

// DefaultConfig returns default configuration for layer fetching logic.
func DefaultConfig() Config {
	return Config{RequestTimeout: 10}
}

// NewLogic creates a new instance of layer fetching logic
func NewLogic(ctx context.Context, cfg Config, blocks blockHandler, atxs atxHandler, poet poetDB, atxIDs atxIDsDB, txs TxProcessor,
	host *lp2p.Host, fetcher fetch.Fetcher, layers layerDB, log log.Log) *Logic {
	l := &Logic{
		log:            log,
		fetcher:        fetcher,
		host:           host,
		layerBlocksRes: make(map[types.LayerID]*layerResult),
		layerBlocksChs: make(map[types.LayerID][]chan LayerPromiseResult),
		poetProofs:     poet,
		atxs:           atxs,
		layerDB:        layers,
		blockHandler:   blocks,
		atxIds:         atxIDs,
		txs:            txs,
		goldenATXID:    cfg.GoldenATXID,
	}
	l.atxsrv = server.New(host, layersProtocol+"atx", l.epochATXsReqReceiver)
	l.blocksrv = server.New(host, layersProtocol+"block", l.layerBlocksReqReceiver)
	return l
}

const (
	layersProtocol = "/layers/2.0/"
)

// Start starts layerFetcher logic and fetch component.
func (l *Logic) Start() {
	l.fetcher.Start()
}

// Close closes all running workers.
func (l *Logic) Close() {
	l.fetcher.Stop()
}

// AddDBs adds dbs that will be queried when sync requests are received. these databases will be exposed to external callers.
func (l *Logic) AddDBs(blockDB, AtxDB, TxDB, poetDB database.Getter) {
	l.fetcher.AddDB(fetch.BlockDB, blockDB)
	l.fetcher.AddDB(fetch.ATXDB, AtxDB)
	l.fetcher.AddDB(fetch.TXDB, TxDB)
	l.fetcher.AddDB(fetch.POETDB, poetDB)
}

// epochATXsReqReceiver returns the ATXs for the specified epoch.
func (l *Logic) epochATXsReqReceiver(ctx context.Context, msg []byte) ([]byte, error) {
	epoch := types.EpochID(util.BytesToUint32(msg))
	atxs, err := l.atxIds.GetEpochAtxs(epoch)
	if err != nil {
		return nil, fmt.Errorf("get epoch ATXs: %w", err)
	}

	l.log.WithContext(ctx).With().Debug("responded to epoch atxs request",
		epoch,
		log.Int("count", len(atxs)))
	bts, err := types.InterfaceToBytes(atxs)
	if err != nil {
		l.log.WithContext(ctx).With().Panic("failed to serialize epoch atxs", epoch, log.Err(err))
		return bts, fmt.Errorf("serialize: %w", err)
	}

	return bts, nil
}

// layerBlocksReqReceiver returns the block IDs for the specified layer hash,
// it also returns the validation vector for this data and the latest blocks received in gossip.
func (l *Logic) layerBlocksReqReceiver(ctx context.Context, req []byte) ([]byte, error) {
	lyrID := types.BytesToLayerID(req)
	processed := l.layerDB.ProcessedLayer()
	if lyrID.After(processed) {
		return nil, fmt.Errorf("%w: requested layer %v is higher than processed %v", errLayerNotProcessed, lyrID, processed)
	}
	b := &layerBlocks{
		ProcessedLayer: processed,
		Hash:           l.layerDB.GetLayerHash(lyrID),
		AggregatedHash: l.layerDB.GetAggregatedLayerHash(lyrID),
	}
	var err error
	b.Blocks, err = l.layerDB.LayerBlockIds(lyrID)
	if err != nil {
		// database.ErrNotFound should be considered a programming error since we are only responding for
		// layers older than processed layer
		l.log.WithContext(ctx).With().Warning("failed to get layer content", lyrID, log.Err(err))
		return nil, ErrInternal
	}

	if b.InputVector, err = l.layerDB.GetLayerInputVectorByID(lyrID); err != nil {
		l.log.WithContext(ctx).With().Warning("failed to get input vector for layer", lyrID, log.Err(err))
		return nil, ErrInternal
	}

	out, err := types.InterfaceToBytes(b)
	if err != nil {
		l.log.WithContext(ctx).With().Panic("failed to serialize layer blocks response", log.Err(err))
	}

	return out, nil
}

// initLayerPolling returns false if there is an ongoing polling of the given layer content,
// otherwise it initializes the polling and returns true.
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
		responses: make(map[lp2p.Peer]*peerResult),
	}
	return true
}

// PollLayerContent polls peers for the content of a given layer ID.
// it returns a channel for the caller to be notified when responses are received from all peers.
func (l *Logic) PollLayerContent(ctx context.Context, layerID types.LayerID) chan LayerPromiseResult {
	resChannel := make(chan LayerPromiseResult, 1)

	remotePeers := l.host.GetPeers()
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
		l.blocksrv.Request(ctx, peer, layerID.Bytes(), receiveForPeerFunc, errFunc)
	}
	return resChannel
}

// fetchLayerBlocks fetches the content of the block IDs in the specified layerBlocks.
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
	// TODO: revisit this when mesh hash resolution with peers is implemented
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
	// TODO check layer hash to be consistent with the content. if not, blacklist the peer
	return
}

// receiveLayerContent is called when response of block IDs for a layer hash is received from remote peer.
// if enough responses are received, it notifies the channels waiting for the layer blocks result.
func (l *Logic) receiveLayerContent(ctx context.Context, layerID types.LayerID, peer lp2p.Peer, expectedResults int, data []byte, peerErr error) {
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

	// make a copy of data and channels to avoid holding a lock while notifying
	go notifyLayerBlocksResult(ctx, layerID, l.layerDB, l.layerBlocksChs[layerID], result, l.log.WithContext(ctx).WithFields(layerID))
	delete(l.layerBlocksChs, layerID)
	delete(l.layerBlocksRes, layerID)
}

// notifyLayerBlocksResult determines the final result for the layer, and notifies subscribed channels when
// all blocks are fetched for a given layer.
// it deliberately doesn't hold any lock while notifying channels.
func notifyLayerBlocksResult(ctx context.Context, layerID types.LayerID, layerDB layerDB, channels []chan LayerPromiseResult, lyrResult *layerResult, logger log.Log) {
	var (
		missing, success bool
		err              error
	)
	for _, res := range lyrResult.responses {
		if res.err == nil && res.data != nil {
			success = true
		}
		if errors.Is(res.err, ErrBlockNotFetched) {
			// all fetches need to succeed
			missing = true
			err = res.err
			break
		}
		if err == nil {
			err = res.err
		}
	}

	result := LayerPromiseResult{Layer: layerID}
	// we tolerate errors from peers as long as we fetched all known blocks in this layer.
	if missing || !success {
		result.Err = err
	}

	if result.Err == nil {
		// save the input vector
		if len(lyrResult.inputVector) > 0 {
			if err := layerDB.SaveLayerInputVectorByID(ctx, layerID, lyrResult.inputVector); err != nil {
				logger.With().Error("failed to save input vector from peers", log.Err(err))
				result.Err = err
			}
		}
		if len(lyrResult.blocks) == 0 {
			if err := layerDB.SetZeroBlockLayer(layerID); err != nil {
				// this can happen when node actually had received blocks for this layer before. ok to ignore
				logger.With().Warning("failed to set zero-block for layer", layerID, log.Err(err))
			}
		}
	}

	logger.With().Debug("notifying layer blocks result", log.String("blocks", fmt.Sprintf("%+v", result)))
	for _, ch := range channels {
		ch <- result
	}
}

type epochAtxRes struct {
	Error error
	Atxs  []types.ATXID
}

// GetEpochATXs fetches all atxs received by peer for given layer.
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
	if l.host.PeerCount() == 0 {
		return errors.New("no peers")
	}
	l.atxsrv.Request(ctx, fetch.GetRandomPeer(l.host.GetPeers()), id.ToBytes(), receiveForPeerFunc, errFunc)
	l.log.WithContext(ctx).With().Debug("waiting for epoch atx response", id)
	res := <-resCh
	if res.Error != nil {
		return res.Error
	}

	if err := l.GetAtxs(ctx, res.Atxs); err != nil {
		return fmt.Errorf("get ATXs: %w", err)
	}

	return nil
}

// getAtxResults is called when an ATX result is received.
func (l *Logic) getAtxResults(ctx context.Context, hash types.Hash32, data []byte) error {
	l.log.WithContext(ctx).With().Debug("got response for ATX",
		log.String("hash", hash.ShortString()),
		log.Int("dataSize", len(data)))

	if err := l.atxs.HandleAtxData(ctx, data); err != nil {
		return fmt.Errorf("handle ATX data: %w", err)
	}

	return nil
}

func (l *Logic) getTxResult(ctx context.Context, hash types.Hash32, data []byte) error {
	l.log.WithContext(ctx).With().Debug("got response for TX",
		log.String("hash", hash.ShortString()),
		log.Int("dataSize", len(data)))

	if err := l.txs.HandleTxSyncData(data); err != nil {
		return fmt.Errorf("handle tx sync data: %w", err)
	}

	return nil
}

// getPoetResult is handler function to poet proof fetch result.
func (l *Logic) getPoetResult(ctx context.Context, hash types.Hash32, data []byte) error {
	l.log.WithContext(ctx).Debug("got poet ref",
		log.String("hash", hash.ShortString()),
		log.Int("dataSize", len(data)))

	if err := l.poetProofs.ValidateAndStoreMsg(data); err != nil {
		return fmt.Errorf("validate and store message: %w", err)
	}

	return nil
}

// blockReceiveFunc handles blocks received via fetch.
func (l *Logic) blockReceiveFunc(ctx context.Context, data []byte) error {
	if err := l.blockHandler.HandleBlockData(ctx, data); err != nil {
		return fmt.Errorf("handle block data: %w", err)
	}

	return nil
}

// IsSynced indicates if this node is synced.
func (l *Logic) IsSynced(context.Context) bool {
	// todo: add this logic
	return true
}

// ListenToGossip indicates if node is currently accepting packets from gossip.
func (l *Logic) ListenToGossip() bool {
	// todo: add this logic
	return true
}

// Future is a preparation for using actual futures in the code, this will allow to truly execute
// asynchronous reads and receive result only when needed.
type Future struct {
	res chan fetch.HashDataPromiseResult
	ret *fetch.HashDataPromiseResult
}

// Result actually evaluates the result of the fetch task.
func (f *Future) Result() fetch.HashDataPromiseResult {
	if f.ret == nil {
		ret := <-f.res
		f.ret = &ret
	}
	return *f.ret
}

// FetchAtx returns error if ATX was not found.
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

// FetchBlock gets data for a single block id and validates it.
func (l *Logic) FetchBlock(ctx context.Context, id types.BlockID) error {
	res, open := <-l.fetcher.GetHash(id.AsHash32(), fetch.BlockDB, false)
	if !open {
		return fmt.Errorf("stopped on call for id %v", id.String())
	}
	if res.Err != nil {
		return res.Err
	}
	if !res.IsLocal {
		if err := l.blockHandler.HandleBlockData(ctx, res.Data); err != nil {
			return fmt.Errorf("handle block data: %w", err)
		}

		return nil
	}
	return res.Err
}

// GetAtxs gets the data for given atx ids IDs and validates them. returns an error if at least one ATX cannot be fetched.
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
// fetched or validated.
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

// GetTxs fetches the txs provided as IDs and validates them, returns an error if one TX failed to be fetched.
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

// GetPoetProof gets poet proof from remote peer.
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
