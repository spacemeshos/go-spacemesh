package dht

import (
	"bytes"
	"errors"
	"github.com/golang/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/ping/pb"
	"net"
	"strings"
	"time"
)

func (p *discovery) newPingRequestHandler() func(msg server.Message) []byte {
	return func(msg server.Message) []byte {
		p.logger.Info("handle ping request")
		req := &pb.Ping{}
		if err := proto.Unmarshal(msg.Bytes(), req); err != nil {
			p.logger.Error("failed to deserialize ping message err=", err)
			return nil
		}

		if err := p.pingerCallback(msg.Metadata().FromAddress, req); err != nil {
			p.logger.Error("msg contents were not valid err=", err)
			return nil
		}

		//pong
		payload, err := proto.Marshal(&pb.Ping{ID: p.local.PublicKey().Bytes(), ListenAddress: p.local.Address()})

		if err != nil {
			p.logger.Error("Error marshaling response message (Ping)")
			return nil
		}

		return payload
	}
}

func (p *discovery) pingerCallback(from net.Addr, pi *pb.Ping) error {
	//todo: check the address provided with an extra ping before upading. ( if we haven't checked it for a while )
	k, err := p2pcrypto.NewPubkeyFromBytes(pi.ID)

	if err != nil {
		return err
	}

	//extract port
	_, port, err := net.SplitHostPort(pi.ListenAddress)
	if err != nil {
		return err
	}
	var addr string

	if spl := strings.Split(from.String(), ":"); len(spl) > 1 {
		addr, _, err = net.SplitHostPort(from.String())
	}

	if err != nil {
		return err
	}

	// todo: decide on best way to know our ext address
	p.table.Update(node.New(k, net.JoinHostPort(addr, port)))
	return nil
}

func (p *discovery) Ping(peer p2pcrypto.PublicKey) error {
	p.logger.Info("send ping request Peer: %v", peer)
	data := &pb.Ping{ID: p.local.PublicKey().Bytes(), ListenAddress: p.local.Address()}
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

	err = p.msgServer.SendRequest(PINGPONG, payload, peer, foo)

	if err != nil {
		return err
	}

	timeout := time.NewTimer(MessageTimeout) // todo: check whether this is useless because of `requestLifetime`
	select {
	case id := <-ch:
		if id == nil {
			return errors.New("failed sending message")
		}
		if !bytes.Equal(id, peer.Bytes()) {
			return errors.New("got pong with different public key")
		}
	case <-timeout.C:
		return errors.New("ping timeouted")
	}

	return nil
}
