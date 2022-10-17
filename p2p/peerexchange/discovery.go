package peerexchange

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"time"

	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/p2p/host/eventbus"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/addressbook"
)

const (
	// BootNodeTag is the tag used to identify boot nodes in connectionManager.
	BootNodeTag = "bootnode"
)

// Config for Discovery.
type Config struct {
	Bootnodes            []string
	DataDir              string
	CheckInterval        time.Duration      // Interval to check for dead|alive peers in the book.
	CheckTimeout         time.Duration      // Timeout to connect while node check for dead|alive peers in the book.
	CheckPeersNumber     int                // Number of peers to check for dead|alive peers in the book.
	CheckPeersUsedBefore time.Duration      // Time to wait before checking for dead|alive peers in the book.
	PeerExchange         PeerExchangeConfig // Configuration for Peer Exchange protocol
}

// Discovery is struct that holds the protocol components, the protocol definition, the addr book data structure and more.
type Discovery struct {
	logger log.Log
	host   host.Host
	cfg    Config

	cancel context.CancelFunc
	eg     errgroup.Group

	book  *addressbook.AddrBook
	crawl *crawler
}

// New creates a Discovery instance.
func New(logger log.Log, h host.Host, config Config) (*Discovery, error) {
	ctx, cancel := context.WithCancel(context.Background())
	d := &Discovery{
		cfg:    config,
		logger: logger,
		host:   h,
		cancel: cancel,
		book: addressbook.NewAddrBook(
			addressbook.DefaultAddressBookConfigWithDataDir(config.DataDir),
			logger,
		),
	}
	d.eg.Go(func() error {
		d.book.Persist(ctx)
		return ctx.Err()
	})

	bootnodes := make([]*addressbook.AddrInfo, 0, len(config.Bootnodes))
	for _, raw := range config.Bootnodes {
		info, err := addressbook.ParseAddrInfo(raw)
		if err != nil {
			return nil, fmt.Errorf("failed to parse bootstrap node: %w", err)
		}
		bootnodes = append(bootnodes, info)
	}
	best, err := bestHostAddress(h)
	if err != nil {
		return nil, err
	}
	d.book.AddAddresses(bootnodes, best)

	protocol := newPeerExchange(h, d.book, portFromHost(logger, h), logger, config.PeerExchange)
	d.crawl = newCrawler(h, d.book, protocol, logger)
	sub, err := h.EventBus().Subscribe(new(event.EvtLocalAddressesUpdated), eventbus.BufSize(4))
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to eventbus: %w", err)
	}
	d.eg.Go(func() error {
		defer sub.Close()
		for {
			select {
			case <-sub.Out():
				port := portFromHost(logger, h)
				if port != 0 {
					protocol.UpdateExternalPort(port)
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	})
	d.eg.Go(func() error {
		d.CheckBook(ctx)
		return ctx.Err()
	})
	return d, nil
}

// Stop stops the discovery service.
func (d *Discovery) Stop() {
	d.cancel()
	d.eg.Wait()
}

// Bootstrap runs a refresh and tries to get a minimum number of nodes in the addrBook.
func (d *Discovery) Bootstrap(ctx context.Context) error {
	return d.crawl.Bootstrap(ctx)
}

// ExternalPort returns currently configured external port.
func (d *Discovery) ExternalPort() uint16 {
	return d.crawl.disc.ExternalPort()
}

var errNotFound = errors.New("not found")

// bestHostAddress returns routable address if exists, otherwise it returns first available address.
func bestHostAddress(h host.Host) (*addressbook.AddrInfo, error) {
	best, err := bestNetAddress(h)
	if err == nil {
		ip, err := manet.ToIP(best)
		if err != nil {
			return nil, fmt.Errorf("failed to recover ip from host address %v: %w", best, err)
		}
		full := ma.Join(best, ma.StringCast("/p2p/"+h.ID().String()))
		addr := &addressbook.AddrInfo{
			IP:      ip,
			ID:      h.ID(),
			RawAddr: full.String(),
		}
		addr.SetAddr(full)
		return addr, nil
	}
	return &addressbook.AddrInfo{
		IP: net.IP{127, 0, 0, 1},
		ID: h.ID(),
	}, nil
}

func portFromHost(logger log.Log, h host.Host) uint16 {
	addr, err := bestNetAddress(h)
	if err != nil {
		logger.With().Warning("failed to find best host address. host won't be dialable", log.Err(err))
		return 0
	}
	logger.With().Info("selected new best address", log.String("address", addr.String()))
	port, err := portFromAddress(addr)
	if err != nil {
		logger.With().Warning("failed to find port from host. host won't be dialable", log.Err(err))
		return 0
	}
	return port
}

// bestNetAddress returns routable or first one.
func bestNetAddress(h host.Host) (ma.Multiaddr, error) {
	routable, err := routableNetAddress(h)
	if err == nil {
		return routable, nil
	}
	if len(h.Addrs()) > 0 {
		return h.Addrs()[0], nil
	}
	return nil, errNotFound
}

func routableNetAddress(h host.Host) (ma.Multiaddr, error) {
	for _, addr := range h.Addrs() {
		ip, err := manet.ToIP(addr)
		if err != nil {
			return nil, fmt.Errorf("failed to recover ip from host address %v: %w", addr, err)
		}
		if addressbook.IsRoutable(ip) {
			return addr, nil
		}
	}
	return nil, errNotFound
}

func portFromAddress(addr ma.Multiaddr) (uint16, error) {
	netaddr, err := manet.ToNetAddr(addr)
	if err != nil {
		return 0, fmt.Errorf("cast addr %s to netaddr: %w", addr, err)
	}
	_, portStr, err := net.SplitHostPort(netaddr.String())
	if err != nil {
		return 0, fmt.Errorf("parsed netaddr %s is not in expected format: %w", netaddr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, fmt.Errorf("convert port %s to int: %w", portStr, err)
	}
	if int(uint16(port)) < port {
		return 0, fmt.Errorf("port %d cant fit into uint16 %d", port, math.MaxUint16)
	}
	return uint16(port), nil
}
