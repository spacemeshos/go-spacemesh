// Package fetch contains mechanism to fetch data from remote peers
package fetch

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	p2ppeers "github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/rand"
	"sync"
	"time"
)

// HandleDataCallback is the callback that will be called when `hash` data is received, hash of the bytes must match
// hash
type HandleDataCallback func(hash types.Hash32, buf []byte) error

// HandleFailCallback is the callback that will be called when a fetch fails
type HandleFailCallback func(hash types.Hash32, err error)

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
	fetch         server.MessageType = 8
	fetchProtocol                    = "/sync/2.0/"

	batchMaxSize = 20
)

// ErrCouldNotSend is a special type of error indicating fetch could not be done because message could not be sent to peers
type ErrCouldNotSend error

// Fetcher is the general interface of the fetching unit, capable of requesting bytes that corresponds to a hash
// from other remote peers.
type Fetcher interface {
	GetHash(hash types.Hash32, h Hint, dataCallback HandleDataCallback, validateAndSubmit bool) chan HashDataPromiseResult
	GetAllHashes(hash []types.Hash32, hint Hint, hashValidationFunction HandleDataCallback, validateAndSubmit bool) error
}

type network interface {
	GetPeers() []p2ppeers.Peer
	SendRequest(msgType server.MessageType, payload []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte), errHandler func(err error)) error
}

/// request contains all relevant data for a single request for a specified hash
type request struct {
	success          HandleDataCallback
	hash             types.Hash32
	priority         priority
	validateResponse bool
	hint             Hint
	returnChan chan HashDataPromiseResult
}

// requestMessage is the on the wire message that will be send to the peer for hash query
type requestMessage struct {
	Hint Hint
	Hash types.Hash32
}

// responseMessage is the on the wire message that will be send to the this node as response
type responseMessage struct {
	Hash types.Hash32
	data []byte
}

// Config is the configuration file of the Fetch component
type Config struct {
	BatchTimeout      int
	MaxRetiresForPeer int
	BatchSize         int
}

// Fetch is the main struct that contains network peers and logic to batch and dispatch hash fetch requests
type Fetch struct {
	cfg             Config
	log             log.Log
	dbs             map[Hint]database.Database
	activeRequests  map[types.Hash32][]request
	net             network
	requestReceiver chan request
	batchRequestReceiver chan []request
	batchTimeout    *time.Ticker
	stop            chan struct{}
	activeReqM      sync.RWMutex
}

// NewFetch creates a new FEtch struct
func NewFetch(cfg Config, net network, logger log.Log) *Fetch {
	return &Fetch{
		cfg:             cfg,
		log:             logger,
		dbs:             make(map[Hint]database.Database),
		activeRequests:  make(map[types.Hash32][]request),
		net:             net,
		requestReceiver: make(chan request),
		batchTimeout:    time.NewTicker(time.Second * time.Duration(cfg.BatchTimeout)),
		stop:            make(chan struct{}),
	}
}

// Start handling fetch requests
func (f *Fetch) Start() {
	go f.loop()
}

// Stop handling fetch requests
func (f *Fetch) Stop() {
	f.batchTimeout.Stop()
	f.stop <- struct{}{}
}

// here we receive all requests for hashes for all DBs and batch them together before we send the request to peer
// there can be a priority request that will not be batched
func (f *Fetch) loop() {
	for {
		select {
		case req := <-f.requestReceiver:
			// group requests by hash
			f.activeReqM.Lock()
			f.activeRequests[req.hash] = append(f.activeRequests[req.hash], req)
			f.activeReqM.Unlock()
			if req.priority > Low {
				f.sendBatch([]types.Hash32{req.hash})
				break
			}
			if len(f.activeRequests) > batchMaxSize {
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

func (f *Fetch) receiveResponse(data []byte) {
	var responses []responseMessage
	err := types.BytesToInterface(data, &responses)
	if err != nil {
		log.Error("we shold panic here, response was unclear, probably leaking")
	}

	// iterate all hash responses
	for _, resID := range responses {
		//take lock here to make handling of a single hash atomic
		f.activeReqM.Lock()
		// for each hash, call its callbacks
		resCallbacks := f.activeRequests[resID.Hash]
		for _, req := range resCallbacks {
			if req.validateResponse == false {
				err := req.success(resID.Hash, data)
				req.returnChan <- HashDataPromiseResult{
					Err:  err,
					Hash: resID.Hash,
				}
				continue
			}

			// check hash flow, if hash didn't match still call fail function
			actual := types.CalcHash32(data)
			if actual == resID.Hash {
				err := req.success(resID.Hash, data)
				// if data is validates - put the returned value into the db.
				if err == nil {
					err = f.dbs[req.hint].Put(resID.Hash.Bytes(), data)
				}
				req.returnChan <- HashDataPromiseResult{
					Err:  err,
					Hash: resID.Hash,
				}
				continue
			}
			//todo: mark peer as malicious
			err := fmt.Errorf("hash didnt match")
			req.returnChan <- HashDataPromiseResult{
				Err:  err,
				Hash: resID.Hash,
			}
		}
		// remove from active list
		delete(f.activeRequests, resID.Hash)
		f.activeReqM.Unlock()
	}
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

func (f *Fetch) sendBatch(hashes []types.Hash32) { // create a network request and serialise to bytes
	// build list of request messages
	var requests []requestMessage
	f.activeReqM.RLock()
	for _, hash := range hashes {
		req, ok := f.activeRequests[hash]
		if !ok {
			f.log.Error("message invalidated before sent in batch %v", hash)
			continue
		}
		r := requestMessage{Hint: req[0].hint, Hash: hash}
		requests = append(requests, r)
	}
	f.activeReqM.RUnlock()

	// timeout function will be called if no response was received for the hashes sent
	timeoutFunc := func(err error) {
		f.handleHashError(hashes, err)
	}

	bytes, err := types.InterfaceToBytes(requests)
	if err != nil {
		f.handleHashError(hashes, err)
	}
	// try sending request to some random peer
	retries := 0
	for {
		// get random peer
		p := f.getPeer()
		err := f.net.SendRequest(fetch, bytes, p, f.receiveResponse, timeoutFunc)
		// if call succeeded, continue to other requests
		if err == nil {
			break
		}
		//todo: mark number of fails per peer to make it low priority
		retries++
		if retries > f.cfg.MaxRetiresForPeer {
			f.handleHashError(hashes, ErrCouldNotSend(fmt.Errorf("could not send message")))
			break
		}
		f.log.Error("could not send message to peer %v , retrying, retries : %v", p, retries)
	}
}

// handleHashError is called when an error occurred processing batches of the following hashes
func (f *Fetch) handleHashError(hashes []types.Hash32, err error) {
	f.log.Error("cannot send fetch message %v", err)
	f.activeReqM.Lock()
	for _, h := range hashes {
		for _, callback := range f.activeRequests[h] {
			callback.returnChan <- HashDataPromiseResult{
				Err:  err,
				Hash: h,
			}
		}
		delete(f.activeRequests, h)
	}
	f.activeReqM.Unlock()
}

// getPeer returns a random peer from current peer list
func (f *Fetch) getPeer() p2ppeers.Peer {
	peers := f.net.GetPeers()
	if len(peers) == 0 {
		f.log.Panic("cannot send fetch - no peers found")
	}
	rand.Seed(time.Now().Unix()) // initialize global pseudo random generator
	return peers[rand.Intn(len(peers))]
}


type HashDataPromiseResult struct {
	Err  error
	Hash types.Hash32
}

// GetAllHashes gets a list of hashes and validates them with the dataCallback function
// it will return error if any of the hashes were not received.
func (f *Fetch) GetAllHashes(hashes []types.Hash32, hint Hint, hashValidationFunction HandleDataCallback, validateAndSubmit bool) error {
	hashWaiting := make([]chan HashDataPromiseResult, len(hashes))

	for _, id := range hashes {
		callBackChan := f.GetHash(id, hint, hashValidationFunction, validateAndSubmit)
		hashWaiting = append(hashWaiting, callBackChan)
	}

	// wait for all block fetch requests to end
	var retErr error = nil
	for _, hashRes := range hashWaiting {
		res := <-hashRes
		if res.Err != nil {
			f.log.Error("%v hash $v", res.Err, res.Hash)
			retErr = res.Err
		}
	}

	return retErr
}


// GetHash is the regular buffered call to get a specific hash, using provided hash, h as hint the receiving end will
// know where to look for the hash, if function fails to get a response - failCallback will be called, if a response was
// received dataCallback will be called. caller can choose whether to validate that the received bytes match the has by setting
// validateHash to true
func (f *Fetch) GetHash(hash types.Hash32, h Hint, dataCallback HandleDataCallback, validateAndSubmit bool) chan HashDataPromiseResult {
	callbackChan := make(chan HashDataPromiseResult, 1)

	//check if we already have this hash locally
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
		validateAndSubmit,
		h,
		callbackChan,
	}

	f.requestReceiver <- req

	return callbackChan
}


