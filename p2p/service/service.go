package service

import (
	"github.com/spacemeshos/go-spacemesh/p2p/node"
)

// Message is an interface to represent a simple message structure
type Message interface {
	Sender() node.Node
	Bytes() []byte
}

// Service is an interface that represents a networking service (ideally p2p) that we can use to send messages or listen to incoming messages
type Service interface {
	Start() error
	RegisterProtocol(protocol string) chan Message
	SendMessage(nodeID string, protocol string, payload []byte) error
	Broadcast(protocol string, payload []byte) error
	Shutdown()
}

type Data interface {
	messageData()
	Bytes() []byte
}

type Data_Bytes struct {
	Payload []byte
}

type Data_MsgWrapper struct {
	Req     bool
	MsgType uint32
	ReqID   uint64
	Payload []byte
}

func (m Data_Bytes) messageData() {}

func (m Data_Bytes) Bytes() []byte {
	return m.Payload
}

func (m Data_MsgWrapper) messageData() {}

func (m Data_MsgWrapper) Bytes() []byte {
	return m.Payload
}
