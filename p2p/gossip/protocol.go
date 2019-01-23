package gossip

import (
	"errors"
	"fmt"
	"github.com/golang/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"hash/fnv"
	"sync"
	"time"
)

const messageQBufferSize = 100
const propagateHandleBufferSize = 1000 // number of MessageValidation that we alow buffering, above this number protocols will get stuck

const ProtocolName = "/p2p/1.0/gossip"
const protocolVer = "0"

type hash uint32

// fnv.New32 must be used everytime to be sure we get consistent results.
func calcHash(msg []byte, prot string) hash {
	msghash := fnv.New32() // todo: Add nonce to messages instead
	msghash.Write(msg)
	msghash.Write([]byte(prot))
	return hash(msghash.Sum32())
}

// Interface for the underlying p2p layer
type baseNetwork interface {
	SendMessage(peerPubkey p2pcrypto.PublicKey, protocol string, payload []byte) error
	RegisterProtocol(protocol string) chan service.Message
	SubscribePeerEvents() (conn chan p2pcrypto.PublicKey, disc chan p2pcrypto.PublicKey)
	ProcessProtocolMessage(sender p2pcrypto.PublicKey, protocol string, data service.Data, validationChan chan<- service.MessageValidation) error
}

type protocolMessage struct {
	msg *pb.ProtocolMessage
}

// Protocol is the gossip protocol
type Protocol struct {
	log.Log

	config          config.SwarmConfig
	net             baseNetwork
	localNodePubkey p2pcrypto.PublicKey

	peers    map[string]*peer
	shutdown chan struct{}

	oldMessageMu    sync.RWMutex
	oldMessageQ     map[hash]struct{}
	invalidMessageQ map[hash]bool
	peersMutex      sync.RWMutex

	relayQ     chan service.Message
	messageQ   chan protocolMessage
	propagateQ chan service.MessageValidation
}

// NewProtocol creates a new gossip protocol instance. Call Start to start reading peers
func NewProtocol(config config.SwarmConfig, base baseNetwork, localNodePubkey p2pcrypto.PublicKey, log2 log.Log) *Protocol {
	// intentionally not subscribing to peers events so that the channels won't block in case executing Start delays
	relayChan := base.RegisterProtocol(ProtocolName)
	return &Protocol{
		Log:             log2,
		config:          config,
		net:             base,
		localNodePubkey: localNodePubkey,
		peers:           make(map[string]*peer),
		shutdown:        make(chan struct{}),
		oldMessageQ:     make(map[hash]struct{}), // todo : remember to drain this
		invalidMessageQ: make(map[hash]bool),     // todo : remember to drain this
		peersMutex:      sync.RWMutex{},
		relayQ:          relayChan,
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
	pubkey        p2pcrypto.PublicKey
	msgMutex      sync.RWMutex
	knownMessages map[hash]struct{}
	net           sender
}

func newPeer(net sender, pubkey p2pcrypto.PublicKey, log log.Log) *peer {
	return &peer{
		log,
		pubkey,
		sync.RWMutex{},
		make(map[hash]struct{}),
		net,
	}
}

// send sends a gossip message to the peer
func (p *peer) send(msg []byte, checksum hash) error {
	// don't do anything if this peer know this msg
	p.msgMutex.RLock()
	if _, ok := p.knownMessages[checksum]; ok {
		p.msgMutex.RUnlock()
		return errors.New("already got this msg")
	}
	p.msgMutex.RUnlock()
	go func() {
		p.Debug("sending message to peer %v, hash %d", p.pubkey, checksum)
		err := p.net.SendMessage(p.pubkey, ProtocolName, msg)
		if err != nil {
			p.Log.Info("Gossip protocol failed to send msg (calcHash %d) to peer %v, first attempt. err=%v", checksum, p.pubkey, err)
			// doing one retry before giving up
			err = p.net.SendMessage(p.pubkey, "", msg)
			if err != nil {
				p.Log.Info("Gossip protocol failed to send msg (calcHash %d) to peer %v, second attempt. err=%v", checksum, p.pubkey, err)
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

// markMessageAsOld adds the message's hash to the old messages queue so that the message won't be processed in case received again.
// Returns true if message was already processed before
func (prot *Protocol) markMessageAsOld(h hash) bool {
	prot.oldMessageMu.Lock()
	var ok bool
	if _, ok = prot.oldMessageQ[h]; !ok {
		prot.oldMessageQ[h] = struct{}{}
		prot.Log.Debug("marking message as old, hash %v", h)
	} else {
		prot.Log.Debug("message is already old, hash %v", h)
	}
	prot.oldMessageMu.Unlock()
	return ok
}

type Validity int
const (
	Valid Validity = iota
	Invalid
	Unknown
)

func (prot *Protocol) isMessageValid(h hash) Validity {
	prot.oldMessageMu.RLock()
	res, ok := prot.invalidMessageQ[h]
	prot.oldMessageMu.RUnlock()
	if ok {
		if res {
			return Valid
		}
		return Invalid
	}
	return Unknown
}

// markMessageValidity stores the message's validity so that invalid messages won't be propagated in case received again.
func (prot *Protocol) markMessageValidity(h hash, isValid bool) {
	prot.oldMessageMu.Lock()
	prot.invalidMessageQ[h] = isValid
	prot.oldMessageMu.Unlock()
}

func (prot *Protocol) propagateMessage(msg []byte, h hash) {
	prot.peersMutex.RLock()
	for p := range prot.peers {
		peer := prot.peers[p]
		peer.send(msg, h) // non blocking
	}
	prot.peersMutex.RUnlock()
}

func (prot *Protocol) validateMessage(msg *pb.ProtocolMessage) error {
	if cv := msg.Metadata.ClientVersion; cv != protocolVer {
		prot.Log.Error("fail to validate message's protocol version when validating gossip message")
		return fmt.Errorf("bad clientVersion: message version '%s' is incompatible with local version '%s'", cv, protocolVer)
	}
	return nil
}

// Broadcast is the actual broadcast procedure - process the message internally and loop on peers and add the message to their queues
func (prot *Protocol) Broadcast(payload []byte, nextProt string) error {
	prot.Log.Debug("Broadcasting message from type %s", nextProt)
	// add gossip header
	header := &pb.Metadata{
		NextProtocol:  nextProt,
		ClientVersion: protocolVer,
		Timestamp:     time.Now().Unix(),
		AuthPubKey:    prot.localNodePubkey.Bytes(), // TODO: @noam consider replacing this with another reply mechanism
	}

	msg := &pb.ProtocolMessage{
		Metadata: header,
		Data:     &pb.ProtocolMessage_Payload{Payload: payload},
	}

	prot.processMessage(msg)
	return nil
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

func (prot *Protocol) processMessage(msg *pb.ProtocolMessage) {
	var data service.Data
	if payload := msg.GetPayload(); payload != nil {
		data = service.DataBytes{Payload: payload}
	} else if wrap := msg.GetMsg(); wrap != nil {
		prot.Log.Warning("unexpected usage of request-response framework over Gossip - WAS IT IN PURPOSE? ")
		data = &service.DataMsgWrapper{Req: wrap.Req, MsgType: wrap.Type, ReqID: wrap.ReqID, Payload: wrap.Payload}
	}

	protocol := msg.Metadata.NextProtocol
	h := calcHash(data.Bytes(), protocol)

	isOld := prot.markMessageAsOld(h)
	if isOld {
		// todo : - have some more metrics for termination
		// todo	: - maybe tell the peer weg ot this message already?
		validity := prot.isMessageValid(h)
		prot.Log.Debug("got old message, hash %d validity %v", h, validity)
		if validity == Valid {
			prot.propagateMessage(data.Bytes(), h)
		} else {
			// if the message is invalid we don't want to propagate it and we can return. If the message's validity is unknown,
			// since the message is marked as old we can assume that there is another context that currently process this
			// message and will determine its validity, therefore we can return in such case as well
			return
		}
	} else {
		err := prot.net.ProcessProtocolMessage(nil, protocol, data, prot.propagateQ)
		if err != nil {
			prot.Log.Error("failed to process protocol message. protocol = %v err = %v", protocol, err)
			prot.markMessageValidity(h, false)
		}
	}
}

func (prot *Protocol) handleRelayMessage(msgB []byte) {
	msg := &pb.ProtocolMessage{}
	err := proto.Unmarshal(msgB, msg)
	if err != nil {
		prot.Log.Error("failed to unmarshal when handling relay message, err %v", err)
		return
	}

	prot.processMessage(msg)
}

func (prot *Protocol) propagationEventLoop() {
	var err error
loop:
	for {
		select {
		case msgV := <-prot.propagateQ:
			h := calcHash(msgV.Message(), msgV.Protocol())
			prot.markMessageValidity(h, msgV.IsValid())
			if msgV.IsValid() {
				prot.propagateMessage(msgV.Message(), h)
			}
		case <-prot.shutdown:
			err = errors.New("protocol shutdown")
			break loop
		}
	}
	prot.Warning("propagate event loop stopped. err: %v", err)
}

func (prot *Protocol) eventLoop(peerConn chan p2pcrypto.PublicKey, peerDisc chan p2pcrypto.PublicKey) {
	var err error
loop:
	for {
		select {
		case msg, ok := <-prot.relayQ:
			if !ok {
				err = errors.New("channel closed")
				break loop
			}
			// incoming messages from p2p layer for process and relay
			go func() {
				prot.handleRelayMessage(msg.Bytes())
			}()
		case peer := <-peerConn:
			go prot.addPeer(peer)
		case peer := <-peerDisc:
			go prot.removePeer(peer)
		case <-prot.shutdown:
			err = errors.New("protocol shutdown")
			break loop
		}
	}
	prot.Warning("Gossip protocol event loop stopped. err: %v", err)
}

// peersCount returns the number of peers know to the protocol, used for testing only
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
