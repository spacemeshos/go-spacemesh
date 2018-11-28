package gossip

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"hash/fnv"
	"sync"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
)

const messageQBufferSize = 100

const protocolName = "/p2p/1.0/gossip"

// fnv.New32 must be used everytime to be sure we get consistent results.
func checksum(msg []byte) uint32 {
	msghash := fnv.New32() // todo: Add nonce to messages instead
	msghash.Write(msg)
	return msghash.Sum32()
}

// Interface for the underlying p2p layer
type BaseNetwork interface {
	SendMessage(peerPubKey string, protocol string, payload []byte) error
	RegisterProtocol(protocol string) chan service.Message
	SubscribePeerEvents() (conn chan crypto.PublicKey, disc chan crypto.PublicKey)
	ProcessProtocolMessage(sender node.Node, protocol string, payload []byte) error
}

type protocolMessage struct {
	msg     []byte
	author  crypto.PublicKey
	isRelay bool
}

// Protocol is the gossip protocol
type Protocol struct {
	log.Log

	config config.SwarmConfig
	net    BaseNetwork

	peerConn chan crypto.PublicKey
	peerDisc chan crypto.PublicKey
	peers    map[string]*peer
	shutdown chan struct{}

	oldMessageMu sync.RWMutex
	oldMessageQ  map[uint32]struct{}
	peersMutex   sync.RWMutex

	relayQ   chan service.Message
	messageQ chan protocolMessage
}

// NewProtocol creates a new gossip protocol instance. Call Start to start reading peers
func NewProtocol(config config.SwarmConfig, base BaseNetwork, log2 log.Log) *Protocol {
	// intentionally not subscribing to peers events so that the channels won't block in case executing Start delays
	relayChan := base.RegisterProtocol(protocolName)
	return &Protocol{
		Log:         log2,
		config:      config,
		net:         base,
		peerConn:    nil,
		peerDisc:    nil,
		peers:       make(map[string]*peer),
		shutdown:    make(chan struct{}),
		oldMessageQ: make(map[uint32]struct{}), // todo : remember to drain this
		peersMutex:  sync.RWMutex{},
		relayQ:      relayChan,
		messageQ:    make(chan protocolMessage, messageQBufferSize),
	}
}

// sender is an interface for peer's p2p layer
type sender interface {
	SendMessage(peerPubKey string, protocol string, payload []byte) error
}

// peer is a struct storing peer's state
type peer struct {
	log.Log
	pubKey        crypto.PublicKey
	msgMutex      sync.RWMutex
	knownMessages map[uint32]struct{}
	net           sender
}

func newPeer(net sender, pubKey crypto.PublicKey, log log.Log) *peer {
	return &peer{
		log,
		pubKey,
		sync.RWMutex{},
		make(map[uint32]struct{}),
		net,
	}
}

// send sends a gossip message to the peer
func (p *peer) send(msg []byte, checksum uint32) error {
	// don't do anything if this peer know this msg
	p.msgMutex.RLock()
	if _, ok := p.knownMessages[checksum]; ok {
		p.msgMutex.RUnlock()
		return errors.New("already got this msg")
	}
	p.msgMutex.RUnlock()
	go func() {
		err := p.net.SendMessage(p.pubKey.String(), protocolName, msg)
		if err != nil {
			p.Log.Info("Gossip protocol failed to send msg (checksum %d) to peer %v, first attempt. err=%v", checksum, p.pubKey, err)
			// doing one retry before giving up
			err = p.net.SendMessage(p.pubKey.String(), "", msg)
			if err != nil {
				p.Log.Info("Gossip protocol failed to send msg (checksum %d) to peer %v, second attempt. err=%v", checksum, p.pubKey, err)
				return
			}
		}
		p.msgMutex.Lock()
		p.knownMessages[checksum] = struct{}{}
		p.msgMutex.Unlock()
	}()
	return nil
}

func (prot *Protocol) Close() {
	close(prot.shutdown)
}

// markMessage adds the checksum to the old message queue so the message won't be processed in case received again
func (prot *Protocol) markMessage(checksum uint32) {
	prot.oldMessageMu.Lock()
	prot.oldMessageQ[checksum] = struct{}{}
	prot.oldMessageMu.Unlock()
}

func (prot *Protocol) handleProtocolMessage(msg []byte, isRelay bool) {
	checksum := checksum(msg)

	if isRelay {
		// in case the message was received through the relay channel we need to remove the Gossip layer and hand the payload for the next protocol to process
		oldmessage := false

		prot.oldMessageMu.RLock()
		if _, ok := prot.oldMessageQ[checksum]; ok {
			// todo : - have some more metrics for termination
			// todo	: - maybe tell the peer weg ot this message already?
			oldmessage = true
		}
		prot.oldMessageMu.RUnlock()

		if !oldmessage {
			prot.markMessage(checksum)

			// TODO extract the next protocol's payload and hand only it to swarm
			// TODO extract the Gossip author and pass it as the sender
			// TODO extract the next protocol and pass it
			prot.net.ProcessProtocolMessage(node.Node{}, "", msg)
		}
	} else {
		// so we won't process our own messages
		prot.markMessage(checksum)
	}

	prot.peersMutex.RLock()
	for p := range prot.peers {
		peer := prot.peers[p]
		prot.Debug("sending message to peer %v", peer.pubKey)
		peer.send(msg, checksum)  // non blocking
	}
	prot.peersMutex.RUnlock()
}

// Broadcast is the actual broadcast procedure, loop on peers and add the message to their queues
func (prot *Protocol) Broadcast(msg []byte) {
	prot.messageQ <- protocolMessage{msg, nil, false}
}

// Start a loop that process peers events
func (prot *Protocol) Start() {
	prot.peerConn, prot.peerDisc = prot.net.SubscribePeerEvents()
	go prot.eventLoop()
}

func (prot *Protocol) addPeer(peer crypto.PublicKey) {
	prot.peers[peer.String()] = newPeer(prot.net, peer, prot.Log)
}

func (prot *Protocol) removePeer(peer crypto.PublicKey) {
	delete(prot.peers, peer.String())
}

func (prot *Protocol) eventLoop() {
loop:
	for {
		select {
		// incoming messages from p2p layer for relay
		case msg := <-prot.relayQ:
			go func() {prot.messageQ <- protocolMessage{msg.Data(), msg.Sender().PublicKey(), true}}()
		case msg := <-prot.messageQ:
			prot.handleProtocolMessage(msg.msg, msg.isRelay)
		case peer := <-prot.peerConn:
			prot.addPeer(peer)
		case peer := <-prot.peerDisc:
			prot.removePeer(peer)
		case <-prot.shutdown:
			break loop // maybe error ?
		}
	}
}

// peersCount returns the number of peers know to the protocol, used for testing only
func (prot *Protocol) peersCount() int {
	prot.peersMutex.RLock()
	cnt := len(prot.peers)
	prot.peersMutex.RUnlock()
	return cnt
}

// hasPeer returns whether or not a peer is known to the protocol, used for testing only
func (prot *Protocol) hasPeer(key crypto.PublicKey) bool {
	_, ok := prot.peers[key.String()]
	return ok
}