package malfeasance2

import (
	"context"
	"fmt"
	"strconv"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/malfeasance"
	"github.com/spacemeshos/go-spacemesh/sql/marriage"
)

// nolint:unused
type Handler struct {
	logger     *zap.Logger
	db         sql.Executor
	self       p2p.Peer
	nodeIDs    []types.NodeID
	edVerifier *signing.EdVerifier
	tortoise   tortoise

	handlers map[ProofDomain]MalfeasanceHandler
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

		handlers: make(map[ProofDomain]MalfeasanceHandler),
	}
}

func (h *Handler) RegisterHandler(malfeasanceType ProofDomain, handler MalfeasanceHandler) {
	if _, ok := h.handlers[malfeasanceType]; ok {
		h.logger.Panic("handler already registered", zap.Int("malfeasanceType", int(malfeasanceType)))
	}
	h.handlers[malfeasanceType] = handler
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
