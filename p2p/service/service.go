package service

import (
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
)

type MessageValidation struct {
	msg     []byte
	prot    string
	isValid bool
}

func (mv MessageValidation) Message() []byte {
	return mv.msg
}

func (mv MessageValidation) Protocol() string {
	return mv.prot
}

func (mv MessageValidation) IsValid() bool {
	return mv.isValid
}

func NewMessageValidation(msg []byte, prot string, isValid bool) MessageValidation {
	return MessageValidation{msg, prot, isValid}
}

// DirectMessage is an interface that represents a simple direct message structure
type DirectMessage interface {
	Sender() p2pcrypto.PublicKey
	Bytes() []byte
}

// GossipMessage is an interface that represents a simple gossip message structure
type GossipMessage interface {
	Bytes() []byte
	ValidationCompletedChan() chan MessageValidation
	ReportValidation(protocol string, isValid bool)
}

// Service is an interface that represents a networking service (ideally p2p) that we can use to send messages or listen to incoming messages
type Service interface {
	Start() error
	RegisterDirectProtocol(protocol string) chan DirectMessage
	RegisterGossipProtocol(protocol string) chan GossipMessage
	RegisterDirectProtocolWithChannel(protocol string, ingressChannel chan DirectMessage) chan DirectMessage
	SendMessage(peerPubkey p2pcrypto.PublicKey, protocol string, payload []byte) error
	SubscribePeerEvents() (new chan p2pcrypto.PublicKey, del chan p2pcrypto.PublicKey)
	ProcessDirectProtocolMessage(sender p2pcrypto.PublicKey, protocol string, payload Data) error
	ProcessGossipProtocolMessage(protocol string, data Data, validationCompletedChan chan MessageValidation) error
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
