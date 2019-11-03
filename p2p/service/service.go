package service

import (
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"net"
)

type MessageValidation struct {
	sender p2pcrypto.PublicKey
	msg    []byte
	prot   string
}

func (mv MessageValidation) Message() []byte {
	return mv.msg
}

func (mv MessageValidation) Sender() p2pcrypto.PublicKey {
	return mv.sender
}

func (mv MessageValidation) Protocol() string {
	return mv.prot
}

// Metadata is a generic metadata interface
type P2PMetadata struct {
	FromAddress net.Addr
	// add here more fields that are needed by protocols
}

func NewMessageValidation(sender p2pcrypto.PublicKey, msg []byte, prot string) MessageValidation {
	return MessageValidation{sender, msg, prot}
}

// DirectMessage is an interface that represents a simple direct message structure
type DirectMessage interface {
	Metadata() P2PMetadata
	Sender() p2pcrypto.PublicKey
	Bytes() []byte
}

// GossipMessage is an interface that represents a simple gossip message structure
type GossipMessage interface {
	Sender() p2pcrypto.PublicKey
	Bytes() []byte
	ValidationCompletedChan() chan MessageValidation
	ReportValidation(protocol string)
}

// Service is an interface that represents a networking service (ideally p2p) that we can use to send messages or listen to incoming messages
type Service interface {
	Start() error
	RegisterGossipProtocol(protocol string) chan GossipMessage
	SubscribePeerEvents() (new chan p2pcrypto.PublicKey, del chan p2pcrypto.PublicKey)
	Broadcast(protocol string, payload []byte) error
	Shutdown()
}

type Data interface {
	messageData()
	Bytes() []byte
}

type DataBytes struct {
	Payload []byte
}

type DataMsgWrapper struct {
	Req     bool
	MsgType uint32
	ReqID   uint64
	Payload []byte
}

func (m DataBytes) messageData() {}

func (m DataBytes) Bytes() []byte {
	return m.Payload
}

func (m DataMsgWrapper) messageData() {}

func (m DataMsgWrapper) Bytes() []byte {
	return m.Payload
}
