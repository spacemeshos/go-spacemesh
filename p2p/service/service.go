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

func NewMessageValidation(msg []byte, prot string, isValid bool) *MessageValidation {
	return &MessageValidation{msg, prot, isValid}
}

// Message is an interface to represent a simple message structure
type Message interface {
	Sender() p2pcrypto.PublicKey
	Bytes() []byte
	ValidationCompletedChan() chan<- MessageValidation
}

// Service is an interface that represents a networking service (ideally p2p) that we can use to send messages or listen to incoming messages
type Service interface {
	Start() error
	RegisterProtocol(protocol string) chan Message
    RegisterProtocolWithChannel(protocol string, ingressChannel chan Message) chan Message
	SendMessage(peerPubkey p2pcrypto.PublicKey, protocol string, payload []byte) error
	SubscribePeerEvents() (new chan p2pcrypto.PublicKey, del chan p2pcrypto.PublicKey)
	ProcessProtocolMessage(sender p2pcrypto.PublicKey, protocol string, payload Data, validationCompletedChan chan<- MessageValidation) error
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
