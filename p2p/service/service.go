package service

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
)

// Message is an interface to represent a simple message structure
type Message interface {
	Sender() node.Node
	Data() []byte
}

// Service is an interface that represents a networking service (ideally p2p) that we can use to send messages or listen to incoming messages
type Service interface {
	Start() error
	RegisterProtocol(protocol string) chan Message
	SendMessage(nodeID string, protocol string, payload []byte) error
	SubscribePeerEvents() (new chan crypto.PublicKey, del chan crypto.PublicKey)
	ProcessProtocolMessage(sender node.Node, protocol string, payload []byte) error
	Broadcast(protocol string, payload []byte) error
	Shutdown()
}
