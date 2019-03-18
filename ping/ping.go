package ping

import (
	"bytes"
	"errors"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/ping/pb"
	"net"
	"time"
)

const Protocol = "/ping/1.0/"

// PingTimeout is a timeout for ping reply
const PingTimeout = time.Second * 1 // TODO: Parametrize

const PING = server.MessageType(1)

var errPingTimedOut = errors.New("Ping took too long to response")
var errWrongID = errors.New("the peer claims to have a different identity")
var errFailed = errors.New("ping request failed")

type Pinger struct {
	localNode node.Node

	pingCallbacks []func(from net.Addr, ping *pb.Ping) error

	*server.MessageServer
	logger log.Log
	exit   chan struct{}
}

func (p *Pinger) RegisterCallback(f func(from net.Addr, ping *pb.Ping) error) {
	p.pingCallbacks = append(p.pingCallbacks, f)
}

func New(local node.Node, srv server.Service, logger log.Log) *Pinger {
	p := Pinger{
		localNode:     local,
		logger:        logger,
		MessageServer: server.NewMsgServer(srv, Protocol, PingTimeout, make(chan service.DirectMessage), logger),
		exit:          make(chan struct{}),
	}

	p.RegisterMsgHandler(PING, p.newPingRequestHandler())

	return &p
}

func (p *Pinger) newPingRequestHandler() func(msg server.Message) []byte {
	return func(msg server.Message) []byte {
		p.logger.Info("handle ping request")
		req := &pb.Ping{}
		if err := proto.Unmarshal(msg.Bytes(), req); err != nil {
			return nil
		}

		// NOTE: this is blocking, in 2 ways. the functions are blocking the ping response
		// but they also block each other and even cancel each other. if a callback fails
		// the next one won't happen and we won't respond. this should be revisited once we
		// more callbacks or logic to the callback.
		for _, f := range p.pingCallbacks {
			if err := f(msg.Metadata().FromAddress, req); err != nil {
				return nil
			}
		}

		//pong
		payload, err := proto.Marshal(&pb.Ping{ID: p.localNode.PublicKey().Bytes(), ListenAddress: p.localNode.Address()})

		if err != nil {
			p.logger.Error("Error marshaling response message (Ping)")
			return nil
		}

		return payload
	}
}

func (p *Pinger) Ping(peer p2pcrypto.PublicKey) error {
	p.logger.Info("send ping request Peer: %v", peer)
	data := &pb.Ping{ID: p.localNode.PublicKey().Bytes(), ListenAddress: p.localNode.Address()}
	payload, err := proto.Marshal(data)
	if err != nil {
		return err
	}
	ch := make(chan []byte)
	foo := func(msg []byte) {
		defer close(ch)
		p.logger.Info("handle ping response")
		data := &pb.Ping{}
		if err := proto.Unmarshal(msg, data); err != nil {
			p.logger.Error("could not unmarshal block data")
			return
		}

		// todo: if we pinged it we already have id so no need to update
		// todo : but what if id or listen address has changed ?

		ch <- data.ID
	}

	err = p.MessageServer.SendRequest(PING, payload, peer, foo)

	if err != nil {
		return err
	}

	timeout := time.NewTimer(PingTimeout)
	select {
	case id := <-ch:
		if id == nil {
			return errFailed
		}
		if !bytes.Equal(id, peer.Bytes()) {
			return errWrongID
		}
	case <-timeout.C:
		return errPingTimedOut
	}

	return nil
}
