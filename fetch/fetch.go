// Package fetch contains mechanism to fetch data from remote peers
package fetch

import (
	"context"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	p2pconf "github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	p2ppeers "github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/rand"
	"sync"
	"time"
)

// Hint marks which DB should be queried for a certain provided hash
type Hint string

// priority defines whether data will be fetched at once or batched and waited for up to "batchTimeout"
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
	fetch         server.MessageType = 1
	fetchProtocol                    = "/sync/2.0/"
	batchMaxSize                     = 20
)

// ErrCouldNotSend is a special type of error indicating fetch could not be done because message could not be sent to peers
type ErrCouldNotSend error

// Fetcher is the general interface of the fetching unit, capable of requesting bytes that corresponds to a hash
// from other remote peers.
type Fetcher interface {
	GetHash(hash types.Hash32, h Hint, validateHash bool) chan HashDataPromiseResult
	GetHashes(hash []types.Hash32, hint Hint, validateHash bool) map[types.Hash32]chan HashDataPromiseResult
}

/// request contains all relevant data for a single request for a specified hash
type request struct {
	hash                 types.Hash32               // hash is the hash of the data requested
	priority             priority                   // priority is for QoS
	validateResponseHash bool                       // if true perform hash validation on received data
	hint                 Hint                       // the hint from which database to fetch this hash
	returnChan           chan HashDataPromiseResult //channel that will signal if the call succeeded or not
}

// requestMessage is the on the wire message that will be send to the peer for hash query
type requestMessage struct {
	Hint Hint
	Hash types.Hash32
}

// responseMessage is the on the wire message that will be send to the this node as response,
type responseMessage struct {
	Hash types.Hash32
	data []byte
}

// requestBatch is a batch of requests and a hash of all requests as ID
type requestBatch struct {
	ID       types.Hash32
	Requests []requestMessage
}

// SetID calculates the hash of all requests and sets it as this batches ID
func (b *requestBatch) SetID() {
	bts, err := types.InterfaceToBytes(b.Requests)
	if err != nil {
		return
	}
	b.ID = types.CalcHash32(bts)
}

//ToMap converts the array of requests to map so it can be easily invalidated
func (b requestBatch) ToMap() map[types.Hash32]requestMessage {
	m := make(map[types.Hash32]requestMessage)
	for _, r := range b.Requests {
		m[r.Hash] = r
	}
	return m
}

// responseBatch is the response struct send for a requestBatch. the responseBatch ID must be the same
// as stated in requestBatch even if not all data is present
type responseBatch struct {
	ID        types.Hash32
	Responses []responseMessage
}

// Config is the configuration file of the Fetch component
type Config struct {
	BatchTimeout      int // in seconds
	MaxRetiresForPeer int
	BatchSize         int
	RequestTimeout    int // in seconds
}

type peersProvider interface {
	GetPeers() []p2ppeers.Peer
}

// MessageNetwork is a network interface that allows fetch to communicate with other nodes with 'fetch servers'
type MessageNetwork struct {
	*server.MessageServer
	peersProvider
	log.Log
}

// NewMessageNetwork creates a new instance of the fetch network server
func NewMessageNetwork(ctx context.Context, requestTimeOut int, net service.Service, protocol string, log log.Log) *MessageNetwork {
	return &MessageNetwork{
		server.NewMsgServer(ctx, net.(server.Service), protocol, time.Duration(requestTimeOut)*time.Second, make(chan service.DirectMessage, p2pconf.Values.BufferSize), log),
		p2ppeers.NewPeers(net, log.WithName("peers")),
		log,
	}
}

// GetRandomPeer returns a random peer from current peer list
func (f MessageNetwork) GetRandomPeer() p2ppeers.Peer {
	peers := f.peersProvider.GetPeers()
	if len(peers) == 0 {
		f.Log.Panic("cannot send fetch - no peers found")
	}
	rand.Seed(time.Now().Unix()) // initialize global pseudo random generator
	return peers[rand.Intn(len(peers))]
}

type network interface {
	GetRandomPeer() p2ppeers.Peer
	SendRequest(ctx context.Context, msgType server.MessageType, payload []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte), failHandler func(err error)) error
	RegisterBytesMsgHandler(msgType server.MessageType, reqHandler func([]byte) []byte)
}

// Fetch is the main struct that contains network peers and logic to batch and dispatch hash fetch requests
type Fetch struct {
	cfg                  Config
	log                  log.Log
	dbs                  map[Hint]database.Database
	activeRequests       map[types.Hash32][]request
	activeBatches        map[types.Hash32]requestBatch
	net                  network
	requestReceiver      chan request
	batchRequestReceiver chan []request
	batchTimeout         *time.Ticker
	stop                 chan struct{}
	activeReqM           sync.RWMutex
	activeBatchM         sync.RWMutex
	stopM                sync.RWMutex
	onlyOnce             sync.Once
	doneChan             chan struct{}
}

// NewFetch creates a new Fetch struct
func NewFetch(ctx context.Context, cfg Config, network service.Service, logger log.Log) *Fetch {
	srv := NewMessageNetwork(ctx, cfg.RequestTimeout, network, fetchProtocol, logger)

	f := &Fetch{
		cfg:             cfg,
		log:             logger,
		dbs:             make(map[Hint]database.Database),
		activeRequests:  make(map[types.Hash32][]request),
		net:             srv,
		requestReceiver: make(chan request),
		batchTimeout:    time.NewTicker(time.Second * time.Duration(cfg.BatchTimeout)),
		stop:            make(chan struct{}),
		activeBatches:   make(map[types.Hash32]requestBatch),
		doneChan:        make(chan struct{}),
	}

	return f
}

// Start starts handling fetch requests
func (f *Fetch) Start() {
	f.onlyOnce.Do(func() {
		f.net.RegisterBytesMsgHandler(fetch, f.FetchRequestHandler)
		go f.loop()
	})
}

// Stop stops handling fetch requests
func (f *Fetch) Stop() {
	f.batchTimeout.Stop()
	close(f.stop)
	// wait for close to end
	<-f.doneChan
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
func (f *Fetch) AddDB(hint Hint, db database.Database) {
	f.dbs[hint] = db
}

// here we receive all requests for hashes for all DBs and batch them together before we send the request to peer
// there can be a priority request that will not be batched
func (f *Fetch) loop() {
	f.log.Info("starting main loop")
	for {
		select {
		case req := <-f.requestReceiver:
			// group requests by hash
			f.activeReqM.Lock()
			f.activeRequests[req.hash] = append(f.activeRequests[req.hash], req)
			rLen := len(f.activeRequests)
			f.activeReqM.Unlock()
			if req.priority > Low {
				f.sendBatch([]requestMessage{{req.hint, req.hash}})
				break
			}
			if rLen > batchMaxSize {
				f.requestHashBatchFromPeers() // Process the batch.
			}
		case <-f.batchTimeout.C:
			f.requestHashBatchFromPeers() // Process the batch.
		case <-f.stop:
			close(f.doneChan)
			return
		}
	}
}

// FetchRequestHandler handles requests for sync from peersProvider, and basically reads data from database and puts it
// in a response batch
func (f *Fetch) FetchRequestHandler(data []byte) []byte {
	if f.stopped() {
		return nil
	}

	var requestBatch requestBatch
	err := types.BytesToInterface(data, requestBatch)
	if err != nil {
		return []byte{}
	}
	resBatch := responseBatch{
		ID:        requestBatch.ID,
		Responses: make([]responseMessage, 0, len(requestBatch.Requests)),
	}
	// this will iterate all requests and populate appropriate Responses, if there are any missing items they will not
	// be included in the response at all
	for _, r := range requestBatch.Requests {
		db, ok := f.dbs[r.Hint]
		if !ok {
			//db not found, don't return response here
			continue
		}
		res, err := db.Get(r.Hash.Bytes())
		if err != nil {
			// no value found
			continue
		}
		// add response to batch
		m := responseMessage{
			Hash: r.Hash,
			data: res,
		}
		resBatch.Responses = append(resBatch.Responses, m)
	}

	bts, err := types.InterfaceToBytes(resBatch)
	if err != nil {
		f.log.Error("cannot parse message")
		return nil
	}
	return bts
}

// receive data from message server and call response handlers accordingly
func (f *Fetch) receiveResponse(data []byte) {
	if f.stopped() {
		return
	}

	var response responseBatch
	err := types.BytesToInterface(data, &response)
	if err != nil {
		f.log.Error("response was unclear, maybe leaking")
		return
	}

	f.activeBatchM.RLock()
	batch, has := f.activeBatches[response.ID]
	f.activeBatchM.RUnlock()
	if !has {
		f.log.Warning("unknown batch response received, or already invalidated %v", response.ID)
		return
	}

	// convert requests to map so it can be invalidated when reading Responses
	batchMap := batch.ToMap()
	// iterate all hash Responses
	for _, resID := range response.Responses {
		//take lock here to make handling of a single hash atomic
		f.activeReqM.Lock()
		// for each hash, send data on waiting channel
		reqs := f.activeRequests[resID.Hash]
		for _, req := range reqs {
			var err error
			if req.validateResponseHash {
				actual := types.CalcHash32(data)
				if actual != resID.Hash {
					err = fmt.Errorf("hash didnt match")
				}

			}
			req.returnChan <- HashDataPromiseResult{
				Err:  err,
				Hash: resID.Hash,
				Data: resID.data,
			}
			//todo: mark peer as malicious
		}
		//remove from map
		delete(batchMap, resID.Hash)

		// remove from active list
		delete(f.activeRequests, resID.Hash)
		f.activeReqM.Unlock()
	}

	//iterate all requests that didn't return value from peer and invalidate them with error
	err = fmt.Errorf("hash did not return")
	for h := range batchMap {
		f.activeReqM.Lock()
		// for each hash, call its callbacks
		resCallbacks := f.activeRequests[h]
		f.activeReqM.Unlock()
		for _, req := range resCallbacks {
			req.returnChan <- HashDataPromiseResult{
				Err:  err,
				Hash: h,
			}
		}
	}

	//delete the hash of waiting batch
	f.activeBatchM.Lock()
	delete(f.activeBatches, response.ID)
	f.activeBatchM.Unlock()
}

// this is the main function that sends the hash request to the peer
func (f *Fetch) requestHashBatchFromPeers() {
	var requestList []requestMessage
	f.activeReqM.RLock()
	for hash, req := range f.activeRequests {
		requestList = append(requestList, requestMessage{Hash: hash, Hint: req[0].hint})
	}
	f.activeReqM.RUnlock()

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
	var batch requestBatch
	batch.Requests = requests
	batch.SetID()

	f.activeBatchM.Lock()
	f.activeBatches[batch.ID] = batch
	f.activeBatchM.Unlock()
	// timeout function will be called if no response was received for the hashes sent
	timeoutFunc := func(err error) {
		f.handleHashError(batch.ID, err)
	}

	bytes, err := types.InterfaceToBytes(batch)
	if err != nil {
		f.handleHashError(batch.ID, err)
	}
	// try sending batch to some random peer
	retries := 0
	for {
		if f.stopped() {
			return
		}
		// get random peer
		p := f.net.GetRandomPeer()
		err := f.net.SendRequest(context.TODO(), fetch, bytes, p, f.receiveResponse, timeoutFunc)
		// if call succeeded, continue to other requests
		if err != nil {
			retries++
			if retries > f.cfg.MaxRetiresForPeer {
				f.handleHashError(batch.ID, ErrCouldNotSend(fmt.Errorf("could not send message")))
				break
			}
			//todo: mark number of fails per peer to make it low priority
			f.log.Warning("could not send message to peer %v, retrying, retries %v", p, retries)
		} else {
			break
		}

	}
}

// handleHashError is called when an error occurred processing batches of the following hashes
func (f *Fetch) handleHashError(batchHash types.Hash32, err error) {
	f.log.Error("cannot fetch message %v", err)
	f.activeBatchM.RLock()
	batch, ok := f.activeBatches[batchHash]
	if !ok {
		f.activeBatchM.RUnlock()
		f.log.Error("batch invalidated twice %v", batchHash)
	}
	f.activeBatchM.RUnlock()
	f.activeReqM.Lock()
	for _, h := range batch.Requests {
		for _, callback := range f.activeRequests[h.Hash] {
			callback.returnChan <- HashDataPromiseResult{
				Err:  err,
				Hash: h.Hash,
				Data: nil,
			}
		}
		delete(f.activeRequests, h.Hash)
	}
	f.activeReqM.Unlock()

	f.activeBatchM.Lock()
	delete(f.activeBatches, batchHash)
	f.activeBatchM.Unlock()
}

// HashDataPromiseResult is the result strict when requesting data corresponding to the Hash
type HashDataPromiseResult struct {
	Err  error
	Hash types.Hash32
	Data []byte
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
// know where to look for the hash, this function returns HashDataPromiseResult channel that will hold data received or error
func (f *Fetch) GetHash(hash types.Hash32, h Hint, validateHash bool) chan HashDataPromiseResult {
	resChan := make(chan HashDataPromiseResult, 1)

	//check if we already have this hash locally
	if _, ok := f.dbs[h]; !ok {
		f.log.Panic("tried to fetch data from DB that doesn't exist locally: %v", h)
	}

	if b, err := f.dbs[h].Get(hash.Bytes()); err == nil {
		resChan <- HashDataPromiseResult{
			Err:  nil,
			Hash: hash,
			Data: b,
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
	}

	f.requestReceiver <- req

	return resChan
}
