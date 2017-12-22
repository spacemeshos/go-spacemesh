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

	// Send a find_node request for data about a remote node, known only by id, to a specific known remote node
	// Results will include 0 or more nodes and up to count nodes which may or may not
	// Include data about id (as serverNodeId may not know about it)
	// reqId: allows the client to match responses with requests by id
	// serverNodeId - node to send the find request to
	// id - node id to find

	// this should really be named FindClosestNodes
	FindNode(reqId []byte, serverNodeId string, id string) error

	// App logic registers here for typed incoming find-node responses
	Register(callback chan FindNodeResp)
}

type FindNodeResp struct {
	*pb.FindNodeResp
	err   error
	reqId []byte
}

type FindNodeCallbacks chan chan FindNodeResp

type findNodeProtocolImpl struct {
	swarm             Swarm
	callbacks         []chan FindNodeResp
	incomingRequests  MessagesChan
	incomingResponses MessagesChan
	sendErrors        chan SendError
	callbacksRegReq   FindNodeCallbacks // a channel of channels that receive callbacks
}

func NewFindNodeProtocol(s Swarm) FindNodeProtocol {

	p := &findNodeProtocolImpl{
		swarm:             s,
		incomingRequests:  make(MessagesChan, 10),
		incomingResponses: make(MessagesChan, 10),
		sendErrors:        make(chan SendError),
		callbacksRegReq:   make(FindNodeCallbacks, 10),
		callbacks:         make([]chan FindNodeResp, 0), // start with empty slice
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
func (p *findNodeProtocolImpl) FindNode(reqId []byte, serverNodeId string, id string) error {

	nodeId := base58.Decode(id)
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

	// todo: reg for errors and return to client FindNodeResp w err
	req := SendMessageReq{serverNodeId, reqId, payload, p.sendErrors}
	p.swarm.SendMessage(req)

	return nil
}

// Register callback on remotely received found nodes
func (p *findNodeProtocolImpl) Register(callback chan FindNodeResp) {
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
	// todo: pick up this value from the swarm (k const)
	rt.NearestPeers(table.NearestPeersReq{nodeDhtId, count, callback})

	var results []*pb.NodeInfo

	select { // block until we got results from the  routing table or timeout
	case c := <-callback:
		log.Info("Results length: %d", len(c.Peers))
		// get results and filter the requesting node from the result
		results = node.ToNodeInfo(c.Peers, msg.Sender().String())
	case <-time.After(tableQueryTimeout):
		results = []*pb.NodeInfo{} // an empty slice
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
		signedPayload,
		nil}

	p.swarm.SendMessage(resp)
}

// Handle an incoming pong message from a remote node
func (p *findNodeProtocolImpl) handleIncomingResponse(msg IncomingMessage) {

	// process request
	data := &pb.FindNodeResp{}
	err := proto.Unmarshal(msg.Payload(), data)
	if err != nil {
		log.Error("invalid find-node response data: %v", err)
		// we don't know the request id for this bad response so we can't callback clients with the error
		// just drop the bad response - clients should be notified when their outgoing requests times out
		return
	}

	resp := FindNodeResp{data, nil, data.Metadata.ReqId}

	log.Info("Got find-node response from %s. Results: %d, Find-node req id: %", msg.Sender().Pretty(),
		data.NodeInfos, data.Metadata.ReqId)

	for _, n := range data.NodeInfos {
		log.Info("Node response: %s, %s", base58.Encode(n.NodeId), n.TcpAddress)
	}

	// notify clients of the response
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

		case r := <-p.sendErrors:
			// failed to send a message - send a callback to all clients
			resp := FindNodeResp{nil, r.err, r.ReqId}
			for _, c := range p.callbacks {
				go func() { c <- resp }()
			}
		}
	}
}
