package gossip

import (
	"errors"
	"fmt"
	"github.com/btcsuite/btcutil/base58"
	"github.com/golang/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"hash/fnv"
	"sync"
	"time"
)

const messageQBufferSize = 100
const propagateHandleBufferSize = 1000 // number of MessageValidation that we allow buffering, above this number protocols will get stuck

const ProtocolName = "/p2p/1.0/gossip"
const protocolVer = "0"

type hash uint32

// fnv.New32 must be used every time to be sure we get consistent results.
func calcHash(msg []byte, prot string) hash {
	msghash := fnv.New32() // todo: Add nonce to messages instead
	msghash.Write(msg)
	msghash.Write([]byte(prot))
	return hash(msghash.Sum32())
}

// Interface for the underlying p2p layer
type baseNetwork interface {
	SendMessage(peerPubkey p2pcrypto.PublicKey, protocol string, payload []byte) error
	RegisterDirectProtocol(protocol string) chan service.DirectMessage
	SubscribePeerEvents() (conn chan p2pcrypto.PublicKey, disc chan p2pcrypto.PublicKey)
	ProcessGossipProtocolMessage(sender p2pcrypto.PublicKey, protocol string, data service.Data, validationCompletedChan chan service.MessageValidation) error
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

	peers      map[string]*peer
	peersMutex sync.RWMutex

	shutdown chan struct{}

	oldMessageQ  map[hash]struct{}
	oldMessageMu sync.RWMutex

	relayQ     chan service.DirectMessage
	messageQ   chan protocolMessage
	propagateQ chan service.MessageValidation
}

// NewProtocol creates a new gossip protocol instance. Call Start to start reading peers
func NewProtocol(config config.SwarmConfig, base baseNetwork, localNodePubkey p2pcrypto.PublicKey, log2 log.Log) *Protocol {
	// intentionally not subscribing to peers events so that the channels won't block in case executing Start delays
	relayChan := base.RegisterDirectProtocol(ProtocolName)
	return &Protocol{
		Log:             log2,
		config:          config,
		net:             base,
		localNodePubkey: localNodePubkey,
		peers:           make(map[string]*peer),
		shutdown:        make(chan struct{}),
		oldMessageQ:     make(map[hash]struct{}), // todo : remember to drain this
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

// send sends a gossip message to the peer
func (p *peer) send(msg []byte, checksum hash) {
	p.Debug("sending message to peer %v, hash %d", p.pubkey, checksum)
	err := p.net.SendMessage(p.pubkey, ProtocolName, msg)
	if err != nil {
		p.Log.Error("Gossip protocol failed to send msg (calcHash %d) to peer %v, first attempt. err=%v", checksum, p.pubkey, err)
		// doing one retry before giving up
		// TODO: find out if this is really needed
		err = p.net.SendMessage(p.pubkey, "", msg)
		if err != nil {
			p.Log.Error("Gossip protocol failed to send msg (calcHash %d) to peer %v, second attempt. err=%v", checksum, p.pubkey, err)
			return
		}
	}
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

func (prot *Protocol) propagateMessage(payload []byte, h hash, nextProt string, exclude p2pcrypto.PublicKey) {
	// add gossip header
	header := &pb.Metadata{
		NextProtocol:  nextProt,
		ClientVersion: protocolVer,
		Timestamp:     time.Now().Unix(),
		AuthPubkey:    prot.localNodePubkey.Bytes(), // TODO: @noam consider replacing this with another reply mechanism
	}

	msg := &pb.ProtocolMessage{
		Metadata: header,
		Payload:  &pb.Payload{Data: &pb.Payload_Payload{payload}},
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		prot.Log.Error("failed to encode signed message err: %v", err)
		return
	}

	prot.peersMutex.RLock()
peerLoop:
	for p := range prot.peers {
		if exclude.String() == p {
			continue peerLoop
		}
		go prot.peers[p].send(data, h) // non blocking
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
		AuthPubkey:    prot.localNodePubkey.Bytes(), // TODO: @noam consider replacing this with another reply mechanism. get sender from swarm
	}

	msg := &pb.ProtocolMessage{
		Metadata: header,
		Payload:  &pb.Payload{Data: &pb.Payload_Payload{payload}},
	}

	prot.processMessage(prot.localNodePubkey, msg)
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

func (prot *Protocol) processMessage(sender p2pcrypto.PublicKey, msg *pb.ProtocolMessage) {
	data, err := pb.ExtractData(msg.Payload)
	if err != nil {
		prot.Log.Warning("could'nt extract payload from message err=", err)
	} else if _, ok := data.(*service.DataMsgWrapper); ok {
		prot.Log.Warning("unexpected usage of request-response framework over Gossip - WAS IT IN PURPOSE? ")
		return
	}

	protocol := msg.Metadata.NextProtocol
	h := calcHash(data.Bytes(), protocol)

	isOld := prot.markMessageAsOld(h)
	if isOld {
		metrics.OldGossipMessages.With(metrics.ProtocolLabel, protocol).Add(1)
		// todo : - have some more metrics for termination
		// todo	: - maybe tell the peer we got this message already?
		// todo : - maybe block this peer since he sends us old messages
	} else {
		prot.Log.With().Info("new_gossip_message", log.String("from", sender.String()), log.String("auth", base58.Encode(msg.Metadata.AuthPubkey)), log.String("protocol", protocol))
		metrics.NewGossipMessages.With("protocol", protocol).Add(1)
		err := prot.net.ProcessGossipProtocolMessage(sender, protocol, data, prot.propagateQ)
		if err != nil {
			prot.Log.Warning("failed to process protocol message. protocol = %v err = %v", protocol, err)
		}
	}
}

func (prot *Protocol) handleRelayMessage(sender p2pcrypto.PublicKey, msgB []byte) {
	msg := &pb.ProtocolMessage{}
	err := proto.Unmarshal(msgB, msg)
	if err != nil {
		prot.Log.Error("failed to unmarshal when handling relay message, err %v", err)
		return
	}

	prot.processMessage(sender, msg)
}

func (prot *Protocol) propagationEventLoop() {
	var err error
loop:
	for {
		select {
		case msgV := <-prot.propagateQ:
			prot.propagateMessage(msgV.Message(), calcHash(msgV.Message(), msgV.Protocol()), msgV.Protocol(), msgV.Sender())
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
				prot.handleRelayMessage(msg.Sender(), msg.Bytes())
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
