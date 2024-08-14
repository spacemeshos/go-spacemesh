package syncer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

var errMeshHashDiverged = errors.New("mesh hash diverged with peer")

type peerOpinion struct {
	prevAggHash types.Hash32
	peer        p2p.Peer
}

func (s *Syncer) stateSynced() bool {
	current := s.ticker.CurrentLayer()
	return current <= types.GetEffectiveGenesis() ||
		(s.mesh.ProcessedLayer() >= current-1 && !s.stateErr.Load())
}

func (s *Syncer) processLayers(ctx context.Context) error {
	ctx = log.WithNewSessionID(ctx)
	if !s.ticker.CurrentLayer().After(types.GetEffectiveGenesis()) {
		return nil
	}
	if !s.ListenToATXGossip() {
		return errATXsNotSynced
	}

	s.logger.Debug("processing synced layers",
		log.ZContext(ctx),
		zap.Stringer("current", s.ticker.CurrentLayer()),
		zap.Stringer("processed", s.mesh.ProcessedLayer()),
		zap.Stringer("in_state", s.mesh.LatestLayerInState()),
		zap.Stringer("last_synced", s.getLastSyncedLayer()),
	)

	start := min(s.mesh.LatestLayerInState(), s.mesh.ProcessedLayer())
	start = min(start, s.getLastSyncedLayer())
	if start == types.GetEffectiveGenesis() {
		start = start.Add(1)
	}

	// used to make sure we only resync from the same peer once during each run.
	resyncPeers := make(map[p2p.Peer]struct{})
	last := s.getLastSyncedLayer()
	for lid := start; lid <= last; lid++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		current := s.ticker.CurrentLayer()

		// layers should be processed in order. once we skip one layer, there is no point
		// continuing with later layers. return on error

		if s.patrol.IsHareInCharge(lid) {
			lag := types.LayerID(0)
			current := s.ticker.CurrentLayer()
			if current.After(lid) {
				lag = current.Sub(lid.Uint32())
			}
			if lag.Uint32() < s.cfg.HareDelayLayers {
				s.logger.Debug("skipping layer: hare still working",
					log.ZContext(ctx),
					zap.Uint32("layer", lid.Uint32()),
				)
				return errHareInCharge
			}
		}

		// certificate is effective only within hare distance, outside it we don't vote according to other rules.
		certEffective := lid.Add(s.cfg.SyncCertDistance).After(current)
		if certEffective {
			s.processLayerOpinions(ctx, lid, resyncPeers)
		}
		// there is no point in tortoise counting after every single layer, in fact it is wasteful.
		// we periodically invoke counting to evict executed layers.
		if lid.Uint32()%(uint32(max(float64(types.GetLayersPerEpoch())*s.cfg.TallyVotesFrequency, 1))) == 0 ||
			lid == last || s.stateErr.Load() {
			err1 := s.mesh.ProcessLayer(ctx, lid)
			if err1 != nil {
				missing := &mesh.MissingBlocksError{}
				if errors.As(err1, &missing) {
					// we try once as we cannot assume that all blocks that reported as missing are valid
					// we need to continue download layers, as they may be deemed as invalid after counting more votes
					if err := s.dataFetcher.GetBlocks(ctx, missing.Blocks); err == nil {
						err1 = s.mesh.ProcessLayer(ctx, lid)
					} else {
						s.logger.Debug("failed to download blocks", zap.Error(err))
					}
				} else {
					s.logger.Warn("failed to process layer",
						log.ZContext(ctx),
						zap.Uint32("layer", lid.Uint32()),
						zap.Error(err1),
					)
				}
			}
			s.stateErr.Store(err1 != nil)
		}

	}
	s.logger.Debug("end of state sync",
		log.ZContext(ctx),
		zap.Bool("state_synced", s.stateSynced()),
		zap.Stringer("current", s.ticker.CurrentLayer()),
		zap.Stringer("processed", s.mesh.ProcessedLayer()),
		zap.Stringer("in_state", s.mesh.LatestLayerInState()),
		zap.Stringer("last_synced", s.getLastSyncedLayer()),
	)
	return nil
}

func (s *Syncer) processLayerOpinions(ctx context.Context, lid types.LayerID, resyncPeers map[p2p.Peer]struct{}) {
	if opinions, certs, err := s.layerOpinions(ctx, lid); err == nil {
		if len(certs) > 0 {
			if err = s.adopt(ctx, lid, certs); err != nil {
				s.logger.Warn(
					"failed to adopt peer opinions",
					log.ZContext(ctx),
					zap.Uint32("layer", lid.Uint32()),
					zap.Error(err),
				)
			}
		}
		if s.IsSynced(ctx) && !s.cfg.DisableMeshAgreement {
			if err = s.checkMeshAgreement(ctx, lid, opinions); err != nil &&
				errors.Is(err, errMeshHashDiverged) {
				s.logger.Debug("mesh hash diverged, trying to reach agreement",
					log.ZContext(ctx),
					zap.Uint32("layer", lid.Uint32()),
					zap.Stringer("diverged", lid.Sub(1)),
				)
				if err = s.ensureMeshAgreement(ctx, lid, opinions, resyncPeers); err != nil {
					s.logger.Debug("failed to reach mesh agreement with peers",
						log.ZContext(ctx),
						zap.Uint32("layer", lid.Uint32()),
						zap.Error(err),
					)
					hashResolve.Inc()
				} else {
					hashResolveFail.Inc()
				}
			}
		}
	}
}

func (s *Syncer) needCert(lid types.LayerID) (bool, error) {
	cutoff := s.certCutoffLayer()
	if !lid.After(cutoff) {
		return false, nil
	}
	_, err := certificates.Get(s.cdb, lid)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return false, fmt.Errorf("getting cert: %w", err)
	}
	return errors.Is(err, sql.ErrNotFound), nil
}

func (s *Syncer) layerOpinions(
	ctx context.Context,
	lid types.LayerID,
) ([]*peerOpinion, []*types.Certificate, error) {
	peers := s.dataFetcher.SelectBestShuffled(fetch.RedundantPeers)
	if len(peers) == 0 {
		return nil, nil, errNoPeers
	}

	v2OpnPoll.Inc()
	needCert, err := s.needCert(lid)
	if err != nil {
		return nil, nil, err
	}
	opinions, certs, err := s.dataFetcher.PollLayerOpinions(ctx, lid, needCert, peers)
	if err != nil {
		v2OpnErr.Inc()
		s.logger.Debug("failed to fetch opinions2",
			log.ZContext(ctx),
			zap.Uint32("layer", lid.Uint32()),
			zap.Int("peers", len(peers)),
			zap.Bool("need cert", needCert),
			zap.Error(err),
		)
		return nil, nil, fmt.Errorf("PollLayerOpinions: %w", err)
	}
	result := make([]*peerOpinion, 0, len(opinions))
	for _, opn := range opinions {
		result = append(result, &peerOpinion{
			prevAggHash: opn.PrevAggHash,
			peer:        opn.Peer(),
		})
	}
	opinionLayer.Set(float64(lid))
	return result, certs, nil
}

func (s *Syncer) checkMeshAgreement(
	ctx context.Context,
	lid types.LayerID,
	opinions []*peerOpinion,
) error {
	prevHash, err := layers.GetAggregatedHash(s.cdb, lid.Sub(1))
	if err != nil {
		s.logger.Error("failed to get prev agg hash",
			log.ZContext(ctx),
			zap.Uint32("layer", lid.Uint32()),
			zap.Error(err),
		)
		return fmt.Errorf("opinions prev hash: %w", err)
	}
	for _, opn := range opinions {
		if opn.prevAggHash != (types.Hash32{}) && opn.prevAggHash != prevHash {
			return errMeshHashDiverged
		}
	}
	return nil
}

func (s *Syncer) adopt(ctx context.Context, lid types.LayerID, certs []*types.Certificate) error {
	needCert, err := s.needCert(lid)
	if err != nil {
		return err
	}
	if !needCert {
		s.logger.Debug("node already has certificate", log.ZContext(ctx), zap.Uint32("layer", lid.Uint32()))
		return nil
	}
	for _, cert := range certs {
		if err = s.adoptCert(ctx, lid, cert); err != nil {
			s.logger.Warn("failed to adopt cert",
				log.ZContext(ctx),
				zap.Uint32("layer", lid.Uint32()),
				zap.Stringer("block", cert.BlockID),
				zap.Error(err),
			)
		} else {
			s.logger.Debug("adopted cert from peer",
				log.ZContext(ctx),
				zap.Uint32("layer", lid.Uint32()),
				zap.Stringer("block", cert.BlockID),
			)
			break
		}
	}
	return nil
}

func (s *Syncer) certCutoffLayer() types.LayerID {
	cutoff := types.GetEffectiveGenesis()
	last := s.ticker.CurrentLayer()
	if last.Uint32() > s.cfg.SyncCertDistance {
		limit := last.Sub(s.cfg.SyncCertDistance)
		if limit.After(cutoff) {
			cutoff = limit
		}
	}
	return cutoff
}

func (s *Syncer) adoptCert(ctx context.Context, lid types.LayerID, cert *types.Certificate) error {
	if err := s.certHandler.HandleSyncedCertificate(ctx, lid, cert); err != nil {
		return fmt.Errorf("opinions adopt cert: %w", err)
	}
	// it is safer to ask for block after certificate was downloaded, as we know that we ask for block signed by
	// committee so we should not reorder this
	if cert.BlockID != types.EmptyBlockID {
		if err := s.dataFetcher.GetBlocks(ctx, []types.BlockID{cert.BlockID}); err != nil {
			return fmt.Errorf("fetch block in cert %v: %w", cert.BlockID, err)
		}
	}
	// in GetBlocks call block will be also passed to tortoise.OnBlock
	s.tortoise.OnHareOutput(lid, cert.BlockID)
	numCertAdopted.Inc()
	return nil
}

// see https://github.com/spacemeshos/go-spacemesh/issues/2507 for implementation rationale.
func (s *Syncer) ensureMeshAgreement(
	ctx context.Context,
	diffLayer types.LayerID,
	opinions []*peerOpinion,
	resyncPeers map[p2p.Peer]struct{},
) error {
	prevLid := diffLayer.Sub(1)
	prevHash, err := layers.GetAggregatedHash(s.cdb, prevLid)
	if err != nil {
		return fmt.Errorf("mesh hash check previous: %w", err)
	}

	var (
		fork types.LayerID
		ed   *fetch.EpochData
	)
	for _, opn := range opinions {
		if opn.prevAggHash == (types.Hash32{}) {
			continue
		}
		if _, ok := resyncPeers[opn.peer]; ok {
			continue
		}
		if opn.prevAggHash == prevHash {
			s.forkFinder.UpdateAgreement(opn.peer, prevLid, prevHash, time.Now())
			continue
		}

		peer := opn.peer
		s.logger.Debug("found mesh disagreement",
			log.ZContext(ctx),
			zap.Stringer("node_prev_hash", prevHash),
			zap.Stringer("peer", peer),
			zap.Stringer("disagreed", prevLid),
			zap.Stringer("peer_hash", opn.prevAggHash),
		)

		if !s.forkFinder.NeedResync(prevLid, opn.prevAggHash) {
			s.logger.Debug("already resynced based on the same diverged hash",
				log.ZContext(ctx),
				zap.Stringer("node_prev_hash", prevHash),
				zap.Stringer("peer", peer),
				zap.Stringer("disagreed", prevLid),
				zap.Stringer("peer_hash", opn.prevAggHash),
			)
			continue
		}

		// getting the atx IDs targeting this epoch
		ed, err = s.dataFetcher.PeerEpochInfo(ctx, peer, diffLayer.GetEpoch()-1)
		if err != nil {
			s.logger.Warn("failed to get epoch info",
				log.ZContext(ctx),
				zap.Stringer("peer", peer),
				zap.Error(err),
			)
			continue
		}
		missing := s.tortoise.GetMissingActiveSet(diffLayer.GetEpoch(), ed.AtxIDs)
		if len(missing) > 0 {
			s.logger.Debug("fetching missing atxs from peer",
				log.ZContext(ctx),
				zap.Stringer("peer", peer),
				zap.Int("missing atxs", len(missing)),
			)
			// node and peer has different state. check if peer has valid ATXs to back up its opinions
			if err = s.dataFetcher.GetAtxs(ctx, missing); err != nil {
				// if the node cannot download the ATXs claimed by this peer, it does not trust this peer's mesh
				s.logger.Warn("failed to download missing ATX claimed by peer",
					log.ZContext(ctx),
					zap.Stringer("peer", peer),
					zap.Int("missing atxs", len(missing)),
					zap.Error(err))
				continue
			}
		} else {
			s.logger.Debug("peer does not have unknown atx",
				log.ZContext(ctx),
				zap.Stringer("peer", peer),
				zap.Int("num_atxs", len(ed.AtxIDs)),
			)
		}

		// find the divergent layer and adopt the peer's mesh from there
		fork, err = s.forkFinder.FindFork(ctx, peer, prevLid, opn.prevAggHash)
		if err != nil {
			s.logger.Warn("failed to find fork", log.ZContext(ctx), zap.Stringer("peer", peer), zap.Error(err))
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
			if err = s.syncLayer(ctx, lid, peer); err != nil {
				s.logger.Warn("mesh hash failed to sync layer",
					log.ZContext(ctx),
					zap.Stringer("sync_lid", lid),
					zap.Error(err))
			}
			to = lid
		}
		s.logger.Info("mesh hash synced data from peer",
			log.ZContext(ctx),
			zap.Stringer("peer", peer),
			zap.Stringer("from", from),
			zap.Stringer("to", to))
		resyncPeers[opn.peer] = struct{}{}
		s.forkFinder.AddResynced(prevLid, opn.prevAggHash)
	}

	// clear the agreement cache after syncing new data
	s.forkFinder.Purge(true)
	return nil
}
