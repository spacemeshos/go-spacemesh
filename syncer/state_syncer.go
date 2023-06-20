package syncer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap/zapcore"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
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
	current := s.ticker.CurrentLayer()
	return current.Uint32() <= 1 || !s.mesh.ProcessedLayer().Before(current.Sub(1))
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

	start := minLayer(s.mesh.LatestLayerInState(), s.mesh.ProcessedLayer())
	start = minLayer(start, s.getLastSyncedLayer())
	if start == types.GetEffectiveGenesis() {
		start = start.Add(1)
	}

	// used to make sure we only resync from the same peer once during each run.
	resyncPeers := make(map[p2p.Peer]struct{})
	for lid := start; !lid.After(s.getLastSyncedLayer()); lid = lid.Add(1) {
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

		if opinions, err := s.fetchOpinions(ctx, lid); err == nil {
			if err = s.checkMeshAgreement(ctx, lid, opinions); err != nil && errors.Is(err, errMeshHashDiverged) {
				s.logger.WithContext(ctx).With().Debug("mesh hash diverged, trying to reach agreement",
					lid,
					log.Stringer("diverged", lid.Sub(1)),
				)
				if err = s.ensureMeshAgreement(ctx, lid, opinions, resyncPeers); err != nil {
					s.logger.WithContext(ctx).With().Debug("failed to reach mesh agreement with peers",
						lid,
						log.Err(err),
					)
					hashResolve.Inc()
				} else {
					hashResolveFail.Inc()
				}
			}
			if err = s.adopt(ctx, lid, opinions); err != nil {
				s.logger.WithContext(ctx).With().Warning("failed to adopt peer opinions", lid, log.Err(err))
			}
		}
		// even if it fails to fetch opinions, we still go ahead to ProcessLayer so that the tortoise
		// has a chance to count ballots and form its own opinions
		if err := s.processWithRetry(ctx, lid); err != nil {
			s.logger.WithContext(ctx).With().Warning("mesh failed to process layer from sync", lid, log.Err(err))
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

func (s *Syncer) processWithRetry(ctx context.Context, lid types.LayerID) error {
	for {
		origerr := s.mesh.ProcessLayer(ctx, lid)
		if origerr == nil {
			return nil
		}
		var missing *types.ErrorMissing
		if !errors.As(origerr, &missing) {
			return origerr
		}
		s.logger.With().Debug("requesting missing blocks",
			log.Context(ctx),
			log.Inline(missing),
		)
		err := s.dataFetcher.GetBlocks(ctx, missing.Blocks)
		if err != nil {
			return fmt.Errorf("%w: %s", origerr, err)
		}
		blockRequested.Add(float64(len(missing.Blocks)))
	}
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

func (s *Syncer) fetchOpinions(ctx context.Context, lid types.LayerID) ([]*fetch.LayerOpinion, error) {
	s.logger.WithContext(ctx).With().Debug("polling layer opinions", lid)
	opinions, err := s.dataFetcher.PollLayerOpinions(ctx, lid)
	if err != nil {
		s.logger.WithContext(ctx).With().Warning("failed to fetch opinions", lid, log.Err(err))
		return nil, fmt.Errorf("PollLayerOpinions: %w", err)
	}
	opinionLayer.Set(float64(lid))
	return opinions, nil
}

func (s *Syncer) checkMeshAgreement(ctx context.Context, lid types.LayerID, opinions []*fetch.LayerOpinion) error {
	prevHash, err := layers.GetAggregatedHash(s.cdb, lid.Sub(1))
	if err != nil {
		s.logger.WithContext(ctx).With().Error("failed to get prev agg hash", lid, log.Err(err))
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
	needCert, err := s.needCert(ctx, lid)
	if err != nil {
		return err
	}
	if !needCert {
		s.logger.WithContext(ctx).With().Debug("node already has certificate", lid)
		return nil
	}

	var noCert int
	for _, opn := range opinions {
		if needCert {
			if opn.Cert == nil {
				noCert++
				s.logger.WithContext(ctx).With().Debug("peer has no cert", lid, log.Inline(opn))
			} else if err = s.adoptCert(ctx, lid, opn.Cert); err != nil {
				s.logger.WithContext(ctx).With().Warning("failed to adopt cert", lid, log.Inline(opn), log.Err(err))
			} else {
				s.logger.WithContext(ctx).With().Debug("adopted cert from peer", lid, log.Inline(opn))
				needCert = false
				break
			}
		}
	}
	numPeers := len(opinions)
	if needCert {
		if noCert == numPeers {
			s.logger.WithContext(ctx).With().Warning("certificate missing from all peers", lid)
		} else {
			s.logger.WithContext(ctx).With().Warning("no certificate adopted from peers", lid)
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
	numCertAdopted.Inc()
	return nil
}

// see https://github.com/spacemeshos/go-spacemesh/issues/2507 for implementation rationale.
func (s *Syncer) ensureMeshAgreement(
	ctx context.Context,
	diffLayer types.LayerID,
	opinions []*fetch.LayerOpinion,
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
		if opn.PrevAggHash == (types.Hash32{}) {
			continue
		}
		if _, ok := resyncPeers[opn.Peer()]; ok {
			continue
		}
		if opn.PrevAggHash == prevHash {
			s.forkFinder.UpdateAgreement(opn.Peer(), prevLid, prevHash, time.Now())
			continue
		}

		peer := opn.Peer()
		s.logger.WithContext(ctx).With().Debug("found mesh disagreement",
			log.Stringer("node_prev_hash", prevHash),
			log.Stringer("peer", peer),
			log.Stringer("disagreed", prevLid),
			log.Stringer("peer_hash", opn.PrevAggHash),
		)

		if !s.forkFinder.NeedResync(prevLid, opn.PrevAggHash) {
			s.logger.WithContext(ctx).With().Debug("already resynced based on the same diverged hash",
				log.Stringer("node_prev_hash", prevHash),
				log.Stringer("peer", peer),
				log.Stringer("disagreed", prevLid),
				log.Stringer("peer_hash", opn.PrevAggHash),
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
		missing := make(map[types.ATXID]struct{})
		for _, id := range ed.AtxIDs {
			if _, ok := missing[id]; ok {
				continue
			}
			hdr, _ := s.cdb.GetAtxHeader(id)
			if hdr == nil {
				missing[id] = struct{}{}
				continue
			}
		}
		if len(missing) > 0 {
			toFetch := maps.Keys(missing)
			s.logger.WithContext(ctx).With().Debug("fetching missing atxs from peer",
				log.Stringer("peer", peer),
				log.Array("missing_atxs", log.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
					for _, id := range toFetch {
						encoder.AppendString(id.ShortString())
					}
					return nil
				})),
			)
			// node and peer has different state. check if peer has valid ATXs to back up its opinions
			if err = s.dataFetcher.GetAtxs(ctx, toFetch); err != nil {
				// if the node cannot download the ATXs claimed by this peer, it does not trust this peer's mesh
				s.logger.WithContext(ctx).With().Warning("failed to download missing ATX claimed by peer",
					log.Stringer("peer", peer),
					log.Array("missing_atxs", log.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
						for _, id := range toFetch {
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
		fork, err = s.forkFinder.FindFork(ctx, peer, prevLid, opn.PrevAggHash)
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
		resyncPeers[opn.Peer()] = struct{}{}
		s.forkFinder.AddResynced(prevLid, opn.PrevAggHash)
	}

	// clear the agreement cache after syncing new data
	s.forkFinder.Purge(true)
	return nil
}
