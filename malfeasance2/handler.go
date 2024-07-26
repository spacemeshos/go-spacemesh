package malfeasance2

import (
	"context"
	"fmt"
	"slices"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

var errWrongHash = fmt.Errorf("%w: incorrect hash", pubsub.ErrValidationReject)

type Handler struct {
	logger   *zap.Logger
	cdb      *datastore.CachedDB
	self     p2p.Peer
	tortoise tortoise

	handlers map[ProofDomain]MalfeasanceHandler
}

func NewHandler(
	cdb *datastore.CachedDB,
	lg *zap.Logger,
	self p2p.Peer,
	tortoise tortoise,
) *Handler {
	return &Handler{
		cdb:      cdb,
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
	nodeIDs, err := h.handleProof(ctx, msg)
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
		return errWrongHash
	}

	if err := h.storeProof(ctx, InvalidActivation, msg); err != nil {
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

	return nil
}

func (h *Handler) handleProof(ctx context.Context, data []byte) ([]types.NodeID, error) {
	return nil, nil
}

func (h *Handler) storeProof(ctx context.Context, domain ProofDomain, proof []byte) error {
	return nil
}
