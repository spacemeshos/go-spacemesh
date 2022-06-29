// Package fetch contains mechanism to fetch Data from remote peers
package fetch

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	ftypes "github.com/spacemeshos/go-spacemesh/fetch/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

const (
	atxProtocol     = "/atx/1"
	lyrDataProtocol = "/layerdata/1"
	fetchProtocol   = "/fetch/2"

	batchMaxSize = 20
	cacheSize    = 1000
)

var (
	emptyHash = types.Hash32{}

	// errNoPeers is returned when node has no peers.
	errNoPeers = errors.New("no peers")

	// errExceedMaxRetries is returned when MaxRetriesForRequest attempts has been made to fetch data for a hash and failed.
	errExceedMaxRetries = errors.New("fetch failed after max retries for request")

	// errWrongHash is returned when the data in the peer's response does not hash to the same value as requested.
	errWrongHash = errors.New("wrong hash from response")
)

// request contains all relevant Data for a single request for a specified hash.
type request struct {
	hash                 types.Hash32                      // hash is the hash of the Data requested
	validateResponseHash bool                              // if true perform hash validation on received Data
	hint                 datastore.Hint                    // the hint from which database to fetch this hash
	returnChan           chan ftypes.HashDataPromiseResult // channel that will signal if the call succeeded or not
	retries              int
}

// requestMessage is the on the wire message that will be send to the peer for hash query.
type requestMessage struct {
	Hint datastore.Hint
	Hash types.Hash32
}

// responseMessage is the on the wire message that will be send to the this node as response,.
type responseMessage struct {
	Hash types.Hash32
	Data []byte
}

// requestBatch is a batch of requests and a hash of all requests as ID.
type requestBatch struct {
	ID       types.Hash32
	Requests []requestMessage
}

type batchInfo struct {
	requestBatch
	peer p2p.Peer
}

// SetID calculates the hash of all requests and sets it as this batches ID.
func (b *batchInfo) SetID() {
	bts, err := types.InterfaceToBytes(b.Requests)
	if err != nil {
		return
	}
	b.ID = types.CalcHash32(bts)
}

// ToMap converts the array of requests to map so it can be easily invalidated.
func (b batchInfo) ToMap() map[types.Hash32]requestMessage {
	m := make(map[types.Hash32]requestMessage)
	for _, r := range b.Requests {
		m[r.Hash] = r
	}
	return m
}

// responseBatch is the response struct send for a requestBatch. the responseBatch ID must be the same
// as stated in requestBatch even if not all Data is present.
type responseBatch struct {
	ID        types.Hash32
	Responses []responseMessage
}

// Config is the configuration file of the Fetch component.
type Config struct {
	BatchTimeout         int // in milliseconds
	MaxRetriesForPeer    int
	BatchSize            int
	RequestTimeout       int // in seconds
	MaxRetriesForRequest int
}

// DefaultConfig is the default config for the fetch component.
func DefaultConfig() Config {
	return Config{
		BatchTimeout:         50,
		MaxRetriesForPeer:    2,
		BatchSize:            20,
		RequestTimeout:       10,
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

// Fetch is the main struct that contains network peers and logic to batch and dispatch hash fetch requests.
type Fetch struct {
	cfg     Config
	log     log.Log
	bs      *datastore.BlobStore
	host    host
	atxSrv  server.Requestor
	lyrSrv  server.Requestor
	hashSrv server.Requestor

	// activeRequests contains requests that are not processed
	activeRequests map[types.Hash32][]*request
	// pendingRequests contains requests that have been processed and are waiting for responses
	pendingRequests map[types.Hash32][]*request
	// activeBatches contains batches of requests in pendingRequests.
	activeBatches        map[types.Hash32]batchInfo
	requestReceiver      chan request
	batchRequestReceiver chan []request
	batchTimeout         *time.Ticker
	stop                 chan struct{}
	activeReqM           sync.RWMutex
	activeBatchM         sync.RWMutex
	onlyOnce             sync.Once
	doneChan             chan struct{}
	dbLock               sync.RWMutex
	hashToPeers          *HashPeersCache
}

// newFetch creates a new Fetch struct.
func newFetch(cfg Config, h host, bs *datastore.BlobStore, atxS, lyrS, hashS server.Requestor, logger log.Log) *Fetch {
	f := &Fetch{
		cfg:             cfg,
		log:             logger,
		bs:              bs,
		host:            h,
		atxSrv:          atxS,
		lyrSrv:          lyrS,
		hashSrv:         hashS,
		activeRequests:  make(map[types.Hash32][]*request),
		pendingRequests: make(map[types.Hash32][]*request),
		requestReceiver: make(chan request),
		batchTimeout:    time.NewTicker(time.Millisecond * time.Duration(cfg.BatchTimeout)),
		stop:            make(chan struct{}),
		activeBatches:   make(map[types.Hash32]batchInfo),
		doneChan:        make(chan struct{}),
		hashToPeers:     NewHashPeersCache(cacheSize),
	}
	return f
}

// Start starts handling fetch requests.
func (f *Fetch) Start() {
	f.onlyOnce.Do(func() {
		go f.loop()
	})
}

// Stop stops handling fetch requests.
func (f *Fetch) Stop() {
	f.log.Info("stopping fetch")
	f.batchTimeout.Stop()
	close(f.stop)
	f.host.Close()
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

	// wait for close to end
	<-f.doneChan
	f.log.Info("stopped fetch")
}

// stopped returns if we should stop.
func (f *Fetch) stopped() bool {
	select {
	case <-f.stop:
		return true
	default:
		return false
	}
}

// handleNewRequest batches low priority requests and sends a high priority request to the peers right away.
// if there are pending requests for the same hash, it will put the new request, regardless of the priority,
// to the pending list and wait for notification when the earlier request gets response.
// it returns true if a request is sent immediately, or false otherwise.
func (f *Fetch) handleNewRequest(req *request) bool {
	f.activeReqM.Lock()
	if _, ok := f.pendingRequests[req.hash]; ok {
		// hash already being requested. just add the req and wait for the notification
		f.pendingRequests[req.hash] = append(f.pendingRequests[req.hash], req)
		f.activeReqM.Unlock()
		return false
	}
	f.activeRequests[req.hash] = append(f.activeRequests[req.hash], req)
	rLen := len(f.activeRequests)
	f.activeReqM.Unlock()
	f.log.With().Debug("request added to queue", log.String("hash", req.hash.ShortString()))
	if rLen > batchMaxSize {
		go f.requestHashBatchFromPeers() // Process the batch.
		return true
	}
	return false
}

// here we receive all requests for hashes for all DBs and batch them together before we send the request to peer
// there can be a priority request that will not be batched.
func (f *Fetch) loop() {
	f.log.Info("starting fetch main loop")
	for {
		select {
		case req := <-f.requestReceiver:
			f.handleNewRequest(&req)
		case <-f.batchTimeout.C:
			go f.requestHashBatchFromPeers() // Process the batch.
		case <-f.stop:
			close(f.doneChan)
			return
		}
	}
}

// receive Data from message server and call response handlers accordingly.
func (f *Fetch) receiveResponse(data []byte) {
	if f.stopped() {
		return
	}

	var response responseBatch
	err := types.BytesToInterface(data, &response)
	if err != nil {
		f.log.With().Error("response was unclear, maybe leaking", log.Err(err))
		return
	}

	f.activeBatchM.RLock()
	batch, has := f.activeBatches[response.ID]
	f.activeBatchM.RUnlock()
	if !has {
		f.log.With().Warning("unknown batch response received, or already invalidated", log.String("batchHash", response.ID.ShortString()))
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
		actualHash := emptyHash
		for _, req := range reqs {
			var err error
			if req.validateResponseHash {
				if actualHash == emptyHash {
					actualHash = types.CalcHash32(data)
				}
				if actualHash != resID.Hash {
					err = fmt.Errorf("%w: %v, actual %v", errWrongHash, resID.Hash.ShortString(), actualHash.ShortString())
				}
			}
			req.returnChan <- ftypes.HashDataPromiseResult{
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
		f.log.With().Warning("hash not found in response from peer",
			log.String("hint", string(batchMap[h].Hint)),
			log.String("hash", h.ShortString()),
			log.String("peer", batch.peer.String()))
		f.activeReqM.Lock()
		reqs := f.pendingRequests[h]
		invalidatedRequests := 0
		for _, req := range reqs {
			req.retries++
			if req.retries > f.cfg.MaxRetriesForRequest {
				f.log.With().Debug("gave up on hash after max retries",
					log.String("hash", req.hash.ShortString()))
				req.returnChan <- ftypes.HashDataPromiseResult{
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
	var requestList []requestMessage
	f.activeReqM.Lock()
	// only send one request per hash
	for hash, reqs := range f.activeRequests {
		f.log.With().Debug("batching hash request", log.String("hash", hash.ShortString()))
		requestList = append(requestList, requestMessage{Hash: hash, Hint: reqs[0].hint})
		// move the processed requests to pending
		f.pendingRequests[hash] = append(f.pendingRequests[hash], reqs...)
		delete(f.activeRequests, hash)
	}
	f.activeReqM.Unlock()

	f.send(requestList)
}

func (f *Fetch) send(requests []requestMessage) {
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

func (f *Fetch) organizeRequests(requests []requestMessage) map[p2p.Peer][][]requestMessage {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	peer2requests := make(map[p2p.Peer][]requestMessage)
	peers := f.host.GetPeers()

	for _, req := range requests {
		p, exists := f.hashToPeers.GetRandom(req.Hash, rng)
		if !exists {
			p = randomPeer(peers)
		}

		_, ok := peer2requests[p]
		if !ok {
			peer2requests[p] = []requestMessage{req}
		} else {
			peer2requests[p] = append(peer2requests[p], req)
		}
	}

	// split every peer's requests into batches of f.cfg.BatchSize each
	result := make(map[p2p.Peer][][]requestMessage)
	for peer, requests := range peer2requests {
		if len(requests) < f.cfg.BatchSize {
			result[peer] = [][]requestMessage{
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
func (f *Fetch) sendBatch(p p2p.Peer, requests []requestMessage) error {
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
		f.log.With().Warning("error occurred for sendbatch",
			log.String("batch_hash", batch.ID.ShortString()),
			log.Err(err))
		f.handleHashError(batch.ID, err)
	}

	bytes, err := types.InterfaceToBytes(&batch.requestBatch)
	if err != nil {
		f.handleHashError(batch.ID, err)
	}

	// try sending batch to provided peer
	retries := 0
	for {
		if f.stopped() {
			return nil
		}

		f.log.With().Debug("sending request batch to peer",
			log.String("batch_hash", batch.ID.ShortString()),
			log.Int("num_requests", len(batch.Requests)),
			log.String("peer", p.String()))

		err = f.hashSrv.Request(context.TODO(), p, bytes, f.receiveResponse, errorFunc)
		if err == nil {
			break
		}

		retries++
		if retries > f.cfg.MaxRetriesForPeer {
			f.handleHashError(batch.ID, fmt.Errorf("could not send message: %w", err))
			break
		}
		// todo: mark number of fails per peer to make it low priority
		f.log.With().Warning("could not send message to peer",
			log.String("peer", p.String()),
			log.Int("retries", retries))
	}

	return err
}

// handleHashError is called when an error occurred processing batches of the following hashes.
func (f *Fetch) handleHashError(batchHash types.Hash32, err error) {
	f.log.With().Debug("cannot fetch message",
		log.String("batchHash", batchHash.ShortString()),
		log.Err(err))
	f.activeBatchM.RLock()
	batch, ok := f.activeBatches[batchHash]
	if !ok {
		f.activeBatchM.RUnlock()
		f.log.With().Error("batch invalidated twice", log.String("batchHash", batchHash.ShortString()))
		return
	}
	f.activeBatchM.RUnlock()
	f.activeReqM.Lock()
	for _, h := range batch.Requests {
		f.log.With().Debug("error for hash requests",
			log.String("hash", h.Hash.ShortString()),
			log.Int("numSubscribers", len(f.pendingRequests[h.Hash])),
			log.Err(err))
		for _, callback := range f.pendingRequests[h.Hash] {
			callback.returnChan <- ftypes.HashDataPromiseResult{
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
func (f *Fetch) GetHashes(hashes []types.Hash32, hint datastore.Hint, validateHash bool) map[types.Hash32]chan ftypes.HashDataPromiseResult {
	hashWaiting := make(map[types.Hash32]chan ftypes.HashDataPromiseResult)
	for _, id := range hashes {
		resChan := f.GetHash(id, hint, validateHash)
		hashWaiting[id] = resChan
	}

	return hashWaiting
}

// GetHash is the regular buffered call to get a specific hash, using provided hash, h as hint the receiving end will
// know where to look for the hash, this function returns HashDataPromiseResult channel that will hold Data received or error.
func (f *Fetch) GetHash(hash types.Hash32, h datastore.Hint, validateHash bool) chan ftypes.HashDataPromiseResult {
	resChan := make(chan ftypes.HashDataPromiseResult, 1)

	if f.stopped() {
		close(resChan)
		return resChan
	}

	// check if we already have this hash locally
	if b, err := f.bs.Get(h, hash.Bytes()); err == nil {
		resChan <- ftypes.HashDataPromiseResult{
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

// GetLayerData get layer data from peers.
func (f *Fetch) GetLayerData(ctx context.Context, lid types.LayerID, okCB func([]byte, p2p.Peer, int), errCB func(error, p2p.Peer, int)) error {
	remotePeers := f.host.GetPeers()
	numPeers := len(remotePeers)
	if numPeers == 0 {
		return errNoPeers
	}

	for _, p := range remotePeers {
		peer := p
		okFunc := func(data []byte) {
			okCB(data, peer, numPeers)
		}
		errFunc := func(err error) {
			errCB(err, peer, numPeers)
		}
		if err := f.lyrSrv.Request(ctx, peer, lid.Bytes(), okFunc, errFunc); err != nil {
			errFunc(err)
		}
	}
	return nil
}

// GetEpochATXIDs get all ATXIDs targeted for a specified epoch from peers.
func (f *Fetch) GetEpochATXIDs(ctx context.Context, eid types.EpochID, okCB func([]byte, p2p.Peer), errFunc func(error)) error {
	remotePeers := f.host.GetPeers()
	if len(remotePeers) == 0 {
		return errNoPeers
	}

	peer := randomPeer(remotePeers)
	okFunc := func(data []byte) {
		okCB(data, peer)
	}
	if err := f.atxSrv.Request(ctx, peer, eid.ToBytes(), okFunc, errFunc); err != nil {
		return err
	}
	return nil
}

// RegisterPeerHashes registers provided peer for a list of hashes.
func (f *Fetch) RegisterPeerHashes(peer p2p.Peer, hashes []types.Hash32) {
	f.hashToPeers.RegisterPeerHashes(peer, hashes)
}

// AddPeersFromHash adds peers from one hash to others.
func (f *Fetch) AddPeersFromHash(fromHash types.Hash32, toHashes []types.Hash32) {
	f.hashToPeers.AddPeersFromHash(fromHash, toHashes)
}
