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

type Message interface {
	service.DirectMessage
	Data() service.Data
}

func extractPayload(m Message) []byte {
	data := m.Data().(*service.DataMsgWrapper)
	return data.Payload
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
	pendingQueue       *list.List                                   //queue of pending messages
	resHandlers        map[uint64]func(msg []byte)                  //response handlers by request ReqId
	msgRequestHandlers map[MessageType]func(message Message) []byte //request handlers by request type
	ingressChannel     chan service.DirectMessage                   //chan to relay messages into the server
	requestLifetime    time.Duration                                //time a request can stay in the pending queue until evicted
	workerCount        sync.WaitGroup
	workerLimiter      chan struct{}
	exit               chan struct{}
}

type Service interface {
	RegisterDirectProtocolWithChannel(protocol string, ingressChannel chan service.DirectMessage) chan service.DirectMessage
	SendWrappedMessage(nodeID p2pcrypto.PublicKey, protocol string, payload *service.DataMsgWrapper) error
}

func NewMsgServer(network Service, name string, requestLifetime time.Duration, c chan service.DirectMessage, logger log.Log) *MessageServer {
	p := &MessageServer{
		Log:                logger,
		name:               name,
		resHandlers:        make(map[uint64]func(msg []byte)),
		pendingQueue:       list.New(),
		network:            network,
		ingressChannel:     network.RegisterDirectProtocolWithChannel(name, c),
		msgRequestHandlers: make(map[MessageType]func(message Message) []byte),
		requestLifetime:    requestLifetime,
		exit:               make(chan struct{}),
		workerLimiter:      make(chan struct{}, runtime.NumCPU()),
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
	timer := time.NewTicker(p.requestLifetime + time.Millisecond*100)
	for {
		select {
		case <-p.exit:
			p.Debug("shutting down protocol ", p.name)
			close(p.exit)
			return
		case <-timer.C:
			go p.cleanStaleMessages()
		case msg, ok := <-p.ingressChannel:
			p.Debug("new msg received from channel")
			if !ok {
				p.Error("read loop channel was closed")
				return
			}
			p.workerCount.Add(1)
			p.workerLimiter <- struct{}{}
			go func(msg Message) {
				p.handleMessage(msg)
				<-p.workerLimiter
				p.workerCount.Done()
			}(msg.(Message))

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
				p.Debug("cleanStaleMessages no more stale messages ")
				return
			}
		} else {
			p.Debug("cleanStaleMessages queue empty ")
			return
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
			p.Debug("removed request ", e.Value.(Item).id)
			break
		}
	}
	p.Debug("delete request result %v handler", reqID)
	delete(p.resHandlers, reqID)
	p.pendMutex.Unlock()
}

func (p *MessageServer) handleMessage(msg Message) {
	data := msg.Data().(*service.DataMsgWrapper)
	if data.Req {
		p.handleRequestMessage(msg, data)
	} else {
		p.handleResponseMessage(data)
	}
}

func (p *MessageServer) handleRequestMessage(msg Message, data *service.DataMsgWrapper) {
	p.Debug("handleRequestMessage start")

	foo, okFoo := p.msgRequestHandlers[MessageType(data.MsgType)]
	if !okFoo {
		p.Error("handler missing for request %v payload %v", data.ReqID)
		return
	}

	p.Debug("handle request type %v", data.MsgType)
	rmsg := &service.DataMsgWrapper{MsgType: data.MsgType, ReqID: data.ReqID, Payload: foo(msg)}
	if sendErr := p.network.SendWrappedMessage(msg.Sender(), p.name, rmsg); sendErr != nil {
		p.Error("Error sending response message, err:", sendErr)
	}
	p.Debug("handleRequestMessage close")
}

func (p *MessageServer) handleResponseMessage(headers *service.DataMsgWrapper) {
	//get and remove from pendingMap
	p.Log.With().Debug("handleResponseMessage", log.Uint64("req_id", headers.ReqID))
	p.pendMutex.RLock()
	foo, okFoo := p.resHandlers[headers.ReqID]
	p.pendMutex.RUnlock()
	p.removeFromPending(headers.ReqID)
	if okFoo {
		foo(headers.Payload)
	} else {
		p.Error("Cant find handler %v", headers.ReqID)
	}
	p.Debug("handleResponseMessage close")
}

func (p *MessageServer) RegisterMsgHandler(msgType MessageType, reqHandler func(message Message) []byte) {
	p.msgRequestHandlers[msgType] = reqHandler
}

func handlerFromBytesHandler(in func(msg []byte) []byte) func(message Message) []byte {
	return func(message Message) []byte {
		payload := extractPayload(message)
		return in(payload)
	}
}

func (p *MessageServer) RegisterBytesMsgHandler(msgType MessageType, reqHandler func([]byte) []byte) {
	p.RegisterMsgHandler(msgType, handlerFromBytesHandler(reqHandler))
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
	p.Log.With().Debug("sent request", log.Uint64("req_id", reqID))
	return nil
}

func (p *MessageServer) newRequestId() uint64 {
	return atomic.AddUint64(&p.ReqId, 1)
}
