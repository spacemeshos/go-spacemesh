package hare

import (
	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"sync"
	"time"
)

const InboxCapacity = 100

type StartInstanceError error

type Identifiable interface {
	Id() uint32
}

type Inboxer interface {
	createInbox(size uint32) chan *pb.HareMessage
}

type IdentifiableInboxer interface {
	Identifiable
	Inboxer
}

type Validator interface {
	Validate(m *pb.HareMessage) bool
}

// Closer is used to add closeability to an object
type Closer struct {
	channel chan struct{} // closeable go routines listen to this channel
}

func NewCloser() Closer {
	return Closer{make(chan struct{})}
}

// Closes all listening instances (should be called only once)
func (closer *Closer) Close() {
	close(closer.channel)
}

// CloseChannel returns the channel to wait on
func (closer *Closer) CloseChannel() chan struct{} {
	return closer.channel
}

// Broker is responsible for dispatching hare messages to the matching set id listener
type Broker struct {
	Closer
	network    NetworkService
	eValidator Validator
	inbox      chan service.GossipMessage
	outbox     map[uint32]chan *pb.HareMessage
	pending    map[uint32][]*pb.HareMessage
	mutex      sync.RWMutex
	maxReg     uint32
	startTime  time.Time
}

func NewBroker(networkService NetworkService, eValidator Validator) *Broker {
	p := new(Broker)
	p.Closer = NewCloser()
	p.network = networkService
	p.eValidator = eValidator
	p.outbox = make(map[uint32]chan *pb.HareMessage)
	p.pending = make(map[uint32][]*pb.HareMessage, 0)

	return p
}

// Start listening to protocol messages and dispatch messages (non-blocking)
func (broker *Broker) Start() error {
	if !broker.startTime.IsZero() { // Start has been called at least twice
		log.Error("Could not start instance")
		return StartInstanceError(errors.New("instance already started"))
	}

	broker.startTime = time.Now()

	broker.inbox = broker.network.RegisterGossipProtocol(ProtoName)
	go broker.dispatcher()

	return nil
}

// Dispatch incoming messages to the matching set id instance
func (broker *Broker) dispatcher() {
	for {
		select {
		case msg := <-broker.inbox:
			if msg == nil {
				log.Warning("Message validation failed: called with nil")
				msg.ReportValidation(ProtoName, false)
				continue
			}

			hareMsg := &pb.HareMessage{}
			err := proto.Unmarshal(msg.Bytes(), hareMsg)
			if err != nil {
				log.Error("Could not unmarshal message: ", err)
				msg.ReportValidation(ProtoName, false)
				continue
			}

			// message validation
			if hareMsg.Message == nil {
				log.Warning("Message validation failed: message is nil")
				msg.ReportValidation(ProtoName, false)
				continue
			}

			if hareMsg.Message.InstanceId > broker.maxReg+1 { // intended for future unregistered instance
				log.Warning("Message validation failed: instanceId. Max: %v Actual: %v", broker.maxReg, hareMsg.Message.InstanceId)
				msg.ReportValidation(ProtoName, false)
				continue
			}

			if !broker.eValidator.Validate(hareMsg) {
				log.Warning("Message validation failed: eValidator returned false %v", hareMsg)
				msg.ReportValidation(ProtoName, false)
				continue
			}

			// validation passed
			msg.ReportValidation(ProtoName, true)

			instanceId := InstanceId(hareMsg.Message.InstanceId)
			broker.mutex.RLock()
			c, exist := broker.outbox[instanceId.Id()]
			broker.mutex.RUnlock()
			if exist {
				// todo: err if chan is full (len)
				c <- hareMsg
			} else {

				broker.mutex.Lock()
				if _, exist := broker.pending[instanceId.Id()]; !exist {
					broker.pending[instanceId.Id()] = make([]*pb.HareMessage, 0)
				}
				broker.pending[instanceId.Id()] = append(broker.pending[instanceId.Id()], hareMsg)
				broker.mutex.Unlock()
			}

		case <-broker.CloseChannel():
			return
		}
	}
}

// Register a listener to messages
// Note: the registering instance is assumed to be started and accepting messages
func (broker *Broker) Register(idBox IdentifiableInboxer) {
	broker.mutex.Lock()
	if idBox.Id() > broker.maxReg {
		broker.maxReg = idBox.Id()
	}

	broker.outbox[idBox.Id()] = idBox.createInbox(InboxCapacity)

	pendingForInstance := broker.pending[idBox.Id()]
	if pendingForInstance != nil {
		for _, mOut := range pendingForInstance {
			broker.outbox[idBox.Id()] <- mOut
		}
		delete(broker.pending, idBox.Id())
	}

	broker.mutex.Unlock()
}

// Unregister a listener
func (broker *Broker) Unregister(identifiable Identifiable) {
	broker.mutex.Lock()
	delete(broker.outbox, identifiable.Id())
	broker.mutex.Unlock()
}
