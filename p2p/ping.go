package p2p

import (
	"encoding/hex"
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/p2p/pb"
	"github.com/gogo/protobuf/proto"
)

const pingReq = "/ping/1.0/ping-req/"
const pingResp = "/ping/1.0/ping-resp/"

// Ping protocol
// An example of a simple app-level p2p protocol
type Ping interface {
	// send a ping request to a remoteNode
	// reqId: allows the client to match responses with requests by id
	Send(msg string, reqId []byte, remoteNodeId string) error

	// App logic registers her for typed incoming ping responses (pongs)
	Register(callback chan *pb.PingRespData)
}

type Callbacks chan chan *pb.PingRespData

type pingProtocolImpl struct {
	swarm             Swarm
	callbacks         []chan *pb.PingRespData
	incomingRequests  MessagesChan
	incomingResponses MessagesChan
	callbacksRegReq   Callbacks // a channel of channels that receive callbacks
}

func NewPingProtocol(s Swarm) Ping {

	p := &pingProtocolImpl{
		swarm:             s,
		incomingRequests:  make(MessagesChan, 10),
		incomingResponses: make(MessagesChan, 10),
		callbacksRegReq:   make(Callbacks, 10),
		callbacks:         make([]chan *pb.PingRespData, 0), // start with empty slice
	}

	go p.processEvents()

	// demuxer registration
	s.GetDemuxer().RegisterProtocolHandler(ProtocolRegistration{pingReq, p.incomingRequests})
	s.GetDemuxer().RegisterProtocolHandler(ProtocolRegistration{pingResp, p.incomingResponses})

	return p
}

// Send a ping to a remote node
func (p *pingProtocolImpl) Send(msg string, reqId []byte, remoteNodeId string) error {

	metadata := p.swarm.GetLocalNode().NewProtocolMessageMetadata(pingReq, reqId, false)
	data := &pb.PingReqData{metadata, msg}

	// sign data
	sign, err := p.swarm.GetLocalNode().Sign(data)
	data.Metadata.AuthorSign = hex.EncodeToString(sign)

	// marshal the signed data
	payload, err := proto.Marshal(data)
	if err != nil {
		return err
	}

	// send the message
	req := SendMessageReq{remoteNodeId, reqId, payload}
	p.swarm.SendMessage(req)

	return nil
}

// Register callback on remotely received pings
func (p *pingProtocolImpl) Register(callback chan *pb.PingRespData) {
	p.callbacksRegReq <- callback
}

// Handles a ping request from a remote node
// Process the request and send back a ping response (pong) to the remote node
func (p *pingProtocolImpl) handleIncomingRequest(msg IncomingMessage) {

	// process request
	req := &pb.PingReqData{}
	err := proto.Unmarshal(msg.Payload(), req)
	if err != nil {
		log.Warning("Invalid ping request data: %v", err)
		return
	}

	peer := msg.Sender()
	log.Info("Incoming ping peer request from %s. Message: %", peer.Pretty(), req.Ping)

	// generate response
	metadata := p.swarm.GetLocalNode().NewProtocolMessageMetadata(pingResp, req.Metadata.ReqId, false)
	respData := &pb.PingRespData{metadata, req.Ping}

	// sign response
	sign, err := p.swarm.GetLocalNode().SignToString(respData)
	if err != nil {
		log.Info("Failed to sign response")
		return
	}

	respData.Metadata.AuthorSign = sign

	// marshal the signed data
	signedPayload, err := proto.Marshal(respData)
	if err != nil {
		log.Info("Failed to generate response data")
		return
	}

	// send signed data payload
	resp := SendMessageReq{msg.Sender().String(),
		req.Metadata.ReqId,
		signedPayload}

	p.swarm.SendMessage(resp)
}

// Handle an incoming pong message from a remote node
func (p *pingProtocolImpl) handleIncomingResponse(msg IncomingMessage) {

	// process request
	resp := &pb.PingRespData{}
	err := proto.Unmarshal(msg.Payload(), resp)
	if err != nil {
		log.Warning("Invalid ping request data: %v", err)
		return
	}

	log.Info("Got pong response from %s. Ping req id: %", msg.Sender().Pretty(), resp.Pong, resp.Metadata.ReqId)

	// notify clients of th new pong
	for _, c := range p.callbacks {
		// todo: verify that this style of closure is kosher
		go func() { c <- resp }()
	}
}

// Internal event processing loop
func (p *pingProtocolImpl) processEvents() {
	for {
		select {
		case r := <-p.incomingRequests:
			p.handleIncomingRequest(r)

		case r := <-p.incomingResponses:
			p.handleIncomingResponse(r)

		case c := <-p.callbacksRegReq:
			p.callbacks = append(p.callbacks, c)
		}
	}
}
