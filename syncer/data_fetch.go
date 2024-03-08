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
	ids      idProvider
	tortoise system.Tortoise

	mu        sync.Mutex
	atxSynced map[types.EpochID]map[p2p.Peer]struct{}
}

// NewDataFetch creates a new DataFetch instance.
func NewDataFetch(
	msh meshProvider,
	fetch fetcher,
	ids idProvider,
	tortoise system.Tortoise,
	lg log.Log,
) *DataFetch {
	return &DataFetch{
		fetcher:   fetch,
		logger:    lg,
		msh:       msh,
		ids:       ids,
		tortoise:  tortoise,
		atxSynced: map[types.EpochID]map[p2p.Peer]struct{}{},
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

// PollMaliciousProofs polls all peers for malicious NodeIDs.
func (d *DataFetch) PollMaliciousProofs(ctx context.Context) error {
	peers := d.fetcher.SelectBestShuffled(fetch.RedundantPeers)
	logger := d.logger.WithContext(ctx)

	maliciousIDs := make(chan fetch.MaliciousIDs, len(peers))
	var eg errgroup.Group
	fetchErr := threadSafeErr{}
	for _, peer := range peers {
		peer := peer
		eg.Go(func() error {
			data, err := d.fetcher.GetMaliciousIDs(ctx, peer)
			if err != nil {
				malPeerError.Inc()
				logger.With().Debug("failed to get malicious IDs", log.Err(err), log.Stringer("peer", peer))
				fetchErr.join(err)
				return nil
			}
			var malIDs fetch.MaliciousIDs
			if err := codec.Decode(data, &malIDs); err != nil {
				logger.With().Debug("failed to decode", log.Err(err))
				fetchErr.join(err)
				return nil
			}
			logger.With().Debug("received malicious id from peer", log.Stringer("peer", peer))
			maliciousIDs <- malIDs
			return nil
		})
	}
	_ = eg.Wait()
	close(maliciousIDs)

	allIds := make(map[types.NodeID]struct{})
	success := false
	for ids := range maliciousIDs {
		success = true
		for _, id := range ids.NodeIDs {
			allIds[id] = struct{}{}
		}
	}
	if !success {
		return fetchErr.err
	}

	var idsToFetch []types.NodeID
	for nodeID := range allIds {
		if exists, err := d.ids.IdentityExists(nodeID); err != nil {
			logger.With().Error("failed to check identity", log.Err(err))
			continue
		} else if !exists {
			logger.With().Info("malicious identity does not exist", log.Stringer("identity", nodeID))
			continue
		}
		idsToFetch = append(idsToFetch, nodeID)
	}

	if err := d.fetcher.GetMalfeasanceProofs(ctx, idsToFetch); err != nil {
		return fmt.Errorf("getting malfeasance proofs: %w", err)
	}
	return nil
}

// PollLayerData polls all peers for data in the specified layer.
func (d *DataFetch) PollLayerData(ctx context.Context, lid types.LayerID, peers ...p2p.Peer) error {
	if len(peers) == 0 {
		peers = d.fetcher.SelectBestShuffled(fetch.LayersPeer)
		if len(peers) == 0 {
			return errNoPeers
		}
	}

	logger := d.logger.WithContext(ctx).WithFields(lid)
	layerData := make(chan fetch.LayerData, len(peers))
	var eg errgroup.Group
	fetchErr := threadSafeErr{}
	for _, peer := range peers {
		peer := peer
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
		peer := peer
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

func (d *DataFetch) pickAtxPeer(epoch types.EpochID, peers []p2p.Peer) p2p.Peer {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.atxSynced[epoch]; !ok {
		d.atxSynced[epoch] = map[p2p.Peer]struct{}{}
		delete(d.atxSynced, epoch-1)
	}
	for _, p := range peers {
		if _, ok := d.atxSynced[epoch][p]; !ok {
			return p
		}
	}
	return p2p.NoPeer
}

func (d *DataFetch) updateAtxPeer(epoch types.EpochID, peer p2p.Peer) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.atxSynced[epoch][peer] = struct{}{}
}

// GetEpochATXs fetches all ATXs published in the specified epoch from a peer.
func (d *DataFetch) GetEpochATXs(ctx context.Context, epoch types.EpochID) error {
	peers := d.fetcher.SelectBestShuffled(fetch.RedundantPeers)
	if len(peers) == 0 {
		return errNoPeers
	}
	peer := d.pickAtxPeer(epoch, peers)
	if peer == p2p.NoPeer {
		d.logger.WithContext(ctx).With().Debug("synced atxs from all peers",
			epoch,
			log.Int("peers", len(peers)),
		)
		return nil
	}

	ed, err := d.fetcher.PeerEpochInfo(ctx, peer, epoch)
	if err != nil {
		atxPeerError.Inc()
		return fmt.Errorf("get epoch info (peer %v): %w", peer, err)
	}
	if len(ed.AtxIDs) == 0 {
		d.logger.WithContext(ctx).With().Debug("peer have zero atx",
			epoch,
			log.Stringer("peer", peer),
		)
		return nil
	}
	d.updateAtxPeer(epoch, peer)
	d.fetcher.RegisterPeerHashes(peer, types.ATXIDsToHashes(ed.AtxIDs))
	missing := d.tortoise.GetMissingActiveSet(epoch+1, ed.AtxIDs)
	d.logger.WithContext(ctx).With().Debug("fetching atxs",
		epoch,
		log.Stringer("peer", peer),
		log.Int("total", len(ed.AtxIDs)),
		log.Int("missing", len(missing)),
	)
	if len(missing) > 0 {
		if err := d.fetcher.GetAtxs(ctx, missing); err != nil {
			return fmt.Errorf("get ATXs: %w", err)
		}
	}
	return nil
}
