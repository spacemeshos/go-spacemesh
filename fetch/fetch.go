// Package fetch contains mechanism to fetch Data from remote peers
package fetch

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/system"
)

const (
	atxProtocol      = "ax/1"
	lyrDataProtocol  = "ld/1"
	lyrOpnsProtocol  = "lp/1"
	hashProtocol     = "hs/1"
	meshHashProtocol = "mh/1"
	malProtocol      = "ml/1"

	cacheSize = 1000
)

var (
	// errExceedMaxRetries is returned when MaxRetriesForRequest attempts has been made to fetch data for a hash and failed.
	errExceedMaxRetries = errors.New("fetch failed after max retries for request")

	errValidatorsNotSet = errors.New("validators not set")
)

// request contains all relevant Data for a single request for a specified hash.
type request struct {
	ctx       context.Context
	hash      types.Hash32   // hash is the hash of the Data requested
	hint      datastore.Hint // the hint from which database to fetch this hash
	validator dataReceiver
	promise   *promise
	retries   int
}

type promise struct {
	completed chan struct{}
	err       error
}

type batchInfo struct {
	RequestBatch
	peer p2p.Peer
}

// setID calculates the hash of all requests and sets it as this batches ID.
func (b *batchInfo) setID() {
	bts, err := codec.EncodeSlice(b.Requests)
	if err != nil {
		return
	}
	b.ID = types.CalcHash32(bts)
}

func (b *batchInfo) toMap() map[types.Hash32]RequestMessage {
	m := make(map[types.Hash32]RequestMessage)
	for _, r := range b.Requests {
		m[r.Hash] = r
	}
	return m
}

// Config is the configuration file of the Fetch component.
type Config struct {
	BatchTimeout         time.Duration // in milliseconds
	MaxRetriesForPeer    int
	BatchSize, QueueSize int
	RequestTimeout       time.Duration // in seconds
	MaxRetriesForRequest int
}

// DefaultConfig is the default config for the fetch component.
func DefaultConfig() Config {
	return Config{
		BatchTimeout:         time.Millisecond * time.Duration(50),
		MaxRetriesForPeer:    2,
		QueueSize:            20,
		BatchSize:            20,
		RequestTimeout:       time.Second * time.Duration(10),
		MaxRetriesForRequest: 100,
	}
}

// randomPeer returns a random peer from current peer list.
func randomPeer(peers []p2p.Peer) p2p.Peer {
	if len(peers) == 0 {
		log.Fatal("cannot send fetch: no peers found")
	}
	return peers[rand.Intn(len(peers))]
}

// Option is a type to configure a fetcher.
type Option func(*Fetch)

// WithContext configures the shutdown context for the fetcher.
func WithContext(c context.Context) Option {
	return func(f *Fetch) {
		f.shutdownCtx, f.cancel = context.WithCancel(c)
	}
}

// WithConfig configures the config for the fetcher.
func WithConfig(c Config) Option {
	return func(f *Fetch) {
		f.cfg = c
	}
}

// WithLogger configures logger for the fetcher.
func WithLogger(log log.Log) Option {
	return func(f *Fetch) {
		f.logger = log
	}
}

func withServers(s map[string]requester) Option {
	return func(f *Fetch) {
		f.servers = s
	}
}

func withHost(h host) Option {
	return func(f *Fetch) {
		f.host = h
	}
}

// Fetch is the main struct that contains network peers and logic to batch and dispatch hash fetch requests.
type Fetch struct {
	cfg    Config
	logger log.Log
	bs     *datastore.BlobStore
	host   host

	servers    map[string]requester
	validators *dataValidators

	// unprocessed contains requests that are not processed
	unprocessed map[types.Hash32]*request
	// ongoing contains requests that have been processed and are waiting for responses
	ongoing map[types.Hash32]*request
	// batched contains batched ongoing requests.
	batched      map[types.Hash32]*batchInfo
	batchTimeout *time.Ticker
	mu           sync.Mutex
	onlyOnce     sync.Once
	hashToPeers  *HashPeersCache

	shutdownCtx context.Context
	cancel      context.CancelFunc
	eg          errgroup.Group
}

// NewFetch creates a new Fetch struct.
func NewFetch(cdb *datastore.CachedDB, msh meshProvider, b system.BeaconGetter, host *p2p.Host, opts ...Option) *Fetch {
	bs := datastore.NewBlobStore(cdb.Database)
	f := &Fetch{
		cfg:         DefaultConfig(),
		logger:      log.NewNop(),
		bs:          bs,
		host:        host,
		servers:     map[string]requester{},
		unprocessed: make(map[types.Hash32]*request),
		ongoing:     make(map[types.Hash32]*request),
		batched:     make(map[types.Hash32]*batchInfo),
		hashToPeers: NewHashPeersCache(cacheSize),
	}
	for _, opt := range opts {
		opt(f)
	}

	f.batchTimeout = time.NewTicker(f.cfg.BatchTimeout)
	srvOpts := []server.Opt{
		server.WithTimeout(f.cfg.RequestTimeout),
		server.WithLog(f.logger),
	}
	if len(f.servers) == 0 {
		h := newHandler(cdb, bs, msh, b, f.logger)
		f.servers[atxProtocol] = server.New(host, atxProtocol, h.handleEpochInfoReq, srvOpts...)
		f.servers[lyrDataProtocol] = server.New(host, lyrDataProtocol, h.handleLayerDataReq, srvOpts...)
		f.servers[lyrOpnsProtocol] = server.New(host, lyrOpnsProtocol, h.handleLayerOpinionsReq, srvOpts...)
		f.servers[hashProtocol] = server.New(host, hashProtocol, h.handleHashReq, srvOpts...)
		f.servers[meshHashProtocol] = server.New(host, meshHashProtocol, h.handleMeshHashReq, srvOpts...)
		f.servers[malProtocol] = server.New(host, malProtocol, h.handleMaliciousIDsReq, srvOpts...)
	}
	return f
}

type dataValidators struct {
	atx         SyncValidator
	poet        SyncValidator
	ballot      SyncValidator
	block       SyncValidator
	proposal    SyncValidator
	txBlock     SyncValidator
	txProposal  SyncValidator
	malfeasance SyncValidator
}

// SetValidators sets the handlers to validate various mesh data fetched from peers.
func (f *Fetch) SetValidators(
	atx SyncValidator,
	poet SyncValidator,
	ballot SyncValidator,
	block SyncValidator,
	prop SyncValidator,
	txBlock SyncValidator,
	txProposal SyncValidator,
	mal SyncValidator,
) {
	f.validators = &dataValidators{
		atx:         atx,
		poet:        poet,
		ballot:      ballot,
		block:       block,
		proposal:    prop,
		txBlock:     txBlock,
		txProposal:  txProposal,
		malfeasance: mal,
	}
}

// Start starts handling fetch requests.
func (f *Fetch) Start() error {
	if f.validators == nil {
		return errValidatorsNotSet
	}
	f.onlyOnce.Do(func() {
		f.eg.Go(func() error {
			f.loop()
			return nil
		})
	})
	return nil
}

// Stop stops handling fetch requests.
func (f *Fetch) Stop() {
	f.logger.Info("stopping fetch")
	f.batchTimeout.Stop()
	f.cancel()
	if err := f.host.Close(); err != nil {
		f.logger.With().Warning("error closing host", log.Err(err))
	}
	f.mu.Lock()
	for _, req := range f.unprocessed {
		close(req.promise.completed)
	}
	for _, req := range f.ongoing {
		close(req.promise.completed)
	}
	f.mu.Unlock()

	_ = f.eg.Wait()
	f.logger.Info("stopped fetch")
}

// stopped returns if we should stop.
func (f *Fetch) stopped() bool {
	select {
	case <-f.shutdownCtx.Done():
		return true
	default:
		return false
	}
}

// here we receive all requests for hashes for all DBs and batch them together before we send the request to peer
// there can be a priority request that will not be batched.
func (f *Fetch) loop() {
	f.logger.Info("starting fetch main loop")
	for {
		select {
		case <-f.batchTimeout.C:
			f.eg.Go(func() error {
				f.requestHashBatchFromPeers() // Process the batch.
				return nil
			})
		case <-f.shutdownCtx.Done():
			return
		}
	}
}

// receive Data from message server and call response handlers accordingly.
func (f *Fetch) receiveResponse(data []byte) {
	if f.stopped() {
		return
	}

	var response ResponseBatch
	if err := codec.Decode(data, &response); err != nil {
		f.logger.With().Warning("failed to decode batch response", log.Err(err))
		return
	}

	f.logger.With().Debug("received batch response",
		log.Stringer("batch_hash", response.ID),
		log.Int("num_hashes", len(response.Responses)),
	)
	f.mu.Lock()
	batch, ok := f.batched[response.ID]
	delete(f.batched, response.ID)
	f.mu.Unlock()

	if !ok {
		f.logger.With().Warning("unknown batch response received, or already received",
			log.Stringer("batch_hash", response.ID))
		return
	}

	batchMap := batch.toMap()
	// iterate all hash Responses
	for _, resp := range response.Responses {
		f.logger.With().Debug("received response for hash", log.Stringer("hash", resp.Hash))
		f.mu.Lock()
		req, ok := f.ongoing[resp.Hash]
		f.mu.Unlock()

		if !ok {
			f.logger.With().Warning("response received for unknown hash",
				log.Stringer("hash", resp.Hash))
			continue
		}

		rsp := resp
		f.eg.Go(func() error {
			// validation fetch data recursively. offload to another goroutine
			f.hashValidationDone(rsp.Hash, req.validator(req.ctx, batch.peer, rsp.Data))
			return nil
		})
		delete(batchMap, resp.Hash)
	}

	// iterate all requests that didn't return value from peer and notify
	// they will be retried for MaxRetriesForRequest
	for h, r := range batchMap {
		f.logger.With().Warning("hash not found in response from peer",
			log.String("hint", string(r.Hint)),
			log.Stringer("hash", h),
			log.Stringer("peer", batch.peer),
		)
		f.failAfterRetry(r.Hash)
	}
}

func (f *Fetch) hashValidationDone(hash types.Hash32, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	req, ok := f.ongoing[hash]
	if !ok {
		f.logger.With().Error("validation ran for unknown hash", log.Stringer("hash", hash))
		return
	}
	if err != nil {
		req.promise.err = err
	} else {
		f.logger.WithContext(req.ctx).With().Debug("hash request done",
			log.Stringer("hash", hash))
	}
	close(req.promise.completed)
	delete(f.ongoing, hash)
}

func (f *Fetch) failAfterRetry(hash types.Hash32) {
	f.mu.Lock()
	defer f.mu.Unlock()

	req, ok := f.ongoing[hash]
	if !ok {
		f.logger.With().Error("hash missing from ongoing requests", log.Stringer("hash", hash))
		return
	}

	// first check if we have it locally from gossips
	if _, err := f.bs.Get(req.hint, hash.Bytes()); err == nil {
		close(req.promise.completed)
		delete(f.ongoing, hash)
		return
	}

	req.retries++
	if req.retries > f.cfg.MaxRetriesForRequest {
		f.logger.WithContext(req.ctx).With().Warning("gave up on hash after max retries",
			log.Stringer("hash", req.hash),
			log.Int("retries", req.retries))
		req.promise.err = errExceedMaxRetries
		close(req.promise.completed)
	} else {
		// put the request back to the unprocessed list
		f.unprocessed[req.hash] = req
	}
	delete(f.ongoing, hash)
}

// this is the main function that sends the hash request to the peer.
func (f *Fetch) requestHashBatchFromPeers() {
	requestList := f.getUnprocessed()
	f.send(requestList)
}

func (f *Fetch) getUnprocessed() []RequestMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	var requestList []RequestMessage
	// only send one request per hash
	for hash, req := range f.unprocessed {
		f.logger.WithContext(req.ctx).With().Debug("processing hash request", log.Stringer("hash", hash))
		requestList = append(requestList, RequestMessage{Hash: hash, Hint: req.hint})
		// move the processed requests to pending
		f.ongoing[hash] = req
		delete(f.unprocessed, hash)
	}
	return requestList
}

func (f *Fetch) send(requests []RequestMessage) {
	if len(requests) == 0 {
		return
	}
	if f.stopped() {
		return
	}

	peer2batches := f.organizeRequests(requests)

	for peer, peerBatches := range peer2batches {
		for _, reqs := range peerBatches {
			batch := &batchInfo{
				RequestBatch: RequestBatch{
					Requests: reqs,
				},
				peer: peer,
			}
			batch.setID()
			_ = f.sendBatch(peer, batch)
		}
	}
}

func (f *Fetch) organizeRequests(requests []RequestMessage) map[p2p.Peer][][]RequestMessage {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	peer2requests := make(map[p2p.Peer][]RequestMessage)
	peers := f.host.GetPeers()
	if len(peers) == 0 {
		panic("no peers")
	}

	for _, req := range requests {
		p, exists := f.hashToPeers.GetRandom(req.Hash, req.Hint, rng)
		if !exists {
			p = randomPeer(peers)
		}

		_, ok := peer2requests[p]
		if !ok {
			peer2requests[p] = []RequestMessage{req}
		} else {
			peer2requests[p] = append(peer2requests[p], req)
		}
	}

	// split every peer's requests into batches of f.cfg.BatchSize each
	result := make(map[p2p.Peer][][]RequestMessage)
	for peer, reqs := range peer2requests {
		if len(reqs) < f.cfg.BatchSize {
			result[peer] = [][]RequestMessage{
				reqs,
			}
			continue
		}
		for i := 0; i < len(reqs); i += f.cfg.BatchSize {
			j := i + f.cfg.BatchSize
			if j > len(reqs) {
				j = len(reqs)
			}
			result[peer] = append(result[peer], reqs[i:j])
		}
	}
	return result
}

// sendBatch dispatches batched request messages to provided peer.
func (f *Fetch) sendBatch(p p2p.Peer, batch *batchInfo) error {
	f.mu.Lock()
	f.batched[batch.ID] = batch
	f.mu.Unlock()

	f.logger.With().Debug("sending batch request",
		log.Stringer("batch_hash", batch.ID),
		log.Stringer("peer", batch.peer))
	// timeout function will be called if no response was received for the hashes sent
	errorFunc := func(err error) {
		f.logger.With().Warning("failed to send batch",
			log.Stringer("batch_hash", batch.ID),
			log.Err(err))
		f.handleHashError(batch.ID, err)
	}

	bytes, err := codec.Encode(&batch.RequestBatch)
	if err != nil {
		f.handleHashError(batch.ID, err)
	}

	// try sending batch to provided peer
	retries := 0
	for {
		if f.stopped() {
			return nil
		}

		f.logger.With().Debug("sending batched request to peer",
			log.Stringer("batch_hash", batch.ID),
			log.Int("num_requests", len(batch.Requests)),
			log.Stringer("peer", p))

		err = f.servers[hashProtocol].Request(f.shutdownCtx, p, bytes, f.receiveResponse, errorFunc)
		if err == nil {
			break
		}

		retries++
		if retries > f.cfg.MaxRetriesForPeer {
			f.handleHashError(batch.ID, fmt.Errorf("batched request failed w retries: %w", err))
			break
		}
		f.logger.With().Warning("batched request failed",
			log.Stringer("peer", p),
			log.Int("retries", retries),
			log.Err(err))
	}

	return err
}

// handleHashError is called when an error occurred processing batches of the following hashes.
func (f *Fetch) handleHashError(batchHash types.Hash32, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.logger.With().Debug("failed batch fetch", log.Stringer("batch_hash", batchHash), log.Err(err))
	batch, ok := f.batched[batchHash]
	if !ok {
		f.logger.With().Error("batch not found", log.Stringer("batch_hash", batchHash))
		return
	}
	for _, br := range batch.Requests {
		req, ok := f.ongoing[br.Hash]
		if !ok {
			f.logger.With().Warning("hash missing from ongoing requests", log.Stringer("hash", br.Hash))
			continue
		}
		f.logger.WithContext(req.ctx).With().Warning("hash request failed",
			log.Stringer("hash", req.hash),
			log.Err(err))
		req.promise.err = err
		close(req.promise.completed)
		delete(f.ongoing, req.hash)
	}
	delete(f.batched, batchHash)
}

// getHash is the regular buffered call to get a specific hash, using provided hash, h as hint the receiving end will
// know where to look for the hash, this function returns HashDataPromiseResult channel that will hold Data received or error.
func (f *Fetch) getHash(ctx context.Context, hash types.Hash32, h datastore.Hint, receiver dataReceiver) (*promise, error) {
	if f.stopped() {
		return nil, f.shutdownCtx.Err()
	}

	// check if we already have this hash locally
	if _, err := f.bs.Get(h, hash.Bytes()); err == nil {
		return nil, nil
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.ongoing[hash]; ok {
		f.logger.WithContext(ctx).With().Debug("request ongoing", log.Stringer("hash", hash))
		return f.ongoing[hash].promise, nil
	}

	if _, ok := f.unprocessed[hash]; !ok {
		f.unprocessed[hash] = &request{
			ctx:       ctx,
			hash:      hash,
			hint:      h,
			validator: receiver,
			promise: &promise{
				completed: make(chan struct{}, 1),
			},
		}
		f.logger.WithContext(ctx).With().Debug("hash request added to queue",
			log.Stringer("hash", hash),
			log.Int("queued", len(f.unprocessed)))
	} else {
		f.logger.WithContext(ctx).With().Debug("hash request already in queue",
			log.Stringer("hash", hash),
			log.Int("retries", f.unprocessed[hash].retries),
			log.Int("queued", len(f.unprocessed)))
	}
	if len(f.unprocessed) >= f.cfg.QueueSize {
		f.eg.Go(func() error {
			f.requestHashBatchFromPeers() // Process the batch.
			return nil
		})
	}
	return f.unprocessed[hash].promise, nil
}

// RegisterPeerHashes registers provided peer for a list of hashes.
func (f *Fetch) RegisterPeerHashes(peer p2p.Peer, hashes []types.Hash32) {
	if peer == f.host.ID() {
		return
	}
	f.hashToPeers.RegisterPeerHashes(peer, hashes)
}

func (f *Fetch) GetPeers() []p2p.Peer {
	return f.host.GetPeers()
}
