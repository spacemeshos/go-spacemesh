package gossip

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"sync"
)

const oldMessageCacheSize = 10000
const propagateHandleBufferSize = 1000 // number of MessageValidation that we allow buffering, above this number protocols will get stuck

const ProtocolName = "/p2p/1.0/gossip"
const protocolVer = "0"

// Interface for the underlying p2p layer
type baseNetwork interface {
	SendMessage(peerPubkey p2pcrypto.PublicKey, protocol string, payload []byte) error
	SubscribePeerEvents() (conn chan p2pcrypto.PublicKey, disc chan p2pcrypto.PublicKey)
	ProcessGossipProtocolMessage(sender p2pcrypto.PublicKey, protocol string, data service.Data, validationCompletedChan chan service.MessageValidation) error
}

// Protocol is the gossip protocol
type Protocol struct {
	log.Log

	config          config.SwarmConfig
	net             baseNetwork
	localNodePubkey p2pcrypto.PublicKey

	peers      map[p2pcrypto.PublicKey]*peer
	peersMutex sync.RWMutex

	shutdown chan struct{}

	oldMessageQ *types.DoubleCache

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
		peers:           make(map[p2pcrypto.PublicKey]*peer),
		shutdown:        make(chan struct{}),
		oldMessageQ:     types.NewDoubleCache(oldMessageCacheSize), // todo : remember to drain this
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
func (prot *Protocol) markMessageAsOld(h types.Hash12) bool {
	ok := prot.oldMessageQ.GetOrInsert(h)
	return ok
}

func (prot *Protocol) propagateMessage(payload []byte, h types.Hash12, nextProt string, exclude p2pcrypto.PublicKey) {
	//TODO soon : don't wait for mesaage to send and if we finished sending last message one of the peers send the next message to him.
	// limit the number of simultaneous sends. *consider other messages (mainly sync)
	prot.peersMutex.RLock()
	peers := make([]p2pcrypto.PublicKey, 0, len(prot.peers))
	for p := range prot.peers {
		peers = append(peers, p)
	}
	prot.peersMutex.RUnlock()
	var wg sync.WaitGroup
peerLoop:
	for _, p := range peers {
		if exclude == p {
			continue peerLoop
		}
		wg.Add(1)
		go func(pubkey p2pcrypto.PublicKey) {
			// TODO: replace peer ?
			err := prot.net.SendMessage(pubkey, nextProt, payload)
			if err != nil {
				prot.Warning("Failed sending %v msg %v to %v, reason=%v", nextProt, h, p, err)
			}
			wg.Done()
		}(p)
	}
	wg.Wait()
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
	prot.peers[peer] = newPeer(prot.net, peer, prot.Log)
	prot.Log.With().Info("adding peer", log.String("peer", peer.String()))
	prot.peersMutex.Unlock()
}

func (prot *Protocol) removePeer(peer p2pcrypto.PublicKey) {
	prot.peersMutex.Lock()
	delete(prot.peers, peer)
	prot.Log.With().Info("deleting peer", log.String("peer", peer.String()))
	prot.peersMutex.Unlock()
}

func (prot *Protocol) processMessage(sender p2pcrypto.PublicKey, protocol string, msg service.Data) error {
	h := types.CalcMessageHash12(msg.Bytes(), protocol)

	isOld := prot.markMessageAsOld(h)
	if isOld {
		metrics.OldGossipMessages.With(metrics.ProtocolLabel, protocol).Add(1)
		// todo : - have some more metrics for termination
		// todo	: - maybe tell the peer we got this message already?
		// todo : - maybe block this peer since he sends us old messages
		prot.Log.With().Debug("old_gossip_message", log.String("from", sender.String()), log.String("protocol", protocol), log.String("hash", util.Bytes2Hex(h[:])))
		return nil
	}

	prot.Log.Event().Debug("new_gossip_message", log.String("from", sender.String()), log.String("protocol", protocol), log.String("hash", util.Bytes2Hex(h[:])))
	metrics.NewGossipMessages.With("protocol", protocol).Add(1)
	return prot.net.ProcessGossipProtocolMessage(sender, protocol, msg, prot.propagateQ)
}

func (prot *Protocol) propagationEventLoop() {
	var err error
loop:
	for {
		metrics.PropagationQueueLen.Set(float64(len(prot.propagateQ)))
		select {
		case msgV := <-prot.propagateQ:
			h := types.CalcMessageHash12(msgV.Message(), msgV.Protocol())
			prot.Log.With().Debug("new_gossip_message_relay", log.String("protocol", msgV.Protocol()), log.String("hash", util.Bytes2Hex(h[:])))
			prot.propagateMessage(msgV.Message(), h, msgV.Protocol(), msgV.Sender())
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
	_, ok := prot.peers[key]
	prot.peersMutex.RUnlock()
	return ok
}
