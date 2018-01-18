package p2p

import (
	"github.com/spacemeshos/go-spacemesh/log"
)

type IncomingMessage interface {
	Sender() Peer
	Protocol() string
	Payload() []byte
}

func NewIncomingMessage(sender Peer, protocol string, payload []byte) IncomingMessage {
	return &IncomingMessageImpl{
		sender:   sender,
		protocol: protocol,
		payload:  payload,
	}
}

// a protocol message
type IncomingMessageImpl struct {
	sender   Peer
	protocol string
	payload  []byte
}

func (i *IncomingMessageImpl) Sender() Peer {
	return i.sender
}

func (i *IncomingMessageImpl) Protocol() string {
	return i.protocol
}

func (i *IncomingMessageImpl) Payload() []byte {
	return i.payload
}

type MessagesChan chan IncomingMessage

type ProtocolRegistration struct {
	Protocol string
	Handler  MessagesChan
}

// Demuxer is responsible for routing incoming network messages back to protocol handlers based on message protocols
// Limitations: only supports 1 handler per protocol for now.

// todo: add unit tests

type Demuxer interface {
	RegisterProtocolHandler(handler ProtocolRegistration)
	RouteIncomingMessage(msg IncomingMessage)
}

type demuxImpl struct {

	// internal state
	handlers map[string]MessagesChan

	// caps
	incomingMessages     chan IncomingMessage
	registrationRequests chan ProtocolRegistration
}

func NewDemuxer() Demuxer {

	d := &demuxImpl{
		handlers:             make(map[string]MessagesChan),
		incomingMessages:     make(chan IncomingMessage, 20),
		registrationRequests: make(chan ProtocolRegistration, 20),
	}

	go d.processEvents()

	return d
}

func (d *demuxImpl) RegisterProtocolHandler(handler ProtocolRegistration) {
	d.registrationRequests <- handler
}

func (d *demuxImpl) RouteIncomingMessage(msg IncomingMessage) {
	d.incomingMessages <- msg
}

func (d *demuxImpl) processEvents() {
	for {
		select {
		case msg := <-d.incomingMessages:
			handler := d.handlers[msg.Protocol()]
			if handler == nil {
				log.Warning("failed to route an incoming message - no registered handler for protocol", msg.Protocol())
			} else {
				go func() { // async send to handler so we can keep processing messages
					handler <- msg
				}()
			}

		case reg := <-d.registrationRequests:
			d.handlers[reg.Protocol] = reg.Handler
		}
	}
}
