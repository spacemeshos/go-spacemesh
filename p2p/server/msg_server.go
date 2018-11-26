package server

import (
	"container/list"
	"errors"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"sync"
	"sync/atomic"
	"time"
)

type MessageType uint32

type ServerService interface {
	service.Service
	SendWrappedMessage(nodeID string, protocol string, payload *service.Data_MsgWrapper) error
}

type ServerMessage interface {
	service.Message
	Data() *service.Data_MsgWrapper
}

type Item struct {
	id        uint64
	timestamp time.Time
}

type Message_Server struct {
	ReqId              uint64 //request id
	name               string //server name
	network            ServerService
	pendMutex          sync.RWMutex
	pendingMap         map[uint64]chan interface{}             //pending messages by request ReqId
	pendingQueue       *list.List                              //queue of pending messages
	resHandlers        map[uint64]func(msg []byte)             //response handlers by request ReqId
	msgRequestHandlers map[MessageType]func(msg []byte) []byte //request handlers by request type
	ingressChannel     chan service.Message                    //chan to relay messages into the server
	requestLifetime    time.Duration                           //time a request can stay in the pending queue until evicted
}

func NewMsgServer(network ServerService, name string, requestLifetime time.Duration) *Message_Server {
	p := &Message_Server{
		name:               name,
		pendingMap:         make(map[uint64]chan interface{}),
		resHandlers:        make(map[uint64]func(msg []byte)),
		pendingQueue:       list.New(),
		network:            network,
		ingressChannel:     network.RegisterProtocol(name),
		msgRequestHandlers: make(map[MessageType]func(msg []byte) []byte),
		requestLifetime:    requestLifetime,
	}

	go p.readLoop()
	return p
}

func (p *Message_Server) readLoop() {
	for {
		timer := time.NewTicker(10 * time.Second)
		select {
		case <-timer.C:
			go p.cleanStaleMessages()
		case msg, ok := <-p.ingressChannel:
			if !ok {
				log.Error("read loop channel was closed")
				break
			}
			//todo add buffer and option to limit number of concurrent goroutines
			go p.handleMessage(msg.(ServerMessage))
		}
	}
}

func (p *Message_Server) cleanStaleMessages() {
	for {
		if elem := p.pendingQueue.Front(); elem != nil {
			item := elem.Value.(Item)
			if time.Since(item.timestamp) > p.requestLifetime {
				p.removeFromPending(item.id, elem)
			} else {
				return
			}
		}
	}
}

func (p *Message_Server) handleMessage(msg ServerMessage) {
	if msg.Data().Req {
		p.handleRequestMessage(msg.Sender().PublicKey(), msg.Data())
	} else {
		p.handleResponseMessage(msg.Data())
	}
}

func (p *Message_Server) handleRequestMessage(sender crypto.PublicKey, headers *service.Data_MsgWrapper) {

	if payload := p.msgRequestHandlers[MessageType(headers.MsgType)](headers.Payload); payload != nil {
		rmsg := &service.Data_MsgWrapper{MsgType: headers.MsgType, ReqID: headers.ReqID, Payload: payload}
		sendErr := p.network.SendWrappedMessage(sender.String(), p.name, rmsg)
		if sendErr != nil {
			log.Error("Error sending response message, err:", sendErr)
		}
	}
}

func (p *Message_Server) handleResponseMessage(headers *service.Data_MsgWrapper) {

	//get and remove from pendingMap
	p.pendMutex.Lock()
	pend, okPend := p.pendingMap[headers.ReqID]
	foo, okFoo := p.resHandlers[headers.ReqID]
	delete(p.pendingMap, headers.ReqID)
	delete(p.resHandlers, headers.ReqID)
	p.pendMutex.Unlock()

	if okPend {
		if okFoo {
			foo(headers.Payload)
		} else {
			pend <- headers.Payload
		}
	}
}

func (p *Message_Server) removeFromPending(reqID uint64, req *list.Element) {
	p.pendMutex.Lock()
	if req != nil {
		p.pendingQueue.Remove(req)
	}
	delete(p.pendingMap, reqID)
	delete(p.resHandlers, reqID)
	p.pendMutex.Unlock()
}

func (p *Message_Server) RegisterMsgHandler(msgType MessageType, reqHandler func(msg []byte) []byte) {
	p.msgRequestHandlers[msgType] = reqHandler
}

func (p *Message_Server) SendAsyncRequest(msgType MessageType, payload []byte, address crypto.PublicKey, resHandler func(msg []byte)) error {

	reqID := p.newRequestId()
	msg := &service.Data_MsgWrapper{Req: true, ReqID: reqID, MsgType: uint32(msgType), Payload: payload}
	respc := make(chan interface{})
	p.pendMutex.Lock()
	p.pendingMap[reqID] = respc
	p.resHandlers[reqID] = resHandler
	item := p.pendingQueue.PushBack(Item{id: reqID, timestamp: time.Now()})
	p.pendMutex.Unlock()

	if sendErr := p.network.SendWrappedMessage(address.String(), p.name, msg); sendErr != nil {
		p.removeFromPending(reqID, item)
		return sendErr
	}

	return nil
}

func (p *Message_Server) newRequestId() uint64 {
	return atomic.AddUint64(&p.ReqId, 1)
}

func (p *Message_Server) SendRequest(msgType MessageType, payload []byte, address crypto.PublicKey, timeout time.Duration) (interface{}, error) {
	reqID := p.newRequestId()

	msg := &service.Data_MsgWrapper{Req: true, ReqID: reqID, MsgType: uint32(msgType), Payload: payload}
	respc := make(chan interface{})

	p.pendMutex.Lock()
	p.pendingMap[reqID] = respc
	p.pendMutex.Unlock()

	defer p.removeFromPending(reqID, nil)

	if sendErr := p.network.SendWrappedMessage(address.String(), p.name, msg); sendErr != nil {
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
		log.Error("peer took too long to respond, request id: ", reqID, "request type: ", msgType)
	}
	return nil, errors.New("peer took too long to respond")

}
