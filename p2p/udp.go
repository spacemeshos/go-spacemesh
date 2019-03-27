package p2p

import (
	"errors"
	"fmt"
	"net"

	"github.com/golang/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/log"
	inet "github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

const maxMessageSize = 2048

// todo : calculate real udp max message size

// Lookuper is a service used to lookup for nodes we know already
type Lookuper interface {
	InternalLookup(key node.DhtID) []node.Node
}

type udpNetwork interface {
	Start() error
	Shutdown()

	IncomingMessages() chan inet.UDPMessageEvent
	Send(to node.Node, data []byte) error
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
	err := mux.network.Start()
	if err != nil {
		return err
	}
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
				err := mux.processUDPMessage(event.Size, event.Addr, event.Message)
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

// todo: no need to return chan, but stay consistent with api
// RegisterDirectProtocolWithChannel registers a protocol on a channel
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

	msgchan <- &udpProtocolMessage{metadata, sender, data.Bytes()}

	return nil
}

func (mux *UDPMux) localLookup(key p2pcrypto.PublicKey) (node.Node, error) {
	res := mux.lookuper.InternalLookup(node.NewDhtID(key.Bytes()))
	if res == nil || len(res) == 0 {
		return node.EmptyNode, errors.New("coudld'nt find requested key node")
	}

	if res[0].PublicKey().String() != key.String() {
		return node.EmptyNode, errors.New("coudld'nt find requested key node")
	}

	return res[0], nil
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
	var peer node.Node

	peer, err = mux.localLookup(peerPubkey)

	if err != nil {
		return err
	}

	//todo: Session (maybe use cpool ?)

	protomessage := &pb.ProtocolMessage{
		Metadata: NewProtocolMessageMetadata(mux.local.PublicKey(), protocol),
	}

	switch x := payload.(type) {
	case service.DataBytes:
		protomessage.Data = &pb.ProtocolMessage_Payload{Payload: x.Bytes()}
	case *service.DataMsgWrapper:
		protomessage.Data = &pb.ProtocolMessage_Msg{Msg: &pb.MessageWrapper{Type: x.MsgType, Req: x.Req, ReqID: x.ReqID, Payload: x.Payload}}
	case nil:
		// The field is not set.
	default:
		return fmt.Errorf("protocolMsg has unexpected type %T", x)
	}

	data, err := proto.Marshal(protomessage)
	if err != nil {
		return fmt.Errorf("failed to encode signed message err: %v", err)
	}

	if len(data) > maxMessageSize {
		return errors.New(fmt.Sprintf("message too big (%v bytes). max allowed size = %d bytes", len(data), maxMessageSize))
	}

	// TODO: node.address should have IP address, UDP and TCP PORT.
	// 		 for now assuming it's the same port for both.

	return mux.network.Send(peer, data)
}

type udpProtocolMessage struct {
	meta   service.P2PMetadata
	sender p2pcrypto.PublicKey
	msg    []byte
}

func (upm *udpProtocolMessage) Sender() p2pcrypto.PublicKey {
	return upm.sender
}

func (upm *udpProtocolMessage) Metadata() service.P2PMetadata {
	return upm.meta
}

func (upm *udpProtocolMessage) Bytes() []byte {
	return upm.msg
}

// processUDPMessage processes a udp message received and passes it to the protocol, it adds related p2p metadata.
func (mux *UDPMux) processUDPMessage(size int, fromaddr net.Addr, buf []byte) error {
	msg := &pb.ProtocolMessage{}
	err := proto.Unmarshal(buf[0:size], msg)
	if err != nil {
		return errors.New("could'nt deserialize message")
	}
	sender, err := p2pcrypto.NewPubkeyFromBytes(msg.Metadata.AuthPubkey)
	if err != nil {
		return errors.New("no valid pubkey in message")
	}
	//mux.logger.Debug("Received %v from %s at %s", sender, fromaddr)

	var data service.Data

	if payload := msg.GetPayload(); payload != nil {
		data = service.DataBytes{Payload: payload}
	} else if wrap := msg.GetMsg(); wrap != nil {
		data = &service.DataMsgWrapper{Req: wrap.Req, MsgType: wrap.Type, ReqID: wrap.ReqID, Payload: wrap.Payload}
	}

	p2pmeta := service.P2PMetadata{fromaddr}

	return mux.ProcessDirectProtocolMessage(sender, msg.Metadata.NextProtocol, data, p2pmeta)

}
