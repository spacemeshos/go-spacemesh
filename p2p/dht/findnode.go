package dht

import (
	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/dht/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"sync"
	"time"
)

// todo: should be a kad param and configurable
const maxNearestNodesResults = 20
const tableQueryTimeout = time.Second * 3
const findNodeTimeout = 1 * time.Minute

// Protocol name
const protocol = "/dht/1.0/find-node/"

// ErrEncodeFailed is returned when we failed to encode data to byte array
var ErrEncodeFailed = errors.New("failed to encode data")

type findNodeResults struct {
	results []node.Node
	err     error
}

type findNodeProtocol struct {
	service service.Service

	pending      map[crypto.UUID]chan findNodeResults
	pendingMutex sync.RWMutex

	ingressChannel chan service.Message

	log log.Log

	rt RoutingTable
}

type localService interface {
	LocalNode() *node.LocalNode
}

// NewFindNodeProtocol creates a new FindNodeProtocol instance.
func newFindNodeProtocol(service service.Service, rt RoutingTable) *findNodeProtocol {

	p := &findNodeProtocol{
		rt:             rt,
		pending:        make(map[crypto.UUID]chan findNodeResults),
		ingressChannel: service.RegisterProtocol(protocol),
		service:        service,
	}

	if srv, ok := service.(localService); ok {
		p.log = srv.LocalNode().Log
	} else {
		p.log = log.AppLog
	}

	go p.readLoop()

	return p
}

func (p *findNodeProtocol) sendRequestMessage(server p2pcrypto.PublicKey, payload []byte, reqID crypto.UUID,
	responseChan chan findNodeResults) (bool, error) {

	findnode := &pb.FindNode{}
	findnode.Req = true
	findnode.ReqID = reqID[:]
	findnode.Payload = payload

	msg, err := proto.Marshal(findnode)
	if err != nil {
		return false, err
	}

	p.pendingMutex.Lock()
	p.pending[reqID] = responseChan
	p.pendingMutex.Unlock()

	return true, p.service.SendMessage(server, protocol, msg)
}

func (p *findNodeProtocol) sendResponseMessage(server p2pcrypto.PublicKey, reqID, payload []byte) error {
	findnode := &pb.FindNode{}
	findnode.Req = false
	findnode.ReqID = reqID
	findnode.Payload = payload

	msg, err := proto.Marshal(findnode)
	if err != nil {
		return err
	}
	return p.service.SendMessage(server, protocol, msg)
}

// FindNode Send a single find node request to a remote node
func (p *findNodeProtocol) FindNode(serverNode node.Node, target p2pcrypto.PublicKey) ([]node.Node, error) {

	var err error

	nodeID := target.Bytes()
	data := &pb.FindNodeReq{
		NodeID:     nodeID,
		MaxResults: maxNearestNodesResults,
	}

	payload, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}

	reqID := crypto.NewUUID()
	respc := make(chan findNodeResults)

	pending, err := p.sendRequestMessage(serverNode.PublicKey(), payload, reqID, respc)

	if err != nil {
		if pending {
			p.pendingMutex.Lock()
			delete(p.pending, reqID)
			p.pendingMutex.Unlock()
		}
		return nil, err
	}

	timeout := time.NewTimer(findNodeTimeout)

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

		go func(msg service.Message) {

			headers := &pb.FindNode{}
			err := proto.Unmarshal(msg.Bytes(), headers)
			if err != nil {
				log.Error("Error handling incoming FindNode ", err)
				return
			}

			if headers.Req {
				p.handleIncomingRequest(msg.Sender(), headers.ReqID, headers.Payload)
				return
			}
			reqid := headers.ReqID
			var creqid crypto.UUID
			copy(creqid[:], reqid) // ugly way to copy slice to array. todo : find better way ?
			p.handleIncomingResponse(creqid, headers.Payload)
		}(msg)
	}

}

// Handles a find node request from a remote node
// Process the request and send back the response to the remote node
func (p *findNodeProtocol) handleIncomingRequest(sender p2pcrypto.PublicKey, reqID, msg []byte) {
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
	timer := time.NewTimer(tableQueryTimeout)
	select { // block until we got results from the  routing table or timeout
	case c := <-callback:
		results = toNodeInfo(c.Peers, sender.String())
	case <-timer.C:
		results = []*pb.NodeInfo{} // an empty slice
	}

	respData := &pb.FindNodeResp{NodeInfos: results}
	payload, err := proto.Marshal(respData)
	if err != nil {
		return
	}

	err = p.sendResponseMessage(sender, reqID, payload)
	if err != nil {
		p.log.Error("failed sending response message to %v, err:%v", sender.String(), err)
	}
}

// Handle an incoming pong message from a remote node
func (p *findNodeProtocol) handleIncomingResponse(reqID crypto.UUID, msg []byte) {
	// process request
	data := &pb.FindNodeResp{}
	err := proto.Unmarshal(msg, data)
	if err != nil {
		p.sendResponse(reqID, findNodeResults{nil, err})
		return
	}

	// update routing table with newly found nodes
	nodes := fromNodeInfos(data.NodeInfos)

	p.sendResponse(reqID, findNodeResults{nodes, nil})
}

func (p *findNodeProtocol) sendResponse(reqID crypto.UUID, results findNodeResults) {
	p.pendingMutex.RLock()
	pend, ok := p.pending[reqID]
	p.pendingMutex.RUnlock()

	if ok {
		p.pendingMutex.Lock()
		delete(p.pending, reqID)
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
		pubk, err := p2pcrypto.NewPubkeyFromBytes(n.NodeId)
		if err != nil {
			// TODO Error handling, problem : don't break everything because one messed up nodeinfo
			log.Error("There was an error parsing nodeid : ", n.NodeId, ", skipping it. err: ", err)
			continue
		}
		node := node.New(pubk, n.Address)
		res[i] = node

	}
	return res
}
