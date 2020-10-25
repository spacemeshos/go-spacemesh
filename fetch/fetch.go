// Package fetch contains mechanism to fetch data from remote peersProvider
package fetch

import (
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

type (
	// DataCallbackHandler is the callback that will be called when `hash` data is received, hash of the bytes must match
	// hash
	DataCallbackHandler func(hash types.Hash32, buf []byte) error
)

// Hint marks which DB should be queried for a certain provided hash
type Hint string

// priority defines whether data will be fetched at once or batched and waited for up to "batchTimeout"
// until fetched
type priority uint16

// Message priority constants
const (
	// Low will perform batched calls
	Low = 0
	// High will call fetch immediately
	High = 1
)

const (
	fetch         server.MessageType = 1
	fetchProtocol                    = "/sync/2.0/"

	batchMaxSize = 20
)

// ErrCouldNotSend is a special type of error indicating fetch could not be done because message could not be sent to peersProvider
type ErrCouldNotSend error

var onlyOnce sync.Once

// Fetcher is the general interface of the fetching unit, capable of requesting bytes that corresponds to a hash
// from other remote peersProvider.
type Fetcher interface {
	GetHash(hash types.Hash32, h Hint, dataCallback DataCallbackHandler, validateAndSubmit bool) chan HashDataPromiseResult
	GetAllHashes(hash []types.Hash32, hint Hint, hashValidationFunction DataCallbackHandler, validateAndSubmit bool) error
}

/// request contains all relevant data for a single request for a specified hash
type request struct {
	successCallback      DataCallbackHandler        // successCallback is the callback called if data was retrieved successfully
	hash                 types.Hash32               // hash is the hash of the data requested
	priority             priority                   // priority is for QoS
	validateResponseHash bool                       // if true perform hash validation on received data
	hint                 Hint                       // the hint from which database to fetch this hash
	returnChan           chan HashDataPromiseResult //channel that will signal if the call succeeded or not
	writeToDB            bool                       // writeToDB puts the returned data in DB
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

// requestBatch is a batch of Requests and a hash of all Requests as ID
type requestBatch struct {
	ID       types.Hash32
	Requests []requestMessage
}

// SetID calculates the hash of all Requests and sets it as this batches ID
func (b *requestBatch) SetID() {
	bts, err := types.InterfaceToBytes(b.Requests)
	if err != nil {
		return
	}
	b.ID = types.CalcHash32(bts)
}

//ToMap converts the array of Requests to map so it can be easily invalidated
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
func NewMessageNetwork(requestTimeOut int, net service.Service, protocol string, log log.Log) *MessageNetwork {
	return &MessageNetwork{
		server.NewMsgServer(net.(server.Service), protocol, time.Duration(requestTimeOut)*time.Second, make(chan service.DirectMessage, p2pconf.Values.BufferSize), log),
		p2ppeers.NewPeers(net, log.WithName("peersProvider")),
		log,
	}
}

// GetRandomPeer returns a random peer from current peer list
func (f MessageNetwork) GetRandomPeer() p2ppeers.Peer {
	peers := f.peersProvider.GetPeers()
	if len(peers) == 0 {
		f.Log.Panic("cannot send fetch - no peersProvider found")
	}
	rand.Seed(time.Now().Unix()) // initialize global pseudo random generator
	return peers[rand.Intn(len(peers))]
}

type network interface {
	GetRandomPeer() p2ppeers.Peer
	SendRequest(msgType server.MessageType, payload []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte), failHandler func(err error)) error
}

// Fetch is the main struct that contains network peersProvider and logic to batch and dispatch hash fetch Requests
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
	stopped              bool
}

// NewFetch creates a new Fetch struct
func NewFetch(cfg Config, network service.Service, logger log.Log) *Fetch {

	srv := NewMessageNetwork(cfg.RequestTimeout, network, fetchProtocol, logger)

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
	}

	srv.RegisterBytesMsgHandler(fetch, f.FetchRequestHandler)

	return f
}

// Start handling fetch Requests
func (f *Fetch) Start() {
	onlyOnce.Do(func() { go f.loop() })
}

// Stop handling fetch Requests
func (f *Fetch) Stop() {
	f.batchTimeout.Stop()
	f.setStopped(true)
	f.stop <- struct{}{}
}

// getStopped returns if we should stop
func (f *Fetch) getStopped() bool {
	f.stopM.RLock()
	defer f.stopM.RUnlock()
	return f.stopped
}

// getStopped returns if we should stop
func (f *Fetch) setStopped(stop bool) {
	f.stopM.Lock()
	defer f.stopM.Unlock()
	f.stopped = stop
}

// AddDB adds a DB with corresponding hint
// all network peersProvider will be able to query this DB
func (f *Fetch) AddDB(hint Hint, db database.Database) {
	f.dbs[hint] = db
}

// here we receive all Requests for hashes for all DBs and batch them together before we send the request to peer
// there can be a priority request that will not be batched
func (f *Fetch) loop() {
	for {
		select {
		case req := <-f.requestReceiver:
			// group Requests by hash
			f.activeReqM.Lock()
			f.activeRequests[req.hash] = append(f.activeRequests[req.hash], req)
			rLen := len(f.activeRequests)
			f.activeReqM.Unlock()
			if req.priority > Low {
				f.sendBatch([]types.Hash32{req.hash})
				break
			}
			if rLen > batchMaxSize {
				f.requestHashBatchFromPeers() // Process the batch.
			}
		case <-f.batchTimeout.C:
			f.requestHashBatchFromPeers() // Process the batch.
		case batchedRequest := <-f.batchRequestReceiver: // special case to when a certain batch is needed at once
			allHashes := make([]types.Hash32, 0, len(batchedRequest))
			for _, req := range batchedRequest {
				f.activeReqM.Lock()
				f.activeRequests[req.hash] = append(f.activeRequests[req.hash], batchedRequest...)
				f.activeReqM.Unlock()
				allHashes = append(allHashes, req.hash)
			}
			f.sendBatch(allHashes)
			break

		case <-f.stop:
			return
		}
	}
}

// FetchRequestHandler handles Requests for sync from peersProvider, and basically reads data from database and puts it
// in a response batch
func (f *Fetch) FetchRequestHandler(data []byte) []byte {
	if f.getStopped() {
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
	// this will iterate all Requests and populate appropriate Responses, if there are any missing items they will not
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
	if f.getStopped() {
		return
	}

	var response responseBatch
	err := types.BytesToInterface(data, &response)
	if err != nil {
		f.log.Error("we should panic here, response was unclear, probably leaking")
		return
	}

	f.activeBatchM.RLock()
	batch, has := f.activeBatches[response.ID]
	f.activeBatchM.RUnlock()
	if !has {
		f.log.Warning("unknown batch response received, or already invalidated %v", response.ID)
		return
	}

	// convert Requests to map so it can be invalidated when reading Responses
	batchMap := batch.ToMap()
	// iterate all hash Responses
	for _, resID := range response.Responses {
		//take lock here to make handling of a single hash atomic
		f.activeReqM.Lock()
		// for each hash, call its callbacks
		reqs := f.activeRequests[resID.Hash]
		for _, req := range reqs {
			var err error
			if req.validateResponseHash {
				actual := types.CalcHash32(data)
				if actual != resID.Hash {
					err = fmt.Errorf("hash didnt match")
				}

			}
			if err != nil {
				req.returnChan <- HashDataPromiseResult{
					Err:  err,
					Hash: resID.Hash,
				}
				continue
			}
			err = req.successCallback(resID.Hash, resID.data)
			if err == nil && req.writeToDB {
				err = f.dbs[req.hint].Put(resID.Hash.Bytes(), data)
			}
			req.returnChan <- HashDataPromiseResult{
				Err:  err,
				Hash: resID.Hash,
			}
			//todo: mark peer as malicious
		}
		//remove from map
		delete(batchMap, resID.Hash)

		// remove from active list
		delete(f.activeRequests, resID.Hash)
		f.activeReqM.Unlock()
	}

	//iterate all Requests that didn't return value from peer and invalidate them with error
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
	var requestList []types.Hash32
	f.activeReqM.RLock()
	for hash := range f.activeRequests {
		requestList = append(requestList, hash)
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

func (f *Fetch) sendBatch(hashes []types.Hash32) { // create a network batch and serialise to bytes
	// build list of batch messages
	var batch requestBatch
	f.activeReqM.RLock()
	for _, hash := range hashes {
		req, ok := f.activeRequests[hash]
		if !ok {
			f.log.Warning("message invalidated before sent in batch %v", hash)
			continue
		}
		r := requestMessage{Hint: req[0].hint, Hash: hash}
		batch.Requests = append(batch.Requests, r)
	}
	f.activeReqM.RUnlock()
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
		if f.getStopped() {
			return
		}
		// get random peer
		p := f.net.GetRandomPeer()
		err := f.net.SendRequest(fetch, bytes, p, f.receiveResponse, timeoutFunc)
		// if call succeeded, continue to other Requests
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
}

// GetAllHashes gets a list of hashes and validates them with the dataCallback function
// it will return error if any of the hashes were not received.
func (f *Fetch) GetAllHashes(hashes []types.Hash32, hint Hint, hashValidationFunction DataCallbackHandler, validateAndSubmit bool) error {
	hashWaiting := make([]chan HashDataPromiseResult, len(hashes))

	for _, id := range hashes {
		callBackChan := f.GetHash(id, hint, hashValidationFunction, validateAndSubmit)
		hashWaiting = append(hashWaiting, callBackChan)
	}

	// wait for all fetch Requests to end
	var retErr error
	for _, hashRes := range hashWaiting {
		res := <-hashRes
		if res.Err != nil {
			f.log.Error("error fetching hash $v err %v", res.Hash, res.Err)
			retErr = res.Err
		}
	}

	return retErr
}

// GetHash is the regular buffered call to get a specific hash, using provided hash, h as hint the receiving end will
// know where to look for the hash, if function fails in any way- result will be sent to HashDataPromiseResult, if a response was
// received dataCallback will be called. caller can choose whether to validate that the received bytes match the has by setting
// validateHash to true
func (f *Fetch) GetHash(hash types.Hash32, h Hint, dataCallback DataCallbackHandler, validateHash bool) chan HashDataPromiseResult {
	callbackChan := make(chan HashDataPromiseResult, 1)

	//check if we already have this hash locally
	if _, ok := f.dbs[h]; !ok {
		f.log.Panic("tried to fetch data from DB that doesn't exist locally: %v", h)
	}

	if _, err := f.dbs[h].Get(hash.Bytes()); err == nil {
		callbackChan <- HashDataPromiseResult{
			Err:  nil,
			Hash: hash,
		}
		return callbackChan
	}

	// if not present in db, call fetching of the item
	req := request{
		dataCallback,
		hash,
		Low,
		validateHash,
		h,
		callbackChan,
		false,
	}

	f.requestReceiver <- req

	return callbackChan
}
