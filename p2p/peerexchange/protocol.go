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
	"go.uber.org/atomic"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/addressbook"
)

const (
	protocolName = "/peerexchange/v1.0.0"
	// messageTimeout is the timeout for the whole stream lifetime.
	messageTimeout = 10 * time.Second
)

// Peer Exchange protocol configuration.
type PeerExchangeConfig struct {
	// Peers that have not been successfully connected to for
	// this time are considered stale and not shared with other peers
	stalePeerTimeout time.Duration `mapstructure:"stale-peer-timeout"`
}

func DefaultPeerExchangeConfig() PeerExchangeConfig {
	return PeerExchangeConfig{
		stalePeerTimeout: time.Minute * 30,
	}
}

type peerExchange struct {
	advertise atomic.Pointer[ma.Multiaddr]

	h      host.Host
	book   *addressbook.AddrBook
	logger log.Log
	config PeerExchangeConfig
}

// newPeerExchange is a constructor for a protocol protocol provider.
func newPeerExchange(h host.Host, rt *addressbook.AddrBook, advertise ma.Multiaddr, log log.Log, config PeerExchangeConfig) *peerExchange {
	pe := &peerExchange{
		h:      h,
		book:   rt,
		logger: log,
		config: config,
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
	addr = addr.Encapsulate(id)
	info, err := addressbook.ParseAddrInfo(addr.String())
	if err != nil {
		logger.Debug("failed to parse address", log.Stringer("address", addr), log.Err(err))
		return
	}
	p.book.AddAddress(info, info)

	results := p.book.GetKnownAddressesCache()
	response := make([]string, 0, len(results))
	now := time.Now()
	for _, addr := range results {
		if addr.Addr.ID == stream.Conn().RemotePeer() {
			continue
		}
		// Filter out addresses that have not been connected for a while
		// but optimistically keep addresses we have not tried yet.
		if addr.LastAttempt.IsZero() || now.Sub(addr.LastSuccess) < p.config.stalePeerTimeout {
			response = append(response, addr.Addr.RawAddr)
		}
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
func (p *peerExchange) Request(ctx context.Context, pid peer.ID) ([]*addressbook.AddrInfo, error) {
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

	addrs, _, err := codec.DecodeStringSlice(bufio.NewReader(stream))
	if err != nil {
		return nil, fmt.Errorf("failed to read addresses in response: %w", err)
	}

	infos := make([]*addressbook.AddrInfo, 0, len(addrs))
	for _, raw := range addrs {
		info, err := addressbook.ParseAddrInfo(raw)
		if err != nil {
			return nil, fmt.Errorf("failed parsing: %w", err)
		}
		if info.ID == p.h.ID() {
			return nil, fmt.Errorf("peer shouldn't reply with address for our id")
		}
		infos = append(infos, info)
	}
	return infos, nil
}
