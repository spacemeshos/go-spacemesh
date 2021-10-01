// Package server is used to wrap the p2p services to define multiple req-res messages under one protocol.
package server

import (
	"container/list"
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"golang.org/x/sync/errgroup"
)

// MessageType is a uint32 used to distinguish between server messages inside a single protocol.
type MessageType uint32

const (
	// PingPong is the ping protocol ID
	PingPong MessageType = iota
	// GetAddresses is the findnode protocol ID
	GetAddresses
	// LayerBlocksMsg is used to fetch block IDs for a given layer hash
	LayerBlocksMsg
	// AtxIDsMsg is used to fetch ATXs for a given epoch
	AtxIDsMsg
	// Fetch is used to fetch data for a given hash
	Fetch
	// RequestTimeSync is used for time synchronization with peers.
	RequestTimeSync
)

// Message is helper type for `MessegeServer` messages.
type Message interface {
	service.DirectMessage
	Data() service.Data
}

func extractPayload(m Message) []byte {
	data := m.Data().(*service.DataMsgWrapper)
	return data.Payload
}

// item is queue entry used to match responds to sent requests.
type item struct {
	id        uint64
	timestamp time.Time
}

type responseHandlers struct {
	okCallback   func(msg []byte)
	failCallBack func(err error)
}

// ErrShuttingDown is returned to the peer when the node is shutting down
var ErrShuttingDown = errors.New("node is shutting down")

// ErrBadRequest is returned to the peer upon failure to parse the request
var ErrBadRequest = errors.New("unable to parse request")

// ErrRequestTimeout is returned to the caller when the request times out
var ErrRequestTimeout = errors.New("request timed out")

type response struct {
	Data []byte
	// use a string instead of an error because error is an interface and cannot be
	// serialized like concrete types.
	ErrorStr string
}

func (r *response) getError() error {
	if len(r.ErrorStr) > 0 {
		return errors.New(r.ErrorStr)
	}
	return nil
}

// SerializeResponse serializes the response data returned by SendRequest
func SerializeResponse(data []byte, err error) []byte {
	resp := response{Data: data}
	if err != nil {
		resp.ErrorStr = err.Error()
	}
	bytes, err := types.InterfaceToBytes(&resp)
	if err != nil {
		log.Panic("failed to serialize response", log.Err(err))
	}
	return bytes
}

// deserializeResponse deserializes the response data returned by SendRequest
func deserializeResponse(data []byte) (*response, error) {
	var resp response
	err := types.BytesToInterface(data, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// MessageServer is a request-response multiplexer on top of the p2p layer. it provides a way to register
// message types on top of a protocol and declare request and response handlers. it matches incoming responses to requests.
type MessageServer struct {
	ReqID              uint64 //request id (must be declared first to ensure 8 byte alignment on 32-bit systems, required by atomic operations)
	name               string //server name
	network            Service
	pendMutex          sync.RWMutex
	pendingQueue       *list.List                                                     //queue of pending messages
	resHandlers        map[uint64]responseHandlers                                    //response handlers by request ReqID
	msgRequestHandlers map[MessageType]func(context.Context, Message) ([]byte, error) //request handlers by request type
	ingressChannel     chan service.DirectMessage                                     //chan to relay messages into the server
	requestLifetime    time.Duration
	workerLimiter      chan struct{}
	eg                 errgroup.Group
	cancel             context.CancelFunc
	log.Log
}

// Service is the subset of method used by MessageServer for p2p communications.
type Service interface {
	RegisterDirectProtocolWithChannel(protocol string, ingressChannel chan service.DirectMessage) chan service.DirectMessage
	SendWrappedMessage(ctx context.Context, nodeID p2pcrypto.PublicKey, protocol string, payload *service.DataMsgWrapper) error
}

// NewMsgServer registers a protocol and returns a new server to declare request and response handlers on.
func NewMsgServer(ctx context.Context, network Service, name string, requestLifetime time.Duration, c chan service.DirectMessage, logger log.Log) *MessageServer {
	ctx, cancel := context.WithCancel(ctx)
	p := &MessageServer{
		Log:                logger,
		name:               name,
		resHandlers:        make(map[uint64]responseHandlers),
		pendingQueue:       list.New(),
		network:            network,
		ingressChannel:     network.RegisterDirectProtocolWithChannel(name, c),
		msgRequestHandlers: make(map[MessageType]func(context.Context, Message) ([]byte, error)),
		requestLifetime:    requestLifetime,
		cancel:             cancel,
		workerLimiter:      make(chan struct{}, runtime.NumCPU()),
	}
	p.eg.Go(func() error {
		return p.readLoop(ctx)
	})
	return p
}

// Close stops the MessageServer
func (p *MessageServer) Close() {
	p.With().Info("closing message server")
	p.cancel()
	p.With().Info("waiting for message workers to finish")
	p.eg.Wait()
	p.With().Info("message workers all done")
}

// readLoop reads incoming messages and matches them to requests or responses.
func (p *MessageServer) readLoop(ctx context.Context) error {
	sctx := log.WithNewSessionID(ctx)
	timer := time.NewTicker(p.requestLifetime + time.Millisecond*100)
	defer timer.Stop()
	defer p.With().Info("shutting down protocol", log.String("protocol", p.name))
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			p.eg.Go(func() error {
				p.cleanStaleMessages()
				return nil
			})
		case msg, ok := <-p.ingressChannel:
			// generate new reqID for message
			ctx := log.WithNewRequestID(ctx)
			p.WithContext(ctx).Debug("new msg received from channel")
			if !ok {
				p.WithContext(ctx).Error("read loop channel was closed")
				return context.Canceled
			}
			select {
			case p.workerLimiter <- struct{}{}:
				p.eg.Go(func() error {
					p.handleMessage(sctx, msg.(Message)) // pass session ctx to log session id
					<-p.workerLimiter
					return nil
				})
			case <-ctx.Done():
				return ctx.Err()
			}
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
			item := elem.Value.(item)
			if time.Since(item.timestamp) > p.requestLifetime {
				p.With().Debug("cleanStaleMessages remove request", log.Uint64("id", item.id))
				p.pendMutex.RLock()
				foo, okFoo := p.resHandlers[item.id]
				p.pendMutex.RUnlock()
				if okFoo {
					foo.failCallBack(ErrRequestTimeout)
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
		if reqID == e.Value.(item).id {
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

func (p *MessageServer) handleRequestMessage(ctx context.Context, msg Message, req *service.DataMsgWrapper) {
	logger := p.WithContext(ctx)
	logger.Debug("handleRequestMessage start")

	foo, okFoo := p.msgRequestHandlers[MessageType(req.MsgType)]
	if !okFoo {
		logger.With().Error("handler missing for request",
			log.Uint64("p2p_request_id", req.ReqID),
			log.String("protocol", p.name),
			log.Uint32("p2p_msg_type", req.MsgType))
		return
	}

	logger.With().Debug("handle request", log.Uint32("p2p_msg_type", req.MsgType))
	data, err := foo(ctx, msg)
	payload := SerializeResponse(data, err)
	resp := &service.DataMsgWrapper{MsgType: req.MsgType, ReqID: req.ReqID, Payload: payload}
	if sendErr := p.network.SendWrappedMessage(ctx, msg.Sender(), p.name, resp); sendErr != nil {
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
		resp, err := deserializeResponse(headers.Payload)
		if err != nil {
			logger.With().Warning("failed to deserialize response", log.Err(err))
			foo.failCallBack(err)
		} else {
			peerErr := resp.getError()
			if peerErr != nil {
				foo.failCallBack(peerErr)
			} else {
				foo.okCallback(resp.Data)
			}
		}
	} else {
		logger.With().Error("can't find handler", log.Uint64("p2p_request_id", headers.ReqID))
	}
	logger.Debug("handleResponseMessage close")
}

// RegisterMsgHandler sets the handler to act on a specific message request.
func (p *MessageServer) RegisterMsgHandler(msgType MessageType, reqHandler func(context.Context, Message) ([]byte, error)) {
	p.msgRequestHandlers[msgType] = reqHandler
}

func handlerFromBytesHandler(in func(context.Context, []byte) ([]byte, error)) func(context.Context, Message) ([]byte, error) {
	return func(ctx context.Context, message Message) ([]byte, error) {
		payload := extractPayload(message)
		return in(ctx, payload)
	}
}

// RegisterBytesMsgHandler sets the handler to act on a specific message request.
func (p *MessageServer) RegisterBytesMsgHandler(msgType MessageType, reqHandler func(context.Context, []byte) ([]byte, error)) {
	p.RegisterMsgHandler(msgType, handlerFromBytesHandler(reqHandler))
}

// SendRequest sends a request of a specific message.
func (p *MessageServer) SendRequest(ctx context.Context, msgType MessageType, payload []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte), errorHandler func(err error)) error {
	reqID := p.newReqID()

	// Add requestID to context
	ctx = log.WithNewRequestID(ctx,
		log.Uint64("p2p_request_id", reqID),
		log.Uint32("p2p_msg_type", uint32(msgType)),
		log.FieldNamed("recipient", address))
	p.pendMutex.Lock()
	p.resHandlers[reqID] = responseHandlers{resHandler, errorHandler}
	p.pendingQueue.PushBack(item{id: reqID, timestamp: time.Now()})
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
