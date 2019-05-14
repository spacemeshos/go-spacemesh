package discovery

import (
	"errors"
	"github.com/golang/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/discovery/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"time"
)

func (p *protocol) newGetAddressesRequestHandler() func(msg server.Message) []byte {
	return func(msg server.Message) []byte {
		start := time.Now()
		p.logger.Debug("Got a find_node request at from ", msg.Sender().String())

		// TODO: if we don't know who is that peer (a.k.a first time we hear from this address)
		// 		 we must ensure that he's indeed listening on that address = check last pong

		results := p.table.AddressCache()
		//todo: limit results to message size

		resp := &pb.Addresses{NodeInfos: marshalNodeInfo(results, msg.Sender().String())}

		payload, err := proto.Marshal(resp)

		if err != nil {
			p.logger.Error("Error marshaling response message (Ping)")
			return nil
		}

		p.logger.Debug("responding a find_node request at from after", msg.Sender().String(), time.Now().Sub(start))

		return payload
	}
}

// GetAddresses Send a single find node request to a remote node
func (p *protocol) GetAddresses(server p2pcrypto.PublicKey) ([]NodeInfo, error) {
	start := time.Now()
	var err error

	// response handler
	ch := make(chan []NodeInfo)
	resHandler := func(msg []byte) {
		defer close(ch)
		//p.logger.Info("handle find_node response from %v", serverNode.String())
		data := &pb.Addresses{}
		if err := proto.Unmarshal(msg, data); err != nil {
			p.logger.Error("could not unmarshal Addresses message")
			return
		}

		//todo: check that we're not pass max results ?

		ch <- unmarshalNodeInfo(data.NodeInfos)
	}

	err = p.msgServer.SendRequest(GET_ADDRESSES, []byte(""), server, resHandler)

	if err != nil {
		return nil, err
	}

	timeout := time.NewTimer(MessageTimeout)
	select {
	case nodes := <-ch:
		if nodes == nil {
			return nil, errors.New("empty result set")
		}
		p.logger.With().Debug("getaddress_time_to_recv", log.String("from", server.String()), log.Duration("time_elapsed", time.Now().Sub(start)))
		return nodes, nil
	case <-timeout.C:
		return nil, errors.New("request timed out")
	}
}

// ToNodeInfo returns marshaled protobufs identity infos slice from a slice of RemoteNodeData.
// filterId: identity id to exclude from the result
func marshalNodeInfo(nodes []NodeInfo, filterID string) []*pb.NodeInfo {
	// init empty slice
	var res []*pb.NodeInfo
	for _, n := range nodes {

		if n.String() == filterID {
			continue
		}

		res = append(res, &pb.NodeInfo{
			NodeId:     n.PublicKey().Bytes(),
			TCPAddress: n.Address(),
			UDPAddress: n.udpAddress,
		})
	}
	return res
}

// FromNodeInfos converts a list of NodeInfo to a list of Node.
func unmarshalNodeInfo(nodes []*pb.NodeInfo) []NodeInfo {
	res := make([]NodeInfo, len(nodes))
	for i, n := range nodes {
		pubk, err := p2pcrypto.NewPubkeyFromBytes(n.NodeId)
		if err != nil {
			// TODO Error handling, problem : don't break everything because one messed up nodeinfo
			log.Error("There was an error parsing nodeid : ", n.NodeId, ", skipping it. err: ", err)
			continue
		}
		nd := node.New(pubk, n.TCPAddress)
		node := NodeInfoFromNode(nd, n.UDPAddress)
		res[i] = node

	}
	return res
}
