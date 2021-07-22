package peersync

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
)

type waitPeersReq struct {
	ch  chan []p2pcrypto.PublicKey
	min int
}

type peersWatcher struct {
	added, expired chan p2pcrypto.PublicKey
	requests       chan *waitPeersReq
}

func (p *peersWatcher) waitPeers(ctx context.Context, n int) ([]p2pcrypto.PublicKey, error) {
	req := waitPeersReq{min: n, ch: make(chan []p2pcrypto.PublicKey, 1)}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case p.requests <- &req:
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case prs := <-req.ch:
		return prs, nil
	}
}

func (p *peersWatcher) run(ctx context.Context) error {

	var (
		peersMap = map[p2pcrypto.PublicKey]struct{}{}
		requests = map[*waitPeersReq]struct{}{}
	)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case peer := <-p.added:
			peersMap[peer] = struct{}{}
			for req := range requests {
				if len(peersMap) >= req.min {
					req.ch <- mapToSlice(peersMap)
					delete(requests, req)
				}
			}
		case peer := <-p.expired:
			delete(peersMap, peer)
		case req := <-p.requests:
			if len(peersMap) >= req.min {
				req.ch <- mapToSlice(peersMap)
			} else {
				requests[req] = struct{}{}
			}
		}
	}
}

func mapToSlice(peersMap map[p2pcrypto.PublicKey]struct{}) []p2pcrypto.PublicKey {
	rst := make([]p2pcrypto.PublicKey, 0, len(peersMap))
	for peer := range peersMap {
		rst = append(rst, peer)
	}
	return rst
}
