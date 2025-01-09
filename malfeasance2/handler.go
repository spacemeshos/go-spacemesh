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
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/malfeasance"
	"github.com/spacemeshos/go-spacemesh/sql/marriage"
)

var (
	ErrMalformedData  = fmt.Errorf("%w: malformed data", pubsub.ErrValidationReject)
	ErrWrongHash      = fmt.Errorf("%w: incorrect hash", pubsub.ErrValidationReject)
	ErrUnknownVersion = fmt.Errorf("%w: unknown version", pubsub.ErrValidationReject)
	ErrUnknownDomain  = fmt.Errorf("%w: unknown domain", pubsub.ErrValidationReject)
)

type Handler struct {
	logger     *zap.Logger
	db         sql.Executor
	self       p2p.Peer
	nodeIDs    []types.NodeID
	edVerifier *signing.EdVerifier
	tortoise   tortoise

	handlers map[ProofDomain]MalfeasanceHandler

	// metrics
	numProofs        *prometheus.CounterVec
	numInvalidProofs *prometheus.CounterVec
	numMalformed     prometheus.Counter
}

func NewHandler(
	db sql.Executor,
	lg *zap.Logger,
	self p2p.Peer,
	nodeIDs []types.NodeID,
	edVerifier *signing.EdVerifier,
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
		db:         db,
		logger:     lg,
		self:       self,
		nodeIDs:    nodeIDs,
		edVerifier: edVerifier,
		tortoise:   tortoise,

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

	nodeIDs, err := h.handleProof(ctx, proof)
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
		zap.Array("valid_for", zapcore.ArrayMarshalerFunc(func(arr zapcore.ArrayEncoder) error {
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

	nodeIDs, err := h.handleProof(ctx, proof)
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
		zap.Array("valid_for", zapcore.ArrayMarshalerFunc(func(arr zapcore.ArrayEncoder) error {
			for _, id := range nodeIDs {
				arr.AppendString(id.String())
			}
			return nil
		})),
	)
	return nil
}

func (h *Handler) handleProof(ctx context.Context, proof MalfeasanceProof) ([]types.NodeID, error) {
	if proof.Version != 0 {
		// unsupported proof version
		return nil, ErrUnknownVersion
	}

	handler, ok := h.handlers[proof.Domain]
	if !ok {
		// unknown proof domain
		return nil, fmt.Errorf("%w: %d", ErrUnknownDomain, proof.Domain)
	}

	id, err := handler.Validate(ctx, proof.Proof)
	if err != nil {
		h.countInvalidProof(proof)
		return nil, err
	}

	validIDs := make(map[types.NodeID]struct{}, len(proof.Certificates)+1)
	validIDs[id] = struct{}{} // id has already been proven to be malfeasant

	// check certificates provided with the proof
	for _, cert := range proof.Certificates {
		if !h.edVerifier.Verify(signing.MARRIAGE, cert.TargetID, cert.SmesherID.Bytes(), cert.Signature) {
			continue
		}
		validIDs[cert.TargetID] = struct{}{} // TODO(mafa): this doesn't confirm that `TargetID` agreed to the marriage!
		validIDs[cert.SmesherID] = struct{}{}
	}

	return maps.Keys(validIDs), nil
}

func (h *Handler) storeProof(ctx context.Context, nodeIDs []types.NodeID, proof []byte, domain ProofDomain) error {
	if len(nodeIDs) > 1 {
		if err := h.updateMarriages(ctx, nodeIDs); err != nil {
			h.logger.Error("failed to update marriage set for valid malfeasance proof",
				log.ZContext(ctx),
				zap.Array("valid_for", zapcore.ArrayMarshalerFunc(func(arr zapcore.ArrayEncoder) error {
					for _, id := range nodeIDs {
						arr.AppendString(id.String())
					}
					return nil
				})),
				zap.Error(err),
			)
			return fmt.Errorf("update marriages: %w", err)
		}
	}

	var mID *marriage.ID
	if len(nodeIDs) > 1 {
		var err error
		id, err := marriage.FindIDByNodeID(h.db, nodeIDs[0])
		if err != nil {
			return fmt.Errorf("store malfeasance proof for %s: fetch marryID: %w", nodeIDs[0], err)
		}
		mID = new(marriage.ID)
		*mID = id
	}

	if err := malfeasance.AddProof(h.db, nodeIDs[0], mID, proof, int(domain), time.Now()); err != nil {
		return fmt.Errorf("store malfeasance proof for %s: %w", nodeIDs[0], err)
	}
	for _, nodeID := range nodeIDs[1:] {
		if err := malfeasance.SetMalicious(h.db, nodeID, *mID, time.Now()); err != nil {
			return fmt.Errorf("update malfeasance state for %s: %w", nodeID.ShortString(), err)
		}
	}

	return nil
}

// TODO(mafa): updating marriage information.
func (h *Handler) updateMarriages(ctx context.Context, nodeIDs []types.NodeID) error {
	return nil
}
