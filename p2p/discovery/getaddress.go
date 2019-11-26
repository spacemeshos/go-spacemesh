package discovery

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"time"
)

// todo : calculate real udp max message size

func (p *protocol) newGetAddressesRequestHandler() func(msg server.Message) []byte {
	return func(msg server.Message) []byte {
		t := time.Now()
		plogger := p.logger.WithFields(log.String("type", "getaddresses"), log.String("from", msg.Sender().String()))
		plogger.Debug("got request")

		// If we don't know who is that peer (a.k.a first time we hear from this address)
		// we must ensure that he's indeed listening on that address = check last pong
		ka, err := p.table.LookupKnownAddress(msg.Sender())
		if err != nil {
			p.logger.Error("Error looking up message sender (GetAddress): %v", msg.Sender())
			return nil
		}
		// Check if we've pinged this peer recently enough
		if ka.NeedsPing() {
			if err := p.Ping(msg.Sender()); err != nil {
				p.logger.Error("Error pinging peer (GetAddress): %v", msg.Sender())
				return nil
			}
		}
		p.logger.Debug("Passed ping check, recently pinged Peer %v", msg.Sender())

		results := p.table.AddressCache()
		// remove the sender from the list
		for i, addr := range results {
			if addr.PublicKey() == msg.Sender() {
				results[i] = results[len(results)-1]
				results = results[:len(results)-1]
				break
			}
		}

		//todo: limit results to message size
		//todo: what to do if we have no addresses?
		resp, err := types.InterfaceToBytes(results)

		if err != nil {
			plogger.Error("Error marshaling response message (GetAddress) %v", err)
			return nil
		}

		plogger.With().Debug("Sending response", log.Int("size", len(results)), log.Duration("time_to_make", time.Since(t)))
		return resp
	}
}

// GetAddresses Send a single find node request to a remote node
func (p *protocol) GetAddresses(server p2pcrypto.PublicKey) ([]*node.NodeInfo, error) {
	start := time.Now()
	var err error

	plogger := p.logger.WithFields(log.String("type", "getaddresses"), log.String("to", server.String()))

	plogger.Debug("sending request")

	// response handler
	ch := make(chan []*node.NodeInfo)
	resHandler := func(msg []byte) {
		defer close(ch)
		nodes := make([]*node.NodeInfo, 0, getAddrMax)
		err := types.BytesToInterface(msg, &nodes)
		//todo: check that we're not pass max results ?
		if err != nil {
			plogger.Warning("could not deserialize bytes to NodeInfo, skipping packet err=", err)
			return
		}

		if len(nodes) > getAddrMax {
			plogger.Warning("addresses response from %v size is too large, ignoring. got: %v, expected: < %v", server.String(), len(nodes), getAddrMax)
			return
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
		plogger.With().Debug("getaddress_time_to_recv", log.Int("len", len(nodes)), log.Duration("time_elapsed", time.Now().Sub(start)))
		return nodes, nil
	case <-timeout.C:
		return nil, errors.New("request timed out")
	}
}
