package hare

import (
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"sync"
)

const InboxCapacity = 100

// Stopper is used to add stoppability to an object
type Stopper struct {
	channel chan struct{} // stoppable go routines listen to this channel
}

func NewStopper() Stopper {
	return Stopper{make(chan struct{})}
}

// Stops all listening instances (should be called only once)
func (stopper *Stopper) Stop() {
	close(stopper.channel)
}

// StopChannel returns the channel channel to wait on
func (stopper *Stopper) StopChannel() chan struct{} {
	return stopper.channel
}

// Broker is responsible for dispatching hare messages to the matching layer listener
type Broker struct {
	Stopper
	network NetworkService
	inbox   chan service.Message
	outbox  map[uint32]chan *pb.HareMessage
	mutex   sync.RWMutex
}

func NewBroker(networkService NetworkService) *Broker {
	p := new(Broker)
	p.Stopper = NewStopper()
	p.network = networkService
	p.outbox = make(map[uint32]chan *pb.HareMessage)

	return p
}

// Start listening to protocol messages and dispatch messages
func (broker *Broker) Start() {
	broker.inbox = broker.network.RegisterProtocol(ProtoName)

	go broker.dispatcher()
}

// Dispatch incoming messages to the matching layer instance
func (broker *Broker) dispatcher() {
	for {
		select {
		case msg := <-broker.inbox:
			hareMsg := &pb.HareMessage{}
			err := proto.Unmarshal(msg.Data(), hareMsg)
			if err != nil {
				log.Error("Could not unmarshal message: ", err)
				continue
			}

			layerId := NewLayerId(hareMsg.Message.Layer)

			broker.mutex.RLock()
			if c, exist := broker.outbox[layerId.Id()]; exist {
				c <- hareMsg
			}
			broker.mutex.RUnlock()

		case <-broker.StopChannel():
			return
		}
	}
}

// CreateInbox creates and returns the message channel associated with the given layer
func (broker *Broker) CreateInbox(iden Identifiable) chan *pb.HareMessage {
	var id = iden.Id()

	broker.mutex.RLock()
	if _, exist := broker.outbox[id]; exist {
		panic("CreateInbox called more than once per layer")
	}
	broker.mutex.RUnlock()

	broker.mutex.Lock()
	broker.outbox[id] = make(chan *pb.HareMessage, InboxCapacity) // create new channel
	broker.mutex.Unlock()

	broker.mutex.RLock()
	defer broker.mutex.RUnlock()
	return broker.outbox[id]
}
