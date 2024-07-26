package malfeasance2

import (
	"context"
	"fmt"
	"slices"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
)

var (
	ErrMalformedData  = fmt.Errorf("%w: malformed data", pubsub.ErrValidationReject)
	ErrWrongHash      = fmt.Errorf("%w: incorrect hash", pubsub.ErrValidationReject)
	ErrUnknownVersion = fmt.Errorf("%w: unknown version", pubsub.ErrValidationReject)
	ErrUnknownDomain  = fmt.Errorf("%w: unknown domain", pubsub.ErrValidationReject)
)

type Handler struct {
	logger   *zap.Logger
	db       sql.Executor
	self     p2p.Peer
	tortoise tortoise

	handlers map[ProofDomain]MalfeasanceHandler
}

func NewHandler(
	db sql.Executor,
	lg *zap.Logger,
	self p2p.Peer,
	tortoise tortoise,
) *Handler {
	return &Handler{
		db:       db,
		logger:   lg,
		self:     self,
		tortoise: tortoise,

		handlers: make(map[ProofDomain]MalfeasanceHandler),
	}
}

func (h *Handler) RegisterHandler(malfeasanceType ProofDomain, handler MalfeasanceHandler) {
	h.handlers[malfeasanceType] = handler
}

func (h *Handler) HandleSynced(ctx context.Context, expHash types.Hash32, _ p2p.Peer, msg []byte) error {
	var proof MalfeasanceProof
	if err := codec.Decode(msg, &proof); err != nil {
		h.logger.Warn("failed to decode malfeasance proof",
			zap.Stringer("exp_hash", expHash),
			zap.Error(err),
		)
		return ErrMalformedData
	}

	nodeIDs, err := h.handleProof(ctx, proof)
	if err != nil {
		h.logger.Warn("failed to handle synced malfeasance proof",
			zap.Stringer("exp_hash", expHash),
			zap.Error(err),
		)
		return err
	}

	if !slices.Contains(nodeIDs, types.NodeID(expHash)) {
		h.logger.Warn("synced malfeasance proof invalid for requested nodeID",
			zap.Stringer("exp_hash", expHash),
		)
		return ErrWrongHash
	}

	if err := h.storeProof(ctx, proof.Domain, msg); err != nil {
		h.logger.Warn("failed to store synced malfeasance proof",
			zap.Stringer("exp_hash", expHash),
			zap.Error(err),
		)
		return err
	}
	return nil
}

func (h *Handler) HandleGossip(ctx context.Context, peer p2p.Peer, msg []byte) error {
	if peer == h.self {
		// ignore messages from self, we already validate and persist proofs when publishing
		return nil
	}

	var proof MalfeasanceProof
	if err := codec.Decode(msg, &proof); err != nil {
		h.logger.Warn("failed to decode malfeasance proof",
			zap.Stringer("peer", peer),
			zap.Error(err),
		)
		return ErrMalformedData
	}

	_, err := h.handleProof(ctx, proof)
	if err != nil {
		h.logger.Warn("failed to handle gossiped malfeasance proof",
			zap.Stringer("peer", peer),
			zap.Error(err),
		)
		return err
	}

	if err := h.storeProof(ctx, proof.Domain, msg); err != nil {
		h.logger.Warn("failed to store synced malfeasance proof",
			zap.Error(err),
		)
		return err
	}

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

	return handler.Validate(ctx, proof.Proof)
}

func (h *Handler) storeProof(ctx context.Context, domain ProofDomain, proof []byte) error {
	return nil
}
