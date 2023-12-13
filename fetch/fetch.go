// Package fetch contains mechanism to fetch Data from remote peers
package fetch

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/fetch/peers"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/system"
)

const (
	atxProtocol      = "ax/1"
	lyrDataProtocol  = "ld/1"
	hashProtocol     = "hs/1"
	meshHashProtocol = "mh/1"
	malProtocol      = "ml/1"
	OpnProtocol      = "lp/2"

	cacheSize = 1000

	RedundantPeers = 20
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
	BatchSize, QueueSize int
	RequestTimeout       time.Duration // in seconds
	MaxRetriesForRequest int
	PeersRateThreshold   float64 `mapstructure:"peers-rate-threshold"`
}

// DefaultConfig is the default config for the fetch component.
func DefaultConfig() Config {
	return Config{
		BatchTimeout:         time.Millisecond * time.Duration(50),
		QueueSize:            20,
		BatchSize:            20,
		RequestTimeout:       25 * time.Second,
		MaxRetriesForRequest: 100,
		PeersRateThreshold:   0.02,
	}
}

// randomPeer returns a random peer from current peer list.
func randomPeer(peers []p2p.Peer) p2p.Peer {
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
	peers  *peers.Peers

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
func NewFetch(
	cdb *datastore.CachedDB,
	clock layerClock,
	msh meshProvider,
	b system.BeaconGetter,
	host *p2p.Host,
	opts ...Option,
) *Fetch {
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
	f.peers = peers.New()
	// NOTE(dshulyak) this is to avoid tests refactoring.
	// there is one test that covers this part.
	if host != nil {
		connectedf := func(peer p2p.Peer) {
			f.logger.With().Debug("add peer", log.Stringer("id", peer))
			f.peers.Add(peer)
		}
		host.Network().Notify(&network.NotifyBundle{
			ConnectedF: func(_ network.Network, c network.Conn) {
				connectedf(c.RemotePeer())
			},
			DisconnectedF: func(_ network.Network, c network.Conn) {
				f.logger.With().Debug("remove peer", log.Stringer("id", c.RemotePeer()))
				f.peers.Delete(c.RemotePeer())
			},
		})
		for _, peer := range host.GetPeers() {
			if host.Connected(peer) {
				connectedf(peer)
			}
		}
	}

	f.batchTimeout = time.NewTicker(f.cfg.BatchTimeout)
	srvOpts := []server.Opt{
		server.WithTimeout(f.cfg.RequestTimeout),
		server.WithLog(f.logger),
	}
	if len(f.servers) == 0 {
		h := newHandler(cdb, clock, bs, msh, b, f.logger)
		f.servers[atxProtocol] = server.New(host, atxProtocol, h.handleEpochInfoReq, srvOpts...)
		f.servers[lyrDataProtocol] = server.New(
			host,
			lyrDataProtocol,
			h.handleLayerDataReq,
			srvOpts...)
		f.servers[hashProtocol] = server.New(host, hashProtocol, h.handleHashReq, srvOpts...)
		f.servers[meshHashProtocol] = server.New(
			host,
			meshHashProtocol,
			h.handleMeshHashReq,
			srvOpts...)
		f.servers[malProtocol] = server.New(host, malProtocol, h.handleMaliciousIDsReq, srvOpts...)
		f.servers[OpnProtocol] = server.New(
			host,
			OpnProtocol,
			h.handleLayerOpinionsReq2,
			srvOpts...)
	}
	return f
}

type dataValidators struct {
	atx         SyncValidator
	poet        SyncValidator
	ballot      SyncValidator
	activeset   SyncValidator
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
	activeset SyncValidator,
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
		activeset:   activeset,
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
			f.hashValidationDone(rsp.Hash, req.validator(req.ctx, rsp.Hash, batch.peer, rsp.Data))
			return nil
		})
		delete(batchMap, resp.Hash)
	}

	// iterate all requests that didn't return value from peer and notify
	// they will be retried for MaxRetriesForRequest
	for h, r := range batchMap {
		f.logger.With().Debug("hash not found in response from peer",
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
			log.Int("retries", req.retries),
		)
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
		f.logger.WithContext(req.ctx).
			With().
			Debug("processing hash request", log.Stringer("hash", hash))
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
			f.sendBatch(peer, batch)
		}
	}
}

func (f *Fetch) organizeRequests(requests []RequestMessage) map[p2p.Peer][][]RequestMessage {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	peer2requests := make(map[p2p.Peer][]RequestMessage)

	best := f.peers.SelectBest(RedundantPeers)
	if len(best) == 0 {
		f.logger.Info("cannot send batch: no peers found")
		f.mu.Lock()
		defer f.mu.Unlock()
		errNoPeer := errors.New("no peers")
		for _, msg := range requests {
			if req, ok := f.ongoing[msg.Hash]; ok {
				req.promise.err = errNoPeer
				close(req.promise.completed)
				delete(f.ongoing, req.hash)
			} else {
				f.logger.With().Error("ongoing request missing",
					log.Stringer("hash", msg.Hash),
					log.String("hint", string(msg.Hint)),
				)
			}
		}
		return nil
	}
	for _, req := range requests {
		hashPeers := f.hashToPeers.GetRandom(req.Hash, req.Hint, rng)
		target := f.peers.SelectBestFrom(hashPeers)
		if target == p2p.NoPeer {
			target = randomPeer(best)
		}
		_, ok := peer2requests[target]
		if !ok {
			peer2requests[target] = []RequestMessage{req}
		} else {
			peer2requests[target] = append(peer2requests[target], req)
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
func (f *Fetch) sendBatch(peer p2p.Peer, batch *batchInfo) {
	if f.stopped() {
		return
	}
	f.mu.Lock()
	f.batched[batch.ID] = batch
	f.mu.Unlock()
	f.logger.With().Debug("sending batched request to peer",
		log.Stringer("batch_hash", batch.ID),
		log.Int("num_requests", len(batch.Requests)),
		log.Stringer("peer", peer),
	)
	// Request is asynchronous,
	// it will return errors only if size of the bytes buffer is large
	// or target peer is not connected
	start := time.Now()
	errf := func(err error) {
		f.logger.With().Warning("failed to send batch",
			log.Stringer("batch_hash", peer), log.Err(err),
		)
		f.peers.OnFailure(peer)
		f.handleHashError(batch.ID, err)
	}
	err := f.servers[hashProtocol].Request(
		f.shutdownCtx,
		peer,
		codec.MustEncode(&batch.RequestBatch),
		func(buf []byte) {
			f.peers.OnLatency(peer, time.Since(start))
			f.receiveResponse(buf)
		},
		errf,
	)
	if err != nil {
		errf(err)
	}
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
			f.logger.With().
				Warning("hash missing from ongoing requests", log.Stringer("hash", br.Hash))
			continue
		}
		f.logger.WithContext(req.ctx).With().Warning("hash request failed",
			log.Stringer("hash", req.hash),
			log.Err(err))
		req.promise.err = err
		peerErrors.WithLabelValues(string(req.hint)).Inc()
		close(req.promise.completed)
		delete(f.ongoing, req.hash)
	}
	delete(f.batched, batchHash)
}

// getHash is the regular buffered call to get a specific hash, using provided hash, h as hint the receiving end will
// know where to look for the hash, this function returns HashDataPromiseResult channel that will hold Data received or error.
func (f *Fetch) getHash(
	ctx context.Context,
	hash types.Hash32,
	h datastore.Hint,
	receiver dataReceiver,
) (*promise, error) {
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

func (f *Fetch) SelectBestShuffled(n int) []p2p.Peer {
	// shuffle to split the load between peers with good latency.
	// and it avoids sticky behavior, when temporarily faulty peer had good latency in the past.
	peers := f.peers.SelectBest(n)
	rand.Shuffle(len(peers), func(i, j int) {
		peers[i], peers[j] = peers[j], peers[i]
	})
	return peers
}
