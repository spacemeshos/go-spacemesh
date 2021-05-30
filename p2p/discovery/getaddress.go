package discovery

import (
	"context"
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"time"
)

// todo : calculate real udp max message size

func (p *protocol) newGetAddressesRequestHandler() func(context.Context, server.Message) []byte {
	return func(ctx context.Context, msg server.Message) []byte {
		t := time.Now()
		plogger := p.logger.WithContext(ctx).WithFields(log.String("type", "getaddresses"),
			log.String("from", msg.Sender().String()))
		plogger.Debug("got request")

		// TODO: if we don't know who is that peer (a.k.a first time we hear from this address)
		// 		 we must ensure that he's indeed listening on that address = check last pong

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
			plogger.With().Error("error marshaling response message (GetAddress)", log.Err(err))
			return nil
		}

		plogger.With().Debug("sending response",
			log.Int("size", len(results)),
			log.Duration("time_to_make", time.Since(t)))
		return resp
	}
}

// GetAddresses Send a get address request to a remote node, it will block and return the results returned from the node.
func (p *protocol) GetAddresses(ctx context.Context, server p2pcrypto.PublicKey) ([]*node.Info, error) {
	start := time.Now()
	var err error

	plogger := p.logger.WithContext(ctx).WithFields(log.String("type", "getaddresses"),
		log.FieldNamed("to", server))

	plogger.Debug("sending request")

	// response handler
	ch := make(chan []*node.Info)
	resHandler := func(msg []byte) {
		defer close(ch)
		nodes := make([]*node.Info, 0, getAddrMax)
		err := types.BytesToInterface(msg, &nodes)
		//todo: check that we're not pass max results ?
		if err != nil {
			plogger.With().Warning("could not deserialize bytes, skipping packet", log.Err(err))
			return
		}

		if len(nodes) > getAddrMax {
			plogger.With().Warning("addresses response too large, ignoring",
				log.FieldNamed("from", server),
				log.Int("got", len(nodes)),
				log.Int("expected", getAddrMax))
			return
		}

		ch <- nodes
	}

	err = p.msgServer.SendRequest(ctx, GetAddresses, []byte(""), server, resHandler, func(err error) {})

	if err != nil {
		return nil, err
	}

	timeout := time.NewTimer(MessageTimeout)
	select {
	case nodes := <-ch:
		if nodes == nil {
			return nil, errors.New("empty result set")
		}
		plogger.With().Debug("getaddress_time_to_recv",
			log.Int("len", len(nodes)),
			log.Duration("time_elapsed", time.Since(start)))
		return nodes, nil
	case <-timeout.C:
		return nil, errors.New("getaddress request timed out")
	}
}
