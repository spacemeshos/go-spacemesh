package discovery

import (
	"errors"
	"github.com/nullstyle/go-xdr/xdr"
	"github.com/spacemeshos/go-spacemesh/log"
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

		resp, err := marshalNodeInfo(results, msg.Sender().String())

		if err != nil {
			p.logger.Error("Error marshaling response message (Ping)")
			return nil
		}

		p.logger.Debug("responding a find_node request at from after", msg.Sender().String(), time.Now().Sub(start))

		return resp
	}
}

// GetAddresses Send a single find node request to a remote node
func (p *protocol) GetAddresses(server p2pcrypto.PublicKey) ([]*node.NodeInfo, error) {
	start := time.Now()
	var err error

	// response handler
	ch := make(chan []*node.NodeInfo)
	resHandler := func(msg []byte) {
		defer close(ch)
		nodes, err := unmarshalNodeInfo(msg)
		//todo: check that we're not pass max results ?
		if err != nil {
			p.logger.Warning("could not deserialize bytes to NodeInfo, skipping packet err=", err)
		}

		ch <- nodes
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
func marshalNodeInfo(nodes []*node.NodeInfo, filterID string) ([]byte, error) {
	// init empty slice
	nds := make([]*node.NodeInfo, 0, len(nodes))
	for _, n := range nodes {
		if n.ID.String() == filterID {
			continue
		}
		nds = append(nds, n)
	}
	return xdr.Marshal(nds)
}

// FromNodeInfos converts a list of NodeInfo to a list of Node.
func unmarshalNodeInfo(nodes []byte) ([]*node.NodeInfo, error) {
	n := make([]*node.NodeInfo, 0, 25)
	_, err := xdr.Unmarshal(nodes, &n)
	if err != nil {
		return nil, err
	}
	return n, nil
}
