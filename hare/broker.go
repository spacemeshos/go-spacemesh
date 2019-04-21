package hare

import (
	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"sync"
)

const InboxCapacity = 100

type StartInstanceError error

type Validator interface {
	Validate(m *Msg) bool
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

// Broker is responsible for dispatching hare messages to the matching set objectId listener
type Broker struct {
	Closer
	network    NetworkService
	eValidator Validator
	inbox      chan service.GossipMessage
	outbox     map[InstanceId]chan *Msg
	pending    map[InstanceId][]*Msg
	tasks      chan func()
	maxReg     InstanceId
	isStarted  bool
	log        log.Log
}

func NewBroker(networkService NetworkService, eValidator Validator, closer Closer, log log.Log) *Broker {
	p := new(Broker)
	p.Closer = closer
	p.network = networkService
	p.eValidator = eValidator
	p.outbox = make(map[InstanceId]chan *Msg)
	p.pending = make(map[InstanceId][]*Msg)
	p.tasks = make(chan func())
	p.maxReg = 1
	p.log = log

	return p
}

// Start listening to protocol messages and dispatch messages (non-blocking)
func (broker *Broker) Start() error {
	if broker.isStarted { // Start has been called at least twice
		broker.log.Error("Could not start instance")
		return StartInstanceError(errors.New("instance already started"))
	}

	broker.isStarted = true

	broker.inbox = broker.network.RegisterGossipProtocol(protoName)
	go broker.eventLoop()

	return nil
}

// Dispatch incoming messages to the matching set objectId instance
func (broker *Broker) eventLoop() {
	for {
		select {
		case msg := <-broker.inbox:
			futureMsg := false

			if msg == nil {
				broker.log.Error("Message validation failed: called with nil")
				continue
			}

			hareMsg := &pb.HareMessage{}
			err := proto.Unmarshal(msg.Bytes(), hareMsg)
			if err != nil {
				broker.log.Error("Could not unmarshal message: ", err)
				continue
			}

			// message validation
			if hareMsg.Message == nil {
				broker.log.Warning("Message validation failed: message is nil")
				continue
			}

			expInstId := broker.maxReg + 1 //  max expect current max + 1
			msgInstId := InstanceId(hareMsg.Message.InstanceId)
			// far future unregistered instance
			if msgInstId > expInstId {
				broker.log.Warning("Message validation failed: instanceId. Max: %v Actual: %v", expInstId, msgInstId)
				continue
			}

			// near future
			if msgInstId == expInstId {
				futureMsg = true
			}

			// TODO: should receive the state querier or have a msg constructor
			iMsg, err := newMsg(hareMsg, mockStateQuerier{})
			if err != nil {
				log.Warning("Message validation failed: could not construct msg err=%v", err)
				continue
			}

			if !broker.eValidator.Validate(iMsg) {
				broker.log.Warning("Message validation failed: eValidator returned false %v", hareMsg)
				continue
			}

			// validation passed
			msg.ReportValidation(protoName)

			c, exist := broker.outbox[msgInstId]
			if exist {
				// todo: err if chan is full (len)
				c <- iMsg
				continue
			} else { // message arrived before registration
				futureMsg = true
			}

			if futureMsg {
				broker.log.Info("Broker identified future message")
				if _, exist := broker.pending[msgInstId]; !exist {
					broker.pending[msgInstId] = make([]*Msg, 0)
				}
				broker.pending[msgInstId] = append(broker.pending[msgInstId], iMsg)
			}

		case task := <-broker.tasks:
			task()
		case <-broker.CloseChannel():
			broker.log.Warning("Broker exiting")
			return
		}
	}
}

// Register a listener to messages
// Note: the registering instance is assumed to be started and accepting messages
func (broker *Broker) Register(id InstanceId) chan *Msg {
	inbox := make(chan *Msg, InboxCapacity)

	wg := sync.WaitGroup{}
	wg.Add(1)
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

		wg.Done()
	}
	broker.tasks <- regRequest
	wg.Wait()

	return inbox
}

// Unregister a listener
func (broker *Broker) Unregister(id InstanceId) {
	wg := sync.WaitGroup{}

	wg.Add(1)
	broker.tasks <- func() {
		delete(broker.outbox, id)
		wg.Done()
	}

	wg.Wait()
}
