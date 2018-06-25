package dht

import (
	"encoding/hex"
	"errors"
	"github.com/btcsuite/btcutil/base58"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/dht/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"sync"
	"time"
)

// todo: should be a kad param and configurable
const maxNearestNodesResults = 20
const tableQueryTimeout = time.Minute * 1

// Protocol name
const protocol = "/dht/1.0/find-node/"

// ErrEncodeFailed is returned when we failed to encode data to byte array
var ErrEncodeFailed = errors.New("failed to encode data")

type findNodeResults struct {
	err     error
	results []node.Node
}

type findNodeProtocol struct {
	service Service

	pending      map[string]chan findNodeResults
	pendingMutex sync.RWMutex

	ingressChannel chan Message

	rt RoutingTable
}

// NewFindNodeProtocol creates a new FindNodeProtocol instance.
func newFindNodeProtocol(service Service, rt RoutingTable) *findNodeProtocol {

	p := &findNodeProtocol{
		rt:             rt,
		pending:        make(map[string]chan findNodeResults),
		ingressChannel: service.RegisterProtocol(protocol),
		service:        service,
	}

	go p.readLoop()

	return p
}

func (p *findNodeProtocol) sendRequestMessage(server crypto.PublicKey, payload []byte) ([]byte, error) {
	reqID := crypto.UUID()
	findnode := &pb.FindNode{}
	findnode.Req = true
	findnode.ReqID = reqID
	findnode.Payload = payload

	msg, err := proto.Marshal(findnode)
	if err != nil {
		return nil, err
	}

	go p.service.SendMessage(server, msg)
	return reqID, nil
}

func (p *findNodeProtocol) sendResponseMessage(server crypto.PublicKey, reqID, payload []byte) error {
	findnode := &pb.FindNode{}
	findnode.Req = false
	findnode.ReqID = reqID
	findnode.Payload = payload

	msg, err := proto.Marshal(findnode)
	if err != nil {
		return err
	}

	go p.service.SendMessage(server, msg)
	return nil
}

// FindNode Send a single find node request to a remote node
// id: base58 encoded remote node id
func (p *findNodeProtocol) FindNode(serverNode node.Node, target string) ([]node.Node, error) {

	var err error

	nodeID := base58.Decode(target)
	data := &pb.FindNodeReq{
		NodeID:     nodeID,
		MaxResults: maxNearestNodesResults,
	}

	payload, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}
	respc := make(chan findNodeResults)
	reqid, err := p.sendRequestMessage(serverNode.PublicKey(), payload)

	if err != nil {
		return nil, err
	}
	p.pendingMutex.Lock()
	p.pending[hex.EncodeToString(reqid)] = respc
	p.pendingMutex.Unlock()

	timeout := time.NewTimer(time.Minute)

	select {
	case response := <-respc:
		if response.err != nil {
			return nil, response.err
		}

		for _, n := range response.results {
			p.rt.Update(n)
		}

		return response.results, nil
	case <-timeout.C:
		err = errors.New("findnode took too long to respond")
	}

	return nil, err
}

func (p *findNodeProtocol) readLoop() {
	for {
		msg, ok := <-p.ingressChannel
		if !ok {
			// Channel is closed.
			break
		}

		go func(msg Message) {

			headers := &pb.FindNode{}
			err := proto.Unmarshal(msg.Data(), headers)
			if err != nil {
				//TODO : Handle errors
				return
			}

			p.rt.Update(msg.Sender())

			if headers.Req {
				p.handleIncomingRequest(msg.Sender().PublicKey(), headers.ReqID, headers.Payload)
				return
			}
			p.handleIncomingResponse(headers.ReqID, headers.Payload)
		}(msg)
	}

}

// Handles a find node request from a remote node
// Process the request and send back the response to the remote node
func (p *findNodeProtocol) handleIncomingRequest(sender crypto.PublicKey, reqID, msg []byte) {
	req := &pb.FindNodeReq{}
	err := proto.Unmarshal(msg, req)
	if err != nil {
		return
	}

	// use the dht table to generate the response
	nodeDhtID := node.NewDhtID(req.NodeID)

	callback := make(PeersOpChannel)

	count := int(crypto.MinInt32(req.MaxResults, maxNearestNodesResults))

	// get up to count nearest peers to nodeDhtId
	p.rt.NearestPeers(NearestPeersReq{ID: nodeDhtID, Count: count, Callback: callback})

	var results []*pb.NodeInfo

	select { // block until we got results from the  routing table or timeout
	case c := <-callback:
		peers := []node.Node{}
		for i := range c.Peers { // we don't want to send peer to itself
			peers = append(peers, c.Peers[i])
		}
		results = toNodeInfo(peers, sender.String())
	case <-time.After(tableQueryTimeout):
		results = []*pb.NodeInfo{} // an empty slice
	}

	respData := &pb.FindNodeResp{NodeInfos: results}
	payload, err := proto.Marshal(respData)
	if err != nil {
		return
	}

	err = p.sendResponseMessage(sender, reqID, payload)
	if err != nil {
		// TODO : handle errors.
	}
}

// Handle an incoming pong message from a remote node
func (p *findNodeProtocol) handleIncomingResponse(reqID, msg []byte) {
	// process request
	data := &pb.FindNodeResp{}
	err := proto.Unmarshal(msg, data)
	if err != nil {
		p.sendResponse(reqID, findNodeResults{err, nil})
		return
	}

	// update routing table with newly found nodes
	nodes := fromNodeInfos(data.NodeInfos)

	p.sendResponse(reqID, findNodeResults{nil, nodes})
}

func (p *findNodeProtocol) sendResponse(reqID []byte, results findNodeResults) {
	ridstr := hex.EncodeToString(reqID)

	p.pendingMutex.RLock()
	pend, ok := p.pending[ridstr]
	p.pendingMutex.RUnlock()

	if ok {
		p.pendingMutex.Lock()
		delete(p.pending, ridstr)
		p.pendingMutex.Unlock()
		pend <- results
	}
}

// ToNodeInfo returns marshaled protobufs identity infos slice from a slice of RemoteNodeData.
// filterId: identity id to exclude from the result
func toNodeInfo(nodes []node.Node, filterID string) []*pb.NodeInfo {
	// init empty slice
	var res []*pb.NodeInfo
	for _, n := range nodes {

		if n.String() == filterID {
			continue
		}

		res = append(res, &pb.NodeInfo{
			NodeId:  n.PublicKey().Bytes(),
			Address: n.Address(),
		})
	}
	return res
}

// FromNodeInfos converts a list of NodeInfo to a list of Node.
func fromNodeInfos(nodes []*pb.NodeInfo) []node.Node {
	res := make([]node.Node, len(nodes))
	for i, n := range nodes {
		pubk, err := crypto.NewPublicKey(n.NodeId)
		if err != nil {
			// TODO Error handling, problem : don't break everything because one messed up nodeinfo
			continue
		}
		node := node.New(pubk, n.Address)
		res[i] = node

	}
	return res
}
