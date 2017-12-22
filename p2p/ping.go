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
	Send(msg string, reqId []byte, remoteNodeId string)

	// App logic registers her for typed incoming ping responses (pongs) or send errors
	Register(callback chan SendPingResp)
}

// Send ping op response - contains response data or error
type SendPingResp struct {
	*pb.PingRespData
	err   error
	reqId []byte
}

type Callbacks chan chan SendPingResp

type pingProtocolImpl struct {
	swarm Swarm

	callbacks []chan SendPingResp

	incomingRequests  MessagesChan
	incomingResponses MessagesChan
	sendErrors        chan SendError
	callbacksRegReq   Callbacks // a channel of channels that receive callbacks to send ping
}

func NewPingProtocol(s Swarm) Ping {

	p := &pingProtocolImpl{
		swarm:             s,
		incomingRequests:  make(MessagesChan, 10),
		incomingResponses: make(MessagesChan, 10),
		callbacksRegReq:   make(Callbacks, 10),
		sendErrors:        make(chan SendError, 3),
		callbacks:         make([]chan SendPingResp, 0), // start with empty slice
	}

	go p.processEvents()

	// demuxer registration
	s.GetDemuxer().RegisterProtocolHandler(ProtocolRegistration{pingReq, p.incomingRequests})
	s.GetDemuxer().RegisterProtocolHandler(ProtocolRegistration{pingResp, p.incomingResponses})

	return p
}

// Send a ping to a remote node
func (p *pingProtocolImpl) Send(msg string, reqId []byte, remoteNodeId string) {

	metadata := p.swarm.GetLocalNode().NewProtocolMessageMetadata(pingReq, reqId, false)
	data := &pb.PingReqData{metadata, msg}

	// sign data
	sign, err := p.swarm.GetLocalNode().Sign(data)
	data.Metadata.AuthorSign = hex.EncodeToString(sign)

	// marshal the signed data
	payload, err := proto.Marshal(data)
	if err != nil {
		return
	}

	req := SendMessageReq{remoteNodeId, reqId, payload, p.sendErrors}

	p.swarm.SendMessage(req)
}

// Register callback on remotely received pings
func (p *pingProtocolImpl) Register(callback chan SendPingResp) {
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

	// add/update local dht table
	p.swarm.getRoutingTable().Update(peer.GetRemoteNodeData())

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
		signedPayload,
		nil}

	p.swarm.SendMessage(resp)
}

// Handle an incoming pong message from a remote node
func (p *pingProtocolImpl) handleIncomingResponse(msg IncomingMessage) {

	// process request
	data := &pb.PingRespData{}
	err := proto.Unmarshal(msg.Payload(), data)
	if err != nil {
		// we can't extract the request id from the response so we just terminate
		// without sending a callback - this case should be handeled as a timeout
		log.Error("Invalid ping request data: %v", err)
		return
	}

	log.Info("Got pong response from %s. Ping req id: %", msg.Sender().Pretty(), data.Pong, data.Metadata.ReqId)

	// according to kad receiving a ping response should update sender in local dht table
	p.swarm.getRoutingTable().Update(msg.Sender().GetRemoteNodeData())

	resp := SendPingResp{data, err, data.Metadata.ReqId}

	// notify clients of the new pong or error
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

		case r := <-p.sendErrors:
			resp := SendPingResp{nil, r.err, r.ReqId}
			for _, c := range p.callbacks {
				go func() { c <- resp }()
			}
		}
	}
}
