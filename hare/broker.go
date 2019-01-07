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
	network NetworkService
	inbox   chan service.Message
	outbox  map[uint32]chan *pb.HareMessage
	mutex   sync.RWMutex
}

func NewBroker(networkService NetworkService) *Broker {
	p := new(Broker)
	p.Closer = NewCloser()
	p.network = networkService
	p.outbox = make(map[uint32]chan *pb.HareMessage)

	return p
}

// Start listening to protocol messages and dispatch messages (non-blocking)
func (broker *Broker) Start() error {
	if broker.inbox != nil { // Start has been called at least twice
		log.Error("Could not start instance")
		return StartInstanceError(errors.New("instance already started"))
	}

	broker.inbox = broker.network.RegisterProtocol(ProtoName)

	go broker.dispatcher()

	return nil
}

// Dispatch incoming messages to the matching set id instance
func (broker *Broker) dispatcher() {
	for {
		select {
		case msg := <-broker.inbox:
			log.Info("Received msg in broker")

			hareMsg := &pb.HareMessage{}
			err := proto.Unmarshal(msg.Bytes(), hareMsg)
			if err != nil {
				log.Error("Could not unmarshal message: ", err)
				continue
			}

			instanceId := NewBytes32(hareMsg.Message.InstanceId)

			broker.mutex.RLock()
			c, exist := broker.outbox[instanceId.Id()]
			broker.mutex.RUnlock()
			if exist {
				c <- hareMsg
			}

		case <-broker.CloseChannel():
			return
		}
	}
}

// Register a listener to messages
func (broker *Broker) Register(idBox IdentifiableInboxer) {
	broker.mutex.Lock()
	broker.outbox[idBox.Id()] = idBox.createInbox(InboxCapacity)
	broker.mutex.Unlock()
}

// Unregister a listener
func (broker *Broker) Unregister(identifiable Identifiable) {
	broker.mutex.Lock()
	delete(broker.outbox, identifiable.Id())
	broker.mutex.Unlock()
}
