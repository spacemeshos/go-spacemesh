package p2p

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"gopkg.in/op/go-logging.v1"
)

// IncomingMessage defines an incoming a p2p protocol message components.
type IncomingMessage interface {
	Sender() Peer
	Protocol() string
	Payload() []byte
}

// NewIncomingMessage creates a new IncomingMessage from provided components.
func NewIncomingMessage(sender Peer, protocol string, payload []byte) IncomingMessage {
	return &IncomingMessageImpl{
		sender:   sender,
		protocol: protocol,
		payload:  payload,
	}
}

// IncomingMessageImpl implements IncomingMessage.
type IncomingMessageImpl struct {
	sender   Peer
	protocol string
	payload  []byte
}

// Sender returns the message sender peer.
func (i *IncomingMessageImpl) Sender() Peer {
	return i.sender
}

// Protocol returns the message protocol string.
func (i *IncomingMessageImpl) Protocol() string {
	return i.protocol
}

// Payload returns the binary message payload.
func (i *IncomingMessageImpl) Payload() []byte {
	return i.payload
}

// MessagesChan is a channel of IncomingMessages.
type MessagesChan chan IncomingMessage

// ProtocolRegistration defines required protocol demux registration data.
type ProtocolRegistration struct {
	Protocol string
	Handler  MessagesChan
}

// Demuxer is responsible for routing incoming network messages
// back to protocol handlers based on message protocols.
// Limitations - type only supports 1 handler per protocol for now.
type Demuxer interface {
	RegisterProtocolHandler(handler ProtocolRegistration)
	RouteIncomingMessage(msg IncomingMessage)
}

type demuxImpl struct {
	log *logging.Logger

	// internal state
	handlers map[string]MessagesChan

	// caps
	incomingMessages     chan IncomingMessage
	registrationRequests chan ProtocolRegistration
}

// NewDemuxer creates a new Demuxer.
func NewDemuxer(log *logging.Logger) Demuxer {

	d := &demuxImpl{
		log:                  log,
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

func (d *demuxImpl) routeMessage(msg IncomingMessage) {
	d.log.Debug("Muxing %v message from %v ", msg.Protocol(), msg.Sender().Pretty())
	handler := d.handlers[msg.Protocol()]
	if handler == nil {
		log.Warning("failed to route an incoming message - no registered handler for protocol", msg.Protocol())
	} else {
		go func() { // async send to handler so we can keep processing messages
			handler <- msg
		}()
	}
}

func (d *demuxImpl) processEvents() {
	for {
		select {
		case msg := <-d.incomingMessages:
			d.routeMessage(msg)

		case reg := <-d.registrationRequests:
			d.handlers[reg.Protocol] = reg.Handler
		}
	}
}
