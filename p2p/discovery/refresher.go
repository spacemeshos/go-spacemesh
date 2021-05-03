package discovery

import (
	"context"
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"net"
	"time"
)

// XXX TODO: move this impl to the upper protocol struct

const (

	// minTimeBetweenQueries is a minimum time between attempts to query a peer.
	minTimeBetweenQueries = 5 * time.Second
	// lastQueriesCacheSiz9e is the maximum size of the query cache map
	lastQueriesCacheSize = 100

	maxConcurrentRequests = 3

	maxTries = 10
)

var defaultBackoffFunc = func(tries int) time.Duration { return time.Second * time.Duration(tries) }

// ErrBootAbort is returned when when bootstrap is canceled by context cancel
var ErrBootAbort = errors.New("bootstrap canceled by signal")

type pingerGetAddresser interface {
	Ping(context.Context, p2pcrypto.PublicKey) error
	GetAddresses(context.Context, p2pcrypto.PublicKey) ([]*node.Info, error)
}

// refresher is used to bootstrap and requestAddresses peers in the addrbook
type refresher struct {
	logger       log.Log
	localAddress *node.Info

	book      addressBook
	bootNodes []*node.Info

	backoffFunc func(tries int) time.Duration

	disc        pingerGetAddresser
	lastQueries map[p2pcrypto.PublicKey]time.Time
}

func newRefresher(local p2pcrypto.PublicKey, book addressBook, disc pingerGetAddresser, bootnodes []*node.Info, logger log.Log) *refresher {
	//todo: trigger requestAddresses every X with random nodes
	return &refresher{
		logger:       logger,
		localAddress: &node.Info{ID: local.Array(), IP: net.IPv4zero},
		book:         book,
		disc:         disc,
		bootNodes:    bootnodes,
		backoffFunc:  defaultBackoffFunc,
		lastQueries:  make(map[p2pcrypto.PublicKey]time.Time),
	}
}

// Bootstrap tries to collect `numpeers` new peers into the routing table. it stops if ctx is cancelled,
// otherwise it will keep trying. if the routing table is empty, bootnodes are loaded from provided config.
func (r *refresher) Bootstrap(ctx context.Context, numpeers int) error {
	var err error
	var servers []*node.Info
	var tries int

	// The following loop will add the pre-set bootstrap nodes and query them for results
	// if there were any results but we didn't reach numpeers it will try the same procedure
	// using the results as servers to query. every failed loop will wait a backoff
	// to let other nodes populate before flooding with queries.
	size := r.book.NumAddresses()
	if size == 0 {
		r.book.AddAddresses(r.bootNodes, r.localAddress)
		size = len(r.bootNodes)
		defer func() {
			// currently we only have  the discovery address of bootnodes in the configuration so let them pick their own neighbors.
			for _, b := range r.bootNodes {
				r.book.RemoveAddress(b.PublicKey())
			}
		}()
	}

	r.logger.Info("Bootstrap: starting with %v sized table", size)

loop:
	for {
		srv := r.book.AddressCache() // get fresh members to query
		servers = srv[:util.Min(numpeers, len(srv))]
		res := r.requestAddresses(ctx, servers)
		tries++

		newsize := r.book.NumAddresses()
		wanted := numpeers

		r.logger.With().Info("bootstrap attempt finished", log.Int("tries", tries), log.Int("results", len(res)), log.Int("addbook_size", newsize))

		if newsize-size >= wanted {
			r.logger.Info("Achieved bootstrap objective, got %v needed %v", newsize-size, wanted)
			break
		}

		if tries >= maxTries && newsize >= wanted {
			// NOTE: maybe we need to set the error here ?
			r.logger.Info("Stopping bootstrap after %v tries, address book already contains %v peers", tries, newsize)
			break
		}

		timer := time.NewTimer(r.backoffFunc(tries)) // BACKOFF

		//todo: stop refreshes with context
		select {
		case <-ctx.Done():
			err = ErrBootAbort
			break loop
		case <-timer.C:
			continue
		}
	}

	return err
}

func expire(m map[p2pcrypto.PublicKey]time.Time) {
	t := time.Now()
	c := 0
	for k, v := range m {
		if t.Sub(v) > minTimeBetweenQueries {
			delete(m, k)
			return
		}
		c++ // is better than go ?
		if c >= lastQueriesCacheSize {
			// delete last randomly selected key if none meet requirements
			//todo: use slice to keep order? this might result with quite new peer deleted
			delete(m, k)
			return
		}
	}
}

type queryResult struct {
	src *node.Info
	res []*node.Info
	err error
}

// pingThenGetAddresses is sending a ping, then getaddress, then return results on given chan.
func pingThenGetAddresses(ctx context.Context, p pingerGetAddresser, addr *node.Info, qr chan queryResult) {
	// TODO: check whether we pinged recently and maybe skip pinging
	err := p.Ping(ctx, addr.PublicKey())

	if err != nil {
		qr <- queryResult{src: addr, err: err}
		return
	}
	res, err := p.GetAddresses(ctx, addr.PublicKey())

	if err != nil {
		qr <- queryResult{src: addr, err: err}
		return
	}
	qr <- queryResult{addr, res, nil}
}

// requestAddresses will crawl the network looking for new peer addresses.
func (r *refresher) requestAddresses(ctx context.Context, servers []*node.Info) []*node.Info {
	// todo: here we stop only after we've tried querying or queried all addrs
	// 	maybe we should stop after we've reached a certain amount ? (needMoreAddresses..)
	// todo: revisit this area to think about if lastQueries is even needed, since we're going
	// 		to probably sleep for more than minTimeBetweenQueries between running requestAddresses
	//		calls. maybe we can use only the seen cache instead.
	var out []*node.Info

	seen := make(map[p2pcrypto.PublicKey]struct{})
	seen[r.localAddress.PublicKey()] = struct{}{}

	now := time.Now()
	pending := 0

	reschan := make(chan queryResult)
	for {
		for i := 0; i < len(servers) && pending < maxConcurrentRequests; i++ {
			addr := servers[i]
			lastQuery, ok := r.lastQueries[addr.PublicKey()]
			// Do not attempt to connect query peers we recently queried
			if ok && now.Sub(lastQuery) < minTimeBetweenQueries {
				continue
			}

			pending++

			if len(r.lastQueries) >= lastQueriesCacheSize {
				expire(r.lastQueries)
			}

			r.lastQueries[addr.PublicKey()] = time.Now()

			go pingThenGetAddresses(ctx, r.disc, addr, reschan)
		}

		if pending == 0 {
			return out
		}

		select {
		case cr := <-reschan:
			pending--
			if cr.err != nil {
				//todo: consider error and maybe remove
				//todo: count failed queries and remove not functioning
				r.logger.Warning("Peer %v didn't respond to protocol queries - err:%v", cr.src.String(), cr.err)
				continue
			}
			if cr.res != nil && len(cr.res) > 0 {
				for _, a := range cr.res {
					if _, ok := seen[a.PublicKey()]; ok {
						continue
					}

					if _, ok := r.lastQueries[a.PublicKey()]; ok {
						continue
					}
					out = append(out, a)
					r.book.AddAddress(a, cr.src)
					seen[a.PublicKey()] = struct{}{}
				}
			}
		case <-ctx.Done():
			return nil
		}

	}
}
