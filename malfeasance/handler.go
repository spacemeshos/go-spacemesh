package malfeasance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
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
)

// Handler processes MalfeasanceProof from gossip and, if deems it valid, propagates it to peers.
type Handler struct {
	logger     log.Log
	cdb        *datastore.CachedDB
	self       p2p.Peer
	nodeID     types.NodeID
	cp         consensusProtocol
	edVerifier SigVerifier
	tortoise   tortoise
}

func NewHandler(
	cdb *datastore.CachedDB,
	lg log.Log,
	self p2p.Peer,
	nodeID types.NodeID,
	cp consensusProtocol,
	edVerifier SigVerifier,
	tortoise tortoise,
) *Handler {
	return &Handler{
		logger:     lg,
		cdb:        cdb,
		self:       self,
		nodeID:     nodeID,
		cp:         cp,
		edVerifier: edVerifier,
		tortoise:   tortoise,
	}
}

func (h *Handler) reportMalfeasance(smesher types.NodeID, mp *types.MalfeasanceProof) {
	h.tortoise.OnMalfeasance(smesher)
	events.ReportMalfeasance(smesher, mp)
	if h.nodeID == smesher {
		events.EmitOwnMalfeasanceProof(smesher, mp)
	}
}

// HandleSyncedMalfeasanceProof is the sync validator for MalfeasanceProof.
func (h *Handler) HandleSyncedMalfeasanceProof(ctx context.Context, expHash types.Hash32, _ p2p.Peer, data []byte) error {
	var p types.MalfeasanceProof
	if err := codec.Decode(data, &p); err != nil {
		numMalformed.Inc()
		h.logger.With().Error("malformed message (sync)", log.Context(ctx), log.Err(err))
		return errMalformedData
	}
	nodeID, err := h.validateAndSave(ctx, &types.MalfeasanceGossip{MalfeasanceProof: p})
	if err == nil && types.Hash32(nodeID) != expHash {
		return fmt.Errorf("%w: malfesance proof want %s, got %s", errWrongHash, expHash.ShortString(), nodeID.ShortString())
	}
	return err
}

// HandleMalfeasanceProof is the gossip receiver for MalfeasanceGossip.
func (h *Handler) HandleMalfeasanceProof(ctx context.Context, peer p2p.Peer, data []byte) error {
	var p types.MalfeasanceGossip
	if err := codec.Decode(data, &p); err != nil {
		numMalformed.Inc()
		h.logger.With().Error("malformed message", log.Context(ctx), log.Err(err))
		return errMalformedData
	}
	if peer == h.self {
		id, err := Validate(ctx, h.logger, h.cdb, h.edVerifier, h.cp, &p)
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

func (h *Handler) validateAndSave(ctx context.Context, p *types.MalfeasanceGossip) (types.NodeID, error) {
	nodeID, err := Validate(ctx, h.logger, h.cdb, h.edVerifier, h.cp, p)
	if err != nil {
		return types.EmptyNodeID, err
	}
	if err := h.cdb.WithTx(ctx, func(dbtx *sql.Tx) error {
		malicious, err := identities.IsMalicious(dbtx, nodeID)
		if err != nil {
			return fmt.Errorf("check known malicious: %w", err)
		} else if malicious {
			h.logger.WithContext(ctx).With().Debug("known malicious identity", log.Stringer("smesher", nodeID))
			return ErrKnownProof
		}
		encoded, err := codec.Encode(&p.MalfeasanceProof)
		if err != nil {
			h.logger.With().Panic("failed to encode MalfeasanceProof", log.Err(err))
		}
		if err := identities.SetMalicious(dbtx, nodeID, encoded, time.Now()); err != nil {
			return fmt.Errorf("add malfeasance proof: %w", err)
		}
		return nil
	}); err != nil {
		if !errors.Is(err, ErrKnownProof) {
			h.logger.WithContext(ctx).With().Error("failed to save MalfeasanceProof",
				log.Stringer("smesher", nodeID),
				log.Inline(p),
				log.Err(err),
			)
		}
		return types.EmptyNodeID, err
	}
	h.reportMalfeasance(nodeID, &p.MalfeasanceProof)
	h.cdb.CacheMalfeasanceProof(nodeID, &p.MalfeasanceProof)
	updateMetrics(p.Proof)
	h.logger.WithContext(ctx).With().Info("new malfeasance proof",
		log.Stringer("smesher", nodeID),
		log.Inline(p),
	)
	return nodeID, nil
}

func Validate(
	ctx context.Context,
	logger log.Log,
	cdb *datastore.CachedDB,
	edVerifier SigVerifier,
	cp consensusProtocol,
	p *types.MalfeasanceGossip,
) (types.NodeID, error) {
	var (
		nodeID types.NodeID
		err    error
	)
	switch p.Proof.Type {
	case types.HareEquivocation:
		nodeID, err = validateHareEquivocation(ctx, logger, cdb, edVerifier, &p.MalfeasanceProof)
	case types.MultipleATXs:
		nodeID, err = validateMultipleATXs(ctx, logger, cdb, edVerifier, &p.MalfeasanceProof)
	case types.MultipleBallots:
		nodeID, err = validateMultipleBallots(ctx, logger, cdb, edVerifier, &p.MalfeasanceProof)
	default:
		return nodeID, errors.New("unknown malfeasance type")
	}

	if err != nil {
		if !errors.Is(err, ErrKnownProof) {
			logger.WithContext(ctx).With().Warning("failed to validate malfeasance proof",
				log.Inline(p),
				log.Err(err),
			)
		}
		return nodeID, err
	}

	// msg is valid
	if p.Eligibility != nil && cp != nil {
		// when cp is nil, the data is generated by the node itself
		if err = validateHareEligibility(ctx, logger, nodeID, cp, p); err != nil {
			logger.WithContext(ctx).With().Warning("failed to validate hare eligibility",
				log.Stringer("smesher", nodeID),
				log.Inline(p),
				log.Err(err),
			)
			return nodeID, err
		}
	}
	return nodeID, nil
}

func updateMetrics(tp types.Proof) {
	switch tp.Type {
	case types.HareEquivocation:
		numProofsHare.Inc()
	case types.MultipleATXs:
		numProofsATX.Inc()
	case types.MultipleBallots:
		numProofsBallot.Inc()
	}
}

func checkIdentityExists(db sql.Executor, nodeID types.NodeID) error {
	_, err := atxs.GetLastIDByNodeID(db, nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNotFound) {
			return errors.New("identity does not exist")
		}
		return err
	}
	return nil
}

func validateHareEligibility(
	ctx context.Context,
	logger log.Log,
	nodeID types.NodeID,
	cp consensusProtocol,
	gossip *types.MalfeasanceGossip,
) error {
	if cp == nil || gossip == nil || gossip.Eligibility == nil {
		logger.WithContext(ctx).Fatal("invalid input")
	}

	emsg := gossip.Eligibility
	if nodeID != emsg.NodeID {
		return fmt.Errorf("mismatch node id")
	}
	// any type of MalfeasanceProof can be accompanied by a hare eligibility
	// forward the eligibility to hare for the running consensus processes.
	cp.HandleEligibility(ctx, emsg)
	return nil
}

func validateHareEquivocation(
	ctx context.Context,
	logger log.Log,
	db sql.Executor,
	edVerifier SigVerifier,
	proof *types.MalfeasanceProof,
) (types.NodeID, error) {
	if proof.Proof.Type != types.HareEquivocation {
		return types.EmptyNodeID, fmt.Errorf("wrong malfeasance type. want %v, got %v", types.HareEquivocation, proof.Proof.Type)
	}
	var (
		firstNid types.NodeID
		firstMsg types.HareProofMsg
	)
	hp, ok := proof.Proof.Data.(*types.HareProof)
	if !ok {
		return types.EmptyNodeID, errors.New("wrong message type for hare equivocation")
	}
	for _, msg := range hp.Messages {
		if !edVerifier.Verify(signing.HARE, msg.SmesherID, msg.SignedBytes(), msg.Signature) {
			return types.EmptyNodeID, errors.New("invalid signature")
		}
		if firstNid == types.EmptyNodeID {
			if err := checkIdentityExists(db, msg.SmesherID); err != nil {
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
	logger.With().Warning("received invalid hare malfeasance proof",
		log.Context(ctx),
		log.Stringer("first_smesher", hp.Messages[0].SmesherID),
		log.Object("first_proof", &hp.Messages[0].InnerMsg),
		log.Stringer("second_smesher", hp.Messages[1].SmesherID),
		log.Object("second_proof", &hp.Messages[1].InnerMsg),
	)
	numInvalidProofsHare.Inc()
	return types.EmptyNodeID, errors.New("invalid hare malfeasance proof")
}

func validateMultipleATXs(
	ctx context.Context,
	logger log.Log,
	db sql.Executor,
	edVerifier SigVerifier,
	proof *types.MalfeasanceProof,
) (types.NodeID, error) {
	if proof.Proof.Type != types.MultipleATXs {
		return types.EmptyNodeID, fmt.Errorf("wrong malfeasance type. want %v, got %v", types.MultipleATXs, proof.Proof.Type)
	}
	var (
		firstNid types.NodeID
		firstMsg types.AtxProofMsg
	)
	ap, ok := proof.Proof.Data.(*types.AtxProof)
	if !ok {
		return types.EmptyNodeID, errors.New("wrong message type for multiple ATXs")
	}
	for _, msg := range ap.Messages {
		if !edVerifier.Verify(signing.ATX, msg.SmesherID, msg.SignedBytes(), msg.Signature) {
			return types.EmptyNodeID, errors.New("invalid signature")
		}
		if firstNid == types.EmptyNodeID {
			if err := checkIdentityExists(db, msg.SmesherID); err != nil {
				return types.EmptyNodeID, fmt.Errorf("check identity in atx malfeasance %v: %w", msg.SmesherID, err)
			}
			firstNid = msg.SmesherID
			firstMsg = msg
		} else if msg.SmesherID == firstNid {
			if msg.InnerMsg.PublishEpoch == firstMsg.InnerMsg.PublishEpoch &&
				msg.InnerMsg.MsgHash != firstMsg.InnerMsg.MsgHash {
				return msg.SmesherID, nil
			}
		}
	}
	logger.With().Warning("received invalid atx malfeasance proof",
		log.Context(ctx),
		log.Stringer("first_smesher", ap.Messages[0].SmesherID),
		log.Object("first_proof", &ap.Messages[0].InnerMsg),
		log.Stringer("second_smesher", ap.Messages[1].SmesherID),
		log.Object("second_proof", &ap.Messages[1].InnerMsg),
	)
	numInvalidProofsATX.Inc()
	return types.EmptyNodeID, errors.New("invalid atx malfeasance proof")
}

func validateMultipleBallots(
	ctx context.Context,
	logger log.Log,
	db sql.Executor,
	edVerifier SigVerifier,
	proof *types.MalfeasanceProof,
) (types.NodeID, error) {
	if proof.Proof.Type != types.MultipleBallots {
		return types.EmptyNodeID, fmt.Errorf("wrong malfeasance type. want %v, got %v", types.MultipleBallots, proof.Proof.Type)
	}
	var (
		firstNid types.NodeID
		firstMsg types.BallotProofMsg
		err      error
	)
	bp, ok := proof.Proof.Data.(*types.BallotProof)
	if !ok {
		return types.EmptyNodeID, errors.New("wrong message type for multi ballots")
	}
	for _, msg := range bp.Messages {
		if !edVerifier.Verify(signing.BALLOT, msg.SmesherID, msg.SignedBytes(), msg.Signature) {
			return types.EmptyNodeID, errors.New("invalid signature")
		}
		if firstNid == types.EmptyNodeID {
			if err = checkIdentityExists(db, msg.SmesherID); err != nil {
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
	logger.With().Warning("received invalid ballot malfeasance proof",
		log.Context(ctx),
		log.Stringer("first_smesher", bp.Messages[0].SmesherID),
		log.Object("first_proof", &bp.Messages[0].InnerMsg),
		log.Stringer("second_smesher", bp.Messages[1].SmesherID),
		log.Object("second_proof", &bp.Messages[1].InnerMsg),
	)
	numInvalidProofsBallot.Inc()
	return types.EmptyNodeID, errors.New("invalid ballot malfeasance proof")
}
