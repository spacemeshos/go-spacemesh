package malfeasance2

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
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

func (h *Handler) countProof(mp MalfeasanceProof) {
	h.handlers[mp.Domain].ReportProof(numProofs)
}

func (h *Handler) countInvalidProof(mp MalfeasanceProof) {
	h.handlers[mp.Domain].ReportInvalidProof(numInvalidProofs)
}

func (h *Handler) reportMalfeasance(smesher types.NodeID, proof []byte) {
	h.tortoise.OnMalfeasance(smesher)
	events.ReportMalfeasance(smesher, proof)
	if slices.Contains(h.nodeIDs, smesher) {
		events.EmitOwnMalfeasanceProof(smesher, proof)
	}
}

func (h *Handler) Info(data []byte) (map[string]string, error) {
	var p MalfeasanceProof
	if err := codec.Decode(data, &p); err != nil {
		return nil, fmt.Errorf("decode malfeasance proof: %w", err)
	}
	mh, ok := h.handlers[p.Domain]
	if !ok {
		return nil, fmt.Errorf("unknown malfeasance domain %d", p.Domain)
	}
	properties, err := mh.Info(p.Proof)
	if err != nil {
		return nil, fmt.Errorf("malfeasance info: %w", err)
	}
	properties["domain"] = strconv.FormatUint(uint64(p.Domain), 10)
	return properties, nil
}

func (h *Handler) HandleSynced(ctx context.Context, expHash types.Hash32, peer p2p.Peer, msg []byte) error {
	var proof MalfeasanceProof
	if err := codec.Decode(msg, &proof); err != nil {
		numMalformed.Inc()
		return ErrMalformedData
	}

	nodeIDs, err := h.handleProof(ctx, proof)
	if err != nil {
		return err
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
					arr.AppendString(id.ShortString())
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

	if err := h.storeProof(ctx, proof.Domain, msg); err != nil {
		return fmt.Errorf("store synced malfeasance proof: %w", err)
	}

	for _, id := range nodeIDs {
		h.reportMalfeasance(id, msg)
	}
	h.countProof(proof)
	h.logger.Debug("synced malfeasance proof",
		log.ZContext(ctx),
		log.ZShortStringer("requested", expHash),
		zap.Array("valid_for", zapcore.ArrayMarshalerFunc(func(arr zapcore.ArrayEncoder) error {
			for _, id := range nodeIDs {
				arr.AppendString(id.ShortString())
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
		numMalformed.Inc()
		return ErrMalformedData
	}

	nodeIDs, err := h.handleProof(ctx, proof)
	if err != nil {
		return err
	}

	if err := h.storeProof(ctx, proof.Domain, msg); err != nil {
		return fmt.Errorf("store gossiped malfeasance proof: %w", err)
	}

	for _, id := range nodeIDs {
		h.reportMalfeasance(id, msg)
	}
	h.countProof(proof)
	h.logger.Debug("received gossiped malfeasance proof",
		log.ZContext(ctx),
		zap.Array("valid_for", zapcore.ArrayMarshalerFunc(func(arr zapcore.ArrayEncoder) error {
			for _, id := range nodeIDs {
				arr.AppendString(id.ShortString())
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

	validIDs := make([]types.NodeID, 0, len(proof.Certificates)+1)
	validIDs = append(validIDs, id) // id has already been proven to be malfeasant

	// check certificates provided with the proof
	// TODO(mafa): only works if the main identity becomes malfeasant - try different approach with merkle proofs
	for _, cert := range proof.Certificates {
		if id != cert.TargetID {
			continue
		}
		if !h.edVerifier.Verify(signing.MARRIAGE, cert.TargetID, cert.SmesherID.Bytes(), cert.Signature) {
			continue
		}
		validIDs = append(validIDs, cert.SmesherID)
	}

	return validIDs, nil
}

// TODO(mafa): store proof in db by
//   - updating marriage information if needed (e.g. new smesher in the malfeasant marriage certificate set)
//   - storing the proof for the identity that was proven to be malicious
func (h *Handler) storeProof(ctx context.Context, domain ProofDomain, proof []byte) error {
	_ = h.db
	return nil
}
