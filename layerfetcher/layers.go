// Package layerfetcher fetches layers from remote peers
package layerfetcher

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/bootstrap"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sql"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./layers.go

// atxHandler defines handling function for incoming ATXs.
type atxHandler interface {
	HandleAtxData(context.Context, []byte, p2p.Peer) error
}

// blockHandler defines handling function for blocks.
type blockHandler interface {
	HandleBlockData(context.Context, []byte, p2p.Peer) error
}

// ballotHandler defines handling function for ballots.
type ballotHandler interface {
	HandleBallotData(context.Context, []byte, p2p.Peer) error
}

// proposalHandler defines handling function for proposals.
type proposalHandler interface {
	HandleProposalData(context.Context, []byte, p2p.Peer) error
}

// txHandler is an interface for handling TX data received in sync.
type txHandler interface {
	HandleSyncTransaction(context.Context, []byte, p2p.Peer) error
}

// layerDB is an interface that returns layer data and blocks.
type layerDB interface {
	GetLayerHash(types.LayerID) (types.Hash32, error)
	GetAggregatedLayerHash(types.LayerID) (types.Hash32, error)
	LayerBallotIDs(types.LayerID) ([]types.BallotID, error)
	LayerBlockIds(types.LayerID) ([]types.BlockID, error)
	GetHareConsensusOutput(types.LayerID) (types.BlockID, error)
	SaveHareConsensusOutput(context.Context, types.LayerID, types.BlockID) error
	ProcessedLayer() types.LayerID
	SetZeroBlockLayer(types.LayerID) error
}

type atxIDsDB interface {
	GetEpochAtxs(epochID types.EpochID) ([]types.ATXID, error)
}

// poetDB is an interface to reading and storing poet proofs.
type poetDB interface {
	ValidateAndStoreMsg(data []byte) error
}

type network interface {
	bootstrap.Provider
	server.Host
}

var (
	// ErrNoPeers is returned when node has no peers.
	ErrNoPeers = errors.New("no peers")
	// ErrInternal is returned from the peer when the peer encounters an internal error.
	ErrInternal = errors.New("unspecified error returned by peer")
	// ErrLayerDataNotFetched is returned when any layer data is not fetched successfully.
	ErrLayerDataNotFetched = errors.New("layer data not fetched")

	// errLayerNotProcessed is returned when requested layer was not yet processed.
	errLayerNotProcessed = errors.New("requested layer is not yet processed")
)

// peerResult captures the response from each peer.
type peerResult struct {
	data *layerData
	err  error
}

// layerResult captures expected content of a layer across peers.
type layerResult struct {
	layerID    types.LayerID
	ballots    map[types.BallotID]struct{}
	blocks     map[types.BlockID]struct{}
	hareOutput types.BlockID
	responses  map[p2p.Peer]*peerResult
}

// LayerPromiseResult is the result of trying to fetch data for an entire layer.
type LayerPromiseResult struct {
	Err   error
	Layer types.LayerID
}

// Logic is the struct containing components needed to follow layer fetching logic.
type Logic struct {
	log              log.Log
	fetcher          fetch.Fetcher
	atxsrv, blocksrv server.Requestor
	host             network
	mutex            sync.Mutex
	layerBlocksRes   map[types.LayerID]*layerResult
	layerBlocksChs   map[types.LayerID][]chan LayerPromiseResult
	poetProofs       poetDB
	atxHandler       atxHandler
	ballotHandler    ballotHandler
	blockHandler     blockHandler
	proposalHandler  proposalHandler
	txHandler        txHandler
	layerDB          layerDB
	atxIds           atxIDsDB
	goldenATXID      types.ATXID
}

// Config defines configuration for layer fetching logic.
type Config struct {
	fetch.Config
	GoldenATXID types.ATXID
}

// DefaultConfig returns default configuration for layer fetching logic.
func DefaultConfig() Config {
	return Config{
		Config: fetch.DefaultConfig(),
	}
}

const (
	blockProtocol = "/block/1.0.0"
	atxProtocol   = "/atx/1.0.0"
)

// DataHandlers collects handlers for different data type.
type DataHandlers struct {
	ATX      atxHandler
	Ballot   ballotHandler
	Block    blockHandler
	Proposal proposalHandler
	TX       txHandler
}

// NewLogic creates a new instance of layer fetching logic.
func NewLogic(ctx context.Context, cfg Config, poet poetDB, atxIDs atxIDsDB, layers layerDB,
	host *p2p.Host, handlers DataHandlers, dbStores fetch.LocalDataSource, log log.Log,
) *Logic {
	l := &Logic{
		log:             log,
		fetcher:         fetch.NewFetch(ctx, cfg.Config, host, dbStores, log.WithName("fetch")),
		host:            host,
		goldenATXID:     cfg.GoldenATXID,
		layerBlocksRes:  make(map[types.LayerID]*layerResult),
		layerBlocksChs:  make(map[types.LayerID][]chan LayerPromiseResult),
		poetProofs:      poet,
		layerDB:         layers,
		atxIds:          atxIDs,
		atxHandler:      handlers.ATX,
		ballotHandler:   handlers.Ballot,
		blockHandler:    handlers.Block,
		proposalHandler: handlers.Proposal,
		txHandler:       handlers.TX,
	}
	l.atxsrv = server.New(host, atxProtocol, l.epochATXsReqReceiver, server.WithLog(log))
	l.blocksrv = server.New(host, blockProtocol, l.layerContentReqReceiver, server.WithLog(log))
	return l
}

// Start starts layerFetcher logic and fetch component.
func (l *Logic) Start() {
	l.fetcher.Start()
}

// Close closes all running workers.
func (l *Logic) Close() {
	l.fetcher.Stop()
}

// epochATXsReqReceiver returns the ATXs for the specified epoch.
func (l *Logic) epochATXsReqReceiver(ctx context.Context, msg []byte) ([]byte, error) {
	epoch := types.EpochID(util.BytesToUint32(msg))
	atxs, err := l.atxIds.GetEpochAtxs(epoch)
	if err != nil {
		return nil, fmt.Errorf("get epoch ATXs: %w", err)
	}

	l.log.WithContext(ctx).With().Debug("responded to epoch atx request",
		epoch,
		log.Int("count", len(atxs)))
	bts, err := codec.Encode(atxs)
	if err != nil {
		l.log.WithContext(ctx).With().Panic("failed to serialize epoch atx", epoch, log.Err(err))
		return bts, fmt.Errorf("serialize: %w", err)
	}

	return bts, nil
}

// layerContentReqReceiver returns the block IDs for the specified layer hash,
// it also returns the validation vector for this data and the latest blocks received in gossip.
func (l *Logic) layerContentReqReceiver(ctx context.Context, req []byte) ([]byte, error) {
	lyrID := types.BytesToLayerID(req)
	processed := l.layerDB.ProcessedLayer()
	if lyrID.After(processed) {
		return nil, fmt.Errorf("%w: requested layer %v is higher than processed %v", errLayerNotProcessed, lyrID, processed)
	}
	ld := &layerData{ProcessedLayer: processed}
	var err error
	ld.Hash, err = l.layerDB.GetLayerHash(lyrID)
	if err != nil {
		l.log.WithContext(ctx).With().Warning("failed to get layer hash", lyrID, log.Err(err))
		return nil, ErrInternal
	}
	ld.AggregatedHash, err = l.layerDB.GetAggregatedLayerHash(lyrID)
	if err != nil {
		l.log.WithContext(ctx).With().Warning("failed to get aggregated layer hash", lyrID, log.Err(err))
		return nil, ErrInternal
	}
	ld.Ballots, err = l.layerDB.LayerBallotIDs(lyrID)
	if err != nil {
		// database.ErrNotFound should be considered a programming error since we are only responding for
		// layers older than processed layer
		l.log.WithContext(ctx).With().Warning("failed to get layer ballots", lyrID, log.Err(err))
		return nil, ErrInternal
	}
	ld.Blocks, err = l.layerDB.LayerBlockIds(lyrID)
	if err != nil {
		// database.ErrNotFound should be considered a programming error since we are only responding for
		// layers older than processed layer
		l.log.WithContext(ctx).With().Warning("failed to get layer blocks", lyrID, log.Err(err))
		return nil, ErrInternal
	}
	if ld.HareOutput, err = l.layerDB.GetHareConsensusOutput(lyrID); err != nil {
		l.log.WithContext(ctx).With().Warning("failed to get hare output for layer", lyrID, log.Err(err))
		return nil, ErrInternal
	}

	out, err := codec.Encode(ld)
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
		ballots:   make(map[types.BallotID]struct{}),
		blocks:    make(map[types.BlockID]struct{}),
		responses: make(map[p2p.Peer]*peerResult),
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
		if err := l.blocksrv.Request(ctx, peer, layerID.Bytes(), receiveForPeerFunc, errFunc); err != nil {
			errFunc(err)
		}
	}
	return resChannel
}

// fetchLayerData fetches the all content referenced in layerData.
func (l *Logic) fetchLayerData(ctx context.Context, logger log.Log, peer p2p.Peer, layerID types.LayerID, blocks *layerData) error {
	logger = logger.WithFields(log.Int("num_ballots", len(blocks.Ballots)), log.Int("num_blocks", len(blocks.Blocks)))
	l.mutex.Lock()
	lyrResult := l.layerBlocksRes[layerID]
	var ballotsToFetch []types.BallotID
	for _, ballotID := range blocks.Ballots {
		if _, ok := lyrResult.ballots[ballotID]; !ok {
			// not yet fetched
			lyrResult.ballots[ballotID] = struct{}{}
			ballotsToFetch = append(ballotsToFetch, ballotID)
		}
	}
	var blocksToFetch []types.BlockID
	for _, blkID := range blocks.Blocks {
		if _, ok := lyrResult.blocks[blkID]; !ok {
			// not yet fetched
			lyrResult.blocks[blkID] = struct{}{}
			blocksToFetch = append(blocksToFetch, blkID)
		}
	}
	if lyrResult.hareOutput == types.EmptyBlockID {
		if blocks.HareOutput != types.EmptyBlockID {
			logger.With().Info("adopting hare output", blocks.HareOutput)
			lyrResult.hareOutput = blocks.HareOutput
		}
	} else if blocks.HareOutput != types.EmptyBlockID && lyrResult.hareOutput != blocks.HareOutput {
		logger.With().Warning("found different hare output from peer", blocks.HareOutput)
		// TODO do something in combination of the layer hash difference and resolve differences with peers
	}
	l.mutex.Unlock()

	l.fetcher.TrackBallotPeers(ctx, peer, ballotsToFetch)

	logger.With().Debug("fetching new ballots", log.Int("to_fetch", len(ballotsToFetch)))
	if err := l.GetBallots(ctx, ballotsToFetch, peer); err != nil {
		// fail sync for the entire layer
		return err
	}

	l.fetcher.TrackBlockPeers(ctx, peer, blocksToFetch)

	logger.With().Debug("fetching new blocks", log.Int("to_fetch", len(blocksToFetch)))
	if err := l.GetBlocks(ctx, blocksToFetch); err != nil {
		logger.With().Warning("failed fetching new blocks", log.Err(err))
		// syntactically invalid blocks are expected from malicious peers
	}
	return nil
}

func extractPeerResult(logger log.Log, layerID types.LayerID, data []byte, peerErr error) (result peerResult) {
	result.err = peerErr
	if peerErr != nil {
		logger.With().Debug("received peer error for layer content", layerID, log.Err(peerErr))
		return
	}

	var blocks layerData
	if err := codec.Decode(data, &blocks); err != nil {
		logger.With().Debug("error converting bytes to layerData", log.Err(err))
		result.err = err
		return
	}

	result.data = &blocks
	// TODO check layer hash to be consistent with the content. if not, blacklist the peer
	return
}

// receiveLayerContent is called when response of block IDs for a layer hash is received from remote peer.
// if enough responses are received, it notifies the channels waiting for the layer blocks result.
func (l *Logic) receiveLayerContent(ctx context.Context, layerID types.LayerID, peer p2p.Peer, expectedResults int, data []byte, peerErr error) {
	logger := l.log.WithContext(ctx).WithFields(layerID, log.String("peer", peer.String()))
	logger.Debug("received layer content from peer")
	peerRes := extractPeerResult(logger, layerID, data, peerErr)
	if peerRes.err == nil {
		if err := l.fetchLayerData(ctx, logger, peer, layerID, peerRes.data); err != nil {
			peerRes.err = ErrLayerDataNotFetched
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
	go notifyLayerDataResult(ctx, layerID, l.layerDB, l.layerBlocksChs[layerID], result, l.log.WithContext(ctx).WithFields(layerID))
	delete(l.layerBlocksChs, layerID)
	delete(l.layerBlocksRes, layerID)
}

// notifyLayerDataResult determines the final result for the layer, and notifies subscribed channels when
// all blocks are fetched for a given layer.
// it deliberately doesn't hold any lock while notifying channels.
func notifyLayerDataResult(ctx context.Context, layerID types.LayerID, layerDB layerDB, channels []chan LayerPromiseResult, lyrResult *layerResult, logger log.Log) {
	var (
		missing, success bool
		err              error
	)
	for _, res := range lyrResult.responses {
		if res.err == nil && res.data != nil {
			success = true
		}
		if errors.Is(res.err, ErrLayerDataNotFetched) {
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
		// FIXME should not blindly save hare output from peer, potentially overwriting local hare's
		// consensus result. see https://github.com/spacemeshos/go-spacemesh/issues/3200
		if err := layerDB.SaveHareConsensusOutput(ctx, layerID, lyrResult.hareOutput); err != nil {
			logger.With().Error("failed to save hare output from peers", log.Err(err))
			result.Err = err
		}
		if len(lyrResult.blocks) == 0 {
			if err := layerDB.SetZeroBlockLayer(layerID); err != nil {
				// this can happen when node actually had received blocks for this layer before. ok to ignore
				logger.With().Warning("failed to set zero-block for layer", layerID, log.Err(err))
			}
		}
	}

	logger.With().Debug("notifying layer content result", log.String("layer", fmt.Sprintf("%+v", result)))
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
		err := codec.Decode(data, &atxsIDs)
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
	peer := fetch.GetRandomPeer(l.host.GetPeers())
	if err := l.atxsrv.Request(ctx, peer, id.ToBytes(), receiveForPeerFunc, errFunc); err != nil {
		return fmt.Errorf("failed to send request to the peer: %w", err)
	}
	l.log.WithContext(ctx).With().Debug("waiting for epoch atx response", id)
	res := <-resCh
	if res.Error != nil {
		return res.Error
	}

	l.log.WithContext(ctx).With().Debug("tracking peer for atxs",
		log.Int("to_fetch", len(res.Atxs)),
		log.String("peer", peer.String()))
	l.fetcher.TrackATXPeers(ctx, peer, res.Atxs)

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

	if err := l.atxHandler.HandleAtxData(ctx, data, p2p.AnyPeer); err != nil {
		return fmt.Errorf("handle ATX data %s len %d: %w", hash, len(data), err)
	}

	return nil
}

// getPoetResult is handler function to poet proof fetch result.
func (l *Logic) getPoetResult(ctx context.Context, hash types.Hash32, data []byte) error {
	l.log.WithContext(ctx).Debug("got poet ref",
		log.String("hash", hash.ShortString()),
		log.Int("dataSize", len(data)))

	if err := l.poetProofs.ValidateAndStoreMsg(data); err != nil && !errors.Is(err, sql.ErrObjectExists) {
		return fmt.Errorf("validate and store message: %w", err)
	}

	return nil
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

// GetAtxs gets the data for given atx ids IDs and validates them. returns an error if at least one ATX cannot be fetched.
func (l *Logic) GetAtxs(ctx context.Context, ids []types.ATXID) error {
	if len(ids) == 0 {
		return nil
	}
	l.log.WithContext(ctx).With().Debug("requesting atxs from peer", log.Int("num_atxs", len(ids)))
	hashes := types.ATXIDsToHashes(ids)
	if errs := l.getHashes(ctx, hashes, fetch.ATXDB, p2p.AnyPeer, l.atxHandler.HandleAtxData); len(errs) > 0 {
		return errs[0]
	}
	return nil
}

type dataReceiver func(context.Context, []byte, p2p.Peer) error

func (l *Logic) getHashes(ctx context.Context, hashes []types.Hash32, hint fetch.Hint, peer p2p.Peer, receiver dataReceiver) []error {
	errs := make([]error, 0, len(hashes))
	results := l.fetcher.GetHashes(hashes, hint, false)
	for hash, resC := range results {
		res, open := <-resC
		if !open {
			l.log.WithContext(ctx).With().Info("res channel closed",
				log.String("hint", string(hint)),
				log.String("hash", hash.ShortString()))
			continue
		}
		if res.Err != nil {
			l.log.WithContext(ctx).With().Warning("cannot find hash",
				log.String("hint", string(hint)),
				log.String("hash", hash.String()),
				log.Err(res.Err))
			errs = append(errs, res.Err)
			continue
		}
		if !res.IsLocal {
			if err := receiver(ctx, res.Data, peer); err != nil {
				l.log.WithContext(ctx).With().Warning("failed to handle data",
					log.String("hint", string(hint)),
					log.String("hash", hash.String()),
					log.Err(err))
				errs = append(errs, err)
			}
		}
	}
	return errs
}

// GetBallots gets data for the specified BallotIDs and validates them.
func (l *Logic) GetBallots(ctx context.Context, ids []types.BallotID, peer p2p.Peer) error {
	if len(ids) == 0 {
		return nil
	}
	l.log.WithContext(ctx).With().Debug("requesting ballots from peer", log.Int("num_ballots", len(ids)))
	hashes := types.BallotIDsToHashes(ids)
	if errs := l.getHashes(ctx, hashes, fetch.BallotDB, peer, l.ballotHandler.HandleBallotData); len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// GetProposals gets the data for given proposal IDs from peers.
func (l *Logic) GetProposals(ctx context.Context, ids []types.ProposalID) error {
	if len(ids) == 0 {
		return nil
	}
	l.log.WithContext(ctx).With().Debug("requesting proposals from peer", log.Int("num_proposals", len(ids)))
	hashes := types.ProposalIDsToHashes(ids)
	if errs := l.getHashes(ctx, hashes, fetch.ProposalDB, p2p.AnyPeer, l.proposalHandler.HandleProposalData); len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// GetBlocks gets the data for given block IDs from peers.
func (l *Logic) GetBlocks(ctx context.Context, ids []types.BlockID) error {
	if len(ids) == 0 {
		return nil
	}
	l.log.WithContext(ctx).With().Debug("requesting blocks from peer", log.Int("num_blocks", len(ids)))
	hashes := types.BlockIDsToHashes(ids)
	if errs := l.getHashes(ctx, hashes, fetch.BlockDB, p2p.AnyPeer, l.blockHandler.HandleBlockData); len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// GetTxs fetches the txs provided as IDs and validates them, returns an error if one TX failed to be fetched.
func (l *Logic) GetTxs(ctx context.Context, ids []types.TransactionID) error {
	if len(ids) == 0 {
		return nil
	}
	l.log.WithContext(ctx).With().Debug("requesting txs from peer", log.Int("num_txs", len(ids)))
	hashes := types.TransactionIDsToHashes(ids)
	if errs := l.getHashes(ctx, hashes, fetch.TXDB, p2p.AnyPeer, l.txHandler.HandleSyncTransaction); len(errs) > 0 {
		return errs[0]
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

// TrackBlockPeers is a wrapper around fetcher's TrackBlockPeers.
func (l *Logic) TrackBlockPeers(ctx context.Context, peer p2p.Peer, ids []types.BlockID) {
	l.fetcher.TrackBlockPeers(ctx, peer, ids)
}

// TrackBallotPeers is a wrapper around fetcher's TrackBallotPeers.
func (l *Logic) TrackBallotPeers(ctx context.Context, peer p2p.Peer, ids []types.BallotID) {
	l.fetcher.TrackBallotPeers(ctx, peer, ids)
}

// TrackATXPeers is a wrapper around fetcher's TrackATXPeers.
func (l *Logic) TrackATXPeers(ctx context.Context, peer p2p.Peer, ids []types.ATXID) {
	l.fetcher.TrackATXPeers(ctx, peer, ids)
}
