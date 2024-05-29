package malfeasance

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/spacemeshos/go-scale"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

var (
	ErrKnownProof    = errors.New("known proof")
	errMalformedData = fmt.Errorf("%w: malformed data", pubsub.ErrValidationReject)
	errWrongHash     = fmt.Errorf("%w: incorrect hash", pubsub.ErrValidationReject)
	errInvalidProof  = fmt.Errorf("%w: invalid proof", pubsub.ErrValidationReject)
)

type (
	MalfeasanceHandlerV1 func(ctx context.Context, data scale.Type) (types.NodeID, []string, error)
	MalfeasanceHandlerV2 func(ctx context.Context, data []byte) (types.NodeID, []string, error)
)

// Handler processes MalfeasanceProof from gossip and, if deems it valid, propagates it to peers.
type Handler struct {
	logger *zap.Logger
	cdb    *datastore.CachedDB

	handlersV1 map[uint8]MalfeasanceHandlerV1
	handlersV2 map[uint8]MalfeasanceHandlerV2

	self     p2p.Peer
	nodeIDs  []types.NodeID
	tortoise tortoise
}

func NewHandler(
	cdb *datastore.CachedDB,
	lg *zap.Logger,
	self p2p.Peer,
	nodeID []types.NodeID,
	tortoise tortoise,
) *Handler {
	return &Handler{
		logger:   lg,
		cdb:      cdb,
		self:     self,
		nodeIDs:  nodeID,
		tortoise: tortoise,

		handlersV1: make(map[uint8]MalfeasanceHandlerV1),
		handlersV2: make(map[uint8]MalfeasanceHandlerV2),
	}
}

func (h *Handler) RegisterHandlerV1(malfeasanceType uint8, handler MalfeasanceHandlerV1) {
	h.handlersV1[malfeasanceType] = handler
}

func (h *Handler) RegisterHandlerV2(malfeasanceType uint8, handler MalfeasanceHandlerV2) {
	h.handlersV2[malfeasanceType] = handler
}

func (h *Handler) reportMalfeasance(smesher types.NodeID, mp *wire.MalfeasanceProof) {
	h.tortoise.OnMalfeasance(smesher)
	events.ReportMalfeasance(smesher, mp)
	if slices.Contains(h.nodeIDs, smesher) {
		events.EmitOwnMalfeasanceProof(smesher, mp)
	}
}

// HandleSyncedMalfeasanceProof is the sync validator for MalfeasanceProof.
func (h *Handler) HandleSyncedMalfeasanceProof(
	ctx context.Context,
	expHash types.Hash32,
	_ p2p.Peer,
	data []byte,
) error {
	var p wire.MalfeasanceProof
	if err := codec.Decode(data, &p); err != nil {
		numMalformed.Inc()
		h.logger.Error("malformed message (sync)", log.ZContext(ctx), zap.Error(err))
		return errMalformedData
	}
	nodeID, err := h.validateAndSave(ctx, &wire.MalfeasanceGossip{MalfeasanceProof: p})
	if err == nil && types.Hash32(nodeID) != expHash {
		return fmt.Errorf(
			"%w: malfeasance proof want %s, got %s",
			errWrongHash,
			expHash.ShortString(),
			nodeID.ShortString(),
		)
	}
	return err
}

// HandleMalfeasanceProof is the gossip receiver for MalfeasanceGossip.
func (h *Handler) HandleMalfeasanceProof(ctx context.Context, peer p2p.Peer, data []byte) error {
	var p wire.MalfeasanceGossip
	if err := codec.Decode(data, &p); err != nil {
		numMalformed.Inc()
		h.logger.Error("malformed message", log.ZContext(ctx), zap.Error(err))
		return errMalformedData
	}
	if peer == h.self {
		id, metricLabels, err := h.Validate(ctx, &p)
		if err != nil {
			numInvalidProofs.WithLabelValues(metricLabels...).Inc()
			return err
		}
		h.reportMalfeasance(id, &p.MalfeasanceProof)
		// node saves malfeasance proof eagerly/atomically with the malicious data.
		// it has validated the proof before saving to db.
		numProofs.WithLabelValues(metricLabels...).Inc()
		return nil
	}
	_, err := h.validateAndSave(ctx, &p)
	return err
}

func (h *Handler) validateAndSave(ctx context.Context, p *wire.MalfeasanceGossip) (types.NodeID, error) {
	if p.Eligibility != nil {
		return types.EmptyNodeID, fmt.Errorf(
			"%w: eligibility field was deprecated with hare3",
			pubsub.ErrValidationReject,
		)
	}
	nodeID, metricLabels, err := h.Validate(ctx, p)
	if err != nil {
		numInvalidProofs.WithLabelValues(metricLabels...).Inc()
		return types.EmptyNodeID, err
	}
	if err := h.cdb.WithTx(ctx, func(dbtx *sql.Tx) error {
		malicious, err := identities.IsMalicious(dbtx, nodeID)
		if err != nil {
			return fmt.Errorf("check known malicious: %w", err)
		} else if malicious {
			h.logger.Debug("known malicious identity", log.ZContext(ctx), zap.Stringer("smesher", nodeID))
			return ErrKnownProof
		}
		encoded, err := codec.Encode(&p.MalfeasanceProof)
		if err != nil {
			h.logger.Panic("failed to encode MalfeasanceProof", zap.Error(err))
		}
		if err := identities.SetMalicious(dbtx, nodeID, encoded, time.Now()); err != nil {
			return fmt.Errorf("add malfeasance proof: %w", err)
		}
		return nil
	}); err != nil {
		if !errors.Is(err, ErrKnownProof) {
			h.logger.Error("failed to save MalfeasanceProof",
				log.ZContext(ctx),
				zap.Stringer("smesher", nodeID),
				zap.Inline(p),
				zap.Error(err),
			)
		}
		return types.EmptyNodeID, err
	}
	h.reportMalfeasance(nodeID, &p.MalfeasanceProof)
	h.cdb.CacheMalfeasanceProof(nodeID, &p.MalfeasanceProof)
	numProofs.WithLabelValues(metricLabels...).Inc()
	h.logger.Info("new malfeasance proof",
		log.ZContext(ctx),
		zap.Stringer("smesher", nodeID),
		zap.Inline(p),
	)
	return nodeID, nil
}

func (h *Handler) Validate(ctx context.Context, p *wire.MalfeasanceGossip) (types.NodeID, []string, error) {
	handler, ok := h.handlersV1[p.Proof.Type]
	if !ok {
		return types.EmptyNodeID, nil, fmt.Errorf("%w: unknown malfeasance type", errInvalidProof)
	}

	nodeID, metricLabels, err := handler(ctx, p.Proof.Data)
	if err == nil {
		return nodeID, metricLabels, nil
	}
	h.logger.Warn("malfeasance proof failed validation",
		log.ZContext(ctx),
		zap.Inline(p),
		zap.Error(err),
	)
	return nodeID, metricLabels, fmt.Errorf("%w: %v", errInvalidProof, err)
}
