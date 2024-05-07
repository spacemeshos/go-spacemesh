package malfeasance

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"

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

// Handler processes MalfeasanceProof from gossip and, if deems it valid, propagates it to peers.
type Handler struct {
	logger       log.Log
	cdb          *datastore.CachedDB
	self         p2p.Peer
	nodeIDs      []types.NodeID
	edVerifier   SigVerifier
	tortoise     tortoise
	postVerifier postVerifier
}

func NewHandler(
	cdb *datastore.CachedDB,
	lg log.Log,
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
	}
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
		h.logger.With().Error("malformed message (sync)", log.Context(ctx), log.Err(err))
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
		h.logger.With().Error("malformed message", log.Context(ctx), log.Err(err))
		return errMalformedData
	}
	if peer == h.self {
		id, err := Validate(ctx, h.logger, h.cdb, h.edVerifier, h.postVerifier, &p)
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
	nodeID, err := Validate(ctx, h.logger, h.cdb, h.edVerifier, h.postVerifier, p)
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
	postVerifier postVerifier,
	p *wire.MalfeasanceGossip,
) (types.NodeID, error) {
	var (
		nodeID types.NodeID
		err    error
	)
	switch p.Proof.Type {
	case wire.HareEquivocation:
		nodeID, err = validateHareEquivocation(ctx, logger, cdb, edVerifier, &p.MalfeasanceProof)
	case wire.MultipleATXs:
		nodeID, err = validateMultipleATXs(ctx, logger, cdb, edVerifier, &p.MalfeasanceProof)
	case wire.MultipleBallots:
		nodeID, err = validateMultipleBallots(ctx, logger, cdb, edVerifier, &p.MalfeasanceProof)
	case wire.InvalidPostIndex:
		proof := p.MalfeasanceProof.Proof.Data.(*wire.InvalidPostIndexProof) // guaranteed to work by scale func
		nodeID, err = validateInvalidPostIndex(ctx, logger, cdb, edVerifier, postVerifier, proof)
	case wire.InvalidPrevATX:
		proof := p.MalfeasanceProof.Proof.Data.(*wire.InvalidPrevATXProof) // guaranteed to work by scale func
		nodeID, err = validateInvalidPrevATX(ctx, cdb, edVerifier, proof)
	default:
		return nodeID, fmt.Errorf("%w: unknown malfeasance type", errInvalidProof)
	}

	switch {
	case err == nil:
		return nodeID, nil
	case errors.Is(err, ErrKnownProof):
		return nodeID, err
	}
	logger.WithContext(ctx).With().Warning("malfeasance proof failed validation", log.Inline(p), log.Err(err))
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

func validateHareEquivocation(
	ctx context.Context,
	logger log.Log,
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
	proof *wire.MalfeasanceProof,
) (types.NodeID, error) {
	if proof.Proof.Type != wire.MultipleATXs {
		return types.EmptyNodeID, fmt.Errorf(
			"wrong malfeasance type. want %v, got %v",
			wire.MultipleATXs,
			proof.Proof.Type,
		)
	}
	var (
		firstNid types.NodeID
		firstMsg wire.AtxProofMsg
	)
	ap, ok := proof.Proof.Data.(*wire.AtxProof)
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

func validateInvalidPostIndex(
	ctx context.Context,
	logger log.Log,
	db sql.Executor,
	edVerifier SigVerifier,
	postVerifier postVerifier,
	proof *wire.InvalidPostIndexProof,
) (types.NodeID, error) {
	atx := &proof.Atx
	if !edVerifier.Verify(signing.ATX, atx.SmesherID, atx.SignedBytes(), atx.Signature) {
		return types.EmptyNodeID, errors.New("invalid signature")
	}
	commitmentAtx := atx.CommitmentATX
	if commitmentAtx == nil {
		atx, err := atxs.CommitmentATX(db, atx.SmesherID)
		if err != nil {
			return types.EmptyNodeID, fmt.Errorf("getting commitment ATX: %w", err)
		}
		commitmentAtx = &atx
	}
	post := (*shared.Proof)(atx.NIPost.Post)
	meta := &shared.ProofMetadata{
		NodeId:          atx.SmesherID[:],
		CommitmentAtxId: commitmentAtx[:],
		NumUnits:        atx.NumUnits,
		Challenge:       atx.NIPost.PostMetadata.Challenge,
		LabelsPerUnit:   atx.NIPost.PostMetadata.LabelsPerUnit,
	}
	if err := postVerifier.Verify(
		ctx,
		post,
		meta,
		verifying.SelectedIndex(int(proof.InvalidIdx)),
	); err != nil {
		return atx.SmesherID, nil
	}
	numInvalidProofsPostIndex.Inc()
	return types.EmptyNodeID, errors.New("invalid post index malfeasance proof - POST is valid")
}

func validateInvalidPrevATX(
	ctx context.Context,
	db sql.Executor,
	edVerifier SigVerifier,
	proof *wire.InvalidPrevATXProof,
) (types.NodeID, error) {
	atx1 := proof.Atx1
	if !edVerifier.Verify(signing.ATX, atx1.SmesherID, atx1.SignedBytes(), atx1.Signature) {
		return types.EmptyNodeID, errors.New("atx1: invalid signature")
	}

	atx2 := proof.Atx2
	if !edVerifier.Verify(signing.ATX, atx2.SmesherID, atx2.SignedBytes(), atx2.Signature) {
		return types.EmptyNodeID, errors.New("atx2: invalid signature")
	}

	if atx1.ID() == atx2.ID() {
		numInvalidProofsPrevATX.Inc()
		return types.EmptyNodeID, errors.New("invalid old prev ATX malfeasance proof: ATX IDs are the same")
	}
	if atx1.PrevATXID != atx2.PrevATXID {
		numInvalidProofsPrevATX.Inc()
		return types.EmptyNodeID, errors.New("invalid old prev ATX malfeasance proof: prev ATX IDs are different")
	}
	return atx1.SmesherID, nil
}
