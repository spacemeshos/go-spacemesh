package syncer

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/system"
)

var errNoPeers = errors.New("no peers")

// DataFetch contains the logic of fetching mesh data.
type DataFetch struct {
	fetcher

	logger   log.Log
	msh      meshProvider
	tortoise system.Tortoise
}

// NewDataFetch creates a new DataFetch instance.
func NewDataFetch(
	msh meshProvider,
	fetch fetcher,
	tortoise system.Tortoise,
	lg log.Log,
) *DataFetch {
	return &DataFetch{
		fetcher:  fetch,
		logger:   lg,
		msh:      msh,
		tortoise: tortoise,
	}
}

type threadSafeErr struct {
	err error
	mu  sync.Mutex
}

func (e *threadSafeErr) join(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.err = errors.Join(e.err, err)
}

// PollLayerData polls all peers for data in the specified layer.
func (d *DataFetch) PollLayerData(ctx context.Context, lid types.LayerID, peers ...p2p.Peer) error {
	if len(peers) == 0 {
		peers = d.fetcher.SelectBestShuffled(fetch.RedundantPeers)
		if len(peers) == 0 {
			return errNoPeers
		}
	}

	logger := d.logger.WithContext(ctx).WithFields(lid)
	layerData := make(chan fetch.LayerData, len(peers))
	var eg errgroup.Group
	fetchErr := threadSafeErr{}
	for _, peer := range peers {
		eg.Go(func() error {
			data, err := d.fetcher.GetLayerData(ctx, peer, lid)
			if err != nil {
				layerPeerError.Inc()
				logger.With().Debug("failed to get layer data", log.Err(err), log.Stringer("peer", peer))
				fetchErr.join(err)
				return nil
			}
			var ld fetch.LayerData
			if err := codec.Decode(data, &ld); err != nil {
				logger.With().Debug("failed to decode", log.Err(err))
				fetchErr.join(err)
				return nil
			}
			logger.With().Debug("received layer data from peer", log.Stringer("peer", peer))
			registerLayerHashes(d.fetcher, peer, &ld)
			layerData <- ld
			return nil
		})
	}
	_ = eg.Wait()
	close(layerData)

	allBallots := make(map[types.BallotID]struct{})
	success := false
	for ld := range layerData {
		success = true
		for _, id := range ld.Ballots {
			allBallots[id] = struct{}{}
		}
	}
	if !success {
		return fetchErr.err
	}

	if err := d.fetcher.GetBallots(ctx, maps.Keys(allBallots)); err != nil {
		return fmt.Errorf("getting ballots: %w", err)
	}
	return nil
}

// registerLayerHashes registers hashes with the peer that provides these hashes.
func registerLayerHashes(fetcher fetcher, peer p2p.Peer, data *fetch.LayerData) {
	if len(data.Ballots) == 0 {
		return
	}
	var layerHashes []types.Hash32
	for _, ballotID := range data.Ballots {
		layerHashes = append(layerHashes, ballotID.AsHash32())
	}
	fetcher.RegisterPeerHashes(peer, layerHashes)
}

func (d *DataFetch) PollLayerOpinions(
	ctx context.Context,
	lid types.LayerID,
	needCert bool,
	peers []p2p.Peer,
) ([]*fetch.LayerOpinion, []*types.Certificate, error) {
	logger := d.logger.WithContext(ctx).WithFields(lid)
	opinions := make(chan *fetch.LayerOpinion, len(peers))
	var eg errgroup.Group
	fetchErr := threadSafeErr{}
	for _, peer := range peers {
		eg.Go(func() error {
			data, err := d.fetcher.GetLayerOpinions(ctx, peer, lid)
			if err != nil {
				opnsPeerError.Inc()
				logger.With().Debug("received peer error for layer opinions", log.Err(err), log.Stringer("peer", peer))
				fetchErr.join(err)
				return nil
			}
			var lo fetch.LayerOpinion
			if err := codec.Decode(data, &lo); err != nil {
				logger.With().Debug("failed to decode layer opinion", log.Err(err))
				fetchErr.join(err)
				return nil
			}
			logger.With().Debug("received layer opinion", log.Stringer("peer", peer))
			lo.SetPeer(peer)
			opinions <- &lo
			return nil
		})
	}
	_ = eg.Wait()
	close(opinions)

	var allOpinions []*fetch.LayerOpinion
	success := false
	for op := range opinions {
		success = true
		allOpinions = append(allOpinions, op)
	}
	if !success {
		return nil, nil, fetchErr.err
	}

	certs := make([]*types.Certificate, 0, len(allOpinions))
	if needCert {
		peerCerts := map[types.BlockID][]p2p.Peer{}
		for _, opinion := range allOpinions {
			if opinion.Certified == nil {
				continue
			}
			if _, ok := peerCerts[*opinion.Certified]; !ok {
				peerCerts[*opinion.Certified] = []p2p.Peer{}
			}
			peerCerts[*opinion.Certified] = append(peerCerts[*opinion.Certified], opinion.Peer())
			// note that we want to fetch block certificate for types.EmptyBlockID as well,
			// but we don't need to register hash for the actual block fetching
			if *opinion.Certified != types.EmptyBlockID {
				d.fetcher.RegisterPeerHashes(
					opinion.Peer(),
					[]types.Hash32{opinion.Certified.AsHash32()},
				)
			}
		}
		for bid, bidPeers := range peerCerts {
			cert, err := d.fetcher.GetCert(ctx, lid, bid, bidPeers)
			if err != nil {
				certPeerError.Inc()
				continue
			}
			certs = append(certs, cert)
		}
	}
	return allOpinions, certs, nil
}
