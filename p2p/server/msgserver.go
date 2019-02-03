package server

import (
	"container/list"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
    "runtime"
	"sync"
	"sync/atomic"
	"time"
)

type MessageType uint32

type Service interface {
	service.Service
	SendWrappedMessage(nodeID p2pcrypto.PublicKey, protocol string, payload *service.DataMsgWrapper) error
}

type Message interface {
	service.DirectMessage
	Data() service.Data
}

type Item struct {
	id        uint64
	timestamp time.Time
}

type MessageServer struct {
	log.Log
	ReqId              uint64 //request id
	name               string //server name
	network            Service
	pendMutex          sync.RWMutex
	pendingQueue       *list.List                              //queue of pending messages
	resHandlers        map[uint64]func(msg []byte)             //response handlers by request ReqId
	msgRequestHandlers map[MessageType]func(msg []byte) []byte //request handlers by request type
	ingressChannel     chan service.DirectMessage              //chan to relay messages into the server
	requestLifetime    time.Duration                           //time a request can stay in the pending queue until evicted
	workerCount        sync.WaitGroup
    workerLimiter      chan int
	exit               chan struct{}
}

func NewMsgServer(network Service, name string, requestLifetime time.Duration, c chan service.DirectMessage, logger log.Log) *MessageServer {
	p := &MessageServer{
		Log:                logger,
		name:               name,
		resHandlers:        make(map[uint64]func(msg []byte)),
		pendingQueue:       list.New(),
		network:            network,
        ingressChannel:     network.RegisterDirectProtocolWithChannel(name, c),
		msgRequestHandlers: make(map[MessageType]func(msg []byte) []byte),
		requestLifetime:    requestLifetime,
		exit:               make(chan struct{}),
        workerLimiter:      make(chan int, runtime.NumCPU()),
	}

	go p.readLoop()
	return p
}

func (p *MessageServer) Close() {
	p.exit <- struct{}{}
	<-p.exit
	p.workerCount.Wait()
}

func (p *MessageServer) readLoop() {
	for {
		timer := time.NewTicker(10 * time.Second)
		select {
		case <-p.exit:
			p.Debug("shutting down protocol ", p.name)
			close(p.exit)
			return
		case <-timer.C:
			go p.cleanStaleMessages()
		case msg, ok := <-p.ingressChannel:
			if !ok {
				p.Error("read loop channel was closed")
				break
			}

            p.workerLimiter <- 1
			p.workerCount.Add(1)
			go func() {
				defer p.workerCount.Done()
				p.handleMessage(msg.(Message))
                <-p.workerLimiter
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
				p.Debug("cleanStaleMessages remove request ", item.id)
				p.removeFromPending(item.id)
			} else {
				return
			}
		}
	}
}

func (p *MessageServer) removeFromPending(reqID uint64) {
	var next *list.Element
	p.pendMutex.Lock()
	for e := p.pendingQueue.Front(); e != nil; e = next {
		next = e.Next()
		if reqID == e.Value.(Item).id {
			p.pendingQueue.Remove(e)
			break
		}
	}
	delete(p.resHandlers, reqID)
	p.pendMutex.Unlock()
}

func (p *MessageServer) handleMessage(msg Message) {
	data := msg.Data().(*service.DataMsgWrapper)
	if data.Req {
		p.handleRequestMessage(msg.Sender(), data)
	} else {
		p.handleResponseMessage(data)
	}
}

func (p *MessageServer) handleRequestMessage(sender p2pcrypto.PublicKey, headers *service.DataMsgWrapper) {
	if payload := p.msgRequestHandlers[MessageType(headers.MsgType)](headers.Payload); payload != nil {
		rmsg := &service.DataMsgWrapper{MsgType: headers.MsgType, ReqID: headers.ReqID, Payload: payload}
		sendErr := p.network.SendWrappedMessage(sender, p.name, rmsg)
		if sendErr != nil {
			p.Error("Error sending response message, err:", sendErr)
		}
	}
}

func (p *MessageServer) handleResponseMessage(headers *service.DataMsgWrapper) {
	//get and remove from pendingMap
	p.pendMutex.Lock()
	foo, okFoo := p.resHandlers[headers.ReqID]
	p.pendMutex.Unlock()
	p.removeFromPending(headers.ReqID)
	if okFoo {
		foo(headers.Payload)
	}
}

func (p *MessageServer) RegisterMsgHandler(msgType MessageType, reqHandler func(msg []byte) []byte) {
	p.msgRequestHandlers[msgType] = reqHandler
}

func (p *MessageServer) SendRequest(msgType MessageType, payload []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte)) error {
	reqID := p.newRequestId()
	p.pendMutex.Lock()
	p.resHandlers[reqID] = resHandler
	p.pendingQueue.PushBack(Item{id: reqID, timestamp: time.Now()})
	p.pendMutex.Unlock()
	msg := &service.DataMsgWrapper{Req: true, ReqID: reqID, MsgType: uint32(msgType), Payload: payload}
	if sendErr := p.network.SendWrappedMessage(address, p.name, msg); sendErr != nil {
		p.Error("sending message failed ", msg, " error: ", sendErr)
		p.removeFromPending(reqID)
		return sendErr
	}
	return nil
}

func (p *MessageServer) newRequestId() uint64 {
	return atomic.AddUint64(&p.ReqId, 1)
}
