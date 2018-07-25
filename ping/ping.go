package ping

import (
	"github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/ping/pb"
	"sync"
	"time"
)

type Pinger interface {
	PublicKey() crypto.PublicKey
}

const protocol = "/ping/1.0/"
const PingTimeout = time.Second * 10 // TODO: Parametrize

var responses = map[string]string{"hello": "world"}
var responseMutex sync.RWMutex

func AddResponse(req, res string) {
	responseMutex.Lock()
	responses[req] = res
	responseMutex.Unlock()
}

var ErrPingTimedOut = errors.New("Ping took too long to response")

type Ping struct {
	p2p p2p.Service

	pending    map[crypto.UUID]chan *pb.Ping
	pendMuxtex sync.RWMutex

	ingressChannel chan service.Message
}

func New(p2p p2p.Service) *Ping {
	p := &Ping{pending: make(map[crypto.UUID]chan *pb.Ping)}
	p.p2p = p2p
	p.ingressChannel = p2p.RegisterProtocol(protocol)
	go p.readLoop()
	return p
}

func (p *Ping) readLoop() {
	for {
		msg, ok := <-p.ingressChannel
		if !ok {
			// Channel is closed.
			break
		}

		go func(msg service.Message) {
			ping := &pb.Ping{}
			err := proto.Unmarshal(msg.Data(), ping)
			if err != nil {
				log.Error("failed to read incoming ping message err:", err)
				// TODO : handle errors in readloop
			}

			if ping.Req {
				log.Info("Ping: Request from (%v) - Message : %v", msg.Sender().Pretty(), ping.Message)
				err := p.handleRequest(msg.Sender(), ping)
				if err != nil {
					log.Error("Error handling ping request", err)
				}
				return
			}

			p.handleResponse(ping)

		}(msg)
	}
}

func (p *Ping) Ping(target, msg string) (string, error) {
	var response string
	reqid := crypto.NewUUID()
	ping := &pb.Ping{
		ReqID:   reqid[:],
		Req:     true,
		Message: msg,
	}
	pchan, err := p.sendRequest(target, reqid, ping)
	if err != nil {
		return response, err
	}

	timer := time.NewTimer(PingTimeout)
	select {
	case res := <-pchan:
		response = res.Message
		p.pendMuxtex.Lock()
		delete(p.pending, reqid)
		p.pendMuxtex.Unlock()
	case <-timer.C:
		return response, ErrPingTimedOut
	}

	return response, nil
}

func (p *Ping) sendRequest(target string, reqid crypto.UUID, ping *pb.Ping) (chan *pb.Ping, error) {
	pchan := make(chan *pb.Ping)
	p.pendMuxtex.Lock()
	p.pending[reqid] = pchan
	p.pendMuxtex.Unlock()

	remove := func() {
		p.pendMuxtex.Lock()
		delete(p.pending, reqid)
		p.pendMuxtex.Unlock()
	}

	payload, err := proto.Marshal(ping)
	if err != nil {
		remove()
		return nil, err
	}

	err = p.p2p.SendMessage(target, protocol, payload)
	if err != nil {
		remove()
		return nil, err
	}

	return pchan, nil
}

func (p *Ping) handleRequest(sender Pinger, ping *pb.Ping) error {
	responseMutex.RLock()
	resp, ok := responses[ping.Message]
	responseMutex.RUnlock()

	if !ok {
		resp = ping.Message
	}

	pingResp := &pb.Ping{
		ReqID:   ping.ReqID,
		Req:     false,
		Message: resp,
	}

	bin, err := proto.Marshal(pingResp)
	if err != nil {
		return err
	}
	log.Debug("Ping: Responding with %v", resp)
	return p.p2p.SendMessage(sender.PublicKey().String(), protocol, bin)
}

func (p *Ping) handleResponse(ping *pb.Ping) {
	reqid := ping.ReqID
	var creqid crypto.UUID
	copy(creqid[:], reqid)
	p.pendMuxtex.RLock()
	c, ok := p.pending[creqid]
	p.pendMuxtex.RUnlock()
	if ok {
		c <- ping
		p.pendMuxtex.Lock()
		delete(p.pending, creqid)
		p.pendMuxtex.Unlock()
	}
}

//
//import (
//	"encoding/hex"
//
//	"github.com/gogo/protobuf/proto"
//	"github.com/spacemeshos/go-spacemesh/ping/pb"
//)
//
//const pingReq = "/ping/1.0/ping-req/"
//const pingResp = "/ping/1.0/ping-resp/"
//
//// Ping protocol
//// An example of a simple app-level p2p protocol
//
//type Ping struct {
//
//	pending []chan SendPingResp
//
//	incomingRequests  MessagesChan
//	incomingResponses MessagesChan
//	sendErrors        chan SendError
//	callbacksRegReq   Callbacks // a channel of channels that receive callbacks to send ping
//}
//
//// NewPingProtocol creates a new Ping protocol implementation
//func NewPingProtocol(s swarm) Ping {
//
//	p := &pingProtocolImpl{
//		swarm:             s,
//		incomingRequests:  make(MessagesChan, 10),
//		incomingResponses: make(MessagesChan, 10),
//		callbacksRegReq:   make(Callbacks, 10),
//		sendErrors:        make(chan SendError, 3),
//		callbacks:         make([]chan SendPingResp, 0), // start with empty slice
//	}
//
//	go p.processEvents()
//
//	// demuxer registration
//	s.GetDemuxer().RegisterProtocolHandler(ProtocolRegistration{pingReq, p.incomingRequests})
//	s.GetDemuxer().RegisterProtocolHandler(ProtocolRegistration{pingResp, p.incomingResponses})
//
//	return p
//}
//
//// Send a ping to a remote node
//func (p *pingProtocolImpl) Send(msg string, reqID []byte, remoteNodeID string) {
//
//	metadata := newProtocolMessageMetadata(p.swarm.localNode().PublicKey(), pingReq, reqID, false)
//	data := &pb.PingReqData{Metadata: metadata, Ping: msg}
//
//	tosign, err := proto.Marshal(data)
//	if err != nil {
//		p.swarm.localNode().Error("Encoding protobuf failed err:", err)
//		return
//	}
//
//	// sign data
//	sign, err := p.swarm.localNode().PrivateKey().Sign(tosign)
//	if err != nil {
//		p.swarm.localNode().Error("Signing payload failed err:", err)
//		return
//	}
//	data.Metadata.AuthorSign = hex.EncodeToString(sign)
//
//	// marshal the signed data
//	payload, err := proto.Marshal(data)
//	if err != nil {
//		p.swarm.localNode().Error("failed to marshal data", err)
//		return
//	}
//
//	p.swarm.localNode().Info("Sending Ping message to (%v)", remoteNodeID)
//	req := SendMessageReq{remoteNodeID, reqID, payload, p.sendErrors}
//	p.swarm.SendMessage(req)
//}
//
//// SubscribeOnNewConnections callback on remotely received pings
//func (p *pingProtocolImpl) Register(callback chan SendPingResp) {
//	p.callbacksRegReq <- callback
//}
//
//// Handles a ping request from a remote node
//// Process the request and send back a ping response (pong) to the remote node
//func (p *pingProtocolImpl) handleIncomingRequest(msg IncomingMessage) {
//
//	// process request
//	req := &pb.PingReqData{}
//	err := proto.Unmarshal(msg.Payload(), req)
//	if err != nil {
//		p.swarm.localNode().Warning("Invalid ping request data", err)
//		return
//	}
//
//	peer := msg.Sender()
//
//	p.swarm.localNode().Info("Incoming ping peer request from %s. Message: %s", peer.Pretty(), req.Ping)
//
//	// add/update local dht table as we just heard from a peer
//	p.swarm.RoutingTable().Update(peer)
//
//	// generate response
//	metadata := newProtocolMessageMetadata(p.swarm.localNode().PublicKey(), pingResp, req.Metadata.ReqId, false)
//	respData := &pb.PingRespData{Metadata: metadata, Pong: req.Ping}
//
//	tosign, err := proto.Marshal(respData)
//	if err != nil {
//		p.swarm.localNode().Error("Encoding proto failed, err:", err)
//	}
//	// sign response
//	sign, err := p.swarm.localNode().PrivateKey().Sign(tosign)
//	if err != nil {
//		p.swarm.localNode().Error("Failed to sign response", err)
//		return
//	}
//
//	respData.Metadata.AuthorSign = hex.EncodeToString(sign)
//
//	// marshal the signed data
//	signedPayload, err := proto.Marshal(respData)
//	if err != nil {
//		p.swarm.localNode().Error("Failed to generate response data", err)
//		return
//	}
//
//	// send signed data payload
//	resp := SendMessageReq{msg.Sender().String(),
//		req.Metadata.ReqId,
//		signedPayload,
//		nil}
//
//	p.swarm.SendMessage(resp)
//}
//
//// Handle an incoming pong message from a remote node
//func (p *pingProtocolImpl) handleIncomingResponse(msg IncomingMessage) {
//
//	// process request
//	data := &pb.PingRespData{}
//	err := proto.Unmarshal(msg.Payload(), data)
//	if err != nil {
//		// we can't extract the request id from the response so we just terminate
//		// without sending a callback - this case should be handeled as a timeout
//		p.swarm.localNode().Error("Invalid ping request data", err)
//		return
//	}
//
//	p.swarm.localNode().Info("Got pong response `%v` from %s. Ping req id: %v", data.Pong, msg.Sender().Pretty(),
//		hex.EncodeToString(data.Metadata.ReqId))
//
//	// according to kad receiving a ping response should update sender in local dht table
//	p.swarm.RoutingTable().Update(msg.Sender())
//
//	resp := SendPingResp{data, err, data.Metadata.ReqId}
//
//	// notify clients of the new pong or error
//	for _, c := range p.callbacks {
//		go func(c chan SendPingResp) { c <- resp }(c)
//	}
//}
//
//// Internal event processing loop
//func (p *pingProtocolImpl) processEvents() {
//	for {
//		select {
//		case r := <-p.incomingRequests:
//			p.handleIncomingRequest(r)
//
//		case r := <-p.incomingResponses:
//			p.handleIncomingResponse(r)
//
//		case c := <-p.callbacksRegReq:
//			p.callbacks = append(p.callbacks, c)
//
//		case r := <-p.sendErrors:
//			resp := SendPingResp{nil, r.err, r.ReqID}
//			for _, c := range p.callbacks {
//				go func(c chan SendPingResp) { c <- resp }(c)
//			}
//		}
//	}
//}
