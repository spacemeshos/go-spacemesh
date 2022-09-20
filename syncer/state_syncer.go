package syncer

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

var (
	errNoOpinionsAvailable = errors.New("no layer opinions available from peers")
	errNoOpinionsAdopted   = errors.New("no opinions are adopted from peers")
	errCertificateMissing  = errors.New("certificates missing from all peers")
)

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
	if !start.Before(s.getLastSyncedLayer()) {
		return nil
	}

	for lid := start.Add(1); !lid.After(s.getLastSyncedLayer()); lid = lid.Add(1) {
		if s.isClosed() {
			return errShuttingDown
		}

		// layers should be processed in order. once we skip one layer, there is no point
		// continuing with later layers. return on error
		if _, err := s.beacon.GetBeacon(lid.GetEpoch()); err != nil {
			s.logger.WithContext(ctx).With().Debug("beacon not available", lid)
			return errBeaconNotAvailable
		}

		if s.patrol.IsHareInCharge(lid) {
			lag := types.NewLayerID(0)
			current := s.ticker.GetCurrentLayer()
			if current.After(lid) {
				lag = current.Sub(lid.Uint32())
			}
			if lag.Value < s.conf.HareDelayLayers {
				s.logger.WithContext(ctx).With().Info("skip validating layer: hare still working", lid)
				return errHareInCharge
			}
		}

		_ = s.fetchLayerOpinions(ctx, lid)
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

func sortOpinions(opinions []*fetch.LayerOpinion) {
	sort.Slice(opinions, func(i, j int) bool {
		io := opinions[i]
		jo := opinions[j]
		if io.EpochWeight != jo.EpochWeight {
			return io.EpochWeight > jo.EpochWeight
		}
		if io.Verified != jo.Verified {
			return io.Verified.After(jo.Verified)
		}
		if io.Cert != nil && jo.Cert != nil {
			// TODO: maybe uses peer's p2p scores to break tie
			return strings.Compare(io.Peer().String(), jo.Peer().String()) < 0
		}
		return io.Cert != nil
	})
}

func (s *Syncer) needCert(logger log.Log, lid types.LayerID) (bool, error) {
	cutoff := s.certCutoffLayer()
	if lid.Before(cutoff) {
		return false, nil
	}
	cert, err := layers.GetCert(s.db, lid)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		logger.With().Error("state sync failed to get cert", log.Err(err))
		return false, err
	}
	return cert == nil, nil
}

func (s *Syncer) needValidity(logger log.Log, lid types.LayerID) (bool, error) {
	count, err := blocks.CountContextualValidity(s.db, lid)
	if err != nil {
		logger.With().Error("state sync failed to get validity", log.Err(err))
		return false, err
	}
	return count == 0, nil
}

func (s *Syncer) fetchLayerOpinions(ctx context.Context, lid types.LayerID) error {
	logger := s.logger.WithContext(ctx).WithFields(lid)
	logger.Info("polling layer opinions")
	ch := s.fetcher.PollLayerOpinions(ctx, lid)
	select {
	case res := <-ch:
		if res.Err != nil {
			return fmt.Errorf("PollLayerOpinions: %w", res.Err)
		}
		if len(res.Opinions) == 0 {
			logger.Debug("no opinions available from peers")
			return errNoOpinionsAvailable
		}

		// TODO: check if the node agree with peers' aggregated hashes

		if err := s.adopt(ctx, lid, res.Opinions); err != nil {
			return err
		}
	case <-ctx.Done():
	}
	return nil
}

func (s *Syncer) adopt(ctx context.Context, lid types.LayerID, opinions []*fetch.LayerOpinion) error {
	logger := s.logger.WithContext(ctx).WithFields(lid)
	latestVerified := s.mesh.LastVerified()
	if !latestVerified.Before(lid) {
		logger.With().Debug("opinions older than own verified layer")
		return nil
	}

	sortOpinions(opinions)

	needCert, err := s.needCert(logger, lid)
	if err != nil {
		return err
	}
	needValidity, err := s.needValidity(logger, lid)
	if err != nil {
		return err
	}
	if !needCert && !needValidity {
		logger.With().Debug("node already has local opinions")
		return nil
	}

	numCert := 0
	for _, opn := range opinions {
		if !opn.Verified.After(latestVerified) {
			logger.With().Debug("node has same/higher verified layer than peers",
				log.Stringer("verified", latestVerified),
				log.Stringer("peers_verified", opn.Verified))
			continue
		}

		if opn.Cert != nil {
			numCert++
		}

		// TODO: detect multiple hare certificate in the same network
		// https://github.com/spacemeshos/go-spacemesh/issues/3467
		if needCert {
			if opn.Cert == nil {
				logger.With().Debug("peer has no cert", log.Inline(opn))
			} else if err := s.adoptCert(ctx, lid, opn); err != nil {
				logger.With().Warning("failed to adopt cert", log.Inline(opn), log.Err(err))
			} else {
				logger.With().Info("adopted cert from peer", log.Inline(opn))
				needCert = false
			}
		}
		if needValidity {
			if len(opn.Valid) == 0 && len(opn.Invalid) == 0 {
				logger.With().Debug("peer has no block validity", log.Inline(opn))
			} else if err := s.adoptValidity(ctx, opn); err != nil {
				logger.With().Warning("failed to adopt block validity", log.Inline(opn), log.Err(err))
			} else {
				logger.With().Info("adopted validity from peer", log.Inline(opn))
				needValidity = false
			}
		}
		if !needCert && !needValidity {
			return nil
		}
	}
	if needCert && numCert == 0 {
		return errCertificateMissing
	}
	return errNoOpinionsAdopted
}

func (s *Syncer) certCutoffLayer() types.LayerID {
	cutoff := types.GetEffectiveGenesis()
	// TODO: change this to current layer after https://github.com/spacemeshos/go-spacemesh/issues/2921 is done
	last := s.mesh.ProcessedLayer()
	if last.Uint32() > s.conf.SyncCertDistance {
		limit := last.Sub(s.conf.SyncCertDistance)
		if limit.After(cutoff) {
			cutoff = limit
		}
	}
	return cutoff
}

func (s *Syncer) adoptCert(ctx context.Context, lid types.LayerID, opinion *fetch.LayerOpinion) error {
	if err := s.certHandler.HandleSyncedCertificate(ctx, lid, opinion.Cert); err != nil {
		return fmt.Errorf("state sync adopt cert: %w", err)
	}
	return nil
}

func (s *Syncer) adoptValidity(ctx context.Context, opinion *fetch.LayerOpinion) error {
	all := opinion.Valid
	all = append(all, opinion.Invalid...)
	if len(all) > 0 {
		if err := s.fetcher.GetBlocks(ctx, all); err != nil {
			return fmt.Errorf("opinions get blocks: %w", err)
		}
	}
	for _, bid := range opinion.Valid {
		if err := blocks.SetValid(s.db, bid); err != nil {
			return fmt.Errorf("opinions set valid: %w", err)
		}
	}
	for _, bid := range opinion.Invalid {
		if err := blocks.SetInvalid(s.db, bid); err != nil {
			return fmt.Errorf("opinions set invalid: %w", err)
		}
	}
	return nil
}
