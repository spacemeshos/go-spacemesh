package p2p

import (
	"container/list"
	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"sync"
	"sync/atomic"
	"time"
)

type MessageType uint32

type Item struct {
	id        uint64
	timestamp time.Time
}

type Protocol struct {
	id                 uint64
	name               string
	network            Service
	pendMutex          sync.RWMutex
	pendingMap         map[uint64]chan interface{}
	pendingQueue       *list.List
	resHandlers        map[uint64]func(msg []byte)
	msgRequestHandlers map[MessageType]func(msg []byte) []byte
	ingressChannel     chan service.Message
	requestTimeout     time.Duration
}

func NewProtocol(network Service, name string, requestTimeout time.Duration) *Protocol {
	p := &Protocol{
		name:               name,
		network:            network,
		requestTimeout:     requestTimeout,
		pendingQueue:       list.New(),
		ingressChannel:     network.RegisterProtocol(name),
		pendingMap:         make(map[uint64]chan interface{}),
		resHandlers:        make(map[uint64]func(msg []byte)),
		msgRequestHandlers: make(map[MessageType]func(msg []byte) []byte),
	}

	go p.readLoop()
	return p
}

func (p *Protocol) readLoop() {
	for {
		timer := time.NewTimer(time.Second)
		select {
		case msg, ok := <-p.ingressChannel:
			if !ok {
				log.Error("read loop channel was closed")
				break
			}
			//todo add buffer and option to limit number of concurrent goroutines
			go p.handleMessage(msg)
		case <-timer.C:
			go p.cleanStaleMessages()
		}
	}
}

func (p *Protocol) cleanStaleMessages() {
	for {
		if elem := p.pendingQueue.Front(); elem != nil {
			item := elem.Value.(Item)
			if time.Since(item.timestamp) > p.requestTimeout {
				p.removeFromPending(item.id, elem)
			} else {
				return
			}
		}
	}
}

func (p *Protocol) handleMessage(msg service.Message) {

	headers := &pb.MessageWrapper{}

	if err := proto.Unmarshal(msg.Data(), headers); err != nil {
		log.Error("Error handling incoming ", p.name, " message, ", "request", headers, "err:", err)
		return
	}

	if headers.Req {
		p.handleRequestMessage(msg.Sender().PublicKey(), headers)
	} else {
		p.handleResponseMessage(headers)
	}

}

func (p *Protocol) handleRequestMessage(sender crypto.PublicKey, headers *pb.MessageWrapper) {

	if payload := p.msgRequestHandlers[MessageType(headers.Type)](headers.Payload); payload != nil {
		rmsg, fParseErr := proto.Marshal(&pb.MessageWrapper{Req: false, ReqID: headers.ReqID, Type: headers.Type, Payload: payload})
		if fParseErr != nil {
			log.Error("Error Parsing Protocol message, err:", fParseErr)
			return
		}
		sendErr := p.network.SendMessage(sender.String(), p.name, rmsg)
		if sendErr != nil {
			log.Error("Error sending response message, err:", sendErr)
		}
	}
}

func (p *Protocol) handleResponseMessage(headers *pb.MessageWrapper) {

	//get and remove from pendingMap
	p.pendMutex.RLock()
	pend, okPend := p.pendingMap[headers.ReqID]
	foo, okFoo := p.resHandlers[headers.ReqID]
	delete(p.pendingMap, headers.ReqID)
	delete(p.resHandlers, headers.ReqID)
	p.pendMutex.RUnlock()

	if okPend {
		if okFoo {
			foo(headers.Payload)
		} else {
			pend <- headers.Payload
		}
	}
}

func (p *Protocol) removeFromPending(reqID uint64, req *list.Element) {
	p.pendMutex.Lock()
	if req != nil {
		p.pendingQueue.Remove(req)
	}
	delete(p.pendingMap, reqID)
	delete(p.resHandlers, reqID)
	p.pendMutex.Unlock()
}

func (p *Protocol) RegisterMsgHandler(msgType MessageType, reqHandler func(msg []byte) []byte) {
	p.msgRequestHandlers[msgType] = reqHandler
}

func (p *Protocol) SendAsyncRequest(msgType MessageType, payload []byte, address string, resHandler func(msg []byte)) error {

	reqID := p.newRequestId()
	pbsp := &pb.MessageWrapper{Req: true, ReqID: reqID, Type: uint32(msgType), Payload: payload}
	msg, err := proto.Marshal(pbsp)
	if err != nil {
		return err
	}

	respc := make(chan interface{})
	p.pendMutex.Lock()
	p.pendingMap[reqID] = respc
	p.resHandlers[reqID] = resHandler
	elem := p.pendingQueue.PushBack(Item{id: reqID, timestamp: time.Now()})
	p.pendMutex.Unlock()

	if sendErr := p.network.SendMessage(address, p.name, msg); sendErr != nil {
		p.removeFromPending(reqID, elem)
		return sendErr
	}

	return nil
}

func (p *Protocol) newRequestId() uint64 {
	return atomic.AddUint64(&p.id, 1)
}

func (p *Protocol) SendRequest(msgType MessageType, payload []byte, address string, timeout time.Duration) (interface{}, error) {
	reqID := p.newRequestId()

	pbsp := &pb.MessageWrapper{Req: true, ReqID: reqID, Type: uint32(msgType), Payload: payload}
	msg, err := proto.Marshal(pbsp)
	if err != nil {
		return nil, err
	}

	respc := make(chan interface{})

	p.pendMutex.Lock()
	p.pendingMap[reqID] = respc
	p.pendMutex.Unlock()

	defer p.removeFromPending(reqID, nil)

	if sendErr := p.network.SendMessage(address, p.name, msg); sendErr != nil {
		return nil, sendErr
	}

	timer := time.NewTimer(timeout)
	select {
	case response := <-respc:
		if response != nil {
			return response, nil
		}
		return nil, errors.New("response was nil")
	case <-timer.C:
		err = errors.New("peer took too long to respond")
	}

	return nil, err
}
