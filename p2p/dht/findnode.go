package dht

import (
	"errors"
	"github.com/golang/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/dht/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"time"
)

// todo: should be a kad param and configurable
const maxNearestNodesResults = 20
const tableQueryTimeout = time.Second * 3
const findNodeTimeout = 1 * time.Minute

// Protocol name
//const protocol = "/dht/1.0/find-node/"

// ErrEncodeFailed is returned when we failed to encode data to byte array
var ErrEncodeFailed = errors.New("failed to encode data")

func (p *discovery) newFindNodeRequestHandler() func(msg server.Message) []byte {
	return func(msg server.Message) []byte {
		// TODO: if we don't know who is that peer (a.k.a first time we hear from this address)
		// 		 we must ensure that he's indeed listening on that address.

		req := &pb.FindNodeReq{}
		err := proto.Unmarshal(msg.Bytes(), req)

		if err != nil {
			p.logger.Error("Error opening FIND_NODE message")
			return nil
		}

		// use the dht table to generate the response
		nodeID, err := p2pcrypto.NewPubkeyFromBytes(req.NodeID)

		if err != nil {
			p.logger.Error("Error reading public key from FIND_NODE message")
			return nil
		}

		count := int(crypto.MinInt32(req.MaxResults, maxNearestNodesResults))

		// get up to count nearest peers to nodeDhtId
		results := p.table.InternalLookup(nodeID)

		if len(results) > count {
			results = results[:count]
		}

		resp := &pb.FindNodeResp{NodeInfos: toNodeInfo(results, msg.Sender().String())}

		payload, err := proto.Marshal(resp)

		if err != nil {
			p.logger.Error("Error marshaling response message (Ping)")
			return nil
		}

		return payload
	}
}

// FindNode Send a single find node request to a remote node
func (p *discovery) FindNode(serverNode p2pcrypto.PublicKey, target p2pcrypto.PublicKey) ([]node.Node, error) {
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

	// response handler
	ch := make(chan []node.Node)
	foo := func(msg []byte) {
		defer close(ch)
		p.logger.Info("handle find_node response")
		data := &pb.FindNodeResp{}
		if err := proto.Unmarshal(msg, data); err != nil {
			p.logger.Error("could not unmarshal block data")
			return
		}

		//todo: check that we're not pass max results ?

		ch <- fromNodeInfos(data.NodeInfos)
	}

	err = p.msgServer.SendRequest(FIND_NODE, payload, serverNode, foo)

	if err != nil {
		return nil, err
	}

	timeout := time.NewTimer(findNodeTimeout)
	select {
	case nodes := <-ch:
		if nodes == nil {
			return nil, errors.New("empty result set")
		}
		return nodes, nil
	case <-timeout.C:
		return nil, errors.New("request timed out")
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
