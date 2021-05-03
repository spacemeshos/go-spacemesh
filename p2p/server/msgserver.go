// Package server is used to wrap the p2p services to define multiple req-res messages under one protocol.
package server

import (
	"container/list"
	"context"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// MessageType is a uint32 used to distinguish between server messages inside a single protocol.
type MessageType uint32

// Message is helper type for `MessegeServer` messages.
type Message interface {
	service.DirectMessage
	Data() service.Data
}

func extractPayload(m Message) []byte {
	data := m.Data().(*service.DataMsgWrapper)
	return data.Payload
}

// Item is queue entry used to match responds to sent requests.
type Item struct {
	id        uint64
	timestamp time.Time
}

// ResponseHandlers contains handlers for received response handlers
type ResponseHandlers struct {
	okCallback   func(msg []byte)
	failCallBack func(err error)
}

// MessageServer is a request-response multiplexer on top of the p2p layer. it provides a way to register
// message types on top of a protocol and declare request and response handlers. it matches incoming responses to requests.
type MessageServer struct {
	log.Log
	ReqID              uint64 //request id
	name               string //server name
	network            Service
	pendMutex          sync.RWMutex
	pendingQueue       *list.List                                            //queue of pending messages
	resHandlers        map[uint64]ResponseHandlers                           //response handlers by request ReqID
	msgRequestHandlers map[MessageType]func(context.Context, Message) []byte //request handlers by request type
	ingressChannel     chan service.DirectMessage                            //chan to relay messages into the server
	requestLifetime    time.Duration                                         //time a request can stay in the pending queue until evicted
	workerCount        sync.WaitGroup
	workerLimiter      chan struct{}
	exit               chan struct{}
}

// Service is the subset of method used by MessageServer for p2p communications.
type Service interface {
	RegisterDirectProtocolWithChannel(protocol string, ingressChannel chan service.DirectMessage) chan service.DirectMessage
	SendWrappedMessage(ctx context.Context, nodeID p2pcrypto.PublicKey, protocol string, payload *service.DataMsgWrapper) error
}

// NewMsgServer registers a protocol and returns a new server to declare request and response handlers on.
func NewMsgServer(ctx context.Context, network Service, name string, requestLifetime time.Duration, c chan service.DirectMessage, logger log.Log) *MessageServer {
	p := &MessageServer{
		Log:                logger,
		name:               name,
		resHandlers:        make(map[uint64]ResponseHandlers),
		pendingQueue:       list.New(),
		network:            network,
		ingressChannel:     network.RegisterDirectProtocolWithChannel(name, c),
		msgRequestHandlers: make(map[MessageType]func(context.Context, Message) []byte),
		requestLifetime:    requestLifetime,
		exit:               make(chan struct{}),
		workerLimiter:      make(chan struct{}, runtime.NumCPU()),
	}

	go p.readLoop(log.WithNewSessionID(ctx))
	return p
}

// Close stops the MessageServer
func (p *MessageServer) Close() {
	p.exit <- struct{}{}
	<-p.exit
	p.workerCount.Wait()
}

// readLoop reads incoming messages and matches them to requests or responses.
func (p *MessageServer) readLoop(ctx context.Context) {
	timer := time.NewTicker(p.requestLifetime + time.Millisecond*100)
	defer timer.Stop()
	for {
		select {
		case <-p.exit:
			p.With().Debug("shutting down protocol", log.String("protocol", p.name))
			close(p.exit)
			return
		case <-timer.C:
			go p.cleanStaleMessages()
		case msg, ok := <-p.ingressChannel:
			// generate new reqID for message
			ctx := log.WithNewRequestID(ctx)
			p.WithContext(ctx).Debug("new msg received from channel")
			if !ok {
				p.WithContext(ctx).Error("read loop channel was closed")
				return
			}
			p.workerCount.Add(1)
			p.workerLimiter <- struct{}{}
			go func(msg Message) {
				p.handleMessage(ctx, msg)
				<-p.workerLimiter
				p.workerCount.Done()
			}(msg.(Message))
		}
	}
}

// clean stale messages after request life time expires
func (p *MessageServer) cleanStaleMessages() {
	for {
		p.pendMutex.RLock()
		p.With().Debug("checking for stale messages in msgserver queue",
			log.Int("queue_length", p.pendingQueue.Len()))
		elem := p.pendingQueue.Front()
		p.pendMutex.RUnlock()
		if elem != nil {
			item := elem.Value.(Item)
			if time.Since(item.timestamp) > p.requestLifetime {
				p.With().Debug("cleanStaleMessages remove request", log.Uint64("id", item.id))
				p.pendMutex.RLock()
				foo, okFoo := p.resHandlers[item.id]
				p.pendMutex.RUnlock()
				if okFoo {
					foo.failCallBack(fmt.Errorf("response timeout"))
				}
				p.removeFromPending(item.id)
			} else {
				p.Debug("cleanStaleMessages no more stale messages")
				return
			}
		} else {
			p.Debug("cleanStaleMessages queue empty")
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
			p.With().Debug("removed request", log.Uint64("p2p_request_id", reqID))
			break
		}
	}
	p.With().Debug("delete request result handler", log.Uint64("p2p_request_id", reqID))
	delete(p.resHandlers, reqID)
	p.pendMutex.Unlock()
}

func (p *MessageServer) handleMessage(ctx context.Context, msg Message) {
	data := msg.Data().(*service.DataMsgWrapper)
	if data.Req {
		p.handleRequestMessage(ctx, msg, data)
	} else {
		p.handleResponseMessage(ctx, data)
	}
}

func (p *MessageServer) handleRequestMessage(ctx context.Context, msg Message, data *service.DataMsgWrapper) {
	logger := p.WithContext(ctx)
	logger.Debug("handleRequestMessage start")

	foo, okFoo := p.msgRequestHandlers[MessageType(data.MsgType)]
	if !okFoo {
		logger.With().Error("handler missing for request",
			log.Uint64("p2p_request_id", data.ReqID),
			log.String("protocol", p.name),
			log.Uint32("p2p_msg_type", data.MsgType))
		return
	}

	logger.With().Debug("handle request", log.Uint32("p2p_msg_type", data.MsgType))
	rmsg := &service.DataMsgWrapper{MsgType: data.MsgType, ReqID: data.ReqID, Payload: foo(ctx, msg)}
	if sendErr := p.network.SendWrappedMessage(ctx, msg.Sender(), p.name, rmsg); sendErr != nil {
		logger.With().Error("error sending response message", log.Err(sendErr))
	}
	logger.Debug("handleRequestMessage close")
}

func (p *MessageServer) handleResponseMessage(ctx context.Context, headers *service.DataMsgWrapper) {
	logger := p.WithContext(ctx)

	// get and remove from pendingMap
	logger.With().Debug("handleResponseMessage", log.Uint64("p2p_request_id", headers.ReqID))
	p.pendMutex.RLock()
	foo, okFoo := p.resHandlers[headers.ReqID]
	p.pendMutex.RUnlock()
	p.removeFromPending(headers.ReqID)
	if okFoo {
		foo.okCallback(headers.Payload)
	} else {
		logger.With().Error("can't find handler", log.Uint64("p2p_request_id", headers.ReqID))
	}
	logger.Debug("handleResponseMessage close")
}

// RegisterMsgHandler sets the handler to act on a specific message request.
func (p *MessageServer) RegisterMsgHandler(msgType MessageType, reqHandler func(context.Context, Message) []byte) {
	p.msgRequestHandlers[msgType] = reqHandler
}

func handlerFromBytesHandler(in func(context.Context, []byte) []byte) func(context.Context, Message) []byte {
	return func(ctx context.Context, message Message) []byte {
		payload := extractPayload(message)
		return in(ctx, payload)
	}
}

// RegisterBytesMsgHandler sets the handler to act on a specific message request.
func (p *MessageServer) RegisterBytesMsgHandler(msgType MessageType, reqHandler func(context.Context, []byte) []byte) {
	p.RegisterMsgHandler(msgType, handlerFromBytesHandler(reqHandler))
}

// SendRequest sends a request of a specific message.
func (p *MessageServer) SendRequest(ctx context.Context, msgType MessageType, payload []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte), timeoutHandler func(err error)) error {
	reqID := p.newReqID()

	// Add requestID to context
	ctx = log.WithNewRequestID(ctx,
		log.Uint64("p2p_request_id", reqID),
		log.Uint32("p2p_msg_type", uint32(msgType)),
		log.FieldNamed("recipient", address))
	p.pendMutex.Lock()
	p.resHandlers[reqID] = ResponseHandlers{resHandler, timeoutHandler}
	p.pendingQueue.PushBack(Item{id: reqID, timestamp: time.Now()})
	p.pendMutex.Unlock()
	msg := &service.DataMsgWrapper{Req: true, ReqID: reqID, MsgType: uint32(msgType), Payload: payload}
	if err := p.network.SendWrappedMessage(ctx, address, p.name, msg); err != nil {
		p.WithContext(ctx).With().Error("sending message failed",
			log.Int("msglen", len(payload)),
			log.Err(err))
		p.removeFromPending(reqID)
		return err
	}
	p.WithContext(ctx).Debug("sent request")
	return nil
}

// TODO: make these longer, and random, to make it easier to find them in the logs
func (p *MessageServer) newReqID() uint64 {
	return atomic.AddUint64(&p.ReqID, 1)
}
