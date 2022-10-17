package peerexchange

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
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
	listen *atomic.Uint32

	h      host.Host
	book   *addressbook.AddrBook
	logger log.Log
	config PeerExchangeConfig
}

// newPeerExchange is a constructor for a protocol protocol provider.
func newPeerExchange(h host.Host, rt *addressbook.AddrBook, listen uint16, log log.Log, config PeerExchangeConfig) *peerExchange {
	ga := &peerExchange{
		h:      h,
		book:   rt,
		logger: log,
		listen: atomic.NewUint32(uint32(listen)),
		config: config,
	}
	h.SetStreamHandler(protocolName, ga.handler)
	return ga
}

func (p *peerExchange) handler(stream network.Stream) {
	defer stream.Close()
	t := time.Now()
	logger := p.logger.WithFields(log.String("protocol", protocolName),
		log.String("from", stream.Conn().RemotePeer().Pretty())).With()

	port, _, err := codec.DecodeCompact16(stream)
	if err != nil {
		logger.Warning("failed to decode request into address", log.Err(err))
		return
	}
	logger.Debug("got request", log.Uint16("listen-port", port))
	if port != 0 {
		ip, err := manet.ToIP(stream.Conn().RemoteMultiaddr())
		if err != nil {
			logger.Warning("failed to recover ip from the connection", log.Err(err))
			return
		}
		ma, err := manet.FromNetAddr(&net.TCPAddr{
			IP:   ip,
			Port: int(port),
		})
		if err != nil {
			logger.Error("failed to parse netaddr", log.Err(err))
			return
		}
		raw := fmt.Sprintf("%s/p2p/%s", ma, stream.Conn().RemotePeer())
		info, err := addressbook.ParseAddrInfo(raw)
		if err != nil {
			logger.Error("failed to parse created address", log.String("address", raw), log.Err(err))
			return
		}
		p.book.AddAddress(info, info)
	}

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
		logger.Warning("failed to write response", log.Err(err))
		return
	}
	logger.Debug("response is sent",
		log.Int("size", len(response)),
		log.Duration("time_to_make", time.Since(t)))
}

// UpdateExternalPort updates port that is used for advertisement.
func (p *peerExchange) UpdateExternalPort(port uint16) {
	p.listen.Store(uint32(port))
}

// ExternalPort returns currently configured external port.
func (p *peerExchange) ExternalPort() uint16 {
	return uint16(p.listen.Load())
}

// Request addresses from a remote node, it will block and return the results returned from the node.
func (p *peerExchange) Request(ctx context.Context, pid peer.ID) ([]*addressbook.AddrInfo, error) {
	logger := p.logger.WithContext(ctx).WithFields(
		log.String("type", "getaddresses"),
		log.String("to", pid.String())).With()
	logger.Debug("sending request")

	stream, err := p.h.NewStream(network.WithNoDial(ctx, "existing"), pid, protocolName)
	if err != nil {
		return nil, fmt.Errorf("failed to create a discovery stream: %w", err)
	}
	defer stream.Close()

	_ = stream.SetDeadline(time.Now().Add(messageTimeout))
	defer stream.SetDeadline(time.Time{})
	if _, err := codec.EncodeCompact16(stream, p.ExternalPort()); err != nil {
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
