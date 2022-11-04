package syncer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

var errMeshHashDiverged = errors.New("mesh hash diverged with peer")

func minLayer(a, b types.LayerID) types.LayerID {
	if a.Before(b) {
		return a
	}
	return b
}

func (s *Syncer) stateSynced() bool {
	current := s.ticker.GetCurrentLayer()
	return current.Uint32() <= 1 || !s.mesh.ProcessedLayer().Before(current.Sub(1))
}

func (s *Syncer) processLayers(ctx context.Context) error {
	ctx = log.WithNewSessionID(ctx)
	if !s.ListenToATXGossip() {
		return errATXsNotSynced
	}

	s.logger.WithContext(ctx).With().Info("processing synced layers",
		log.Stringer("processed", s.mesh.ProcessedLayer()),
		log.Stringer("in_state", s.mesh.LatestLayerInState()),
		log.Stringer("last_synced", s.getLastSyncedLayer()))

	start := minLayer(s.mesh.LatestLayerInState(), s.mesh.ProcessedLayer())
	start = minLayer(start, s.getLastSyncedLayer())
	if start == types.GetEffectiveGenesis() {
		start = start.Add(1)
	}

	resyncPeers := make(map[p2p.Peer]struct{})
	for lid := start; !lid.After(s.getLastSyncedLayer()); lid = lid.Add(1) {
		if s.isClosed() {
			return errShuttingDown
		}

		logger := s.logger.WithContext(ctx).WithFields(lid, lid.GetEpoch())
		// layers should be processed in order. once we skip one layer, there is no point
		// continuing with later layers. return on error
		if _, err := s.beacon.GetBeacon(lid.GetEpoch()); err != nil {
			logger.Debug("beacon not available")
			return errBeaconNotAvailable
		}

		if s.patrol.IsHareInCharge(lid) {
			lag := types.NewLayerID(0)
			current := s.ticker.GetCurrentLayer()
			if current.After(lid) {
				lag = current.Sub(lid.Uint32())
			}
			if lag.Value < s.cfg.HareDelayLayers {
				logger.Info("skip validating layer: hare still working")
				return errHareInCharge
			}
		}

		if opinions, err := s.fetchOpinions(ctx, logger, lid); err == nil {
			if err = s.checkMeshAgreement(logger, lid, opinions); err != nil && errors.Is(err, errMeshHashDiverged) {
				logger.With().Warning("mesh hash diverged, trying to reach agreement",
					log.Stringer("diverged", lid.Sub(1)))
				if err = s.ensureMeshAgreement(ctx, logger, lid, opinions, resyncPeers); err != nil {
					logger.With().Warning("failed to reach mesh agreement with peers", log.Err(err))
				}
			}
			if err = s.adopt(ctx, lid, opinions); err != nil {
				logger.With().Warning("failed to adopt peer opinions", log.Err(err))
			}
		}
		// even if it fails to fetch opinions, we still go ahead to ProcessLayer so that the tortoise
		// has a chance to count ballots and form its own opinions

		if err := s.mesh.ProcessLayer(ctx, lid); err != nil {
			s.logger.WithContext(ctx).With().Warning("mesh failed to process layer from sync", lid, log.Err(err))
		}
	}
	s.logger.WithContext(ctx).With().Info("end of state sync",
		log.Bool("state_synced", s.stateSynced()),
		log.Stringer("last_synced", s.getLastSyncedLayer()),
		log.Stringer("processed", s.mesh.ProcessedLayer()))
	return nil
}

func (s *Syncer) needCert(logger log.Log, lid types.LayerID) (bool, error) {
	cutoff := s.certCutoffLayer()
	if !lid.After(cutoff) {
		return false, nil
	}
	cert, err := layers.GetCert(s.cdb, lid)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		logger.With().Error("state sync failed to get cert", log.Err(err))
		return false, err
	}
	return cert == nil, nil
}

func toList[T comparable](set map[T]struct{}) []T {
	result := make([]T, 0, len(set))
	for bid := range set {
		result = append(result, bid)
	}
	return result
}

func (s *Syncer) fetchOpinions(ctx context.Context, logger log.Log, lid types.LayerID) ([]*fetch.LayerOpinion, error) {
	logger.Info("polling layer opinions")
	opinions, err := s.dataFetcher.PollLayerOpinions(ctx, lid)
	if err != nil {
		logger.With().Warning("failed to fetch opinions", log.Err(err))
		return nil, fmt.Errorf("PollLayerOpinions: %w", err)
	}
	return opinions, nil
}

func (s *Syncer) checkMeshAgreement(logger log.Log, lid types.LayerID, opinions []*fetch.LayerOpinion) error {
	prevHash, err := layers.GetAggregatedHash(s.cdb, lid.Sub(1))
	if err != nil {
		logger.With().Error("failed to get prev agg hash", log.Err(err))
		return fmt.Errorf("opinions prev hash: %w", err)
	}
	for _, opn := range opinions {
		if opn.PrevAggHash != (types.Hash32{}) && opn.PrevAggHash != prevHash {
			return errMeshHashDiverged
		}
	}
	return nil
}

func (s *Syncer) adopt(ctx context.Context, lid types.LayerID, opinions []*fetch.LayerOpinion) error {
	logger := s.logger.WithContext(ctx).WithFields(lid)
	needCert, err := s.needCert(logger, lid)
	if err != nil {
		return err
	}
	if !needCert {
		logger.With().Debug("node already has certificate")
		return nil
	}

	var noCert int
	for _, opn := range opinions {
		// TODO: detect multiple hare certificate in the same network
		// https://github.com/spacemeshos/go-spacemesh/issues/3467
		if needCert {
			if opn.Cert == nil {
				noCert++
				logger.With().Debug("peer has no cert", log.Inline(opn))
			} else if err = s.adoptCert(ctx, lid, opn.Cert); err != nil {
				logger.With().Warning("failed to adopt cert", log.Inline(opn), log.Err(err))
			} else {
				logger.With().Info("adopted cert from peer", log.Inline(opn))
				needCert = false
				break
			}
		}
	}
	numPeers := len(opinions)
	if needCert {
		if noCert == numPeers {
			logger.Warning("certificate missing from all peers")
		} else {
			logger.Warning("no certificate adopted from peers")
		}
	}
	return nil
}

func (s *Syncer) certCutoffLayer() types.LayerID {
	cutoff := types.GetEffectiveGenesis()
	// TODO: change this to current layer after https://github.com/spacemeshos/go-spacemesh/issues/2921 is done
	last := s.mesh.ProcessedLayer()
	if last.Uint32() > s.cfg.SyncCertDistance {
		limit := last.Sub(s.cfg.SyncCertDistance)
		if limit.After(cutoff) {
			cutoff = limit
		}
	}
	return cutoff
}

func (s *Syncer) adoptCert(ctx context.Context, lid types.LayerID, cert *types.Certificate) error {
	if cert.BlockID != types.EmptyBlockID {
		if err := s.dataFetcher.GetBlocks(ctx, []types.BlockID{cert.BlockID}); err != nil {
			return fmt.Errorf("fetch block in cert %v: %w", cert.BlockID, err)
		}
	}
	if err := s.certHandler.HandleSyncedCertificate(ctx, lid, cert); err != nil {
		return fmt.Errorf("opnions adopt cert: %w", err)
	}
	return nil
}

// see https://github.com/spacemeshos/go-spacemesh/issues/2507 for implementation rationale.
func (s *Syncer) ensureMeshAgreement(
	ctx context.Context,
	logger log.Log,
	diffLayer types.LayerID,
	opinions []*fetch.LayerOpinion,
	resyncPeers map[p2p.Peer]struct{},
) error {
	logger.Info("checking mesh hash agreement")
	prevLid := diffLayer.Sub(1)
	prevHash, err := layers.GetAggregatedHash(s.cdb, prevLid)
	if err != nil {
		logger.With().Error("mesh hash failed to get prev hash", log.Err(err))
		return fmt.Errorf("mesh hash check previous: %w", err)
	}

	var (
		fork types.LayerID
		ed   *fetch.EpochData
		seen = make(map[types.Hash32]struct{})
	)
	for _, opn := range opinions {
		if opn.PrevAggHash == (types.Hash32{}) {
			continue
		}
		if _, ok := seen[opn.PrevAggHash]; ok {
			continue
		}
		if _, ok := resyncPeers[opn.Peer()]; ok {
			continue
		}
		if opn.PrevAggHash == prevHash {
			s.forkFinder.UpdateAgreement(opn.Peer(), prevLid, prevHash, time.Now())
			continue
		}
		logger.With().Warning("found mesh disagreement",
			log.Stringer("node_prev_hash", prevHash),
			log.Stringer("disagreed", prevLid),
			log.Object("peer_opinion", opn))
		ed, err = s.dataFetcher.PeerEpochInfo(ctx, opn.Peer(), diffLayer.GetEpoch())
		if err != nil {
			logger.With().Warning("failed to get epoch info", log.Stringer("peer", opn.Peer()), log.Err(err))
			continue
		}
		missing := make(map[types.ATXID]struct{})
		ignored := ed.Weight
		for _, id := range ed.AtxIDs {
			if _, ok := missing[id]; ok {
				continue
			}
			hdr, _ := s.cdb.GetAtxHeader(id)
			if hdr == nil {
				missing[id] = struct{}{}
				continue
			}
			ignored -= hdr.GetWeight()
		}
		if len(missing) > 0 {
			toFetch := toList(missing)
			logger.With().Info("fetching missing atxs from peer",
				log.Stringer("peer", opn.Peer()),
				log.Uint64("weight", ignored),
				log.Array("atxs", log.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
					for _, id := range toFetch {
						encoder.AppendString(id.ShortString())
					}
					return nil
				})),
			)
			// node and peer has different state. check if peer has valid ATXs to back up its opinions
			if err = s.dataFetcher.GetAtxs(ctx, toFetch); err != nil {
				// if the node cannot download the ATXs claimed by this peer, it does not trust this peer's mesh
				logger.With().Warning("failed to download missing ATX claimed by peer",
					log.Stringer("peer", opn.Peer()),
					log.Uint64("weight", ignored),
					log.Err(err))
				continue
			}
		} else {
			logger.With().Info("peer does not have unknown atx",
				log.Stringer("peer", opn.Peer()),
				log.Int("num_atxs", len(ed.AtxIDs)))
		}

		// find the divergent layer and adopt the peer's mesh from there
		fork, err = s.forkFinder.FindFork(ctx, opn.Peer(), prevLid, opn.PrevAggHash)
		if err != nil {
			logger.With().Warning("failed to find fork", log.Stringer("peer", opn.Peer()), log.Err(err))
			continue
		}

		from := fork.Add(1)
		to := from
		for lid := from; !lid.After(s.mesh.LatestLayer()); lid = lid.Add(1) {
			// ideally syncer should import opinions from peer and let tortoise decide whether to rerun
			// verifying tortoise or enter full tortoise mode.
			// however, for genesis running full tortoise with 10_000 layers as a sliding window is completely
			// viable. so here we don't sync opinions, just the data.
			// see https://github.com/spacemeshos/go-spacemesh/issues/2507
			if err = s.syncLayer(ctx, lid, opn.Peer()); err != nil {
				logger.With().Warning("mesh hash failed to sync layer",
					log.Stringer("peer", opn.Peer()),
					log.Stringer("sync_lid", lid),
					log.Err(err))
			}
			to = lid
		}
		logger.With().Info("mesh hash synced data from peer",
			log.Stringer("peer", opn.Peer()),
			log.Stringer("from", from),
			log.Stringer("to", to))
		resyncPeers[opn.Peer()] = struct{}{}
		seen[opn.PrevAggHash] = struct{}{}
	}

	// clear the agreement cache after syncing new data
	s.forkFinder.Purge(true)
	return nil
}
