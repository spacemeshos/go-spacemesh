package gossip

import (
	"errors"
	lru "github.com/hashicorp/golang-lru"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"golang.org/x/crypto/sha3"
	"io"
	"sync"
)

const messageQBufferSize = 1000
const propagateHandleBufferSize = 1000 // number of MessageValidation that we allow buffering, above this number protocols will get stuck

const ProtocolName = "/p2p/1.0/gossip"
const protocolVer = "0"

type hash [12]byte

// fnv.New32 must be used every time to be sure we get consistent results.
func calcHash(msg []byte, prot string) hash {
	msghash := sha3.NewShake128() // todo: Add nonce to messages instead
	msghash.Write(msg)
	msghash.Write([]byte(prot))
	var h [12]byte
	buf := make([]byte, 16)
	_, _ = io.ReadFull(msghash, buf)
	copy(h[:], buf[0:12])
	return hash(h)
}

type doubleCache struct {
	size   uint
	cacheA map[hash]struct{}
	cacheB map[hash]struct{}
}

func newDoubleCache(size uint) *doubleCache {
	return &doubleCache{size, make(map[hash]struct{}, size), make(map[hash]struct{}, size)}
}

func (a *doubleCache) lookupOrCreate(key hash) bool {
	_, ok := a.cacheA[key]
	if ok {
		return true
	}
	_, ok = a.cacheB[key]
	if ok {
		return true
	}
	a.insert(key)
	return false
}

func (a *doubleCache) insert(key hash) {
	if uint(len(a.cacheA)) < a.size {
		a.cacheA[key] = struct{}{}
		return
	}
	if uint(len(a.cacheB)) < a.size {
		a.cacheB[key] = struct{}{}
		return
	}
	a.cacheB = a.cacheA
	a.cacheA = make(map[hash]struct{}, a.size)
}

// Interface for the underlying p2p layer
type baseNetwork interface {
	SendMessage(peerPubkey p2pcrypto.PublicKey, protocol string, payload []byte) error
	RegisterDirectProtocol(protocol string) chan service.DirectMessage
	RegisterDirectProtocolWithChannel(protocol string, c chan service.DirectMessage) chan service.DirectMessage
	SubscribePeerEvents() (conn chan p2pcrypto.PublicKey, disc chan p2pcrypto.PublicKey)
	ProcessGossipProtocolMessage(sender p2pcrypto.PublicKey, protocol string, data service.Data, validationCompletedChan chan service.MessageValidation) error
}

type protocolMessage struct {
	msg *pb.ProtocolMessage
}

type MsgCache struct {
	*lru.Cache
}

func NewMsgCache(size int) MsgCache {
	cache, err := lru.New(size)
	if err != nil {
		log.Warning("could not initialize cache ", err)
	}
	return MsgCache{Cache: cache}
}

func (bc *MsgCache) Put(id hash) {
	bc.Cache.Add(id, struct{}{})
}

func (bc MsgCache) Get(id hash) bool {
	_, found := bc.Cache.Get(id)
	return found
}

// Protocol is the gossip protocol
type Protocol struct {
	log.Log

	config          config.SwarmConfig
	net             baseNetwork
	localNodePubkey p2pcrypto.PublicKey

	peers      map[string]*peer
	peersMutex sync.RWMutex

	shutdown chan struct{}

	oldMessageQ  *doubleCache
	oldMessageMu sync.RWMutex

	messageQ   chan protocolMessage
	propagateQ chan service.MessageValidation
}

// NewProtocol creates a new gossip protocol instance. Call Start to start reading peers
func NewProtocol(config config.SwarmConfig, base baseNetwork, localNodePubkey p2pcrypto.PublicKey, log2 log.Log) *Protocol {
	// intentionally not subscribing to peers events so that the channels won't block in case executing Start delays
	return &Protocol{
		Log:             log2,
		config:          config,
		net:             base,
		localNodePubkey: localNodePubkey,
		peers:           make(map[string]*peer),
		shutdown:        make(chan struct{}),
		oldMessageQ:     newDoubleCache(100000), // todo : remember to drain this
		messageQ:        make(chan protocolMessage, messageQBufferSize),
		propagateQ:      make(chan service.MessageValidation, propagateHandleBufferSize),
	}
}

// sender is an interface for peer's p2p layer
type sender interface {
	SendMessage(peerPubkey p2pcrypto.PublicKey, protocol string, payload []byte) error
}

// peer is a struct storing peer's state
type peer struct {
	log.Log
	pubkey p2pcrypto.PublicKey
	net    sender
}

func newPeer(net sender, pubkey p2pcrypto.PublicKey, log log.Log) *peer {
	return &peer{
		log,
		pubkey,
		net,
	}
}

func (prot *Protocol) Close() {
	close(prot.shutdown)
}

// markMessageAsOld adds the message's hash to the old messages queue so that the message won't be processed in case received again.
// Returns true if message was already processed before
func (prot *Protocol) markMessageAsOld(h hash) bool {
	prot.oldMessageMu.Lock()
	ok := prot.oldMessageQ.lookupOrCreate(h)
	//if _, ok = prot.oldMessageQ[h]; !ok {
	//	prot.oldMessageQ[h] = struct{}{}
	//	prot.Log.Debug("marking message as old, hash %v", h)
	//} else {
	//	prot.Log.Debug("message is already old, hash %v", h)
	//}
	prot.oldMessageMu.Unlock()
	return ok
}

func (prot *Protocol) propagateMessage(payload []byte, h hash, nextProt string, exclude p2pcrypto.PublicKey) {
	prot.peersMutex.RLock()
peerLoop:
	for p := range prot.peers {
		if exclude.String() == p {
			continue peerLoop
		}
		go func(pubkey p2pcrypto.PublicKey) {
			// TODO: replace peer ?
			err := prot.net.SendMessage(pubkey, nextProt, payload)
			if err != nil {
				prot.Warning("Failed sending msg %v to %v, reason=%v", h, pubkey, err)
			}
		}(prot.peers[p].pubkey)
	}
	prot.peersMutex.RUnlock()
}

// Broadcast is the actual broadcast procedure - process the message internally and loop on peers and add the message to their queues
func (prot *Protocol) Broadcast(payload []byte, nextProt string) error {
	prot.Log.Debug("Broadcasting message from type %s", nextProt)
	return prot.processMessage(prot.localNodePubkey, nextProt, service.DataBytes{Payload: payload})
	//todo: should this ever return error ? then when processMessage should return error ?. should it block?
}

// Start a loop that process peers events
func (prot *Protocol) Start() {
	peerConn, peerDisc := prot.net.SubscribePeerEvents() // this was start blocks until we registered.
	go prot.eventLoop(peerConn, peerDisc)
	go prot.propagationEventLoop() // TODO consider running several consumers
}

func (prot *Protocol) addPeer(peer p2pcrypto.PublicKey) {
	prot.peersMutex.Lock()
	prot.peers[peer.String()] = newPeer(prot.net, peer, prot.Log)
	prot.peersMutex.Unlock()
}

func (prot *Protocol) removePeer(peer p2pcrypto.PublicKey) {
	prot.peersMutex.Lock()
	delete(prot.peers, peer.String())
	prot.peersMutex.Unlock()
}

func (prot *Protocol) processMessage(sender p2pcrypto.PublicKey, protocol string, msg service.Data) error {
	h := calcHash(msg.Bytes(), protocol)

	isOld := prot.markMessageAsOld(h)
	if isOld {
		// todo: PROMETHEUS
		//metrics.OldGossipMessages.With(metrics.ProtocolLabel, protocol).Add(1)
		// todo : - have some more metrics for termination
		// todo	: - maybe tell the peer we got this message already?
		// todo : - maybe block this peer since he sends us old messages
		prot.Log.With().Debug("old_gossip_message", log.String("from", sender.String()), log.String("protocol", protocol), log.String("hash", common.Bytes2Hex(h[:])))
		return nil
	}

	prot.Log.With().EventInfo("new_gossip_message", log.String("from", sender.String()), log.String("protocol", protocol), log.String("hash", common.Bytes2Hex(h[:])))
	// todo: PROMETHEUS
	//metrics.NewGossipMessages.With("protocol", protocol).Add(1)
	return prot.net.ProcessGossipProtocolMessage(sender, protocol, msg, prot.propagateQ)
}

func (prot *Protocol) propagationEventLoop() {
	var err error
loop:
	for {
		select {
		case msgV := <-prot.propagateQ:
			h := calcHash(msgV.Message(), msgV.Protocol())
			prot.Log.With().EventDebug("new_gossip_message_relay", log.String("protocol", msgV.Protocol()), log.String("hash", common.BytesToHash(h[:]).ShortString()))
			go prot.propagateMessage(msgV.Message(), calcHash(msgV.Message(), msgV.Protocol()), msgV.Protocol(), msgV.Sender())
		case <-prot.shutdown:
			err = errors.New("protocol shutdown")
			break loop
		}
	}
	prot.Error("propagate event loop stopped. err: %v", err)
}

func (prot *Protocol) Relay(sender p2pcrypto.PublicKey, protocol string, msg service.Data) error {
	return prot.processMessage(sender, protocol, msg)
}

func (prot *Protocol) eventLoop(peerConn chan p2pcrypto.PublicKey, peerDisc chan p2pcrypto.PublicKey) {
	// TODO: replace with p2p.Peers
	var err error
loop:
	for {
		select {
		case peer := <-peerConn:
			go prot.addPeer(peer)
		case peer := <-peerDisc:
			go prot.removePeer(peer)
		case <-prot.shutdown:
			err = errors.New("protocol shutdown")
			break loop
		}
	}
	prot.Error("Gossip protocol event loop stopped. err: %v", err)
}

// peersCount returns the number of peers known to the protocol, used for testing only
func (prot *Protocol) peersCount() int {
	prot.peersMutex.RLock()
	cnt := len(prot.peers)
	prot.peersMutex.RUnlock()
	return cnt
}

// hasPeer returns whether or not a peer is known to the protocol, used for testing only
func (prot *Protocol) hasPeer(key p2pcrypto.PublicKey) bool {
	prot.peersMutex.RLock()
	_, ok := prot.peers[key.String()]
	prot.peersMutex.RUnlock()
	return ok
}
