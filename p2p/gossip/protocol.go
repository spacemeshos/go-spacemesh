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
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"time"
	"github.com/golang/protobuf/proto"
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
type BaseNetwork interface {
	SendMessage(peerPubKey string, protocol string, payload []byte) error
	RegisterProtocol(protocol string) chan service.Message
	SubscribePeerEvents() (conn chan crypto.PublicKey, disc chan crypto.PublicKey)
	ProcessProtocolMessage(sender node.Node, protocol string, payload []byte) error
}

type Signer interface {
	PublicKey() crypto.PublicKey
	Sign(data []byte) ([]byte, error)
	ValidateSign(data []byte, sign []byte) error
}

type protocolMessage struct {
	msg *pb.ProtocolMessage
	isRelay bool
}

// Protocol is the gossip protocol
type Protocol struct {
	log.Log

	ProtocolName string
	config config.SwarmConfig
	net    BaseNetwork
	signer Signer

	peerConn chan crypto.PublicKey
	peerDisc chan crypto.PublicKey
	peers    map[string]*peer
	shutdown chan struct{}

	oldMessageMu sync.RWMutex
	oldMessageQ  map[hash]struct{}
	peersMutex   sync.RWMutex

	relayQ   chan service.Message
	messageQ chan protocolMessage
}

// NewProtocol creates a new gossip protocol instance. Call Start to start reading peers
func NewProtocol(config config.SwarmConfig, base BaseNetwork, signer Signer, log2 log.Log) *Protocol {
	// intentionally not subscribing to peers events so that the channels won't block in case executing Start delays
	relayChan := base.RegisterProtocol(ProtocolName)
	return &Protocol{
		Log:         log2,
		ProtocolName: "/p2p/1.0/gossip",
		config:      config,
		net:         base,
		signer:      signer,
		peerConn:    nil,
		peerDisc:    nil,
		peers:       make(map[string]*peer),
		shutdown:    make(chan struct{}),
		oldMessageQ: make(map[hash]struct{}), // todo : remember to drain this
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
	knownMessages map[hash]struct{}
	net           sender
}

func newPeer(net sender, pubKey crypto.PublicKey, log log.Log) *peer {
	return &peer{
		log,
		pubKey,
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
		err := p.net.SendMessage(p.pubKey.String(), ProtocolName, msg)
		if err != nil {
			p.Log.Info("Gossip protocol failed to send msg (calcHash %d) to peer %v, first attempt. err=%v", checksum, p.pubKey, err)
			// doing one retry before giving up
			err = p.net.SendMessage(p.pubKey.String(), "", msg)
			if err != nil {
				p.Log.Info("Gossip protocol failed to send msg (calcHash %d) to peer %v, second attempt. err=%v", checksum, p.pubKey, err)
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

// markMessage adds the calcHash to the old message queue so the message won't be processed in case received again
func (prot *Protocol) markMessage(h hash) {
	prot.oldMessageMu.Lock()
	prot.oldMessageQ[h] = struct{}{}
	prot.oldMessageMu.Unlock()
}

func (prot *Protocol) propagateMessage(msg []byte, h hash) {
	prot.peersMutex.RLock()
	for p := range prot.peers {
		peer := prot.peers[p]
		prot.Debug("sending message to peer %v, hash %d", peer.pubKey, h)
		peer.send(msg, h) // non blocking
	}
	prot.peersMutex.RUnlock()
}

func (prot *Protocol) validateMessage(msg *pb.ProtocolMessage) error {
	sign := msg.Metadata.MsgSign
	msg.Metadata.MsgSign = nil
	bin, err := proto.Marshal(msg)
	msg.Metadata.MsgSign = sign
	if err != nil {
		prot.Log.Error("failed to marshal message when validating gossip message, err %v", err)
		return err
	}

	err = prot.signer.ValidateSign(bin, sign)
	if err != nil {
		prot.Log.Error("fail to validate message's signature when validating gossip message, err %v", err)
		return err
	}

	if msg.Metadata.ClientVersion != protocolVer {
		prot.Log.Error("fail to validate message's protocol version when validating gossip message, err %v", err)
		return err
	}
	return nil
}

// Broadcast is the actual broadcast procedure, loop on peers and add the message to their queues
func (prot *Protocol) Broadcast(payload []byte, nextProt string) error {
	// add gossip header
	header := &pb.Metadata{
		NextProtocol:  nextProt,
		ClientVersion: protocolVer,
		Timestamp:     time.Now().Unix(),
		AuthPubKey:    prot.signer.PublicKey().Bytes(),
		MsgSign:       nil,
	}

	msg := &pb.ProtocolMessage{
		Metadata: header,
		Payload:  payload,
	}

	bin, err := proto.Marshal(msg)
	if err != nil {
		prot.Log.Error("failed to marshal message when generating gossip header, err %v", err)
		return err

	}

	sign, err2 := prot.signer.Sign(bin)
	if err2 != nil {
		prot.Log.Error("failed to sign header when generating gossip header, err %v", err)
		return err
	}

	msg.Metadata.MsgSign = sign

	// so we won't process our own messages
	hash := calcHash(payload)
	prot.markMessage(hash)
	prot.propagateMessage(payload, hash)
	return nil
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

func (prot *Protocol) isOldMessage(h hash) bool {
	var oldmessage bool
	prot.oldMessageMu.RLock()
	if _, ok := prot.oldMessageQ[h]; ok {
		oldmessage = true
	} else {
		oldmessage = false
	}
	prot.oldMessageMu.RUnlock()
	return oldmessage
}

func (prot *Protocol) handleRelayMessage(msgB []byte) error {
	hash := calcHash(msgB)

	// in case the message was received through the relay channel we need to remove the Gossip layer and hand the payload for the next protocol to process
	if prot.isOldMessage(hash) {
		// todo : - have some more metrics for termination
		// todo	: - maybe tell the peer weg ot this message already?
		prot.Log.Info("got old message, hash %d", hash)
	} else {
		prot.oldMessageQ[hash] = struct{}{}

		msg := &pb.ProtocolMessage{}
		err := proto.Unmarshal(msgB, msg)
		if err != nil {
			prot.Log.Error("failed to unmarshal when handling relay message, err %v", err)
			return err
		}

		err = prot.validateMessage(msg)
		if err != nil {
			prot.Log.Error("failed to validate message when handling relay message, err %v", err)
			return err
		}
		authKey, err := crypto.NewPublicKey(msg.Metadata.AuthPubKey)
		if err != nil {
			prot.Log.Error("failed to decode the auth public key when handling relay message, err %v", err)
			return err
		}
		go prot.net.ProcessProtocolMessage(node.Node{authKey, nil}, msg.Metadata.NextProtocol, msg.Payload)
	}


	prot.propagateMessage(msgB, hash)

	return nil
}

func (prot *Protocol) eventLoop() {
loop:
	for {
		select {
		case msg := <-prot.relayQ:
			// incoming messages from p2p layer for process and relay
			go func() {
				prot.handleRelayMessage(msg.Data())
			}()
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