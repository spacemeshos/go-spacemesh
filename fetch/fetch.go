// Package fetch contains mechanism to fetch Data from remote peers
package fetch

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	p2pconf "github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/rand"
)

// Hint marks which DB should be queried for a certain provided hash
type Hint string

var emptyHash = types.Hash32{}

// DB hints per DB
const (
	BlockDB Hint = "blocksDB"
	ATXDB   Hint = "ATXDB"
	TXDB    Hint = "TXDB"
	POETDB  Hint = "POETDB"
)

// priority defines whether Data will be fetched at once or batched and waited for up to "batchTimeout"
// until fetched
type priority uint16

// Message priority constants
const (
	// Low will perform batched calls
	Low priority = 0
	// High will call fetch immediately
	High priority = 1
)

const (
	fetchProtocol = "/sync/2.0/"
	batchMaxSize  = 20
)

// ErrCouldNotSend is a special type of error indicating fetch could not be done because message could not be sent to peers
type ErrCouldNotSend error

// ErrExceedMaxRetries is returned when MaxRetriesForRequest attempts has been made to fetch data for a hash and failed
var ErrExceedMaxRetries = errors.New("fetch failed after max retries for request")

// Fetcher is the general interface of the fetching unit, capable of requesting bytes that corresponds to a hash
// from other remote peers.
type Fetcher interface {
	GetHash(hash types.Hash32, h Hint, validateHash bool) chan HashDataPromiseResult
	GetHashes(hash []types.Hash32, hint Hint, validateHash bool) map[types.Hash32]chan HashDataPromiseResult
	AddDB(hint Hint, db database.Getter)
	Stop()
	Start()
}

/// request contains all relevant Data for a single request for a specified hash
type request struct {
	hash                 types.Hash32               // hash is the hash of the Data requested
	priority             priority                   // priority is for QoS
	validateResponseHash bool                       // if true perform hash validation on received Data
	hint                 Hint                       // the hint from which database to fetch this hash
	returnChan           chan HashDataPromiseResult // channel that will signal if the call succeeded or not
	retries              int
}

// requestMessage is the on the wire message that will be send to the peer for hash query
type requestMessage struct {
	Hint Hint
	Hash types.Hash32
}

// responseMessage is the on the wire message that will be send to the this node as response,
type responseMessage struct {
	Hash types.Hash32
	Data []byte
}

// requestBatch is a batch of requests and a hash of all requests as ID
type requestBatch struct {
	ID       types.Hash32
	Requests []requestMessage
}

type batchInfo struct {
	requestBatch
	peer peers.Peer
}

// SetID calculates the hash of all requests and sets it as this batches ID
func (b *batchInfo) SetID() {
	bts, err := types.InterfaceToBytes(b.Requests)
	if err != nil {
		return
	}
	b.ID = types.CalcHash32(bts)
}

// ToMap converts the array of requests to map so it can be easily invalidated
func (b batchInfo) ToMap() map[types.Hash32]requestMessage {
	m := make(map[types.Hash32]requestMessage)
	for _, r := range b.Requests {
		m[r.Hash] = r
	}
	return m
}

// responseBatch is the response struct send for a requestBatch. the responseBatch ID must be the same
// as stated in requestBatch even if not all Data is present
type responseBatch struct {
	ID        types.Hash32
	Responses []responseMessage
}

// Config is the configuration file of the Fetch component
type Config struct {
	BatchTimeout         int // in milliseconds
	MaxRetriesForPeer    int
	BatchSize            int
	RequestTimeout       int // in seconds
	MaxRetriesForRequest int
}

// DefaultConfig is the default config for the fetch component
func DefaultConfig() Config {
	return Config{
		BatchTimeout:         50,
		MaxRetriesForPeer:    2,
		BatchSize:            20,
		RequestTimeout:       10,
		MaxRetriesForRequest: 20,
	}
}

type peersProvider interface {
	GetPeers() []peers.Peer
	PeerCount() uint64
	Close()
}

// MessageNetwork is a network interface that allows fetch to communicate with other nodes with 'fetch servers'
type MessageNetwork struct {
	*server.MessageServer
	peersProvider
	log.Log
}

// Close closes the message network
func (mn MessageNetwork) Close() {
	mn.MessageServer.Close()
	mn.peersProvider.Close()
}

// NewMessageNetwork creates a new instance of the fetch network server
func NewMessageNetwork(ctx context.Context, requestTimeOut int, net service.Service, protocol string, log log.Log) *MessageNetwork {
	return &MessageNetwork{
		server.NewMsgServer(ctx, net.(server.Service), protocol, time.Duration(requestTimeOut)*time.Second, make(chan service.DirectMessage, p2pconf.Values.BufferSize), log),
		peers.Start(net, peers.WithLog(log.WithName("peers"))),
		log,
	}
}

// GetRandomPeer returns a random peer from current peer list
func GetRandomPeer(peers []peers.Peer) peers.Peer {
	if len(peers) == 0 {
		log.Panic("cannot send fetch: no peers found")
	}
	return peers[rand.Intn(len(peers))]
}

// GetPeers return active peers
func (mn MessageNetwork) GetPeers() []peers.Peer {
	return mn.peersProvider.GetPeers()
}

type network interface {
	GetPeers() []peers.Peer
	PeerCount() uint64
	SendRequest(ctx context.Context, msgType server.MessageType, payload []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte), failHandler func(err error)) error
	RegisterBytesMsgHandler(msgType server.MessageType, reqHandler func(ctx context.Context, b []byte) ([]byte, error))
	Close()
}

// Fetch is the main struct that contains network peers and logic to batch and dispatch hash fetch requests
type Fetch struct {
	cfg Config
	log log.Log
	dbs map[Hint]database.Getter
	// activeRequests contains requests that are not processed
	activeRequests map[types.Hash32][]*request
	// pendingRequests contains requests that have been processed and are waiting for responses
	pendingRequests map[types.Hash32][]*request
	// activeBatches contains batches of requests in pendingRequests.
	activeBatches        map[types.Hash32]batchInfo
	net                  network
	requestReceiver      chan request
	batchRequestReceiver chan []request
	batchTimeout         *time.Ticker
	stop                 chan struct{}
	activeReqM           sync.RWMutex
	activeBatchM         sync.RWMutex
	onlyOnce             sync.Once
	doneChan             chan struct{}
	dbLock               sync.RWMutex
}

// NewFetch creates a new Fetch struct
func NewFetch(ctx context.Context, cfg Config, network service.Service, logger log.Log) *Fetch {
	srv := NewMessageNetwork(ctx, cfg.RequestTimeout, network, fetchProtocol, logger)

	f := &Fetch{
		cfg:             cfg,
		log:             logger,
		dbs:             make(map[Hint]database.Getter),
		activeRequests:  make(map[types.Hash32][]*request),
		pendingRequests: make(map[types.Hash32][]*request),
		net:             srv,
		requestReceiver: make(chan request),
		batchTimeout:    time.NewTicker(time.Millisecond * time.Duration(cfg.BatchTimeout)),
		stop:            make(chan struct{}),
		activeBatches:   make(map[types.Hash32]batchInfo),
		doneChan:        make(chan struct{}),
	}

	return f
}

// Start starts handling fetch requests
func (f *Fetch) Start() {
	f.onlyOnce.Do(func() {
		f.net.RegisterBytesMsgHandler(server.Fetch, f.FetchRequestHandler)
		go f.loop()
	})
}

// Stop stops handling fetch requests
func (f *Fetch) Stop() {
	f.log.Info("stopping fetch")
	f.batchTimeout.Stop()
	close(f.stop)
	f.net.Close()
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

// stopped returns if we should stop
func (f *Fetch) stopped() bool {
	select {
	case <-f.stop:
		return true
	default:
		return false
	}
}

// AddDB adds a DB with corresponding hint
// all network peersProvider will be able to query this DB
func (f *Fetch) AddDB(hint Hint, db database.Getter) {
	f.dbLock.Lock()
	f.dbs[hint] = db
	f.dbLock.Unlock()
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
	sendNow := req.priority > Low
	// group requests by hash
	if sendNow {
		f.pendingRequests[req.hash] = append(f.pendingRequests[req.hash], req)
	} else {
		f.activeRequests[req.hash] = append(f.activeRequests[req.hash], req)
	}
	rLen := len(f.activeRequests)
	f.activeReqM.Unlock()
	if sendNow {
		f.sendBatch([]requestMessage{{req.hint, req.hash}})
		f.log.With().Debug("high priority request sent", log.String("hash", req.hash.ShortString()))
		return true
	}
	f.log.With().Debug("request added to queue", log.String("hash", req.hash.ShortString()))
	if rLen > batchMaxSize {
		go f.requestHashBatchFromPeers() // Process the batch.
		return true
	}
	return false
}

// here we receive all requests for hashes for all DBs and batch them together before we send the request to peer
// there can be a priority request that will not be batched
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

// FetchRequestHandler handles requests for sync from peersProvider, and basically reads Data from database and puts it
// in a response batch
func (f *Fetch) FetchRequestHandler(ctx context.Context, data []byte) ([]byte, error) {
	if f.stopped() {
		return nil, server.ErrShuttingDown
	}

	var requestBatch requestBatch
	err := types.BytesToInterface(data, &requestBatch)
	if err != nil {
		f.log.WithContext(ctx).With().Error("failed to parse request", log.Err(err))
		return nil, server.ErrBadRequest
	}
	resBatch := responseBatch{
		ID:        requestBatch.ID,
		Responses: make([]responseMessage, 0, len(requestBatch.Requests)),
	}
	// this will iterate all requests and populate appropriate Responses, if there are any missing items they will not
	// be included in the response at all
	for _, r := range requestBatch.Requests {
		f.dbLock.RLock()
		db, ok := f.dbs[r.Hint]
		f.dbLock.RUnlock()
		if !ok {
			f.log.WithContext(ctx).With().Warning("hint not found in database", log.String("hint", string(r.Hint)))
			continue
		}
		res, err := db.Get(r.Hash.Bytes())
		if err != nil {
			f.log.WithContext(ctx).With().Info("remote peer requested nonexistent hash",
				log.String("hash", r.Hash.ShortString()),
				log.String("hint", string(r.Hint)),
				log.Err(err))
			continue
		} else {
			f.log.WithContext(ctx).With().Debug("responded to hash request",
				log.String("hash", r.Hash.ShortString()),
				log.Int("dataSize", len(res)))
		}
		// add response to batch
		m := responseMessage{
			Hash: r.Hash,
			Data: res,
		}
		resBatch.Responses = append(resBatch.Responses, m)
	}

	bts, err := types.InterfaceToBytes(&resBatch)
	if err != nil {
		f.log.WithContext(ctx).With().Panic("failed to serialize batch id",
			log.String("batch_hash", resBatch.ID.ShortString()))
	}
	f.log.WithContext(ctx).With().Debug("returning response for batch",
		log.String("batch_hash", resBatch.ID.ShortString()),
		log.Int("count_responses", len(resBatch.Responses)),
		log.Int("data_size", len(bts)))
	return bts, nil
}

// receive Data from message server and call response handlers accordingly
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
					err = fmt.Errorf("hash didnt match expected: %v, actual %v", resID.Hash.ShortString(), actualHash.ShortString())
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
				req.returnChan <- HashDataPromiseResult{
					Err:     ErrExceedMaxRetries,
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

// this is the main function that sends the hash request to the peer
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

	// send in batches
	for i := 0; i < len(requestList); i += f.cfg.BatchSize {
		j := i + f.cfg.BatchSize
		if j > len(requestList) {
			j = len(requestList)
		}
		f.sendBatch(requestList[i:j])
	}
}

// sendBatch dispatches batched request messages
func (f *Fetch) sendBatch(requests []requestMessage) {
	// build list of batch messages
	var batch batchInfo
	batch.Requests = requests
	batch.SetID()

	f.activeBatchM.Lock()
	f.activeBatches[batch.ID] = batch
	f.activeBatchM.Unlock()
	// timeout function will be called if no response was received for the hashes sent
	errorFunc := func(err error) {
		f.log.With().Error("error occurred for sendbatch",
			log.String("batch_hash", batch.ID.ShortString()),
			log.Err(err))
		f.handleHashError(batch.ID, err)
	}

	bytes, err := types.InterfaceToBytes(&batch.requestBatch)
	if err != nil {
		f.handleHashError(batch.ID, err)
	}
	// try sending batch to some random peer
	retries := 0
	var p peers.Peer
	for {
		if f.stopped() {
			return
		}

		if f.net.PeerCount() == 0 {
			f.log.With().Error("no peers found, unable to send request batch",
				batch.ID,
				log.Int("items", len(batch.Requests)))
			return
		}

		// get random peer
		p = GetRandomPeer(f.net.GetPeers())
		f.log.With().Debug("sending request batch to peer",
			log.String("batch_hash", batch.ID.ShortString()),
			log.Int("num_requests", len(batch.Requests)),
			log.String("peer", p.String()))
		batch.peer = p
		f.activeBatchM.Lock()
		f.activeBatches[batch.ID] = batch
		f.activeBatchM.Unlock()
		err := f.net.SendRequest(context.TODO(), server.Fetch, bytes, p, f.receiveResponse, errorFunc)

		// if call succeeded, continue to other requests
		if err != nil {
			retries++
			if retries > f.cfg.MaxRetriesForPeer {
				f.handleHashError(batch.ID, ErrCouldNotSend(fmt.Errorf("could not send message: %w", err)))
				break
			}
			// todo: mark number of fails per peer to make it low priority
			f.log.With().Warning("could not send message to peer",
				log.String("peer", p.String()),
				log.Int("retries", retries))
		} else {
			break
		}
	}
}

// handleHashError is called when an error occurred processing batches of the following hashes
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

// HashDataPromiseResult is the result strict when requesting Data corresponding to the Hash
type HashDataPromiseResult struct {
	Err     error
	Hash    types.Hash32
	Data    []byte
	IsLocal bool
}

// GetHashes gets a list of hashes to be fetched and will return a map of hashes and their respective promise channels
func (f *Fetch) GetHashes(hashes []types.Hash32, hint Hint, validateHash bool) map[types.Hash32]chan HashDataPromiseResult {
	hashWaiting := make(map[types.Hash32]chan HashDataPromiseResult)
	for _, id := range hashes {
		resChan := f.GetHash(id, hint, validateHash)
		hashWaiting[id] = resChan
	}

	return hashWaiting
}

// GetHash is the regular buffered call to get a specific hash, using provided hash, h as hint the receiving end will
// know where to look for the hash, this function returns HashDataPromiseResult channel that will hold Data received or error
func (f *Fetch) GetHash(hash types.Hash32, h Hint, validateHash bool) chan HashDataPromiseResult {
	resChan := make(chan HashDataPromiseResult, 1)

	if f.stopped() {
		close(resChan)
		return resChan
	}

	// check if we already have this hash locally
	f.dbLock.RLock()
	db, ok := f.dbs[h]
	f.dbLock.RUnlock()
	if !ok {
		f.log.With().Panic("tried to fetch Data from DB that doesn't exist locally",
			log.String("hint", string(h)))
	}

	if b, err := db.Get(hash.Bytes()); err == nil {
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
		Low,
		validateHash,
		h,
		resChan,
		0,
	}

	f.requestReceiver <- req

	return resChan
}
