package peerexchange

import (
	"bufio"
	"context"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/spacemeshos/go-scale"
	"go.uber.org/atomic"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/book"
)

const (
	protocolName = "/peerexchange/v1.0.0"
	// messageTimeout is the timeout for the whole stream lifetime.
	messageTimeout = 10 * time.Second
	sharedPeers    = 10
)

type peerExchange struct {
	advertise atomic.Pointer[ma.Multiaddr]

	h      host.Host
	book   *book.Book
	logger log.Log
}

// newPeerExchange is a constructor for a protocol protocol provider.
func newPeerExchange(h host.Host, rt *book.Book, advertise ma.Multiaddr, log log.Log) *peerExchange {
	pe := &peerExchange{
		h:      h,
		book:   rt,
		logger: log,
	}
	pe.UpdateAdvertisedAddress(advertise)
	h.SetStreamHandler(protocolName, pe.handler)
	return pe
}

func (p *peerExchange) handler(stream network.Stream) {
	defer stream.Close()
	t := time.Now()
	logger := p.logger.WithFields(log.String("protocol", protocolName),
		log.String("from", stream.Conn().RemotePeer().Pretty())).With()

	buf, _, err := codec.DecodeByteSlice(stream)
	if err != nil {
		logger.Debug("failed to read advertised address", log.Err(err))
		return
	}
	addr, err := ma.NewMultiaddrBytes(buf)
	if err != nil {
		logger.Debug("failed to cast bytes to multiaddr", log.Err(err))
		return
	}
	logger.Debug("got request from address", log.Stringer("address", addr))
	if len(addr.Protocols()) == 1 {
		// in some setups we rely on a behavior that peer can learn routable address
		// of the other node, even if that node is not aware of its own routable address
		ip, err := manet.ToIP(stream.Conn().RemoteMultiaddr())
		if err != nil {
			logger.Debug("failed to recover ip from the connection", log.Err(err))
			return
		}
		ipcomp, err := manet.FromIP(ip)
		if err != nil {
			logger.Error("failed to create multiaddr from ip", log.Stringer("ip", ip))
			return
		}
		addr = ipcomp.Encapsulate(addr)
	}
	id, err := ma.NewComponent("p2p", stream.Conn().RemotePeer().String())
	if err != nil {
		logger.Error("failed to create p2p component", log.Err(err))
		return
	}
	p.book.Add(book.SELF, stream.Conn().RemotePeer().String(), addr.Encapsulate(id))

	share := p.book.TakeShareable(stream.Conn().RemotePeer().String(), sharedPeers)
	response := make([]string, 0, len(share))
	for _, addr := range share {
		response = append(response, addr.String())
	}
	// todo: limit results to message size
	_ = stream.SetDeadline(time.Now().Add(messageTimeout))
	defer stream.SetDeadline(time.Time{})
	wr := bufio.NewWriter(stream)
	_, err = codec.EncodeStringSlice(wr, response)
	if err == nil {
		err = wr.Flush()
	}
	if err != nil {
		logger.Debug("failed to write response", log.Err(err))
		return
	}
	logger.Debug("response is sent",
		log.Int("size", len(response)),
		log.Duration("time_to_make", time.Since(t)))
}

// UpdateAdvertisedAddress updates advertised address.
func (p *peerExchange) UpdateAdvertisedAddress(address ma.Multiaddr) {
	p.advertise.Store(&address)
}

// AdvertisedAddress returns advertised address.
func (p *peerExchange) AdvertisedAddress() ma.Multiaddr {
	return *p.advertise.Load()
}

// Request addresses from a remote node, it will block and return the results returned from the node.
func (p *peerExchange) Request(ctx context.Context, pid peer.ID) ([]string, error) {
	logger := p.logger.WithContext(ctx).WithFields(
		log.String("type", "getaddresses"),
		log.String("to", pid.String())).With()
	advertise := p.AdvertisedAddress()
	logger.Debug("sending request", log.Stringer("advertised address", advertise))

	stream, err := p.h.NewStream(network.WithNoDial(ctx, "existing"), pid, protocolName)
	if err != nil {
		return nil, fmt.Errorf("failed to create a discovery stream: %w", err)
	}
	defer stream.Close()

	_ = stream.SetDeadline(time.Now().Add(messageTimeout))
	defer stream.SetDeadline(time.Time{})
	if _, err := codec.EncodeByteSlice(stream, advertise.Bytes()); err != nil {
		return nil, fmt.Errorf("failed to send GetAddress request: %w", err)
	}

	dec := scale.NewDecoder(bufio.NewReader(stream))
	addrs, _, err := scale.DecodeStringSliceWithLimit(dec, sharedPeers)
	if err != nil {
		return nil, fmt.Errorf("failed to read addresses in response: %w", err)
	}
	return addrs, nil
}
