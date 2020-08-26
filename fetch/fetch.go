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
	Low  = 0
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
	SendRequest(msgType server.MessageType, payload []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte)) error
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

// Config is the configuration file of the Fetch component
type Config struct {
	BatchTimeout      int
	MaxRetiresForPeer int
}

// Fetch is the main struct that contains network peers and logic to batch and dispatch hash fetch requests
type Fetch struct {
	cfg             Config
	log             log.Log
	dbs             map[Hint]database.DB
	requests        map[Hint][]request
	net             network
	requestReceiver chan request
	batchTimeout    *time.Ticker
	stop            chan struct{}
}

// NewFetch creates a new FEtch struct
func NewFetch(cfg Config, net network, logger log.Log) *Fetch {
	return &Fetch{
		cfg:             cfg,
		dbs:             make(map[Hint]database.DB),
		requests:        make(map[Hint][]request),
		net:             net,
		requestReceiver: make(chan request),
		batchTimeout:    time.NewTicker(time.Second * time.Duration(cfg.BatchTimeout)),
		stop:            make(chan struct{}),
		log:             logger,
	}
}

// Start handling fetch requests
func (f *Fetch) Start() {
	go f.loop()
}

// Stop handling fetch requests
func (f *Fetch) Stop() {
	f.stop <- struct{}{}
}

// here we receive all requests for hashes for all DBs and batch them together before we send the request to peer
// there can be a priority request that will not be batched
func (f *Fetch) loop() {
	for {
		select {
		case req := <-f.requestReceiver:
			if req.priority > Low {
				f.requestHashFromPeers(req.hint, []request{req})
				break
			}
			f.requests[req.hint] = append(f.requests[req.hint], req)
		case <-f.batchTimeout.C:
			//todo: limit requests per peer
			for key, list := range f.requests {
				if len(list) > 0 {
					// todo: make pooled
					f.requestHashFromPeers(key, list)
				}
			}
			f.requests = make(map[Hint][]request)
		case <-f.stop:
			return
		}

	}
}

// this is the main function that sends the hash request to the peer
func (f *Fetch) requestHashFromPeers(h Hint, requests []request) {
	// group requests by hash
	byHash := make(map[types.Hash32][]request)
	for _, req := range requests {
		byHash[req.hash] = append(byHash[req.hash], req)
	}

	//create a handler function for each hash, call each registered handler when receiving data
	for hash, requests := range byHash {
		receiverFunc := func(data []byte) {
			for _, req := range requests {
				if req.validateResponse == false {
					req.success(hash, data)
					continue
				}

				// check hash flow, if hash didn't match still call fail function
				actual := types.CalcHash32(data)
				if actual == hash {
					req.success(hash, data)
					continue
				}
				//todo: mark peer as malicious
				req.fail(hash, fmt.Errorf("hash didnt match"))
			}
		}
		// create a network request and serialise to bytes
		r := requestMessage{Hint: h, Hash: hash}
		bytes, err := types.InterfaceToBytes(r)
		if err != nil {
			f.handleHashError(requests, err)
		}
		// try sending request to some random peer
		retries := 0
		for {
			// get random peer
			p := f.getPeer()
			err := f.net.SendRequest(fetch, bytes, p, receiverFunc)
			// if call succeeded, continue to other requests
			if err == nil {
				break
			}
			//todo: mark number of fails per peer to make it low priority
			retries++
			if retries > f.cfg.MaxRetiresForPeer {
				f.handleHashError(requests, ErrCouldNotSend(fmt.Errorf("could not send message")))
				break
			}
			f.log.Error("could not send message to peer %v , retrying, retries : %v", retries)
		}
	}
}

func (f *Fetch) handleHashError(req []request, err error) {
	f.log.Error("cannot send fetch message %v", err)
	for _, r := range req {
		r.fail(r.hash, err)
	}
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
