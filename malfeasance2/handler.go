package malfeasance2

import (
	"context"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// nolint:unused
type Handler struct {
	logger     *zap.Logger
	db         sql.Executor
	self       p2p.Peer
	nodeIDs    []types.NodeID
	edVerifier *signing.EdVerifier
	tortoise   tortoise
}

func NewHandler(
	db sql.Executor,
	lg *zap.Logger,
	self p2p.Peer,
	nodeIDs []types.NodeID,
	edVerifier *signing.EdVerifier,
	tortoise tortoise,
) *Handler {
	return &Handler{
		db:         db,
		logger:     lg,
		self:       self,
		nodeIDs:    nodeIDs,
		edVerifier: edVerifier,
		tortoise:   tortoise,
	}
}

func (h *Handler) Info(ctx context.Context, nodeID types.NodeID) (map[string]string, error) {
	return nil, sql.ErrNotFound
}
