package malfeasance2

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/malfeasance"
	"github.com/spacemeshos/go-spacemesh/sql/marriage"
	"github.com/spacemeshos/go-spacemesh/system"
)

var (
	ErrMalformedData  = fmt.Errorf("%w: malformed data", pubsub.ErrValidationReject)
	ErrWrongHash      = fmt.Errorf("%w: incorrect hash", pubsub.ErrValidationReject)
	ErrUnknownVersion = fmt.Errorf("%w: unknown version", pubsub.ErrValidationReject)
	ErrUnknownDomain  = fmt.Errorf("%w: unknown domain", pubsub.ErrValidationReject)
)

type Handler struct {
	logger   *zap.Logger
	db       sql.StateDatabase
	self     p2p.Peer
	nodeIDs  []types.NodeID
	fetcher  system.Fetcher
	tortoise tortoise

	handlers map[ProofDomain]MalfeasanceHandler

	// metrics
	numProofs        *prometheus.CounterVec
	numInvalidProofs *prometheus.CounterVec
	numMalformed     prometheus.Counter
}

func NewHandler(
	db sql.StateDatabase,
	lg *zap.Logger,
	self p2p.Peer,
	nodeIDs []types.NodeID,
	fetcher system.Fetcher,
	tortoise tortoise,
) *Handler {
	proofCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metrics.Namespace,
			Subsystem: namespace,
			Name:      validProofName,
			Help:      "number of malfeasance proofs",
		},
		[]string{
			domainLabel,
			typeLabel,
		},
	)
	invalidProofCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metrics.Namespace,
			Subsystem: namespace,
			Name:      invalidProofName,
			Help:      "number of invalid malfeasance proofs",
		},
		[]string{
			domainLabel,
			typeLabel,
		},
	)

	return &Handler{
		db:       db,
		logger:   lg,
		self:     self,
		nodeIDs:  nodeIDs,
		fetcher:  fetcher,
		tortoise: tortoise,

		handlers: make(map[ProofDomain]MalfeasanceHandler),

		numProofs:        proofCounter,
		numInvalidProofs: invalidProofCounter,
		numMalformed:     invalidProofCounter.WithLabelValues("mal", "unknown"),
	}
}

func (h *Handler) RegisterHandler(malfeasanceType ProofDomain, handler MalfeasanceHandler) {
	if _, ok := h.handlers[malfeasanceType]; ok {
		h.logger.Panic("handler already registered", zap.Int("malfeasanceType", int(malfeasanceType)))
	}
	h.handlers[malfeasanceType] = handler
}

func (h *Handler) countProof(mp MalfeasanceProof) {
	labels := h.handlers[mp.Domain].ReportLabels(mp.Proof)
	h.numProofs.WithLabelValues(labels...).Inc()
}

func (h *Handler) countInvalidProof(mp MalfeasanceProof) {
	labels := h.handlers[mp.Domain].ReportLabels(mp.Proof)
	h.numInvalidProofs.WithLabelValues(labels...).Inc()
}

func (h *Handler) reportMalfeasance(smesher types.NodeID) {
	h.tortoise.OnMalfeasance(smesher)
	events.ReportMalfeasance(smesher)
	if slices.Contains(h.nodeIDs, smesher) {
		events.EmitOwnMalfeasanceProof(smesher)
	}
}

func (h *Handler) Info(ctx context.Context, nodeID types.NodeID) (map[string]string, error) {
	var (
		isMarried = false
		domain    int
		proof     []byte
	)
	marriageID, err := marriage.FindIDByNodeID(h.db, nodeID)
	if err == nil {
		isMarried = true
		proof, domain, err = malfeasance.MarriageProof(h.db, marriageID)
		if err != nil {
			return nil, fmt.Errorf("get malfeasance proof for married node ID %s: %w", nodeID, err)
		}
	} else {
		proof, domain, err = malfeasance.NodeIDProof(h.db, nodeID)
		if err != nil {
			return nil, fmt.Errorf("get malfeasance proof for node ID %s: %w", nodeID, err)
		}
	}

	mh, ok := h.handlers[ProofDomain(domain)]
	if !ok {
		return nil, fmt.Errorf("unknown malfeasance domain %d", domain)
	}
	properties, err := mh.Info(proof)
	if err != nil {
		return nil, fmt.Errorf("malfeasance info: %w", err)
	}
	properties["domain"] = strconv.FormatUint(uint64(domain), 10)
	if isMarried {
		properties["malicious_id"] = nodeID.String()
	}
	return properties, nil
}

func (h *Handler) HandleSynced(ctx context.Context, expHash types.Hash32, peer p2p.Peer, msg []byte) error {
	var proof MalfeasanceProof
	if err := codec.Decode(msg, &proof); err != nil {
		h.numMalformed.Inc()
		return ErrMalformedData
	}

	nodeIDs, err := h.handleProof(ctx, peer, proof)
	if err != nil {
		return errors.Join(err, pubsub.ErrValidationReject)
	}
	if !slices.Contains(nodeIDs, types.NodeID(expHash)) {
		// we log & return because libp2p will ignore the message if we return an error,
		// but only log "validation ignored" instead of the error we return
		h.logger.Warn("malfeasance proof for wrong identity",
			log.ZContext(ctx),
			zap.Stringer("peer", peer),
			log.ZShortStringer("expected", expHash),
			zap.Array("got", zapcore.ArrayMarshalerFunc(func(arr zapcore.ArrayEncoder) error {
				for _, id := range nodeIDs {
					arr.AppendString(id.String())
				}
				return nil
			})),
		)
		h.countInvalidProof(proof)
		return fmt.Errorf(
			"%w: malfeasance proof not valid for %s",
			ErrWrongHash,
			expHash.ShortString(),
		)
	}

	if err := h.storeProof(ctx, nodeIDs, proof.Proof, proof.Domain); err != nil {
		return fmt.Errorf("store synced malfeasance proof: %w", err)
	}

	for _, id := range nodeIDs {
		h.reportMalfeasance(id)
	}
	h.countProof(proof)
	h.logger.Debug("synced malfeasance proof",
		log.ZContext(ctx),
		log.ZShortStringer("requested", expHash),
		zap.Array("malicious IDs", zapcore.ArrayMarshalerFunc(func(arr zapcore.ArrayEncoder) error {
			for _, id := range nodeIDs {
				arr.AppendString(id.String())
			}
			return nil
		})),
	)
	return nil
}

func (h *Handler) HandleGossip(ctx context.Context, peer p2p.Peer, msg []byte) error {
	if peer == h.self {
		// ignore messages from self, we already validate and persist proofs when publishing
		return nil
	}

	var proof MalfeasanceProof
	if err := codec.Decode(msg, &proof); err != nil {
		h.numMalformed.Inc()
		return ErrMalformedData
	}

	nodeIDs, err := h.handleProof(ctx, peer, proof)
	if err != nil {
		return errors.Join(err, pubsub.ErrValidationReject)
	}

	if err := h.storeProof(ctx, nodeIDs, proof.Proof, proof.Domain); err != nil {
		return fmt.Errorf("store gossiped malfeasance proof: %w", err)
	}

	for _, id := range nodeIDs {
		h.reportMalfeasance(id)
	}
	h.countProof(proof)
	h.logger.Debug("received gossiped malfeasance proof",
		log.ZContext(ctx),
		zap.Array("malicious IDs", zapcore.ArrayMarshalerFunc(func(arr zapcore.ArrayEncoder) error {
			for _, id := range nodeIDs {
				arr.AppendString(id.String())
			}
			return nil
		})),
	)
	return nil
}

func (h *Handler) handleProof(ctx context.Context, peer p2p.Peer, proof MalfeasanceProof) ([]types.NodeID, error) {
	if proof.Version != 0 {
		// unsupported proof version
		return nil, ErrUnknownVersion
	}

	handler, ok := h.handlers[proof.Domain]
	if !ok {
		// unknown proof domain
		return nil, fmt.Errorf("%w: %d", ErrUnknownDomain, proof.Domain)
	}

	nodeID, err := handler.Validate(ctx, proof.Proof)
	if err != nil {
		h.countInvalidProof(proof)
		return nil, err
	}

	if err := h.fetchReferences(ctx, peer, proof.RefATXs); err != nil {
		return nil, fmt.Errorf("fetch references: %w", err)
	}

	mID, err := marriage.FindIDByNodeID(h.db, nodeID)
	switch {
	case errors.Is(err, sql.ErrNotFound):
		// smesher is not married, check if identity exists in the DB
		_, err := atxs.GetFirstIDByNodeID(h.db, nodeID)
		if err != nil {
			return nil, fmt.Errorf("%w: missing proof for identities existence", ErrMalformedData)
		}
		return []types.NodeID{nodeID}, nil
	case err != nil:
		return nil, fmt.Errorf("get marriage ID for %s: %w", nodeID.ShortString(), err)
	}

	ids, err := marriage.NodeIDsByID(h.db, mID)
	if err != nil {
		return nil, fmt.Errorf("get equivocation set for %s: %w", nodeID.ShortString(), err)
	}
	// ensure that the ID for which the proof was created is the first in the equivocation set
	ids = slices.DeleteFunc(ids, func(id types.NodeID) bool { return id == nodeID })
	return append([]types.NodeID{nodeID}, ids...), nil
}

func (h *Handler) fetchReferences(ctx context.Context, peer p2p.Peer, atxIDs []types.ATXID) error {
	if len(atxIDs) == 0 {
		return nil
	}

	hashes := make([]types.Hash32, len(atxIDs))
	for i, id := range atxIDs {
		hashes[i] = id.Hash32()
	}
	h.fetcher.RegisterPeerHashes(peer, hashes)
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		if err := h.fetcher.GetAtxs(ctx, atxIDs, system.WithoutLimiting()); err != nil {
			return fmt.Errorf("missing atxs %s: %w", atxIDs, err)
		}
		return nil
	})
	return eg.Wait()
}

func (h *Handler) storeProof(ctx context.Context, nodeIDs []types.NodeID, proof []byte, domain ProofDomain) error {
	return h.db.WithTxImmediate(ctx, func(tx sql.Transaction) error {
		if len(nodeIDs) == 1 {
			// smesher is not married
			malicious, err := malfeasance.IsMalicious(tx, nodeIDs[0])
			if err != nil {
				return fmt.Errorf("check if smesher is malicious: %w", err)
			}
			if malicious {
				h.logger.Debug("smesher is already marked as malicious",
					zap.String("smesher_id", nodeIDs[0].ShortString()),
				)
				return nil
			}
			if err := malfeasance.AddProof(tx, nodeIDs[0], nil, proof, int(domain), time.Now()); err != nil {
				return fmt.Errorf("store malfeasance proof for %s: %w", nodeIDs[0], err)
			}
			return nil
		}

		mID, err := marriage.FindIDByNodeID(tx, nodeIDs[0])
		if err != nil {
			return fmt.Errorf("get marriage ID for %s: %w", nodeIDs[0].ShortString(), err)
		}
		malicious, err := malfeasance.IsMalicious(tx, nodeIDs[0])
		if err != nil {
			return fmt.Errorf("check if smesher %s is malicious: %w", nodeIDs[0].ShortString(), err)
		}
		if !malicious {
			if err := malfeasance.AddProof(tx, nodeIDs[0], &mID, proof, int(domain), time.Now()); err != nil {
				return fmt.Errorf("store malfeasance proof for %s: %w", nodeIDs[0].ShortString(), err)
			}
		} else {
			h.logger.Debug("smesher is already marked as malicious",
				zap.String("smesher_id", nodeIDs[0].ShortString()),
			)
		}
		for _, nodeID := range nodeIDs[1:] {
			malicious, err := malfeasance.IsMalicious(tx, nodeID)
			if err != nil {
				return fmt.Errorf("check if smesher %s is malicious: %w", nodeID.ShortString(), err)
			}
			if malicious {
				h.logger.Debug("smesher is already marked as malicious",
					zap.String("smesher_id", nodeID.ShortString()),
				)
				continue
			}
			if err := malfeasance.SetMalicious(tx, nodeID, mID, time.Now()); err != nil {
				return fmt.Errorf("update malfeasance state for %s: %w", nodeID.ShortString(), err)
			}
		}
		return nil
	})
}
