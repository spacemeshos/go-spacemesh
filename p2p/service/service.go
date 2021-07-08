// Package service defines basic interfaces to for protocols to consume p2p functionality.
package service

import (
	"context"
	"net"

	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/priorityq"
)

// MessageValidation is a gossip message validation event.
type MessageValidation struct {
	sender p2pcrypto.PublicKey
	msg    []byte
	prot   string
	reqID  string
}

// Message returns the message as bytes
func (mv MessageValidation) Message() []byte {
	return mv.msg
}

// Sender returns the public key of the sender of this message. (might not be the author)
func (mv MessageValidation) Sender() p2pcrypto.PublicKey {
	return mv.sender
}

// Protocol is the protocol this message is targeted to.
func (mv MessageValidation) Protocol() string {
	return mv.prot
}

// RequestID is the originating request ID of the message
func (mv MessageValidation) RequestID() string {
	return mv.reqID
}

// P2PMetadata is a generic metadata interface
type P2PMetadata struct {
	FromAddress net.Addr
	// add here more fields that are needed by protocols
}

// NewMessageValidation creates a message validation struct to pass to the protocol.
func NewMessageValidation(sender p2pcrypto.PublicKey, msg []byte, prot string, reqID string) MessageValidation {
	return MessageValidation{sender, msg, prot, reqID}
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
	IsOwnMessage() bool
	Bytes() []byte
	RequestID() string
	ValidationCompletedChan() chan MessageValidation
	ReportValidation(ctx context.Context, protocol string)
}

// Service is an interface that represents a networking service (ideally p2p) that we can use to send messages or listen to incoming messages
type Service interface {
	Start(ctx context.Context) error
	RegisterGossipProtocol(protocol string, prio priorityq.Priority) chan GossipMessage
	RegisterDirectProtocol(protocol string) chan DirectMessage
	SubscribePeerEvents() (new chan p2pcrypto.PublicKey, del chan p2pcrypto.PublicKey)
	GossipReady() <-chan struct{}
	Broadcast(ctx context.Context, protocol string, payload []byte) error
	Shutdown()
}

// Data is a wrapper around a message that can hold either raw bytes message or a req-res wrapper.
type Data interface {
	Bytes() []byte
}

// DataBytes is a byte array payload wrapper.
type DataBytes struct {
	Payload []byte
}

// DataMsgWrapper is a req-res payload wrapper
type DataMsgWrapper struct {
	Req     bool
	MsgType uint32
	ReqID   uint64
	Payload []byte
}

// Bytes returns the message as bytes
func (m DataBytes) Bytes() []byte {
	return m.Payload
}

// Bytes returns the message as bytes
func (m DataMsgWrapper) Bytes() []byte {
	return m.Payload
}
