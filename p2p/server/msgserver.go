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

// Config holds configuration params
type Config struct {
	BufferSize int
}

type MessageServer struct {
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
	workerCount        sync.WaitGroup
	exit               chan struct{}
}

func NewMsgServer(network ServerService, name string, requestLifetime time.Duration, config *Config) *MessageServer {

	p := &MessageServer{
		name:               name,
		pendingMap:         make(map[uint64]chan interface{}),
		resHandlers:        make(map[uint64]func(msg []byte)),
		pendingQueue:       list.New(),
		network:            network,
		ingressChannel:     network.RegisterProtocol(name, config.BufferSize),
		msgRequestHandlers: make(map[MessageType]func(msg []byte) []byte),
		requestLifetime:    requestLifetime,
		exit:               make(chan struct{}),
	}

	go p.readLoop()
	return p
}

func (p *MessageServer) Close() {
	p.exit <- struct{}{}
	<-p.exit
	p.workerCount.Wait()
	for k, v := range p.pendingMap {
		close(v)
		delete(p.pendingMap, k)
		delete(p.resHandlers, k)
	}
}

func (p *MessageServer) readLoop() {
	for {
		timer := time.NewTicker(10 * time.Second)
		select {
		case <-p.exit:
			log.Debug("shutting down protocol ", p.name)
			close(p.exit)
			return
		case <-timer.C:
			go p.cleanStaleMessages()
		case msg, ok := <-p.ingressChannel:
			if !ok {
				log.Error("read loop channel was closed")
				break
			}
			//todo add buffer and option to limit number of concurrent goroutines
			p.workerCount.Add(1)
			go func() {
				defer p.workerCount.Done()
				p.handleMessage(msg.(ServerMessage))
			}()

		}
	}
}

func (p *MessageServer) cleanStaleMessages() {
	for {
		p.pendMutex.RLock()
		elem := p.pendingQueue.Front()
		p.pendMutex.RUnlock()
		if elem != nil {
			item := elem.Value.(Item)
			if time.Since(item.timestamp) > p.requestLifetime {
				p.removeFromPending(item.id, elem)
			} else {
				return
			}
		}
	}
}
func (p *MessageServer) handleMessage(msg ServerMessage) {
	if msg.Data().Req {
		p.handleRequestMessage(msg.Sender().PublicKey(), msg.Data())
	} else {
		p.handleResponseMessage(msg.Data())
	}
}

func (p *MessageServer) handleRequestMessage(sender crypto.PublicKey, headers *service.Data_MsgWrapper) {

	if payload := p.msgRequestHandlers[MessageType(headers.MsgType)](headers.Payload); payload != nil {
		rmsg := &service.Data_MsgWrapper{MsgType: headers.MsgType, ReqID: headers.ReqID, Payload: payload}
		sendErr := p.network.SendWrappedMessage(sender.String(), p.name, rmsg)
		if sendErr != nil {
			log.Error("Error sending response message, err:", sendErr)
		}
	}
}

func (p *MessageServer) handleResponseMessage(headers *service.Data_MsgWrapper) {

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

func (p *MessageServer) removeFromPending(reqID uint64, req *list.Element) {
	p.pendMutex.Lock()
	if req != nil {
		p.pendingQueue.Remove(req)
	}

	ch, ok := p.pendingMap[reqID]

	if ok {
		close(ch)
	}

	delete(p.pendingMap, reqID)
	delete(p.resHandlers, reqID)
	p.pendMutex.Unlock()
}

func (p *MessageServer) RegisterMsgHandler(msgType MessageType, reqHandler func(msg []byte) []byte) {
	p.msgRequestHandlers[msgType] = reqHandler
}

func (p *MessageServer) SendAsyncRequest(msgType MessageType, payload []byte, address crypto.PublicKey, resHandler func(msg []byte)) error {

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

func (p *MessageServer) newRequestId() uint64 {
	return atomic.AddUint64(&p.ReqId, 1)
}

func (p *MessageServer) SendRequest(msgType MessageType, payload []byte, address crypto.PublicKey, timeout time.Duration) (interface{}, error) {
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
