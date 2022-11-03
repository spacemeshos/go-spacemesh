package syncer

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

var (
	errNoOpinionsAvailable = errors.New("no layer opinions available from peers")
	errNoBetterOpinions    = errors.New("no better opinions from peer")
	errNoCertAdopted       = errors.New("no certificate adopted from peers")
	errNoValidityAdopted   = errors.New("no validity adopted from peers")
	errMissingCertificate  = errors.New("certificate missing from all peers")
	errMissingValidity     = errors.New("validity missing from all peers")
	errMeshHashDiverged    = errors.New("mesh hash diverged with peer")
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

	// at least check state at one layer
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

		var resyncFrom types.LayerID
		if opinions, err := s.fetchOpinions(ctx, logger, lid); err == nil {
			if err = s.adoptOpinions(ctx, logger, lid, opinions); err != nil && errors.Is(err, errMeshHashDiverged) {
				logger.With().Warning("mesh hash diverged, checking agreement",
					log.Stringer("diverged", lid.Sub(1)))
				if resyncFrom, err = s.ensureMeshAgreement(ctx, logger, lid, opinions, resyncPeers); err != nil {
					logger.With().Warning("failed to reach mesh agreement with peers", log.Err(err))
				}
			}
		}
		// even if it fails to fetch opinions, we still go ahead to ProcessLayer so that the tortoise
		// has a chance to count ballots and form its own opinions

		if err := s.mesh.ProcessLayer(ctx, lid, resyncFrom); err != nil {
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

func (s *Syncer) needValidity(logger log.Log, lid types.LayerID) (bool, error) {
	count, err := blocks.CountContextualValidity(s.cdb, lid)
	if err != nil {
		logger.With().Error("state sync failed to get validity", log.Err(err))
		return false, err
	}
	return count == 0, nil
}

func toList[T comparable](set map[T]struct{}) []T {
	result := make([]T, 0, len(set))
	for bid := range set {
		result = append(result, bid)
	}
	return result
}

func (s *Syncer) checkOpinionsIntegrity(ctx context.Context, logger log.Log, opinions []*fetch.LayerOpinion) []*fetch.LayerOpinion {
	goodOpinions := make([]*fetch.LayerOpinion, 0, len(opinions))
	for _, opn := range opinions {
		good := true
		toFetch := make(map[types.BlockID]struct{})
		valids := make(map[types.BlockID]struct{})
		for _, bid := range opn.Valid {
			valids[bid] = struct{}{}
			if bid != types.EmptyBlockID {
				toFetch[bid] = struct{}{}
			}
		}
		for _, bid := range opn.Invalid {
			if _, ok := valids[bid]; ok {
				logger.With().Warning("peer has conflicting opinions", log.Inline(opn))
				good = false
				break
			}
			if bid == types.EmptyBlockID {
				logger.With().Warning("peer has invalid opinions", log.Inline(opn))
				good = false
				break
			}
			toFetch[bid] = struct{}{}
		}
		if good && opn.Cert != nil && opn.Cert.BlockID != types.EmptyBlockID {
			if len(toFetch) == 0 { // no validity is available
				toFetch[opn.Cert.BlockID] = struct{}{}
			} else if _, ok := toFetch[opn.Cert.BlockID]; !ok {
				good = false
			}
		}
		if good && len(toFetch) > 0 {
			bids := toList(toFetch)
			s.dataFetcher.RegisterPeerHashes(opn.Peer(), types.BlockIDsToHashes(bids))
			if err := s.dataFetcher.GetBlocks(ctx, bids); err != nil {
				logger.With().Warning("failed to fetch blocks referenced in peer opinions",
					log.Stringer("peer", opn.Peer()),
					log.Array("blocks", log.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
						for bid := range toFetch {
							encoder.AppendString(bid.String())
						}
						return nil
					})))
				good = false
			}
		}
		if good {
			goodOpinions = append(goodOpinions, opn)
		}
	}
	return goodOpinions
}

func (s *Syncer) fetchOpinions(ctx context.Context, logger log.Log, lid types.LayerID) ([]*fetch.LayerOpinion, error) {
	logger.Info("polling layer opinions")
	opinions, err := s.dataFetcher.PollLayerOpinions(ctx, lid)
	if err != nil {
		logger.With().Warning("failed to fetch opinions", log.Err(err))
		return nil, fmt.Errorf("PollLayerOpinions: %w", err)
	}

	opinions = s.checkOpinionsIntegrity(ctx, logger, opinions)
	if len(opinions) == 0 {
		logger.Warning("no good opinions available from peers")
		return nil, errNoOpinionsAvailable
	}

	return opinions, nil
}

func (s *Syncer) adoptOpinions(ctx context.Context, logger log.Log, lid types.LayerID, opinions []*fetch.LayerOpinion) error {
	// before adopting any opinions, make sure nodes agree on the mesh hash
	var (
		prevHash types.Hash32
		err      error
	)
	prevHash, err = layers.GetAggregatedHash(s.cdb, lid.Sub(1))
	if err != nil {
		logger.With().Error("failed to get prev agg hash", log.Err(err))
		return fmt.Errorf("opinions prev hash: %w", err)
	}
	for _, opn := range opinions {
		if opn.PrevAggHash != (types.Hash32{}) && opn.PrevAggHash != prevHash {
			return errMeshHashDiverged
		}
	}
	if err = s.adopt(ctx, lid, prevHash, opinions); err != nil {
		logger.With().Info("opinions not fully adopted", log.Err(err))
		return err
	}
	return nil
}

func (s *Syncer) adopt(ctx context.Context, lid types.LayerID, prevHash types.Hash32, opinions []*fetch.LayerOpinion) error {
	logger := s.logger.WithContext(ctx).WithFields(lid)
	latestVerified := s.mesh.LastVerified()
	if !latestVerified.Before(lid) {
		logger.With().Debug("opinions older than own verified layer")
		return nil
	}

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

	sortOpinions(opinions)
	var noBetter, noCert, noValidity int
	for _, opn := range opinions {
		if !opn.Verified.After(latestVerified) {
			noBetter++
			logger.With().Debug("node has same/higher verified layer than peers",
				log.Stringer("verified", latestVerified),
				log.Stringer("peers_verified", opn.Verified))
			continue
		}

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
			}
		}
		if needValidity {
			if len(opn.Valid) == 0 && len(opn.Invalid) == 0 {
				noValidity++
				logger.With().Debug("peer has no validity", log.Inline(opn))
			} else if opn.PrevAggHash != prevHash { // double-checking
				logger.With().Warning("peer has different prev hash",
					log.Inline(opn),
					log.Stringer("node_hash", prevHash))
			} else if err = s.adoptValidity(ctx, opn.Valid, opn.Invalid); err != nil {
				logger.With().Warning("failed to adopt validity", log.Inline(opn), log.Err(err))
			} else {
				logger.With().Info("adopted validity from peer",
					log.Inline(opn),
					log.Stringer("node_hash", prevHash))
				needValidity = false
			}
		}
		if !needCert && !needValidity {
			return nil
		}
	}
	numPeers := len(opinions)
	if noBetter == numPeers {
		return errNoBetterOpinions
	}
	if needCert {
		if noCert == numPeers {
			return errMissingCertificate
		}
		return errNoCertAdopted
	}
	if noValidity == numPeers {
		return errMissingValidity
	}
	return errNoValidityAdopted
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
	if err := s.certHandler.HandleSyncedCertificate(ctx, lid, cert); err != nil {
		return fmt.Errorf("opnions adopt cert: %w", err)
	}
	return nil
}

func (s *Syncer) adoptValidity(ctx context.Context, valid, invalid []types.BlockID) error {
	if len(valid)+len(invalid) == 0 {
		return nil
	}
	return s.cdb.WithTx(ctx, func(dbtx *sql.Tx) error {
		for _, bid := range valid {
			if bid == types.EmptyBlockID {
				continue
			}
			if err := blocks.SetValidIfNotSet(dbtx, bid); err != nil {
				return fmt.Errorf("opinions set valid: %w", err)
			}
		}
		for _, bid := range invalid {
			if err := blocks.SetInvalidIfNotSet(dbtx, bid); err != nil {
				return fmt.Errorf("opinions set invalid: %w", err)
			}
		}
		return nil
	})
}

// see https://github.com/spacemeshos/go-spacemesh/issues/2507 for implementation rationale.
func (s *Syncer) ensureMeshAgreement(
	ctx context.Context,
	logger log.Log,
	diffLayer types.LayerID,
	opinions []*fetch.LayerOpinion,
	resyncPeers map[p2p.Peer]struct{},
) (types.LayerID, error) {
	logger.Info("checking mesh hash agreement")
	prevLid := diffLayer.Sub(1)
	prevHash, err := layers.GetAggregatedHash(s.cdb, prevLid)
	if err != nil {
		logger.With().Error("mesh hash failed to get prev hash", log.Err(err))
		return types.LayerID{}, fmt.Errorf("mesh hash check previous: %w", err)
	}

	beacon, err := s.beacon.GetBeacon(diffLayer.GetEpoch())
	if err != nil {
		logger.With().Warning("mesh hash failed to get beacon", log.Err(err))
		return types.LayerID{}, fmt.Errorf("mesh hash get beacon: %w", err)
	}

	var (
		peers         = make([]p2p.Peer, 0, len(opinions))
		minFork, fork types.LayerID
		ed            *fetch.EpochData
		seen          = make(map[types.Hash32]struct{})
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
		if ed.Beacon != beacon {
			logger.With().Warning("detected different beacon",
				log.Stringer("peer", opn.Peer()),
				log.Stringer("peer_beacon", ed.Beacon),
				log.Stringer("beacon", beacon))
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

		peers = append(peers, opn.Peer())
		resyncPeers[opn.Peer()] = struct{}{}
		if minFork == (types.LayerID{}) || fork.Before(minFork) {
			minFork = fork
		}
		seen[opn.PrevAggHash] = struct{}{}
	}

	if minFork == (types.LayerID{}) {
		return types.LayerID{}, nil
	}

	from := minFork.Add(1)
	logger.With().Info("mesh hash syncing data from peers",
		log.Stringer("from", from),
		log.Array("peers", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
			for _, p := range peers {
				encoder.AppendString(p.String())
			}
			return nil
		})))
	to := from
	for lid := from; !lid.After(s.mesh.LatestLayer()); lid = lid.Add(1) {
		// ideally syncer should import opinions from peer and let tortoise decide whether to rerun
		// verifying tortoise or enter full tortoise mode.
		// however, for genesis running full tortoise with 10_000 layers as a sliding window is completely
		// viable. so here we don't sync opinions, just the data.
		// see https://github.com/spacemeshos/go-spacemesh/issues/2507
		if err = s.syncLayer(ctx, lid, peers...); err != nil {
			logger.With().Warning("mesh hash failed to sync layer",
				log.Stringer("sync_lid", lid),
				log.Err(err))
		}
		to = lid
	}
	logger.With().Info("resync'ed mesh from peers",
		log.Int("num_peers", len(peers)),
		log.Stringer("from", from),
		log.Stringer("to", to))

	// clear the agreement cache after importing new opinions
	s.forkFinder.Purge(true)
	return from, nil
}
