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

const ProtocolName = "/p2p/1.0/gossip"
const protocolVer = "0"

type hash uint32

// fnv.New32 must be used everytime to be sure we get consistent results.
func calcHash(msg []byte) hash {
	msghash := fnv.New32() // todo: Add nonce to messages instead
	msghash.Write(msg)
	return hash(msghash.Sum32())
}

// Interface for the underlying p2p layer
type baseNetwork interface {
	SendMessage(peerPubkey p2pcrypto.PublicKey, protocol string, payload []byte) error
	RegisterProtocol(protocol string) chan service.Message
	SubscribePeerEvents() (conn chan p2pcrypto.PublicKey, disc chan p2pcrypto.PublicKey)
	ProcessProtocolMessage(sender p2pcrypto.PublicKey, protocol string, data service.Data) error
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
	invalidMessageQ map[hash]struct{}
	peersMutex      sync.RWMutex

	relayQ   chan service.Message
	messageQ chan protocolMessage
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
		invalidMessageQ: make(map[hash]struct{}), // todo : remember to drain this
		peersMutex:      sync.RWMutex{},
		relayQ:          relayChan,
		messageQ:        make(chan protocolMessage, messageQBufferSize),
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

// markMessage adds the calcHash to the old message queue so the message won't be processed in case received again.
// Returns true if message was already processed before
func (prot *Protocol) markMessage(h hash) bool {
	prot.oldMessageMu.Lock()
	var ok bool
	if _, ok = prot.oldMessageQ[h]; !ok {
		prot.oldMessageQ[h] = struct{}{}
		prot.Log.Debug("marking message as old, hash %v", h)
	} else {
		prot.Log.Debug("message is already old, hash %v", h)
	}
	prot.Log.Debug("marking message as old, hash %v, is already old %v", h, ok)
	prot.oldMessageMu.Unlock()
	return ok
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

	finbin, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	hash := calcHash(finbin)

	// every message that we broadcast we also process, unless it is a message that we already processed before
	isOld := prot.markMessage(hash)
	if !isOld {
		err = prot.processMessage(msg)
		if err != nil {
			return err
		}
	}
	prot.propagateMessage(finbin, hash)
	return nil
}

// Start a loop that process peers events
func (prot *Protocol) Start() {
	peerConn, peerDisc := prot.net.SubscribePeerEvents() // this was start blocks until we registered.
	go prot.eventLoop(peerConn, peerDisc)
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

// marks a hash as old message and check message validity
func (prot *Protocol) markAndValidateMessage(h hash, msg *pb.ProtocolMessage) (isOldMessage, isInvalid bool) {
	prot.oldMessageMu.Lock()
	if _, isOldMessage = prot.oldMessageQ[h]; !isOldMessage {
		prot.oldMessageQ[h] = struct{}{}
	}
	if _, isInvalid = prot.invalidMessageQ[h]; !isInvalid && !isOldMessage{
		err := prot.validateMessage(msg)
		if err != nil {
			prot.Log.Error("failed to validate message when handling relay message, err %v", err)
			isInvalid = true
			prot.invalidMessageQ[h] = struct{}{}
		}
	}
	prot.oldMessageMu.Unlock()
	return
}

func (prot *Protocol) processMessage(msg *pb.ProtocolMessage) error {
	var data service.Data

	if payload := msg.GetPayload(); payload != nil {
		data = service.DataBytes{Payload: payload}
	} else if wrap := msg.GetMsg(); wrap != nil {
		prot.Log.Warning("unexpected usage of request-response framework over Gossip - WAS IT IN PURPOSE? ")
		data = &service.DataMsgWrapper{Req: wrap.Req, MsgType: wrap.Type, ReqID: wrap.ReqID, Payload: wrap.Payload}
	}

	senderPubkey, err := p2pcrypto.NewPubkeyFromBytes(msg.Metadata.AuthPubKey)
	if err != nil {
		prot.Log.Error("failed to decode the auth public key when handling relay message, err %v", err)
		return err
	}

	go prot.net.ProcessProtocolMessage(senderPubkey, msg.Metadata.NextProtocol, data)
	return nil
}

func (prot *Protocol) handleRelayMessage(msgB []byte) {
	hash := calcHash(msgB)
	msg := &pb.ProtocolMessage{}
	err := proto.Unmarshal(msgB, msg)
	if err != nil {
		prot.Log.Error("failed to unmarshal when handling relay message, err %v", err)
		return
	}

	// in case the message was received through the relay channel we need to remove the Gossip layer and hand the
	// payload for the next protocol to process
	isOld, isInvalid := prot.markAndValidateMessage(hash, msg)
	if isInvalid {
		// todo : - have some more metrics for termination
		prot.Log.Info("got invalid message, hash %d, isOld %v", hash, isOld)
		return // not propagating invalid messages
	}
	if isOld {
		// todo : - have some more metrics for termination
		// todo	: - maybe tell the peer weg ot this message already?
		prot.Log.Debug("got old message, hash %d, isInvalid %v", hash, isInvalid)

	} else {
		//todo - processMessage is non-blocking, need to check validation and not propagate invalid messages
		err = prot.processMessage(msg)
		if err != nil {
			return
		}
	}

	prot.propagateMessage(msgB, hash)
	return
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
