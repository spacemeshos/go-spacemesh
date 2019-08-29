package p2p

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/version"
	"net"
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	inet "github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

// Lookuper is a service used to lookup for nodes we know already
type Lookuper func(key p2pcrypto.PublicKey) (*node.NodeInfo, error)

type udpNetwork interface {
	Start() error
	Shutdown()

	IncomingMessages() chan inet.UDPMessageEvent
	Send(to *node.NodeInfo, data []byte) error
}

// UDPMux is a server for receiving and sending udp messages. through protocols.
type UDPMux struct {
	local    *node.LocalNode
	lookuper Lookuper
	network  udpNetwork
	messages map[string]chan service.DirectMessage
	shutdown chan struct{}
	logger   log.Log
}

// NewUDPMux creates a new udp protocol server
func NewUDPMux(localNode *node.LocalNode, lookuper Lookuper, udpNet udpNetwork, logger log.Log) *UDPMux {
	return &UDPMux{
		localNode,
		lookuper,
		udpNet,
		make(map[string]chan service.DirectMessage),
		make(chan struct{}, 1),
		logger,
	}
}

// Start starts the UDPMux
func (mux *UDPMux) Start() error {
	go mux.listenToNetworkMessage()
	return nil
}

// Shutdown closes the server
func (mux *UDPMux) Shutdown() {
	close(mux.shutdown)
	mux.network.Shutdown()
}

func (mux *UDPMux) listenToNetworkMessage() {
	msgChan := mux.network.IncomingMessages()
	for {
		select {
		case msg, ok := <-msgChan:
			if !ok {
				// closed
				return
			}
			go func(event inet.UDPMessageEvent) {
				err := mux.processUDPMessage(event.From, event.FromAddr, event.Message)
				if err != nil {
					mux.logger.Error("Error handing network message err=%v", err)
					// todo: blacklist ?
				}
			}(msg)
		case <-mux.shutdown:
			return
		}
	}
}

// Note: for now udp is only direct.
// todo: no need to return chan, but for now stay consistent with api

// RegisterDirectProtocolWithChannel registers a protocol on a channel, should be done before `Start` was called. not thread-safe
func (mux *UDPMux) RegisterDirectProtocolWithChannel(name string, c chan service.DirectMessage) chan service.DirectMessage {
	mux.messages[name] = c
	return c
}

// ProcessDirectProtocolMessage passes a message to the protocol.
func (mux *UDPMux) ProcessDirectProtocolMessage(sender p2pcrypto.PublicKey, protocol string, data service.Data, metadata service.P2PMetadata) error {
	// route authenticated message to the registered protocol
	msgchan := mux.messages[protocol]

	if msgchan == nil {
		return errors.New("no protocol")
	}

	msgchan <- &udpProtocolMessage{metadata, sender, data}

	return nil
}

// SendWrappedMessage is a proxy method to the sendMessageImpl. it sends a wrapped message and used within MessageServer
func (mux *UDPMux) SendWrappedMessage(nodeID p2pcrypto.PublicKey, protocol string, payload *service.DataMsgWrapper) error {
	return mux.sendMessageImpl(nodeID, protocol, payload)
}

// SendMessage is a proxy method to the sendMessageImpl.
func (mux *UDPMux) SendMessage(peerPubkey p2pcrypto.PublicKey, protocol string, payload []byte) error {
	return mux.sendMessageImpl(peerPubkey, protocol, service.DataBytes{Payload: payload})
}

// sendMessageImpl finds the peer address, wraps the message as a protocol message with p2p metadata and sends it.
func (mux *UDPMux) sendMessageImpl(peerPubkey p2pcrypto.PublicKey, protocol string, payload service.Data) error {
	var err error
	var peer *node.NodeInfo

	peer, err = mux.lookuper(peerPubkey)

	if err != nil {
		return err
	}

	//todo: Session (maybe use cpool ?)

	mt := ProtocolMessageMetadata{protocol,
		config.ClientVersion,
		time.Now().UnixNano(),
		mux.local.PublicKey().Bytes(),
		int32(mux.local.NetworkID()),
	}

	message := ProtocolMessage{
		Metadata: &mt,
	}

	message.Payload, err = CreatePayload(payload)
	if err != nil {
		return fmt.Errorf("can't create payload, err:%v", err)
	}

	data, err := types.InterfaceToBytes(&message)
	if err != nil {
		return fmt.Errorf("failed to encode signed message err: %v", err)
	}

	// TODO: node.address should have IP address, UDP and TCP PORT.
	// 		 for now assuming it's the same port for both.

	mux.logger.Debug("Sending udp message to %v, %v", peer.String())

	return mux.network.Send(peer, data)
}

type udpProtocolMessage struct {
	meta   service.P2PMetadata
	sender p2pcrypto.PublicKey
	msg    service.Data
}

func (upm *udpProtocolMessage) Sender() p2pcrypto.PublicKey {
	return upm.sender
}

func (upm *udpProtocolMessage) Metadata() service.P2PMetadata {
	return upm.meta
}

func (upm *udpProtocolMessage) Bytes() []byte {
	return upm.msg.Bytes()
}

func (upm *udpProtocolMessage) Data() service.Data {
	return upm.msg
}

// processUDPMessage processes a udp message received and passes it to the protocol, it adds related p2p metadata.
func (mux *UDPMux) processUDPMessage(sender p2pcrypto.PublicKey, fromaddr net.Addr, buf []byte) error {
	mux.logger.Debug("Processing message from %v, %v, len:%v", sender.String(), fromaddr.String(), len(buf))
	msg := &ProtocolMessage{}
	err := types.BytesToInterface(buf, msg)
	if err != nil {
		return errors.New("could'nt deserialize message")
	}

	if msg.Metadata.NetworkID != int32(mux.local.NetworkID()) {
		// todo: tell net to blacklist the ip or sender ?
		return fmt.Errorf("wrong NetworkID, want: %v, got: %v", mux.local.NetworkID(), msg.Metadata.NetworkID)
	}

	if t, err := version.CheckNodeVersion(msg.Metadata.ClientVersion, config.MinClientVersion); err != nil || !t {
		return fmt.Errorf("wrong client version want atleast: %v, got: %v, err=%v", config.MinClientVersion, msg.Metadata.ClientVersion, err)
	}

	var data service.Data

	data, err = ExtractData(msg.Payload)

	if err != nil {
		return fmt.Errorf("failed extracting data from message err:%v", err)
	}

	p2pmeta := service.P2PMetadata{fromaddr}

	return mux.ProcessDirectProtocolMessage(sender, msg.Metadata.NextProtocol, data, p2pmeta)

}
