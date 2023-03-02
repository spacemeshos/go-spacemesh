package peerexchange

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/host/eventbus"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/natefinch/atomic"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/book"
)

const peersFile = "peers.txt"

// Config for Discovery.
type Config struct {
	Bootnodes            []string
	DataDir              string
	AdvertiseAddress     string // Address to advertise to a peers.
	MinPeers             int
	FastCrawl, SlowCrawl time.Duration
}

// Discovery is struct that holds the protocol components, the protocol definition, the addr book data structure and more.
type Discovery struct {
	logger log.Log
	host   host.Host
	cfg    Config

	cancel context.CancelFunc
	eg     errgroup.Group

	book  *book.Book
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
		book:   book.New(),
	}
	var advertise ma.Multiaddr
	if len(config.AdvertiseAddress) > 0 {
		var err error
		advertise, err = ma.NewMultiaddr(config.AdvertiseAddress)
		if err != nil {
			return nil, fmt.Errorf("address to advertise (%s) is invalid: %w", config.AdvertiseAddress, err)
		}
		for _, proto := range advertise.Protocols() {
			if proto.Code == ma.P_P2P {
				return nil, fmt.Errorf("address to advertise (%s) includes p2p identity", advertise.String())
			}
		}
	} else {
		var err error
		advertise, err = ma.NewComponent("tcp", strconv.Itoa(int(portFromHost(logger, h))))
		if err != nil {
			return nil, fmt.Errorf("create tcp multiaddr %w", err)
		}
	}
	for _, addr := range config.Bootnodes {
		maddr, id, err := parseIdMaddr(addr)
		if err != nil {
			return nil, err
		}
		d.book.Add(book.SELF, id.String(), maddr)
		d.book.Update(id.String(), book.Protect)
	}
	protocol := newPeerExchange(h, d.book, advertise, logger)
	d.crawl = newCrawler(h, d.book, protocol, logger)
	if len(config.DataDir) != 0 {
		if err := d.recovery(ctx); err != nil {
			return nil, err
		}
	}
	if len(config.AdvertiseAddress) == 0 {
		if err := d.watchPortChanges(ctx, protocol); err != nil {
			return nil, err
		}
	}
	d.scanPeers(ctx)
	return d, nil
}

func (d *Discovery) recovery(ctx context.Context) error {
	fpath := filepath.Join(d.cfg.DataDir, peersFile)
	f, err := os.Open(fpath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("can't recover from file %v: %w", fpath, err)
	}
	defer f.Close()
	if err == nil {
		if err := d.book.Recover(f); err != nil {
			return err
		}
	}
	d.eg.Go(func() error {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				buf := bytes.NewBuffer(nil)
				if err := d.book.Persist(buf); err != nil {
					return err
				}
				if err := atomic.WriteFile(fpath, buf); err != nil {
					return err
				}
			}
		}
	})
	return nil
}

func (d *Discovery) watchPortChanges(ctx context.Context, protocol *peerExchange) error {
	sub, err := d.host.EventBus().Subscribe(new(event.EvtLocalAddressesUpdated), eventbus.BufSize(4))
	if err != nil {
		return fmt.Errorf("failed to subscribe to eventbus: %w", err)
	}
	d.eg.Go(func() error {
		defer sub.Close()
		for {
			select {
			case <-sub.Out():
				port := portFromHost(d.logger, d.host)
				if port != 0 {
					advertise, err := ma.NewComponent("tcp", strconv.Itoa(int(portFromHost(d.logger, d.host))))
					if err != nil {
						d.logger.With().Error("failed to create tcp multiaddr", log.Err(err))
					} else {
						protocol.UpdateAdvertisedAddress(advertise)
					}
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	})
	return nil
}

// Stop stops the discovery service.
func (d *Discovery) Stop() {
	d.cancel()
	d.eg.Wait()
}

// scanPeers scans the network for new connections and peers.
func (d *Discovery) scanPeers(ctx context.Context) {
	period := d.cfg.FastCrawl
	concurrent := 5
	d.eg.Go(func() error {
		for {
			if err := d.crawl.Crawl(ctx, concurrent); err != nil {
				if errors.Is(err, context.Canceled) {
					return nil
				}
				return err
			}
			if len(d.host.Network().Peers()) >= d.cfg.MinPeers {
				period = d.cfg.SlowCrawl
				concurrent = 1
			} else {
				period = d.cfg.FastCrawl
				concurrent = 5
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(period):
			}
		}
	})
}

// AdvertisedAddress returns advertised address.
func (d *Discovery) AdvertisedAddress() ma.Multiaddr {
	return d.crawl.disc.AdvertisedAddress()
}

var errNotFound = errors.New("not found")

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
		if manet.IsPublicAddr(addr) {
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

func parseIdMaddr(raw string) (ma.Multiaddr, peer.ID, error) {
	maddr, err := ma.NewMultiaddr(raw)
	if err != nil {
		return nil, "", err
	}
	_, id := peer.SplitAddr(maddr)
	if id == "" {
		return nil, "", fmt.Errorf("invalid multiaddr: missing p2p part %v", id)
	}
	return maddr, id, err
}
