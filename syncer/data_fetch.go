package syncer

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

var errNoPeers = errors.New("no peers")

// DataFetch contains the logic of fetching mesh data.
type DataFetch struct {
	fetcher

	logger  log.Log
	msh     meshProvider
	ids     idProvider
	asCache activeSetCache

	mu        sync.Mutex
	atxSynced map[types.EpochID]map[p2p.Peer]struct{}
}

// NewDataFetch creates a new DataFetch instance.
func NewDataFetch(
	msh meshProvider,
	fetch fetcher,
	ids idProvider,
	cache activeSetCache,
	lg log.Log,
) *DataFetch {
	return &DataFetch{
		fetcher:   fetch,
		logger:    lg,
		msh:       msh,
		ids:       ids,
		asCache:   cache,
		atxSynced: map[types.EpochID]map[p2p.Peer]struct{}{},
	}
}

type fetchResult struct {
	err error
	mu  sync.Mutex
}

func (e *fetchResult) joinError(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.err = errors.Join(e.err, err)
}

func (e *fetchResult) error() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.err
}

// PollMaliciousProofs polls all peers for malicious NodeIDs.
func (d *DataFetch) PollMaliciousProofs(ctx context.Context) error {
	peers := d.fetcher.SelectBestShuffled(fetch.RedundantPeers)
	logger := d.logger.WithContext(ctx)

	maliciousIDs := make(chan fetch.MaliciousIDs, len(peers))
	var eg errgroup.Group
	result := fetchResult{}
	for _, peer := range peers {
		peer := peer
		eg.Go(func() error {
			data, err := d.fetcher.GetMaliciousIDs(ctx, peer)
			if err != nil {
				malPeerError.Inc()
				logger.With().Debug("failed to get malicious IDs", log.Err(err), log.Stringer("peer", peer))
				result.joinError(err)
				return err
			}
			logger.With().Debug("received malicious id from peer", log.Stringer("peer", peer))
			var malIDs fetch.MaliciousIDs
			if err := codec.Decode(data, &malIDs); err != nil {
				logger.With().Debug("failed to decode", log.Err(err))
				result.joinError(err)
				return err
			}
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
		return result.error()
	}

	var idsToFetch []types.NodeID
	for nodeID := range allIds {
		if exists, err := d.ids.IdentityExists(nodeID); err != nil {
			logger.With().Error("failed to check identity", log.Err(err))
			continue
		} else if !exists {
			logger.With().Info("malicious identity does not exist", log.String("identity", nodeID.String()))
			continue
		}
		idsToFetch = append(idsToFetch, nodeID)
	}

	if len(idsToFetch) > 0 {
		logger.With().Info("fetching malfeasance proofs", log.Int("to_fetch", len(idsToFetch)))
		if err := d.fetcher.GetMalfeasanceProofs(ctx, idsToFetch); err != nil {
			return fmt.Errorf("getting malfeasance proofs: %w", err)
		}
	}

	return nil
}

// PollLayerData polls all peers for data in the specified layer.
func (d *DataFetch) PollLayerData(ctx context.Context, lid types.LayerID, peers ...p2p.Peer) error {
	if len(peers) == 0 {
		peers = d.fetcher.SelectBestShuffled(fetch.RedundantPeers)
		if len(peers) == 0 {
			return errNoPeers
		}
	}

	resp, err := d.fetcher.GetLayerData(ctx, peers, lid)
	if err != nil {
		return err
	}

	logger := d.logger.WithContext(ctx).WithFields(lid)
	alreadyFetched := make(map[types.BallotID]struct{})
	success := false
	var combinedErr error
	for results := 0; results < len(peers); results++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case resp := <-resp:
			logger.With().Debug("received layer data from peer", log.Stringer("peer", resp.Peer))

			if resp.Err != nil {
				layerPeerError.Inc()
				logger.With().Debug("received peer error for layer data", log.Err(resp.Err))
				if !success {
					combinedErr = errors.Join(combinedErr, resp.Err)
				}
				continue
			}
			var ld fetch.LayerData
			if err := codec.Decode(resp.Data, &ld); err != nil {
				logger.With().Debug("error converting bytes to LayerData", log.Err(err))
				if !success {
					combinedErr = errors.Join(combinedErr, err)
				}
				continue
			}
			registerLayerHashes(d.fetcher, resp.Peer, &ld)
			fetchLayerData(ctx, logger, d.fetcher, alreadyFetched, &ld)
			success = true
			combinedErr = nil
		}
	}
	return combinedErr
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

func fetchLayerData(
	ctx context.Context,
	logger log.Log,
	fetcher fetcher,
	alreadyFetched map[types.BallotID]struct{},
	data *fetch.LayerData,
) {
	var ballotsToFetch []types.BallotID
	for _, ballotID := range data.Ballots {
		if _, ok := alreadyFetched[ballotID]; !ok {
			alreadyFetched[ballotID] = struct{}{}
			ballotsToFetch = append(ballotsToFetch, ballotID)
		}
	}

	if len(ballotsToFetch) > 0 {
		logger.With().Debug("fetching new ballots", log.Int("to_fetch", len(ballotsToFetch)))
		if err := fetcher.GetBallots(ctx, ballotsToFetch); err != nil {
			logger.With().Warning("failed fetching new ballots",
				log.Array("ballot_ids", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
					for _, bid := range ballotsToFetch {
						encoder.AppendString(bid.String())
					}
					return nil
				})),
				log.Err(err))

			// syntactically invalid ballots are expected from malicious peers
		}
	}
}

func (d *DataFetch) PollLayerOpinions(
	ctx context.Context,
	lid types.LayerID,
	needCert bool,
	peers []p2p.Peer,
) ([]*fetch.LayerOpinion, []*types.Certificate, error) {
	resp, err := d.fetcher.GetLayerOpinions(ctx, peers, lid)
	if err != nil {
		return nil, nil, fmt.Errorf("requesting layer opinions: %w", err)
	}

	logger := d.logger.WithContext(ctx).WithFields(lid)
	opinions := make([]*fetch.LayerOpinion, 0, len(peers))
	success := false
	var combinedErr error
	for results := 0; results < len(peers); results++ {
		select {
		case <-ctx.Done():
			return opinions, nil, ctx.Err()
		case resp := <-resp:
			logger.Debug("received layer opinions from peer")
			var lo fetch.LayerOpinion
			if resp.Err != nil {
				opnsPeerError.Inc()
				logger.With().Debug("received peer error for layer opinions", log.Err(resp.Err))
				if !success {
					combinedErr = errors.Join(combinedErr, resp.Err)
				}
				continue
			}
			if err := codec.Decode(resp.Data, &lo); err != nil {
				logger.With().Debug("error decoding LayerOpinion", log.Err(err))
				if !success {
					combinedErr = errors.Join(combinedErr, err)
				}
				continue
			}
			lo.SetPeer(resp.Peer)
			opinions = append(opinions, &lo)
			success = true
			combinedErr = nil
		}
	}

	certs := make([]*types.Certificate, 0, len(opinions))
	if needCert {
		peerCerts := map[types.BlockID][]p2p.Peer{}
		for _, opinion := range opinions {
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
	return opinions, certs, combinedErr
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
	missing := d.asCache.GetMissingActiveSet(epoch+1, ed.AtxIDs)
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
