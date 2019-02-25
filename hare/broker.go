package hare

import (
	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

const InboxCapacity = 100

type StartInstanceError error

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
	network      NetworkService
	eValidator   Validator
	inbox        chan service.GossipMessage
	outbox       map[InstanceId]chan *pb.HareMessage
	pending      map[InstanceId][]*pb.HareMessage
	tasks        chan func()
	pendingTasks chan struct{}
	maxReg       InstanceId
	isStarted    bool
}

func NewBroker(networkService NetworkService, eValidator Validator) *Broker {
	p := new(Broker)
	p.Closer = NewCloser()
	p.network = networkService
	p.eValidator = eValidator
	p.outbox = make(map[InstanceId]chan *pb.HareMessage)
	p.pending = make(map[InstanceId][]*pb.HareMessage)
	p.tasks = make(chan func())
	p.pendingTasks = make(chan struct{})

	return p
}

// Start listening to protocol messages and dispatch messages (non-blocking)
func (broker *Broker) Start() error {
	if broker.isStarted { // Start has been called at least twice
		log.Error("Could not start instance")
		return StartInstanceError(errors.New("instance already started"))
	}

	broker.isStarted = true

	broker.inbox = broker.network.RegisterGossipProtocol(ProtoName)
	go broker.eventLoop()

	return nil
}

// Dispatch incoming messages to the matching set id instance
func (broker *Broker) eventLoop() {
	for {
		select {
		case msg := <-broker.inbox:
			futureMsg := false

			if msg == nil {
				log.Error("Message validation failed: called with nil")
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

			expInstId := broker.maxReg
			msgInstId := InstanceId(hareMsg.Message.InstanceId)
			// far future unregistered instance
			if msgInstId > expInstId+1 {
				log.Warning("Message validation failed: instanceId. Max: %v Actual: %v", broker.maxReg, hareMsg.Message.InstanceId)
				msg.ReportValidation(ProtoName, false)
				continue
			}

			// near future
			if msgInstId == expInstId+1 {
				futureMsg = true
			}

			if !broker.eValidator.Validate(hareMsg) {
				log.Warning("Message validation failed: eValidator returned false %v", hareMsg)
				msg.ReportValidation(ProtoName, false)
				continue
			}

			// validation passed
			msg.ReportValidation(ProtoName, true)

			c, exist := broker.outbox[msgInstId]
			if exist {
				// todo: err if chan is full (len)
				c <- hareMsg
			} else if futureMsg {
				if _, exist := broker.pending[msgInstId]; !exist {
					broker.pending[msgInstId] = make([]*pb.HareMessage, 0)
				}
				broker.pending[msgInstId] = append(broker.pending[msgInstId], hareMsg)
			}

		case task := <-broker.tasks:
			task()
		case <-broker.CloseChannel():
			return
		}
	}
}

// Register a listener to messages
// Note: the registering instance is assumed to be started and accepting messages
func (broker *Broker) Register(id InstanceId) chan *pb.HareMessage {
	inbox := make(chan *pb.HareMessage, InboxCapacity)

	regRequest := func() {
		if id > broker.maxReg {
			broker.maxReg = id
		}

		broker.outbox[id] = inbox

		pendingForInstance := broker.pending[id]
		if pendingForInstance != nil {
			for _, mOut := range pendingForInstance {
				broker.outbox[id] <- mOut
			}
			delete(broker.pending, id)
		}

		broker.jobDone()
	}
	broker.tasks <- regRequest
	broker.waitForCompletion()

	return inbox
}

// Unregister a listener
func (broker *Broker) Unregister(id InstanceId) {
	broker.tasks <- func() {
		delete(broker.outbox, id)
		broker.jobDone()
	}

	broker.waitForCompletion()
}

func (broker *Broker) waitForCompletion() {
	<-broker.pendingTasks
}

func (broker *Broker) jobDone() {
	broker.pendingTasks <- struct{}{}
}
