package hare

import (
	"errors"
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

// Broker is responsible for dispatching hare Messages to the matching set objectId listener
type Broker struct {
	Closer
	log.Log
	network      NetworkService
	eValidator   Validator
	stateQuerier StateQuerier
	inbox        chan service.GossipMessage
	outbox       map[InstanceId]chan *Msg
	pending      map[InstanceId][]*Msg
	tasks        chan func()
	maxReg       InstanceId
	isStarted    bool
}

func NewBroker(networkService NetworkService, eValidator Validator, stateQuerier StateQuerier, closer Closer, log log.Log) *Broker {
	p := new(Broker)
	p.Closer = closer
	p.Log = log
	p.network = networkService
	p.eValidator = eValidator
	p.stateQuerier = stateQuerier
	p.outbox = make(map[InstanceId]chan *Msg)
	p.pending = make(map[InstanceId][]*Msg)
	p.tasks = make(chan func())
	p.maxReg = 1

	return p
}

// Start listening to protocol Messages and dispatch Messages (non-blocking)
func (b *Broker) Start() error {
	if b.isStarted { // Start has been called at least twice
		b.Error("Could not start instance")
		return StartInstanceError(errors.New("instance already started"))
	}

	b.isStarted = true

	b.inbox = b.network.RegisterGossipProtocol(protoName)
	go b.eventLoop()

	return nil
}

// Dispatch incoming Messages to the matching set objectId instance
func (b *Broker) eventLoop() {
	for {
		select {
		case msg := <-b.inbox:
			futureMsg := false

			if msg == nil {
				b.Error("Message validation failed: called with nil")
				continue
			}

			hareMsg, err := MessageFromBuffer(msg.Bytes())
			if err != nil {
				b.Error("Could not build message err=%v", err)
				continue
			}

			// InnerMsg validation
			if hareMsg.InnerMsg == nil {
				b.Warning("Message validation failed: InnerMsg is nil")
				continue
			}

			expInstId := b.maxReg + 1 //  max expect current max + 1
			msgInstId := InstanceId(hareMsg.InnerMsg.InstanceId)
			// far future unregistered instance
			if msgInstId > expInstId {
				b.Warning("Message validation failed: InstanceId. Max: %v Actual: %v", expInstId, msgInstId)
				continue
			}

			// near future
			if msgInstId == expInstId {
				futureMsg = true
			}

			// TODO: should receive the state querier or have a msg constructor
			iMsg, err := newMsg(hareMsg, MockStateQuerier{true, nil})
			if err != nil {
				b.Warning("Message validation failed: could not construct msg err=%v", err)
				continue
			}

			if !b.eValidator.Validate(iMsg) {
				b.Warning("Message validation failed: eValidator returned false %v", hareMsg)
				continue
			}

			// validation passed
			msg.ReportValidation(protoName)

			c, exist := b.outbox[msgInstId]
			if exist {
				// todo: err if chan is full (len)
				c <- iMsg
				continue
			} else { // InnerMsg arrived before registration
				futureMsg = true
			}

			if futureMsg {
				if _, exist := b.pending[msgInstId]; !exist {
					b.pending[msgInstId] = make([]*Msg, 0)
				}
				b.pending[msgInstId] = append(b.pending[msgInstId], iMsg)
			}

		case task := <-b.tasks:
			task()
		case <-b.CloseChannel():
			b.Warning("Broker exiting")
			return
		}
	}
}

// Register a listener to Messages
// Note: the registering instance is assumed to be started and accepting Messages
func (b *Broker) Register(id InstanceId) chan *Msg {
	inbox := make(chan *Msg, InboxCapacity)

	wg := sync.WaitGroup{}
	wg.Add(1)
	regRequest := func() {
		if id > b.maxReg {
			b.maxReg = id
		}

		b.outbox[id] = inbox

		pendingForInstance := b.pending[id]
		if pendingForInstance != nil {
			for _, mOut := range pendingForInstance {
				b.outbox[id] <- mOut
			}
			delete(b.pending, id)
		}

		wg.Done()
	}
	b.tasks <- regRequest
	wg.Wait()

	return inbox
}

// Unregister a listener
func (b *Broker) Unregister(id InstanceId) {
	wg := sync.WaitGroup{}

	wg.Add(1)
	b.tasks <- func() {
		delete(b.outbox, id)
		wg.Done()
	}

	wg.Wait()
}
