package p2p

import (
	"encoding/hex"
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/p2p/dht"
	"github.com/UnrulyOS/go-unruly/p2p/dht/table"
	"github.com/UnrulyOS/go-unruly/p2p/node"
	"github.com/UnrulyOS/go-unruly/p2p/pb"
	"github.com/btcsuite/btcutil/base58"
	"github.com/gogo/protobuf/proto"
	"time"
)

const findNodeReq = "/dht/1.0/find-node-req/"
const findNodeResp = "/dht/1.0/find-node-resp/"

// FindNode dht protocol
// Protocol implementing dht FIND_NODE message
type FindNodeProtocol interface {

	// send a find_node request to a remoteNode
	// reqId: allows the client to match responses with requests by id
	FindNode(msg string, reqId []byte, remoteNodeId string) error

	// App logic registers her for typed incoming find-node responses
	Register(callback chan *pb.FindNodeResp)
}

type FindNodeCallbacks chan chan *pb.FindNodeResp

type findNodeProtocolImpl struct {
	swarm             Swarm
	callbacks         []chan *pb.FindNodeResp
	incomingRequests  MessagesChan
	incomingResponses MessagesChan
	callbacksRegReq   FindNodeCallbacks // a channel of channels that receive callbacks
}

func NewFindNodeProtocol(s Swarm) FindNodeProtocol {

	p := &findNodeProtocolImpl{
		swarm:             s,
		incomingRequests:  make(MessagesChan, 10),
		incomingResponses: make(MessagesChan, 10),
		callbacksRegReq:   make(FindNodeCallbacks, 10),
		callbacks:         make([]chan *pb.FindNodeResp, 0), // start with empty slice
	}

	go p.processEvents()

	// demuxer registration
	s.GetDemuxer().RegisterProtocolHandler(ProtocolRegistration{findNodeReq, p.incomingRequests})
	s.GetDemuxer().RegisterProtocolHandler(ProtocolRegistration{findNodeResp, p.incomingResponses})

	return p
}

// todo: should be a kad param and configurable
const maxNearestNodesResults = 20
const tableQueryTimeout = time.Duration(time.Minute * 1)

// Send a single find node request to a remote node
// remoteNodeId - base58 encoded
func (p *findNodeProtocolImpl) FindNode(msg string, reqId []byte, remoteNodeId string) error {

	nodeId := base58.Decode(remoteNodeId)
	metadata := p.swarm.GetLocalNode().NewProtocolMessageMetadata(findNodeReq, reqId, false)
	data := &pb.FindNodeReq{metadata, nodeId, maxNearestNodesResults}

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

// Register callback on remotely received found nodes
func (p *findNodeProtocolImpl) Register(callback chan *pb.FindNodeResp) {
	p.callbacksRegReq <- callback
}

// Handles a find node request from a remote node
// Process the request and send back the response to the remote node
func (p *findNodeProtocolImpl) handleIncomingRequest(msg IncomingMessage) {
	req := &pb.FindNodeReq{}
	err := proto.Unmarshal(msg.Payload(), req)
	if err != nil {
		log.Warning("Invalid find node request data: %v", err)
		return
	}

	peer := msg.Sender()
	log.Info("Incoming find-node request from %s. Requested node id: s%", peer.Pretty(), hex.EncodeToString(req.NodeId))

	// use the dht table to generate the response

	rt := p.swarm.getRoutingTable()
	nodeDhtId := dht.NewIdFromNodeKey(req.NodeId)
	callback := make(table.PeersOpChannel)
	count := int(minInt32(req.MaxResults, maxNearestNodesResults))

	// get up to count nearest peers to nodeDhtId
	rt.NearestPeers(table.NearestPeersReq{nodeDhtId, count, callback})

	var results []*pb.NodeInfo

	select { // block until we got results from the  routing table or timeout
	case c := <-callback:
		log.Info("Results length: %d", len(c.Peers))
		results = node.ToNodeInfo(c.Peers)
	case <-time.After(tableQueryTimeout):
		results = []*pb.NodeInfo{} // empty slice
	}

	// generate response using results
	metadata := p.swarm.GetLocalNode().NewProtocolMessageMetadata(findNodeResp, req.Metadata.ReqId, false)

	// generate response data
	respData := &pb.FindNodeResp{metadata, results}

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
func (p *findNodeProtocolImpl) handleIncomingResponse(msg IncomingMessage) {

	// process request
	resp := &pb.FindNodeResp{}
	err := proto.Unmarshal(msg.Payload(), resp)
	if err != nil {
		log.Warning("Invalid find-node request data: %v", err)
		return
	}

	log.Info("Got find-node response from %s. Results: %d, Find-node req id: %", msg.Sender().Pretty(),
		resp.NodeInfos, resp.Metadata.ReqId)

	for _, n := range resp.NodeInfos {
		log.Info("Node response: %s, %s", base58.Encode(n.NodeId), n.TcpAddress)
	}

	// notify clients of th new pong
	for _, c := range p.callbacks {
		go func() { c <- resp }()
	}
}

// Internal event processing loop
func (p *findNodeProtocolImpl) processEvents() {
	for {
		select {
		case r := <-p.incomingRequests:
			go p.handleIncomingRequest(r)

		case r := <-p.incomingResponses:
			go p.handleIncomingResponse(r)

		case c := <-p.callbacksRegReq:
			p.callbacks = append(p.callbacks, c)
		}
	}
}
