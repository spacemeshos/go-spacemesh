package peerexchange

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/book"
)

// crawler is used to crawl reachable peers and query them for addresses.
type crawler struct {
	logger log.Log
	host   host.Host

	book2 *book.Book
	disc  *peerExchange
}

func newCrawler(h host.Host, book *book.Book, disc *peerExchange, logger log.Log) *crawler {
	return &crawler{
		logger: logger,
		host:   h,
		disc:   disc,
	}
}

// Crawl crawls the network until context is canceled.
func (r *crawler) Crawl(ctx context.Context, period time.Duration) error {
	const concurrent = 5
	eg, gctx := errgroup.WithContext(ctx)
	for {
		addrs := r.book2.DrainQueue(concurrent)
		if len(addrs) == 0 {
			return errors.New("can't connect to the network without addresses")
		}
		for _, addr := range addrs {
			src, err := peer.AddrInfoFromP2pAddr(addr)
			if err != nil {
				return fmt.Errorf("can't parse %v: %w", addr, err)
			}
			eg.Go(func() error {
				err = r.host.Connect(gctx, *src)
				if err != nil {
					r.logger.With().Debug("failed to connect to peer", log.Stringer("peer", src.ID), log.Err(err))
					return nil
				}
				res, err := r.disc.Request(gctx, src.ID)
				if err != nil {
					r.book2.Update(src.ID.String(), book.Fail, book.Disconnected)
					r.host.Peerstore().ClearAddrs(src.ID)
					r.logger.With().Debug("peer failed to respond to protocol queries",
						log.String("peer", src.ID.String()),
						log.Err(err))
				} else {
					r.book2.Update(src.ID.String(), book.Success, book.Connected)
					for _, a := range res {
						if a.ID == r.host.ID() {
							continue
						}
						// TODO(dshulyak) will be correct to call after EventHandshakeComplete is received
						for _, addr := range a.Addrs {
							r.book2.Add(src.ID.String(), a.ID.String(), addr)
						}
					}
				}
				return nil
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
