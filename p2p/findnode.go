package p2p

import (
	"encoding/hex"
	"fmt"
	"github.com/btcsuite/btcutil/base58"
	"github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/dht"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"time"
)

const findNodeReq = "/dht/1.0/find-node-req/"
const findNodeResp = "/dht/1.0/find-node-resp/"

// ErrEncodeFailed is returned when we failed to encode data to byte array
var ErrEncodeFailed = errors.New("failed to encode data")

// FindNodeProtocol provides the dht protocol FIND-NODE message.
type FindNodeProtocol interface {

	// Send a find_node request for data about a remote node, known only by id, to a specific known remote node
	// Results will include 0 or more nodes and up to count nodes which may or may not include data about id (as serverNodeId may not know about it)
	// reqID: allows the client to match responses with requests by id
	// serverNodeId - node to send the find request to
	// id - node id to find

	// this should really be named FindClosestNodes
	FindNode(reqID []byte, serverNodeID string, id string, callback chan FindNodeResp) error
}

// FindNodeResp specifies the data returned by FindNode.
type FindNodeResp struct {
	*pb.FindNodeResp
	err   error
	reqID []byte
}

// RespCallbackRequest specifies callback for a request DhtID.
type RespCallbackRequest struct {
	callback chan FindNodeResp
	reqID    []byte
}

// FindNodeCallbacks is a channel of RespCallbackRequests.
type FindNodeCallbacks chan RespCallbackRequest

type findNodeProtocolImpl struct {
	swarm             Swarm
	callbacks         map[string]chan FindNodeResp
	incomingRequests  MessagesChan
	incomingResponses MessagesChan
	sendErrors        chan SendError
	callbacksRegReq   FindNodeCallbacks // a channel of channels that receives callbacks
	sendCallbackReq   chan FindNodeResp
}

// NewFindNodeProtocol creates a new FindNodeProtocol instance.
func NewFindNodeProtocol(s Swarm) FindNodeProtocol {

	p := &findNodeProtocolImpl{
		swarm:             s,
		incomingRequests:  make(MessagesChan, 10),
		incomingResponses: make(MessagesChan, 10),
		sendErrors:        make(chan SendError),
		callbacksRegReq:   make(FindNodeCallbacks), // not buffered so it is blocked until callback is registered
		callbacks:         make(map[string]chan FindNodeResp),
		sendCallbackReq:   make(chan FindNodeResp),
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
// id: base58 encoded remote node id
func (p *findNodeProtocolImpl) FindNode(reqID []byte, serverNodeID string, id string, callback chan FindNodeResp) error {
	p.swarm.LocalNode().Debug("Sending FindNode(%v) request to %v", id, serverNodeID)
	p.callbacksRegReq <- RespCallbackRequest{callback, reqID}

	nodeID := base58.Decode(id)
	metadata := NewProtocolMessageMetadata(p.swarm.LocalNode().PublicKey(), findNodeReq, reqID, false)
	data := &pb.FindNodeReq{
		Metadata:   metadata,
		NodeId:     nodeID,
		MaxResults: maxNearestNodesResults}

	// sign data
	tosign, err := proto.Marshal(data)
	if err != nil {
		return fmt.Errorf("%v, err: %v", ErrEncodeFailed, err)
	}
	sign, err := p.swarm.LocalNode().PrivateKey().Sign(tosign)
	data.Metadata.AuthorSign = hex.EncodeToString(sign)
	signedPayload, err := proto.Marshal(data)
	if err != nil {
		return fmt.Errorf("%v after signing, err: %v", ErrEncodeFailed, err)
	}
	// send the message
	req := SendMessageReq{serverNodeID, reqID, signedPayload, p.sendErrors}
	p.swarm.SendMessage(req)

	return nil
}

// Handles a find node request from a remote node
// Process the request and send back the response to the remote node
func (p *findNodeProtocolImpl) handleIncomingRequest(msg IncomingMessage) {
	req := &pb.FindNodeReq{}
	err := proto.Unmarshal(msg.Payload(), req)
	if err != nil {
		p.swarm.LocalNode().Warning("Invalid find node request data", err)
		return
	}

	peer := msg.Sender()
	p.swarm.LocalNode().Debug("Incoming find-node request from %s. Requested node id: %v (DhtId: %v)", peer.Pretty(), log.PrettyID(base58.Encode(req.NodeId)), hex.EncodeToString(req.NodeId))

	// use the dht table to generate the response

	rt := p.swarm.RoutingTable()
	nodeDhtID := node.NewDhtID(req.NodeId)
	callback := make(dht.PeersOpChannel)

	count := int(crypto.MinInt32(req.MaxResults, maxNearestNodesResults))

	// get up to count nearest peers to nodeDhtId
	rt.NearestPeers(dht.NearestPeersReq{ID: nodeDhtID, Count: count, Callback: callback})

	var results []*pb.NodeInfo

	select { // block until we got results from the  routing table or timeout
	case c := <-callback:
		p.swarm.LocalNode().Debug("Preparing find-node (%v) results to send to %v", log.PrettyID(base58.Encode(req.NodeId)), peer.Pretty())
		peers := []node.Node{}
		for i := range c.Peers { // we don't want to send peer to itself
			peer := c.Peers[i]
			if peer.String() != msg.Sender().String() {
				peers = append(peers, c.Peers[i])
				p.swarm.LocalNode().Debug("%d: %v", i, peer.Pretty(), peer.Address())
			}
		}
		results = node.ToNodeInfo(peers, msg.Sender().String())
	case <-time.After(tableQueryTimeout):
		results = []*pb.NodeInfo{} // an empty slice
	}

	// generate response using results
	metadata := NewProtocolMessageMetadata(p.swarm.LocalNode().PublicKey(), findNodeResp, req.Metadata.ReqId, false)

	// generate response data
	respData := &pb.FindNodeResp{Metadata: metadata, NodeInfos: results}

	tosign, err := proto.Marshal(respData)
	if err != nil {
		p.swarm.LocalNode().Error("", fmt.Errorf("%v for signing, %v", ErrEncodeFailed, err))
		return
	}
	// sign response
	sign, err := p.swarm.LocalNode().PrivateKey().Sign(tosign)
	if err != nil {
		p.swarm.LocalNode().Debug("Failed to sign response")
		return
	}

	respData.Metadata.AuthorSign = hex.EncodeToString(sign)

	// marshal the signed data
	signedPayload, err := proto.Marshal(respData)
	if err != nil {
		p.swarm.LocalNode().Debug("Failed to generate response data")
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
		p.swarm.LocalNode().Error("Invalid find-node response data", err)
		// we don't know the request id for this bad response so we can't callback clients with the error
		// just drop the bad response - clients should be notified when their outgoing requests times out
		return
	}

	resp := FindNodeResp{data, nil, data.Metadata.ReqId}

	p.swarm.LocalNode().Debug("Got find-node response from %s. Results: %v, Find-node req id: %s", msg.Sender().Pretty(),
		data.NodeInfos, hex.EncodeToString(data.Metadata.ReqId))

	// update routing table with newly found nodes
	nodes := node.FromNodeInfos(data.NodeInfos)
	for _, n := range nodes {
		p.swarm.LocalNode().Debug("Node response: %s, %s", n.String(), n.Address())
		p.swarm.RoutingTable().Update(n)
	}

	go p.SendResponseCallback(resp)
}

// Sends the response callback for this response's request
func (p *findNodeProtocolImpl) SendResponseCallback(resp FindNodeResp) {
	p.sendCallbackReq <- resp
}

// send a response callback to client and remove callback registration
func (p *findNodeProtocolImpl) sendResponseCallback(resp FindNodeResp) {
	key := hex.EncodeToString(resp.reqID)
	callback := p.callbacks[key]
	if callback == nil {
		return
	}

	delete(p.callbacks, key)
	go func() {
		callback <- resp
	}()
}

// Internal event processing loop
func (p *findNodeProtocolImpl) processEvents() {
	for {
		select {
		case r := <-p.incomingRequests:
			go p.handleIncomingRequest(r)

		case r := <-p.incomingResponses:
			go p.handleIncomingResponse(r)

		case c := <-p.callbacksRegReq: // register a new callback
			p.callbacks[hex.EncodeToString(c.reqID)] = c.callback

		case fnr := <-p.sendCallbackReq:
			p.sendResponseCallback(fnr)

		case r := <-p.sendErrors:
			// failed to send a message - send a callback to all clients
			resp := FindNodeResp{nil, r.err, r.ReqID}
			p.sendResponseCallback(resp)
		}
	}
}
