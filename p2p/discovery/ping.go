package discovery

import (
	"bytes"
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"net"
	"time"

	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

func (p *protocol) newPingRequestHandler() func(msg server.Message) []byte {
	return func(msg server.Message) []byte {
		p.logger.Info("handle ping request")
		pinged := &node.NodeInfo{}
		err := types.BytesToInterface(msg.Bytes(), pinged)
		if err != nil {
			p.logger.Error("failed to deserialize ping message err=", err)
			panic("WTF")
			return nil
		}

		if err := p.verifyPinger(msg.Metadata().FromAddress, pinged); err != nil {
			p.logger.Error("msg contents were not valid err=", err)
			return nil
		}

		//pong
		payload, err := types.InterfaceToBytes(p.local)
		// TODO: include the resolved To address
		if err != nil {
			p.logger.Error("Error marshaling response message (Ping)")
			return nil
		}

		return payload
	}
}

func (p *protocol) verifyPinger(from net.Addr, pi *node.NodeInfo) error {
	// todo : Validate ToAddr or drop it.
	// todo: check the address provided with an extra ping before updating. ( if we haven't checked it for a while )
	// todo: decide on best way to know our ext address

	if err := pi.Valid(); err != nil {
		return err
	}

	//TODO: only accept local (unspecified/loopback) IPs from other local ips.
	ipfrom, _, _ := net.SplitHostPort(from.String())
	pi.IP = net.ParseIP(ipfrom)

	// inbound ping is the actual source of this node info
	p.table.AddAddress(pi, pi)
	return nil
}

// Ping notifies `peer` about our p2p identity.
func (p *protocol) Ping(peer p2pcrypto.PublicKey) error {
	p.logger.Debug("send ping request Peer: %v", peer)

	data, err := types.InterfaceToBytes(p.local)
	if err != nil {
		return err
	}
	ch := make(chan []byte)
	foo := func(msg []byte) {
		defer close(ch)
		p.logger.Debug("handle ping response from %v", peer.String())
		sender := &node.NodeInfo{}
		err := types.BytesToInterface(msg, sender)

		if err != nil {
			p.logger.Warning("got unreadable pong. err=%v", err)
			return
		}

		// todo: if we pinged it we already have id so no need to update
		// todo : but what if id or listen address has changed ?

		ch <- sender.ID.Bytes()
	}

	err = p.msgServer.SendRequest(PINGPONG, data, peer, foo)

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
