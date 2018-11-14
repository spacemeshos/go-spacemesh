package p2p

import (
	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"sync"
	"sync/atomic"
	"time"
)

type MessageType uint32

type Protocol struct {
	count              uint32
	name               string
	network            Service
	pendMutex          sync.RWMutex
	pending            map[uint32]chan interface{}
	resHandlers        map[uint32]func(msg []byte)
	msgRequestHandlers map[MessageType]func(msg []byte) []byte
	ingressChannel     chan service.Message
}

func NewProtocol(network Service, name string) *Protocol {
	p := &Protocol{
		name:               name,
		pending:            make(map[uint32]chan interface{}),
		resHandlers:        make(map[uint32]func(msg []byte)),
		network:            network,
		ingressChannel:     network.RegisterProtocol(name),
		msgRequestHandlers: make(map[MessageType]func(msg []byte) []byte),
	}
	go p.readLoop()
	return p
}

func (p *Protocol) readLoop() {
	for {
		msg, ok := <-p.ingressChannel
		if !ok {
			// Channel is closed.
			break
		}
		//todo add buffer and option to limit number of concurrent goroutines
		go p.handleMessage(msg)
	}
}

func (p *Protocol) handleMessage(msg service.Message) {

	headers := &pb.MessageWrapper{}

	if err := proto.Unmarshal(msg.Data(), headers); err != nil {
		log.Error("Error handling incoming ", p.name, " message, ", "request", headers, "err:", err)
		return
	}

	if headers.Req {
		p.handleRequestMessage(msg.Sender().String(), headers)
	} else {
		p.handleResponseMessage(headers)
	}

}

func (p *Protocol) handleRequestMessage(sender string, headers *pb.MessageWrapper) {

	if payload := p.msgRequestHandlers[MessageType(headers.Type)](headers.Payload); payload != nil {
		rmsg, fParseErr := proto.Marshal(&pb.MessageWrapper{Req: false, ReqID: headers.ReqID, Type: headers.Type, Payload: payload})
		if fParseErr != nil {
			log.Error("Error Parsing Protocol message, err:", fParseErr)
			return
		}
		sendErr := p.network.SendMessage(sender, p.name, rmsg)
		if sendErr != nil {
			log.Error("Error sending response message, err:", sendErr)
		}
	}
}

func (p *Protocol) handleResponseMessage(headers *pb.MessageWrapper) {

	//get and remove from pending
	p.pendMutex.RLock()
	pend, okPend := p.pending[headers.ReqID]
	foo, okFoo := p.resHandlers[headers.ReqID]
	delete(p.pending, headers.ReqID)
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

func (p *Protocol) removeFromPending(reqID uint32) {
	p.pendMutex.Lock()
	delete(p.pending, reqID)
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
	p.pending[reqID] = respc
	p.resHandlers[reqID] = resHandler
	p.pendMutex.Unlock()

	if sendErr := p.network.SendMessage(address, p.name, msg); sendErr != nil {
		p.removeFromPending(reqID)
		return sendErr
	}

	return nil
}

func (p *Protocol) newRequestId() uint32 {
	return atomic.AddUint32(&p.count, 1)
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
	p.pending[reqID] = respc
	p.pendMutex.Unlock()

	defer p.removeFromPending(reqID)

	if sendErr := p.network.SendMessage(address, p.name, msg); sendErr != nil {
		return nil, sendErr
	}

	timer := time.NewTimer(timeout)
	select {
	case response := <-respc:
		if response != nil {
			return response, nil
		}
		return nil, errors.New("could not find block")
	case <-timer.C:
		err = errors.New("fetch block took too long to respond")
	}

	return nil, err
}
