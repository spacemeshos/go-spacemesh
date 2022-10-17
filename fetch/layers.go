package fetch

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sql"
)

var (
	// errLayerDataNotFetched is returned when any layer data is not fetched successfully.
	errLayerDataNotFetched = errors.New("layer data not fetched")

	errLayerOpinionsNotFetched = errors.New("layer opinions not fetched")
)

// peerResult captures the response from each peer.
type peerResult struct {
	data    *LayerData
	opinion *LayerOpinion
	err     error
}

// opinionsResult captures opinions of a layer across peers.
type opinionsResult struct {
	responses map[p2p.Peer]*peerResult
}

// dataResult captures content of a layer across peers.
type dataResult struct {
	ballots   map[types.BallotID]struct{}
	blocks    map[types.BlockID]struct{}
	responses map[p2p.Peer]*peerResult
}

// LayerPromiseData is the result of trying to fetch data for an entire layer.
type LayerPromiseData struct {
	Err   error
	Layer types.LayerID
}

// LayerPromiseOpinions is the result of trying to fetch opinions for a layer.
type LayerPromiseOpinions struct {
	Err      error
	Layer    types.LayerID
	Opinions []*LayerOpinion
}

// Logic is the struct containing components needed to follow layer fetching logic.
type Logic struct {
	log     log.Log
	eg      errgroup.Group
	cdb     *datastore.CachedDB
	msh     meshProvider
	fetcher fetcher

	mu          sync.Mutex
	dataResults map[types.LayerID]*dataResult
	dataChs     map[types.LayerID][]chan LayerPromiseData
	opnResults  map[types.LayerID]*opinionsResult
	opnChs      map[types.LayerID][]chan LayerPromiseOpinions

	poetHandler     poetHandler
	atxHandler      atxHandler
	ballotHandler   ballotHandler
	blockHandler    blockHandler
	proposalHandler proposalHandler
	txHandler       txHandler
}

// DataHandlers collects handlers for different data type.
type DataHandlers struct {
	ATX      atxHandler
	Ballot   ballotHandler
	Block    blockHandler
	Proposal proposalHandler
	TX       txHandler
	Poet     poetHandler
}

// NewLogic creates a new instance of layer fetching logic.
func NewLogic(cfg Config, cdb *datastore.CachedDB, msh meshProvider, host *p2p.Host, handlers DataHandlers, log log.Log) *Logic {
	l := &Logic{
		log:             log,
		msh:             msh,
		cdb:             cdb,
		dataResults:     make(map[types.LayerID]*dataResult),
		dataChs:         make(map[types.LayerID][]chan LayerPromiseData),
		opnResults:      make(map[types.LayerID]*opinionsResult),
		opnChs:          make(map[types.LayerID][]chan LayerPromiseOpinions),
		poetHandler:     handlers.Poet,
		atxHandler:      handlers.ATX,
		ballotHandler:   handlers.Ballot,
		blockHandler:    handlers.Block,
		proposalHandler: handlers.Proposal,
		txHandler:       handlers.TX,
	}
	bs := datastore.NewBlobStore(cdb.Database)
	h := newHandler(cdb, bs, msh, log)
	atxSrv := server.New(host, atxProtocol, h.handleEpochATXIDsReq,
		server.WithTimeout(time.Duration(cfg.RequestTimeout)*time.Second),
		server.WithLog(log),
	)
	lyrSrv := server.New(host, lyrDataProtocol, h.handleLayerDataReq,
		server.WithTimeout(time.Duration(cfg.RequestTimeout)*time.Second),
		server.WithLog(log),
	)
	opnSrv := server.New(host, lyrOpnsProtocol, h.handleLayerOpinionsReq,
		server.WithTimeout(time.Duration(cfg.RequestTimeout)*time.Second),
		server.WithLog(log),
	)
	hashSrv := server.New(host, hashProtocol, h.handleHashReq,
		server.WithTimeout(time.Duration(cfg.RequestTimeout)*time.Second),
		server.WithLog(log),
	)
	l.fetcher = newFetch(cfg, host, bs, atxSrv, lyrSrv, opnSrv, hashSrv, log.WithName("fetch"))
	return l
}

// Start starts fetcher logic and fetch component.
func (l *Logic) Start() {
	l.fetcher.Start()
}

// Close closes all running workers.
func (l *Logic) Close() {
	l.fetcher.Stop()
	_ = l.eg.Wait()
}

// initDataPolling returns false if there is an ongoing polling of the given layer content,
// otherwise it initializes the polling and returns true.
func (l *Logic) initDataPolling(layerID types.LayerID, ch chan LayerPromiseData) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.dataChs[layerID] = append(l.dataChs[layerID], ch)
	if _, ok := l.dataResults[layerID]; ok {
		// polling of data for this layerID is ongoing, just wait for the polling to finish and get notified
		return false
	}
	l.dataResults[layerID] = &dataResult{
		ballots:   make(map[types.BallotID]struct{}),
		blocks:    make(map[types.BlockID]struct{}),
		responses: make(map[p2p.Peer]*peerResult),
	}
	return true
}

// PollLayerData polls peers for the data in the specified layer.
// it returns a channel for the caller to be notified when responses are received from all peers.
func (l *Logic) PollLayerData(ctx context.Context, layerID types.LayerID) chan LayerPromiseData {
	ch := make(chan LayerPromiseData, 1)
	if !l.initDataPolling(layerID, ch) {
		return ch
	}

	okFunc := func(data []byte, peer p2p.Peer, numPeers int) {
		l.receiveData(ctx, layerID, peer, numPeers, data, nil)
	}
	errFunc := func(err error, peer p2p.Peer, numPeers int) {
		l.receiveData(ctx, layerID, peer, numPeers, nil, err)
	}
	if err := l.fetcher.GetLayerData(ctx, layerID, okFunc, errFunc); err != nil {
		ch <- LayerPromiseData{Layer: layerID, Err: err}
	}
	return ch
}

// initOpinionsPolling returns false if there is an ongoing polling of the given layer opinions,
// otherwise it initializes the polling and returns true.
func (l *Logic) initOpinionsPolling(layerID types.LayerID, ch chan LayerPromiseOpinions) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.opnChs[layerID] = append(l.opnChs[layerID], ch)
	if _, ok := l.opnResults[layerID]; ok {
		// polling of opinions for this layerID is ongoing, just wait for the polling to finish and get notified
		return false
	}
	l.opnResults[layerID] = &opinionsResult{
		responses: make(map[p2p.Peer]*peerResult),
	}
	return true
}

// PollLayerOpinions polls peers for the opinions on blocks in the specified layer.
// it returns a channel for the caller to be notified when responses are received from all peers.
func (l *Logic) PollLayerOpinions(ctx context.Context, layerID types.LayerID) chan LayerPromiseOpinions {
	ch := make(chan LayerPromiseOpinions, 1)
	if !l.initOpinionsPolling(layerID, ch) {
		return ch
	}

	okFunc := func(data []byte, peer p2p.Peer, numPeers int) {
		l.receiveOpinions(ctx, layerID, peer, numPeers, data, nil)
	}
	errFunc := func(err error, peer p2p.Peer, numPeers int) {
		l.receiveOpinions(ctx, layerID, peer, numPeers, nil, err)
	}
	if err := l.fetcher.GetLayerOpinions(ctx, layerID, okFunc, errFunc); err != nil {
		ch <- LayerPromiseOpinions{Layer: layerID, Err: err}
	}
	return ch
}

func (l *Logic) receiveOpinions(ctx context.Context, layerID types.LayerID, peer p2p.Peer, expectedResults int, data []byte, peerErr error) {
	logger := l.log.WithContext(ctx).WithFields(layerID, log.Stringer("peer", peer))
	logger.Debug("received layer opinions from peer")

	pr := &peerResult{
		err: peerErr,
	}
	var lo LayerOpinion
	if peerErr != nil {
		logger.With().Debug("received peer error for layer opinions", log.Err(peerErr))
	} else if pr.err = codec.Decode(data, &lo); pr.err != nil {
		logger.With().Debug("error converting bytes to LayerOpinion", log.Err(pr.err))
	} else {
		lo.SetPeer(peer)
		pr.opinion = &lo
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	l.opnResults[layerID].responses[peer] = pr
	if len(l.opnResults[layerID].responses) < expectedResults {
		logger.With().Debug("not yet received all responses from peers",
			log.Int("received", len(l.opnResults[layerID].responses)),
			log.Int("expected", expectedResults))
		return
	}

	res := LayerPromiseOpinions{
		Layer:    layerID,
		Opinions: make([]*LayerOpinion, 0, expectedResults),
	}
	for _, pr := range l.opnResults[layerID].responses {
		if pr.err == nil {
			res.Opinions = append(res.Opinions, pr.opinion)
		}
	}
	if len(res.Opinions) == 0 {
		res.Err = errLayerOpinionsNotFetched
	}

	chs := l.opnChs[layerID]
	l.eg.Go(func() error {
		notifyOpinionsResult(ctx, chs, res)
		return nil
	})
	delete(l.opnChs, layerID)
	delete(l.opnResults, layerID)
}

func notifyOpinionsResult(ctx context.Context, channels []chan LayerPromiseOpinions, res LayerPromiseOpinions) {
	for _, ch := range channels {
		select {
		case ch <- res:
		case <-ctx.Done():
		}
	}
}

// registerLayerHashes registers provided hashes with provided peer.
func (l *Logic) registerLayerHashes(peer p2p.Peer, data *LayerData) {
	if data == nil {
		return
	}
	var layerHashes []types.Hash32
	for _, ballotID := range data.Ballots {
		layerHashes = append(layerHashes, ballotID.AsHash32())
	}
	for _, blkID := range data.Blocks {
		layerHashes = append(layerHashes, blkID.AsHash32())
	}
	if len(layerHashes) == 0 {
		return
	}

	l.fetcher.RegisterPeerHashes(peer, layerHashes)
}

// fetchLayerData fetches the all data referenced in layerData.
func (l *Logic) fetchLayerData(ctx context.Context, logger log.Log, layerID types.LayerID, ldata *LayerData) error {
	logger = logger.WithFields(log.Int("num_ballots", len(ldata.Ballots)), log.Int("num_blocks", len(ldata.Blocks)))
	l.mu.Lock()
	lyrResult := l.dataResults[layerID]
	var ballotsToFetch []types.BallotID
	for _, ballotID := range ldata.Ballots {
		if _, ok := lyrResult.ballots[ballotID]; !ok {
			// not yet fetched
			lyrResult.ballots[ballotID] = struct{}{}
			ballotsToFetch = append(ballotsToFetch, ballotID)
		}
	}
	var blocksToFetch []types.BlockID
	for _, blkID := range ldata.Blocks {
		if _, ok := lyrResult.blocks[blkID]; !ok {
			// not yet fetched
			lyrResult.blocks[blkID] = struct{}{}
			blocksToFetch = append(blocksToFetch, blkID)
		}
	}
	l.mu.Unlock()

	logger.With().Debug("fetching new ballots", log.Int("to_fetch", len(ballotsToFetch)))
	if err := l.GetBallots(ctx, ballotsToFetch); err != nil {
		logger.With().Warning("failed fetching new ballots",
			log.Array("ballot_ids", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
				for _, bid := range ballotsToFetch {
					encoder.AppendString(bid.String())
				}
				return nil
			})),
			log.Err(err))
		// syntactically invalid ballots are expected from malicious peers
	}

	logger.With().Debug("fetching new blocks", log.Int("to_fetch", len(blocksToFetch)))
	if err := l.GetBlocks(ctx, blocksToFetch); err != nil {
		logger.With().Warning("failed fetching new blocks",
			log.Array("block_ids", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
				for _, bid := range blocksToFetch {
					encoder.AppendString(bid.String())
				}
				return nil
			})),
			log.Err(err))
		// syntactically invalid blocks are expected from malicious peers
	}
	return nil
}

func extractPeerResult(logger log.Log, layerID types.LayerID, data []byte, peerErr error) (result peerResult) {
	result.err = peerErr
	if peerErr != nil {
		logger.With().Debug("received peer error for layer data", layerID, log.Err(peerErr))
		return
	}

	var ldata LayerData
	if err := codec.Decode(data, &ldata); err != nil {
		logger.With().Debug("error converting bytes to LayerData", log.Err(err))
		result.err = err
		return
	}

	result.data = &ldata
	// TODO check layer hash to be consistent with the data. if not, blacklist the peer
	return
}

// receiveData is called when response of block IDs for a layer hash is received from remote peer.
// if enough responses are received, it notifies the channels waiting for the layer blocks result.
func (l *Logic) receiveData(ctx context.Context, layerID types.LayerID, peer p2p.Peer, expectedResults int, data []byte, peerErr error) {
	logger := l.log.WithContext(ctx).WithFields(layerID, log.Stringer("peer", peer))
	logger.Debug("received layer data from peer")
	peerRes := extractPeerResult(logger, layerID, data, peerErr)

	l.registerLayerHashes(peer, peerRes.data)

	if peerRes.err == nil {
		if err := l.fetchLayerData(ctx, logger, layerID, peerRes.data); err != nil {
			peerRes.err = errLayerDataNotFetched
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// check if we have all responses from peers
	result := l.dataResults[layerID]
	result.responses[peer] = &peerRes
	if len(result.responses) < expectedResults {
		logger.With().Debug("not yet received layer blocks from all peers",
			log.Int("received", len(result.responses)),
			log.Int("expected", expectedResults))
		return
	}

	// make a copy of data and channels to avoid holding a lock while notifying
	chs := l.dataChs[layerID]
	l.eg.Go(func() error {
		notifyDataResult(ctx, l.log.WithContext(ctx).WithFields(layerID), layerID, l.msh, chs, result)
		return nil
	})
	delete(l.dataChs, layerID)
	delete(l.dataResults, layerID)
}

// notifyDataResult determines the final result for the layer, and notifies subscribed channels when
// all blocks are fetched for a given layer.
// it deliberately doesn't hold any lock while notifying channels.
func notifyDataResult(ctx context.Context, logger log.Log, layerID types.LayerID, msh meshProvider, channels []chan LayerPromiseData, lyrResult *dataResult) {
	var (
		missing, success bool
		err              error
	)
	for _, res := range lyrResult.responses {
		if res.err == nil && res.data != nil {
			success = true
		}
		if errors.Is(res.err, errLayerDataNotFetched) {
			// all fetches need to succeed
			missing = true
			err = res.err
			break
		}
		if err == nil {
			err = res.err
		}
	}

	result := LayerPromiseData{Layer: layerID}
	// we tolerate errors from peers as long as we fetched all known blocks in this layer.
	if missing || !success {
		result.Err = err
	}

	if result.Err == nil {
		if len(lyrResult.blocks) == 0 {
			if err := msh.SetZeroBlockLayer(ctx, layerID); err != nil {
				// this can happen when node actually had received blocks for this layer before. ok to ignore
				logger.With().Warning("failed to set zero-block for layer", layerID, log.Err(err))
			}
		}
	}

	logger.With().Debug("notifying layer data result", log.String("layer", fmt.Sprintf("%+v", result)))
	for _, ch := range channels {
		select {
		case ch <- result:
		case <-ctx.Done():
		}
	}
}

type epochAtxRes struct {
	Peer  p2p.Peer
	Error error
	Atxs  []types.ATXID
}

// GetEpochATXs fetches all atxs received by peer for given layer.
func (l *Logic) GetEpochATXs(ctx context.Context, eid types.EpochID) error {
	resCh := make(chan epochAtxRes, 1)

	// build receiver function
	okFunc := func(data []byte, peer p2p.Peer) {
		atxsIDs, err := codec.DecodeSlice[types.ATXID](data)
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
	if err := l.fetcher.GetEpochATXIDs(ctx, eid, okFunc, errFunc); err != nil {
		return fmt.Errorf("failed to send request to the peer: %w", err)
	}
	l.log.WithContext(ctx).With().Debug("waiting for epoch atx response", eid)
	res := <-resCh
	if res.Error != nil {
		return res.Error
	}

	l.log.WithContext(ctx).With().Debug("tracking peer for atxs",
		log.Int("to_fetch", len(res.Atxs)),
		log.Stringer("peer", res.Peer))
	atxHashes := types.ATXIDsToHashes(res.Atxs)
	l.fetcher.RegisterPeerHashes(res.Peer, atxHashes)

	if err := l.GetAtxs(ctx, res.Atxs); err != nil {
		return fmt.Errorf("get ATXs: %w", err)
	}

	return nil
}

// GetAtxs gets the data for given atx IDs and validates them. returns an error if at least one ATX cannot be fetched.
func (l *Logic) GetAtxs(ctx context.Context, ids []types.ATXID) error {
	if len(ids) == 0 {
		return nil
	}
	l.log.WithContext(ctx).With().Debug("requesting atxs from peer", log.Int("num_atxs", len(ids)))
	hashes := types.ATXIDsToHashes(ids)
	if errs := l.getHashes(ctx, hashes, datastore.ATXDB, l.atxHandler.HandleAtxData); len(errs) > 0 {
		return errs[0]
	}
	return nil
}

type dataReceiver func(context.Context, []byte) error

func (l *Logic) getHashes(ctx context.Context, hashes []types.Hash32, hint datastore.Hint, receiver dataReceiver) []error {
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
			if err := receiver(ctx, res.Data); err != nil {
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
func (l *Logic) GetBallots(ctx context.Context, ids []types.BallotID) error {
	if len(ids) == 0 {
		return nil
	}
	l.log.WithContext(ctx).With().Debug("requesting ballots from peer", log.Int("num_ballots", len(ids)))
	hashes := types.BallotIDsToHashes(ids)
	if errs := l.getHashes(ctx, hashes, datastore.BallotDB, l.ballotHandler.HandleSyncedBallot); len(errs) > 0 {
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
	if errs := l.getHashes(ctx, hashes, datastore.ProposalDB, l.proposalHandler.HandleSyncedProposal); len(errs) > 0 {
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
	if errs := l.getHashes(ctx, hashes, datastore.BlockDB, l.blockHandler.HandleSyncedBlock); len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// GetProposalTxs fetches the txs provided as IDs and validates them, returns an error if one TX failed to be fetched.
func (l *Logic) GetProposalTxs(ctx context.Context, ids []types.TransactionID) error {
	return l.getTxs(ctx, ids, l.txHandler.HandleProposalTransaction)
}

// GetBlockTxs fetches the txs provided as IDs and saves them, they will be validated
// before block is applied.
func (l *Logic) GetBlockTxs(ctx context.Context, ids []types.TransactionID) error {
	return l.getTxs(ctx, ids, l.txHandler.HandleBlockTransaction)
}

func (l *Logic) getTxs(ctx context.Context, ids []types.TransactionID, receiver dataReceiver) error {
	if len(ids) == 0 {
		return nil
	}
	l.log.WithContext(ctx).With().Debug("requesting txs from peer", log.Int("num_txs", len(ids)))
	hashes := types.TransactionIDsToHashes(ids)
	if errs := l.getHashes(ctx, hashes, datastore.TXDB, receiver); len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// GetPoetProof gets poet proof from remote peer.
func (l *Logic) GetPoetProof(ctx context.Context, id types.Hash32) error {
	l.log.WithContext(ctx).With().Debug("getting poet proof", log.String("hash", id.ShortString()))
	res := <-l.fetcher.GetHash(id, datastore.POETDB, false)
	if res.Err != nil {
		return res.Err
	}
	// if result is local we don't need to process it again
	if !res.IsLocal {
		l.log.WithContext(ctx).Debug("got poet ref",
			log.String("hash", id.ShortString()),
			log.Int("dataSize", len(res.Data)))

		if err := l.poetHandler.ValidateAndStoreMsg(res.Data); err != nil && !errors.Is(err, sql.ErrObjectExists) {
			return fmt.Errorf("validate and store message: %w", err)
		}
	}
	return nil
}

// RegisterPeerHashes is a wrapper around fetcher's RegisterPeerHashes.
func (l *Logic) RegisterPeerHashes(peer p2p.Peer, hashes []types.Hash32) {
	l.fetcher.RegisterPeerHashes(peer, hashes)
}

// AddPeersFromHash is a wrapper around fetcher's AddPeersFromHash.
func (l *Logic) AddPeersFromHash(fromHash types.Hash32, toHashes []types.Hash32) {
	l.fetcher.AddPeersFromHash(fromHash, toHashes)
}
