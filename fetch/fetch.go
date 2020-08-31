// Package fetch contains mechanism to fetch data from remote peers
package fetch

import (
	"fmt"
	"github.com/btcsuite/btcd/database"
	"github.com/spacemeshos/go-spacemesh/common/types"
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
type HandleDataCallback func(hash types.Hash32, buf []byte)

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
)

// ErrCouldNotSend is a special type of error indicating fetch could not be done because message could not be sent to peers
type ErrCouldNotSend error

// Fetcher is the general interface of the fetching unit, capable of requesting bytes that corresponds to a hash
// from other remote peers.
type Fetcher interface {
	GetHash(hash types.Hash32, h Hint, dataCallback HandleDataCallback, failCallback HandleFailCallback)
}

type network interface {
	GetPeers() []p2ppeers.Peer
	SendRequest(msgType server.MessageType, payload []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte), errHandler func(err error)) error
}

/// request contains all relevant data for a single request for a specified hash
type request struct {
	success          HandleDataCallback
	fail             HandleFailCallback
	hash             types.Hash32
	priority         priority
	validateResponse bool
	hint             Hint
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
	dbs             map[Hint]database.DB
	activeRequests  map[types.Hash32][]request
	net             network
	requestReceiver chan request
	batchTimeout    *time.Ticker
	stop            chan struct{}
	activeReqM      sync.RWMutex
}

// NewFetch creates a new FEtch struct
func NewFetch(cfg Config, net network, logger log.Log) *Fetch {
	return &Fetch{
		cfg:             cfg,
		log:             logger,
		dbs:             make(map[Hint]database.DB),
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
		case <-f.batchTimeout.C:
			f.requestHashBatchFromPeers() // Process the batch.
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
				req.success(resID.Hash, data)
				continue
			}

			// check hash flow, if hash didn't match still call fail function
			actual := types.CalcHash32(data)
			if actual == resID.Hash {
				req.success(resID.Hash, data)
				continue
			}
			//todo: mark peer as malicious
			req.fail(resID.Hash, fmt.Errorf("hash didnt match"))
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
			callback.fail(h, err)
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

// GetHash is the regular buffered call to get a specific hash, using provided hash, h as hint the receiving end will
// know where to loog for the hash, if function fails to get a response - failCallback will be called, if a response was
// received dataCallback will be called. caller can choose whether to validate that the received bytes mtach the has hby setting
// validateHash to true
func (f *Fetch) GetHash(hash types.Hash32, h Hint, dataCallback HandleDataCallback, failCallback HandleFailCallback, validateHash bool) {
	req := request{
		dataCallback,
		failCallback,
		hash,
		Low,
		validateHash,
		h,
	}

	f.requestReceiver <- req
}
