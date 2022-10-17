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
)

const (
	atxProtocol     = "ax/1"
	lyrDataProtocol = "ld/1"
	lyrOpnsProtocol = "lp/1"
	hashProtocol    = "hs/1"

	batchMaxSize = 20
	cacheSize    = 1000
)

var (
	// errExceedMaxRetries is returned when MaxRetriesForRequest attempts has been made to fetch data for a hash and failed.
	errExceedMaxRetries = errors.New("fetch failed after max retries for request")

	// errWrongHash is returned when the data in the peer's response does not hash to the same value as requested.
	errWrongHash = errors.New("wrong hash from response")
)

// request contains all relevant Data for a single request for a specified hash.
type request struct {
	hash                 types.Hash32               // hash is the hash of the Data requested
	validateResponseHash bool                       // if true perform hash validation on received Data
	hint                 datastore.Hint             // the hint from which database to fetch this hash
	returnChan           chan HashDataPromiseResult // channel that will signal if the call succeeded or not
	retries              int
}

// HashDataPromiseResult is the result strict when requesting Data corresponding to the Hash.
type HashDataPromiseResult struct {
	Err     error
	Hash    types.Hash32
	Data    []byte
	IsLocal bool
}

type batchInfo struct {
	RequestBatch
	peer p2p.Peer
}

// SetID calculates the hash of all requests and sets it as this batches ID.
func (b *batchInfo) SetID() {
	bts, err := codec.EncodeSlice(b.Requests)
	if err != nil {
		return
	}
	b.ID = types.CalcHash32(bts)
}

// ToMap converts the array of requests to map so it can be easily invalidated.
func (b *batchInfo) ToMap() map[types.Hash32]RequestMessage {
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
	BatchSize            int
	RequestTimeout       time.Duration // in seconds
	MaxRetriesForRequest int
}

// DefaultConfig is the default config for the fetch component.
func DefaultConfig() Config {
	return Config{
		BatchTimeout:         time.Millisecond * time.Duration(50),
		MaxRetriesForPeer:    2,
		BatchSize:            20,
		RequestTimeout:       time.Second * time.Duration(10),
		MaxRetriesForRequest: 100,
	}
}

// randomPeer returns a random peer from current peer list.
func randomPeer(peers []p2p.Peer) p2p.Peer {
	if len(peers) == 0 {
		log.Panic("cannot send fetch: no peers found")
	}
	return peers[rand.Intn(len(peers))]
}

// Option is a type to configure a fetcher.
type Option func(*Fetch)

// WithContext configures the shutdown context for the fetcher
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

// WithATXHandler configures the ATX handler of the fetcher.
func WithATXHandler(h atxHandler) Option {
	return func(f *Fetch) {
		f.atxHandler = h
	}
}

// WithBallotHandler configures the Ballot handler of the fetcher.
func WithBallotHandler(h ballotHandler) Option {
	return func(f *Fetch) {
		f.ballotHandler = h
	}
}

// WithBlockHandler configures the Block handler of the fetcher.
func WithBlockHandler(h blockHandler) Option {
	return func(f *Fetch) {
		f.blockHandler = h
	}
}

// WithProposalHandler configures the Proposal handler of the fetcher.
func WithProposalHandler(h proposalHandler) Option {
	return func(f *Fetch) {
		f.proposalHandler = h
	}
}

// WithTXHandler configures the TX handler of the fetcher.
func WithTXHandler(h txHandler) Option {
	return func(f *Fetch) {
		f.txHandler = h
	}
}

// WithPoetHandler configures the PoET handler of the fetcher.
func WithPoetHandler(h poetHandler) Option {
	return func(f *Fetch) {
		f.poetHandler = h
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

	servers         map[string]requester
	poetHandler     poetHandler
	atxHandler      atxHandler
	ballotHandler   ballotHandler
	blockHandler    blockHandler
	proposalHandler proposalHandler
	txHandler       txHandler

	// activeRequests contains requests that are not processed
	activeRequests map[types.Hash32][]*request
	// pendingRequests contains requests that have been processed and are waiting for responses
	pendingRequests map[types.Hash32][]*request
	// activeBatches contains batches of requests in pendingRequests.
	activeBatches   map[types.Hash32]batchInfo
	requestReceiver chan request
	batchTimeout    *time.Ticker
	activeReqM      sync.RWMutex
	activeBatchM    sync.RWMutex
	onlyOnce        sync.Once
	hashToPeers     *HashPeersCache

	shutdownCtx context.Context
	cancel      context.CancelFunc
	eg          errgroup.Group
}

// NewFetch creates a new Fetch struct.
func NewFetch(cdb *datastore.CachedDB, msh meshProvider, host *p2p.Host, opts ...Option) *Fetch {
	bs := datastore.NewBlobStore(cdb.Database)
	f := &Fetch{
		cfg:             DefaultConfig(),
		logger:          log.NewNop(),
		bs:              bs,
		host:            host,
		servers:         map[string]requester{},
		activeRequests:  make(map[types.Hash32][]*request),
		pendingRequests: make(map[types.Hash32][]*request),
		requestReceiver: make(chan request),
		activeBatches:   make(map[types.Hash32]batchInfo),
		hashToPeers:     NewHashPeersCache(cacheSize),
	}
	for _, opt := range opts {
		opt(f)
	}

	f.batchTimeout = time.NewTicker(f.cfg.BatchTimeout)
	if len(f.servers) == 0 {
		h := newHandler(cdb, bs, msh, f.logger)
		f.servers[atxProtocol] = server.New(host, atxProtocol, h.handleEpochATXIDsReq,
			server.WithTimeout(f.cfg.RequestTimeout),
			server.WithLog(f.logger),
		)
		f.servers[lyrDataProtocol] = server.New(host, lyrDataProtocol, h.handleLayerDataReq,
			server.WithTimeout(f.cfg.RequestTimeout),
			server.WithLog(f.logger),
		)
		f.servers[lyrOpnsProtocol] = server.New(host, lyrOpnsProtocol, h.handleLayerOpinionsReq,
			server.WithTimeout(f.cfg.RequestTimeout),
			server.WithLog(f.logger),
		)
		f.servers[hashProtocol] = server.New(host, hashProtocol, h.handleHashReq,
			server.WithTimeout(f.cfg.RequestTimeout),
			server.WithLog(f.logger),
		)
	}
	return f
}

// Start starts handling fetch requests.
func (f *Fetch) Start() {
	f.onlyOnce.Do(func() {
		f.eg.Go(func() error {
			f.loop(f.handleNewRequest)
			return nil
		})
	})
}

// Stop stops handling fetch requests.
func (f *Fetch) Stop() {
	f.logger.Info("stopping fetch")
	f.batchTimeout.Stop()
	f.cancel()
	if err := f.host.Close(); err != nil {
		f.logger.With().Warning("error closing host", log.Err(err))
	}
	f.activeReqM.Lock()
	for _, batch := range f.activeRequests {
		for _, req := range batch {
			close(req.returnChan)
		}
	}
	for _, batch := range f.pendingRequests {
		for _, req := range batch {
			close(req.returnChan)
		}
	}
	f.activeReqM.Unlock()

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

// handleNewRequest batches low priority requests and sends a high priority request to the peers right away.
// if there are pending requests for the same hash, it will put the new request, regardless of the priority,
// to the pending list and wait for notification when the earlier request gets response.
// it returns true if a request is sent immediately, or false otherwise.
func (f *Fetch) handleNewRequest(req *request) {
	f.activeReqM.Lock()
	if _, ok := f.pendingRequests[req.hash]; ok {
		// hash already being requested. just add the req and wait for the notification
		f.pendingRequests[req.hash] = append(f.pendingRequests[req.hash], req)
		f.activeReqM.Unlock()
		return
	}
	f.activeRequests[req.hash] = append(f.activeRequests[req.hash], req)
	rLen := len(f.activeRequests)
	f.activeReqM.Unlock()
	f.logger.With().Debug("request added to queue", log.String("hash", req.hash.ShortString()))
	if rLen > batchMaxSize {
		f.eg.Go(func() error {
			f.requestHashBatchFromPeers() // Process the batch.
			return nil
		})
		return
	}
	return
}

// here we receive all requests for hashes for all DBs and batch them together before we send the request to peer
// there can be a priority request that will not be batched.
func (f *Fetch) loop(handler func(*request)) {
	f.logger.Info("starting fetch main loop")
	for {
		select {
		case req := <-f.requestReceiver:
			handler(&req)
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
	err := codec.Decode(data, &response)
	if err != nil {
		f.logger.With().Error("response was unclear, maybe leaking", log.Err(err))
		return
	}

	f.activeBatchM.RLock()
	batch, has := f.activeBatches[response.ID]
	f.activeBatchM.RUnlock()
	if !has {
		f.logger.With().Warning("unknown batch response received, or already invalidated", log.String("batchHash", response.ID.ShortString()))
		return
	}

	// convert requests to map so it can be invalidated when reading Responses
	batchMap := batch.ToMap()
	// iterate all hash Responses
	for _, resID := range response.Responses {
		// take lock here to make handling of a single hash atomic
		f.activeReqM.Lock()
		// for each hash, send Data on waiting channel
		reqs := f.pendingRequests[resID.Hash]
		actualHash := types.Hash32{}
		for _, req := range reqs {
			var err error
			if req.validateResponseHash {
				if actualHash == (types.Hash32{}) {
					actualHash = types.CalcHash32(data)
				}
				if actualHash != resID.Hash {
					err = fmt.Errorf("%w: %v, actual %v", errWrongHash, resID.Hash.ShortString(), actualHash.ShortString())
				}
			}
			req.returnChan <- HashDataPromiseResult{
				Err:     err,
				Hash:    resID.Hash,
				Data:    resID.Data,
				IsLocal: false,
			}
			// todo: mark peer as malicious
		}
		// remove from map
		delete(batchMap, resID.Hash)

		// remove from pending list
		delete(f.pendingRequests, resID.Hash)
		f.activeReqM.Unlock()
	}

	// iterate all requests that didn't return value from peer and notify
	// they will be retried for MaxRetriesForRequest
	for h := range batchMap {
		if f.stopped() {
			return
		}
		f.logger.With().Warning("hash not found in response from peer",
			log.String("hint", string(batchMap[h].Hint)),
			log.String("hash", h.ShortString()),
			log.String("peer", batch.peer.String()))
		f.activeReqM.Lock()
		reqs := f.pendingRequests[h]
		invalidatedRequests := 0
		for _, req := range reqs {
			req.retries++
			if req.retries > f.cfg.MaxRetriesForRequest {
				f.logger.With().Debug("gave up on hash after max retries",
					log.String("hash", req.hash.ShortString()))
				req.returnChan <- HashDataPromiseResult{
					Err:     errExceedMaxRetries,
					Hash:    req.hash,
					Data:    []byte{},
					IsLocal: false,
				}
				invalidatedRequests++
			} else {
				// put the request back to the active list
				f.activeRequests[req.hash] = append(f.activeRequests[req.hash], req)
			}
		}
		// the remaining requests in pendingRequests is either invalid (exceed MaxRetriesForRequest) or
		// put back to the active list.
		delete(f.pendingRequests, h)
		f.activeReqM.Unlock()
	}

	// delete the hash of waiting batch
	f.activeBatchM.Lock()
	delete(f.activeBatches, response.ID)
	f.activeBatchM.Unlock()
}

// this is the main function that sends the hash request to the peer.
func (f *Fetch) requestHashBatchFromPeers() {
	var requestList []RequestMessage
	f.activeReqM.Lock()
	// only send one request per hash
	for hash, reqs := range f.activeRequests {
		f.logger.With().Debug("batching hash request", log.String("hash", hash.ShortString()))
		requestList = append(requestList, RequestMessage{Hash: hash, Hint: reqs[0].hint})
		// move the processed requests to pending
		f.pendingRequests[hash] = append(f.pendingRequests[hash], reqs...)
		delete(f.activeRequests, hash)
	}
	f.activeReqM.Unlock()

	f.send(requestList)
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
			f.sendBatch(peer, reqs)
		}
	}
}

func (f *Fetch) organizeRequests(requests []RequestMessage) map[p2p.Peer][][]RequestMessage {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	peer2requests := make(map[p2p.Peer][]RequestMessage)
	peers := f.host.GetPeers()

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
	for peer, requests := range peer2requests {
		if len(requests) < f.cfg.BatchSize {
			result[peer] = [][]RequestMessage{
				requests,
			}
			continue
		}
		for i := 0; i < len(requests); i += f.cfg.BatchSize {
			j := i + f.cfg.BatchSize
			if j > len(requests) {
				j = len(requests)
			}
			result[peer] = append(result[peer], requests[i:j])
		}
	}

	return result
}

// sendBatch dispatches batched request messages to provided peer.
func (f *Fetch) sendBatch(p p2p.Peer, requests []RequestMessage) error {
	// build list of batch messages
	var batch batchInfo
	batch.Requests = requests
	batch.peer = p
	batch.SetID()

	f.activeBatchM.Lock()
	f.activeBatches[batch.ID] = batch
	f.activeBatchM.Unlock()

	// timeout function will be called if no response was received for the hashes sent
	errorFunc := func(err error) {
		f.logger.With().Warning("error occurred for sendbatch",
			log.String("batch_hash", batch.ID.ShortString()),
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

		f.logger.With().Debug("sending request batch to peer",
			log.String("batch_hash", batch.ID.ShortString()),
			log.Int("num_requests", len(batch.Requests)),
			log.String("peer", p.String()))

		err = f.servers[hashProtocol].Request(f.shutdownCtx, p, bytes, f.receiveResponse, errorFunc)
		if err == nil {
			break
		}

		retries++
		if retries > f.cfg.MaxRetriesForPeer {
			f.handleHashError(batch.ID, fmt.Errorf("could not send message: %w", err))
			break
		}
		// todo: mark number of fails per peer to make it low priority
		f.logger.With().Warning("could not send message to peer",
			log.String("peer", p.String()),
			log.Int("retries", retries))
	}

	return err
}

// handleHashError is called when an error occurred processing batches of the following hashes.
func (f *Fetch) handleHashError(batchHash types.Hash32, err error) {
	f.logger.With().Debug("cannot fetch message",
		log.String("batchHash", batchHash.ShortString()),
		log.Err(err))
	f.activeBatchM.RLock()
	batch, ok := f.activeBatches[batchHash]
	if !ok {
		f.activeBatchM.RUnlock()
		f.logger.With().Error("batch invalidated twice", log.String("batchHash", batchHash.ShortString()))
		return
	}
	f.activeBatchM.RUnlock()
	f.activeReqM.Lock()
	for _, h := range batch.Requests {
		f.logger.With().Debug("error for hash requests",
			log.String("hash", h.Hash.ShortString()),
			log.Int("numSubscribers", len(f.pendingRequests[h.Hash])),
			log.Err(err))
		for _, callback := range f.pendingRequests[h.Hash] {
			callback.returnChan <- HashDataPromiseResult{
				Err:     err,
				Hash:    h.Hash,
				Data:    nil,
				IsLocal: false,
			}
		}
		delete(f.pendingRequests, h.Hash)
	}
	f.activeReqM.Unlock()

	f.activeBatchM.Lock()
	delete(f.activeBatches, batchHash)
	f.activeBatchM.Unlock()
}

// GetHashes gets a list of hashes to be fetched and will return a map of hashes and their respective promise channels.
func (f *Fetch) GetHashes(hashes []types.Hash32, hint datastore.Hint, validateHash bool) map[types.Hash32]chan HashDataPromiseResult {
	hashWaiting := make(map[types.Hash32]chan HashDataPromiseResult)
	for _, id := range hashes {
		resChan := f.GetHash(id, hint, validateHash)
		hashWaiting[id] = resChan
	}

	return hashWaiting
}

// GetHash is the regular buffered call to get a specific hash, using provided hash, h as hint the receiving end will
// know where to look for the hash, this function returns HashDataPromiseResult channel that will hold Data received or error.
func (f *Fetch) GetHash(hash types.Hash32, h datastore.Hint, validateHash bool) chan HashDataPromiseResult {
	resChan := make(chan HashDataPromiseResult, 1)

	if f.stopped() {
		close(resChan)
		return resChan
	}

	// check if we already have this hash locally
	if b, err := f.bs.Get(h, hash.Bytes()); err == nil {
		resChan <- HashDataPromiseResult{
			Err:     nil,
			Hash:    hash,
			Data:    b,
			IsLocal: true,
		}
		return resChan
	}

	// if not present in db, call fetching of the item
	req := request{
		hash,
		validateHash,
		h,
		resChan,
		0,
	}

	f.requestReceiver <- req

	return resChan
}

// RegisterPeerHashes registers provided peer for a list of hashes.
func (f *Fetch) RegisterPeerHashes(peer p2p.Peer, hashes []types.Hash32) {
	f.hashToPeers.RegisterPeerHashes(peer, hashes)
}

// AddPeersFromHash adds peers from one hash to others.
func (f *Fetch) AddPeersFromHash(fromHash types.Hash32, toHashes []types.Hash32) {
	f.hashToPeers.AddPeersFromHash(fromHash, toHashes)
}

func (f *Fetch) GetPeers() []p2p.Peer {
	return f.host.GetPeers()
}
