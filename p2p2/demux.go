package p2p2

import "github.com/UnrulyOS/go-unruly/log"

type IncomingMessage struct {
	sender   RemoteNode
	protocol string
	msg      []byte
}

type MessagesChan chan IncomingMessage

type ProtocolRegistration struct {
	protocol string
	handler  MessagesChan
}

// Demux is responsible to route incoming messages back to protocol handlers based on the message protocol
type Demuxer interface {
	RegisterProtocolHandler(handler ProtocolRegistration)
	RouteIncomingMessage(msg IncomingMessage)
}

type demuxImpl struct {

	// internal state
	handlers map[string]MessagesChan

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
			handler := d.handlers[msg.protocol]
			if handler == nil {
				log.Warning("failed to route incoming message - no registered handler for protocol %s", msg.protocol)
			} else {
				go func() { // async send to handler so we can keep processing message
					handler <- msg
				}()
			}

		case reg := <-d.registrationRequests:
			d.handlers[reg.protocol] = reg.handler
		}
	}
}
