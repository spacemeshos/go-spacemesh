package peerexchange

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/book"
)

// crawler is used to crawl reachable peers and query them for addresses.
type crawler struct {
	logger log.Log
	host   host.Host

	book *book.Book
	disc *peerExchange
}

func newCrawler(h host.Host, book *book.Book, disc *peerExchange, logger log.Log) *crawler {
	return &crawler{
		logger: logger,
		host:   h,
		disc:   disc,
		book:   book,
	}
}

// Crawl crawls the network until context is canceled.
func (r *crawler) Crawl(ctx context.Context, period time.Duration) error {
	const concurrent = 5
	var eg errgroup.Group
	for {
		addrs := r.book.DrainQueue(concurrent)
		if len(addrs) == 0 {
			return errors.New("can't connect to the network without addresses")
		}
		for _, addr := range addrs {
			src, err := peer.AddrInfoFromP2pAddr(addr)
			if err != nil {
				return fmt.Errorf("can't parse %v: %w", addr, err)
			}
			eg.Go(func() error {
				err = r.host.Connect(ctx, *src)
				if err != nil {
					r.book.Update(src.ID.String(), book.Fail, book.Disconnected)
					r.logger.With().Debug("failed to connect to peer", log.Stringer("peer", src.ID), log.Err(err))
					return nil
				}
				res, err := r.disc.Request(ctx, src.ID)
				if err != nil {
					r.book.Update(src.ID.String(), book.Fail, book.Disconnected)
					r.host.Peerstore().ClearAddrs(src.ID)
					r.logger.With().Debug("peer failed to respond to protocol queries",
						log.String("peer", src.ID.String()),
						log.Err(err))
				} else {
					// TODO(dshulyak) will be correct to call after EventHandshakeComplete is received
					r.book.Update(src.ID.String(), book.Success, book.Connected)
					for _, a := range res {
						maddr, err := ma.NewMultiaddr(a)
						if err != nil {
							return nil
						}
						_, id := peer.SplitAddr(maddr)
						if id == "" {
							return nil
						}
						r.book.Add(src.ID.String(), id.String(), maddr)
					}
				}
				return ctx.Err()
			})
		}
		if err := eg.Wait(); err != nil {
			return err
		}
		select {
		case <-time.After(period):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
