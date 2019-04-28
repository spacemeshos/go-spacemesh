package dht

import (
	"bytes"
	"errors"
	"net"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/p2p/dht/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

func (p *discovery) newPingRequestHandler() func(msg server.Message) []byte {
	return func(msg server.Message) []byte {
		p.logger.Info("handle ping request")
		req := &pb.Ping{}
		if err := proto.Unmarshal(msg.Bytes(), req); err != nil {
			p.logger.Error("failed to deserialize ping message err=", err)
			return nil
		}

		if err := p.verifyPinger(msg.Metadata().FromAddress, req); err != nil {
			p.logger.Error("msg contents were not valid err=", err)
			return nil
		}

		//pong
		payload, err := proto.Marshal(&pb.Ping{
			Me:     &pb.NodeInfo{NodeId: p.local.PublicKey().Bytes(), TCPAddress: p.localTcpAddress, UDPAddress: p.localUdpAddress},
			ToAddr: msg.Metadata().FromAddress.String()})
		// TODO: include the resolve To address

		if err != nil {
			p.logger.Error("Error marshaling response message (Ping)")
			return nil
		}

		return payload
	}
}

func extractAddress(from net.Addr, addrString string) (string, error) {
	//extract port
	_, port, err := net.SplitHostPort(addrString)
	if err != nil {
		return "", err
	}

	addr, _, err := net.SplitHostPort(from.String())
	if err != nil {
		addr = from.String()
	}

	ip := net.ParseIP(addr)
	if ip == nil {
		return "", errors.New("canno't parse incoming ip")
	}

	return net.JoinHostPort(addr, port), nil
}

func (p *discovery) verifyPinger(from net.Addr, pi *pb.Ping) error {
	k, err := p2pcrypto.NewPubkeyFromBytes(pi.Me.NodeId)

	if err != nil {
		return err
	}

	tcp, err := extractAddress(from, pi.Me.TCPAddress)
	if err != nil {
		return err
	}
	udp, err := extractAddress(from, pi.Me.UDPAddress)
	if err != nil {
		return nil
	}

	// todo : Validate ToAddr or drop it.

	//todo: check the address provided with an extra ping before updating. ( if we haven't checked it for a while )
	// todo: decide on best way to know our ext address
	p.table.Update(discNode{node.New(k, tcp), udp})
	return nil
}

func (p *discovery) Ping(peer p2pcrypto.PublicKey) error {
	p.logger.Info("send ping request Peer: %v", peer)

	//addresses := p.table.internalLookup(peer)
	//if len(addresses) == 0 || addresses[0].String() != peer.String() {
	//	return errors.New("Cant find node in routing table")
	//}
	//toAddr := addresses[0].udpAddress

	data := &pb.Ping{Me: &pb.NodeInfo{NodeId: p.local.PublicKey().Bytes(), TCPAddress: p.localTcpAddress, UDPAddress: p.localUdpAddress}, ToAddr: ""}
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

		ch <- data.Me.NodeId
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
