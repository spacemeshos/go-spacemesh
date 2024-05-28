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
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

var (
	ErrKnownProof    = errors.New("known proof")
	errMalformedData = fmt.Errorf("%w: malformed data", pubsub.ErrValidationReject)
	errWrongHash     = fmt.Errorf("%w: incorrect hash", pubsub.ErrValidationReject)
	errInvalidProof  = fmt.Errorf("%w: invalid proof", pubsub.ErrValidationReject)
)

type (
	MalfeasanceHandlerV1 func(ctx context.Context, data scale.Type) (types.NodeID, error)
	MalfeasanceHandlerV2 func(ctx context.Context, data []byte) (types.NodeID, error)
)

// Handler processes MalfeasanceProof from gossip and, if deems it valid, propagates it to peers.
type Handler struct {
	logger *zap.Logger
	cdb    *datastore.CachedDB

	handlersV1 map[uint8]MalfeasanceHandlerV1
	handlersV2 map[uint8]MalfeasanceHandlerV2

	self         p2p.Peer
	nodeIDs      []types.NodeID
	edVerifier   SigVerifier
	tortoise     tortoise
	postVerifier postVerifier
}

func NewHandler(
	cdb *datastore.CachedDB,
	lg *zap.Logger,
	self p2p.Peer,
	nodeID []types.NodeID,
	edVerifier SigVerifier,
	tortoise tortoise,
	postVerifier postVerifier,
) *Handler {
	return &Handler{
		logger:       lg,
		cdb:          cdb,
		self:         self,
		nodeIDs:      nodeID,
		edVerifier:   edVerifier,
		tortoise:     tortoise,
		postVerifier: postVerifier,

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
		id, err := h.Validate(ctx, &p)
		if err != nil {
			return err
		}
		h.reportMalfeasance(id, &p.MalfeasanceProof)
		// node saves malfeasance proof eagerly/atomically with the malicious data.
		// it has validated the proof before saving to db.
		updateMetrics(p.Proof)
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
	nodeID, err := h.Validate(ctx, p)
	if err != nil {
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
	updateMetrics(p.Proof)
	h.logger.Info("new malfeasance proof",
		log.ZContext(ctx),
		zap.Stringer("smesher", nodeID),
		zap.Inline(p),
	)
	return nodeID, nil
}

func (h *Handler) Validate(ctx context.Context, p *wire.MalfeasanceGossip) (types.NodeID, error) {
	var (
		nodeID types.NodeID
		err    error
	)

	handler, ok := h.handlersV1[p.Proof.Type]
	if ok {
		nodeID, err = handler(ctx, p.Proof.Data)
		if err == nil {
			return nodeID, nil
		}
		h.logger.Warn("malfeasance proof failed validation",
			log.ZContext(ctx),
			zap.Inline(p),
			zap.Error(err),
		)
		return nodeID, fmt.Errorf("%w: %v", errInvalidProof, err)
	}

	switch p.Proof.Type {
	case wire.HareEquivocation:
		nodeID, err = validateHareEquivocation(ctx, h.logger, h.cdb, h.edVerifier, &p.MalfeasanceProof)
	case wire.MultipleATXs:
		panic("incorrect implemented test")
	case wire.MultipleBallots:
		nodeID, err = validateMultipleBallots(ctx, h.logger, h.cdb, h.edVerifier, &p.MalfeasanceProof)
	case wire.InvalidPostIndex:
		panic("incorrect implemented test")
	case wire.InvalidPrevATX:
		panic("incorrect implemented test")
	default:
		return nodeID, fmt.Errorf("%w: unknown malfeasance type", errInvalidProof)
	}

	if err == nil {
		return nodeID, nil
	}
	h.logger.Warn("malfeasance proof failed validation",
		log.ZContext(ctx),
		zap.Inline(p),
		zap.Error(err),
	)
	return nodeID, fmt.Errorf("%w: %v", errInvalidProof, err)
}

func updateMetrics(tp wire.Proof) {
	switch tp.Type {
	case wire.HareEquivocation:
		numProofsHare.Inc()
	case wire.MultipleATXs:
		numProofsATX.Inc()
	case wire.MultipleBallots:
		numProofsBallot.Inc()
	case wire.InvalidPostIndex:
		numProofsPostIndex.Inc()
	case wire.InvalidPrevATX:
		numProofsPrevATX.Inc()
	}
}

func hasPublishedAtxs(db sql.Executor, nodeID types.NodeID) error {
	_, err := atxs.GetLastIDByNodeID(db, nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNotFound) {
			return errors.New("identity does not exist")
		}
		return err
	}
	return nil
}

func validateHareEquivocation(
	ctx context.Context,
	logger *zap.Logger,
	db sql.Executor,
	edVerifier SigVerifier,
	proof *wire.MalfeasanceProof,
) (types.NodeID, error) {
	if proof.Proof.Type != wire.HareEquivocation {
		return types.EmptyNodeID, fmt.Errorf(
			"wrong malfeasance type. want %v, got %v",
			wire.HareEquivocation,
			proof.Proof.Type,
		)
	}
	var (
		firstNid types.NodeID
		firstMsg wire.HareProofMsg
	)
	hp, ok := proof.Proof.Data.(*wire.HareProof)
	if !ok {
		return types.EmptyNodeID, errors.New("wrong message type for hare equivocation")
	}
	for _, msg := range hp.Messages {
		if !edVerifier.Verify(signing.HARE, msg.SmesherID, msg.SignedBytes(), msg.Signature) {
			return types.EmptyNodeID, errors.New("invalid signature")
		}
		if firstNid == types.EmptyNodeID {
			if err := hasPublishedAtxs(db, msg.SmesherID); err != nil {
				return types.EmptyNodeID, fmt.Errorf("check identity in hare malfeasance %v: %w", msg.SmesherID, err)
			}
			firstNid = msg.SmesherID
			firstMsg = msg
		} else if msg.SmesherID == firstNid {
			if msg.InnerMsg.Layer == firstMsg.InnerMsg.Layer &&
				msg.InnerMsg.Round == firstMsg.InnerMsg.Round &&
				msg.InnerMsg.MsgHash != firstMsg.InnerMsg.MsgHash {
				return msg.SmesherID, nil
			}
		}
	}
	logger.Warn("received invalid hare malfeasance proof",
		log.ZContext(ctx),
		zap.Stringer("first_smesher", hp.Messages[0].SmesherID),
		zap.Object("first_proof", &hp.Messages[0].InnerMsg),
		zap.Stringer("second_smesher", hp.Messages[1].SmesherID),
		zap.Object("second_proof", &hp.Messages[1].InnerMsg),
	)
	numInvalidProofsHare.Inc()
	return types.EmptyNodeID, errors.New("invalid hare malfeasance proof")
}

func validateMultipleBallots(
	ctx context.Context,
	logger *zap.Logger,
	db sql.Executor,
	edVerifier SigVerifier,
	proof *wire.MalfeasanceProof,
) (types.NodeID, error) {
	if proof.Proof.Type != wire.MultipleBallots {
		return types.EmptyNodeID, fmt.Errorf(
			"wrong malfeasance type. want %v, got %v",
			wire.MultipleBallots,
			proof.Proof.Type,
		)
	}
	var (
		firstNid types.NodeID
		firstMsg wire.BallotProofMsg
		err      error
	)
	bp, ok := proof.Proof.Data.(*wire.BallotProof)
	if !ok {
		return types.EmptyNodeID, errors.New("wrong message type for multi ballots")
	}
	for _, msg := range bp.Messages {
		if !edVerifier.Verify(signing.BALLOT, msg.SmesherID, msg.SignedBytes(), msg.Signature) {
			return types.EmptyNodeID, errors.New("invalid signature")
		}
		if firstNid == types.EmptyNodeID {
			if err = hasPublishedAtxs(db, msg.SmesherID); err != nil {
				return types.EmptyNodeID, fmt.Errorf("check identity in ballot malfeasance %v: %w", msg.SmesherID, err)
			}
			firstNid = msg.SmesherID
			firstMsg = msg
		} else if msg.SmesherID == firstNid {
			if msg.InnerMsg.Layer == firstMsg.InnerMsg.Layer &&
				msg.InnerMsg.MsgHash != firstMsg.InnerMsg.MsgHash {
				return msg.SmesherID, nil
			}
		}
	}
	logger.Warn("received invalid ballot malfeasance proof",
		log.ZContext(ctx),
		zap.Stringer("first_smesher", bp.Messages[0].SmesherID),
		zap.Object("first_proof", &bp.Messages[0].InnerMsg),
		zap.Stringer("second_smesher", bp.Messages[1].SmesherID),
		zap.Object("second_proof", &bp.Messages[1].InnerMsg),
	)
	numInvalidProofsBallot.Inc()
	return types.EmptyNodeID, errors.New("invalid ballot malfeasance proof")
}
