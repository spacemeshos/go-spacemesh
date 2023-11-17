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

	s.logger.WithContext(ctx).With().Debug("processing synced layers",
		log.Stringer("current", s.ticker.CurrentLayer()),
		log.Stringer("processed", s.mesh.ProcessedLayer()),
		log.Stringer("in_state", s.mesh.LatestLayerInState()),
		log.Stringer("last_synced", s.getLastSyncedLayer()),
	)

	start := min(s.mesh.LatestLayerInState(), s.mesh.ProcessedLayer())
	start = min(start, s.getLastSyncedLayer())
	if start == types.GetEffectiveGenesis() {
		start = start.Add(1)
	}

	// used to make sure we only resync from the same peer once during each run.
	resyncPeers := make(map[p2p.Peer]struct{})
	for lid := start; lid <= s.getLastSyncedLayer(); lid++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// layers should be processed in order. once we skip one layer, there is no point
		// continuing with later layers. return on error

		if s.patrol.IsHareInCharge(lid) {
			lag := types.LayerID(0)
			current := s.ticker.CurrentLayer()
			if current.After(lid) {
				lag = current.Sub(lid.Uint32())
			}
			if lag.Uint32() < s.cfg.HareDelayLayers {
				s.logger.WithContext(ctx).With().Debug("skipping layer: hare still working", lid)
				return errHareInCharge
			}
		}

		if opinions, certs, err := s.layerOpinions(ctx, lid); err == nil {
			if len(certs) > 0 {
				if err = s.adopt(ctx, lid, certs); err != nil {
					s.logger.WithContext(ctx).
						With().
						Warning("failed to adopt peer opinions", lid, log.Err(err))
				}
			}
			if s.IsSynced(ctx) {
				if err = s.checkMeshAgreement(ctx, lid, opinions); err != nil &&
					errors.Is(err, errMeshHashDiverged) {
					s.logger.WithContext(ctx).
						With().
						Debug("mesh hash diverged, trying to reach agreement",
							lid,
							log.Stringer("diverged", lid.Sub(1)),
						)
					if err = s.ensureMeshAgreement(ctx, lid, opinions, resyncPeers); err != nil {
						s.logger.WithContext(ctx).
							With().
							Debug("failed to reach mesh agreement with peers",
								lid,
								log.Err(err),
							)
						hashResolve.Inc()
					} else {
						hashResolveFail.Inc()
					}
				}
			}
		}
		// even if it fails to fetch opinions, we still go ahead to ProcessLayer so that the tortoise
		// has a chance to count ballots and form its own opinions
		if err := s.mesh.ProcessLayer(ctx, lid); err != nil {
			if !errors.Is(err, mesh.ErrMissingBlock) {
				s.logger.WithContext(ctx).
					With().
					Warning("mesh failed to process layer from sync", lid, log.Err(err))
			}
			s.stateErr.Store(true)
		} else {
			s.stateErr.Store(false)
		}
	}
	s.logger.WithContext(ctx).With().Debug("end of state sync",
		log.Bool("state_synced", s.stateSynced()),
		log.Stringer("current", s.ticker.CurrentLayer()),
		log.Stringer("processed", s.mesh.ProcessedLayer()),
		log.Stringer("in_state", s.mesh.LatestLayerInState()),
		log.Stringer("last_synced", s.getLastSyncedLayer()),
	)
	return nil
}

func (s *Syncer) needCert(ctx context.Context, lid types.LayerID) (bool, error) {
	cutoff := s.certCutoffLayer()
	if !lid.After(cutoff) {
		return false, nil
	}
	_, err := certificates.Get(s.cdb, lid)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		s.logger.WithContext(ctx).With().Error("state sync failed to get cert", lid, log.Err(err))
		return false, err
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
	needCert, err := s.needCert(ctx, lid)
	if err != nil {
		return nil, nil, err
	}
	opinions, certs, err := s.dataFetcher.PollLayerOpinions(ctx, lid, needCert, peers)
	if err != nil {
		v2OpnErr.Inc()
		s.logger.WithContext(ctx).With().Debug("failed to fetch opinions2",
			lid,
			log.Int("peers", len(peers)),
			log.Bool("need cert", needCert),
			log.Err(err),
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
		s.logger.WithContext(ctx).With().Error("failed to get prev agg hash", lid, log.Err(err))
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
	needCert, err := s.needCert(ctx, lid)
	if err != nil {
		return err
	}
	if !needCert {
		s.logger.WithContext(ctx).With().Debug("node already has certificate", lid)
		return nil
	}
	for _, cert := range certs {
		if err = s.adoptCert(ctx, lid, cert); err != nil {
			s.logger.WithContext(ctx).
				With().
				Warning("failed to adopt cert", lid, cert.BlockID, log.Err(err))
		} else {
			s.logger.WithContext(ctx).With().Info("adopted cert from peer", lid, cert.BlockID)
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
		return fmt.Errorf("opnions adopt cert: %w", err)
	}
	if cert.BlockID != types.EmptyBlockID {
		if err := s.dataFetcher.GetBlocks(ctx, []types.BlockID{cert.BlockID}); err != nil {
			return fmt.Errorf("fetch block in cert %v: %w", cert.BlockID, err)
		}
	}
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
		s.logger.WithContext(ctx).With().Debug("found mesh disagreement",
			log.Stringer("node_prev_hash", prevHash),
			log.Stringer("peer", peer),
			log.Stringer("disagreed", prevLid),
			log.Stringer("peer_hash", opn.prevAggHash),
		)

		if !s.forkFinder.NeedResync(prevLid, opn.prevAggHash) {
			s.logger.WithContext(ctx).
				With().
				Debug("already resynced based on the same diverged hash",
					log.Stringer("node_prev_hash", prevHash),
					log.Stringer("peer", peer),
					log.Stringer("disagreed", prevLid),
					log.Stringer("peer_hash", opn.prevAggHash),
				)
			continue
		}

		// getting the atx IDs targeting this epoch
		ed, err = s.dataFetcher.PeerEpochInfo(ctx, peer, diffLayer.GetEpoch()-1)
		if err != nil {
			s.logger.WithContext(ctx).With().Warning("failed to get epoch info",
				log.Stringer("peer", peer),
				log.Err(err),
			)
			continue
		}
		missing := s.asCache.GetMissingActiveSet(diffLayer.GetEpoch(), ed.AtxIDs)
		if len(missing) > 0 {
			s.logger.WithContext(ctx).With().Debug("fetching missing atxs from peer",
				log.Stringer("peer", peer),
				log.Array("missing_atxs", log.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
					for _, id := range missing {
						encoder.AppendString(id.ShortString())
					}
					return nil
				})),
			)
			// node and peer has different state. check if peer has valid ATXs to back up its opinions
			if err = s.dataFetcher.GetAtxs(ctx, missing); err != nil {
				// if the node cannot download the ATXs claimed by this peer, it does not trust this peer's mesh
				s.logger.WithContext(ctx).
					With().
					Warning("failed to download missing ATX claimed by peer",
						log.Stringer("peer", peer),
						log.Array("missing_atxs", log.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
							for _, id := range missing {
								encoder.AppendString(id.ShortString())
							}
							return nil
						})),
						log.Err(err))
				continue
			}
		} else {
			s.logger.WithContext(ctx).With().Debug("peer does not have unknown atx",
				log.Stringer("peer", peer),
				log.Int("num_atxs", len(ed.AtxIDs)),
			)
		}

		// find the divergent layer and adopt the peer's mesh from there
		fork, err = s.forkFinder.FindFork(ctx, peer, prevLid, opn.prevAggHash)
		if err != nil {
			s.logger.WithContext(ctx).With().Warning("failed to find fork",
				log.Stringer("peer", peer),
				log.Err(err),
			)
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
				s.logger.WithContext(ctx).With().Warning("mesh hash failed to sync layer",
					log.Stringer("sync_lid", lid),
					log.Err(err))
			}
			to = lid
		}
		s.logger.WithContext(ctx).With().Info("mesh hash synced data from peer",
			log.Stringer("peer", peer),
			log.Stringer("from", from),
			log.Stringer("to", to))
		resyncPeers[opn.peer] = struct{}{}
		s.forkFinder.AddResynced(prevLid, opn.prevAggHash)
	}

	// clear the agreement cache after syncing new data
	s.forkFinder.Purge(true)
	return nil
}
