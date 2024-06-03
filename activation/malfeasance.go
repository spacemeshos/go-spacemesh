package activation

import (
	"context"
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

const (
	multiATXs        = "atx"
	invalidPostIndex = "invalid_post_index"
	invalidPrevATX   = "invalid_prev_atx"
)

type MalfeasanceHandler struct {
	logger *zap.Logger
	db     sql.Executor

	edVerifier *signing.EdVerifier
}

func NewMalfeasanceHandler(
	db sql.Executor,
	logger *zap.Logger,
	edVerifier *signing.EdVerifier,
) *MalfeasanceHandler {
	return &MalfeasanceHandler{
		db:     db,
		logger: logger,

		edVerifier: edVerifier,
	}
}

func (mh *MalfeasanceHandler) Validate(ctx context.Context, data wire.ProofData) (types.NodeID, error) {
	ap, ok := data.(*wire.AtxProof)
	if !ok {
		return types.EmptyNodeID, errors.New("wrong message type for multiple ATXs")
	}
	for _, msg := range ap.Messages {
		if !mh.edVerifier.Verify(signing.ATX, msg.SmesherID, msg.SignedBytes(), msg.Signature) {
			return types.EmptyNodeID, errors.New("invalid signature")
		}
	}
	msg1, msg2 := ap.Messages[0], ap.Messages[1]
	ok, err := atxs.IdentityExists(mh.db, msg1.SmesherID)
	if err != nil {
		return types.EmptyNodeID, fmt.Errorf("check identity in atx malfeasance %v: %w", msg1.SmesherID, err)
	}
	if !ok {
		return types.EmptyNodeID, fmt.Errorf("identity does not exist: %v", msg1.SmesherID)
	}

	if msg1.SmesherID == msg2.SmesherID &&
		msg1.InnerMsg.PublishEpoch == msg2.InnerMsg.PublishEpoch &&
		msg1.InnerMsg.MsgHash != msg2.InnerMsg.MsgHash {
		return msg1.SmesherID, nil
	}
	mh.logger.Warn("received invalid atx malfeasance proof",
		log.ZContext(ctx),
		zap.Stringer("first_smesher", msg1.SmesherID),
		zap.Object("first_proof", &msg1.InnerMsg),
		zap.Stringer("second_smesher", msg2.SmesherID),
		zap.Object("second_proof", &msg2.InnerMsg),
	)
	return types.EmptyNodeID, errors.New("invalid atx malfeasance proof")
}

func (mh *MalfeasanceHandler) ReportProof(numProofs *prometheus.CounterVec) {
	numProofs.WithLabelValues(multiATXs).Inc()
}

func (mh *MalfeasanceHandler) ReportInvalidProof(numInvalidProofs *prometheus.CounterVec) {
	numInvalidProofs.WithLabelValues(multiATXs).Inc()
}

type InvalidPostIndexHandler struct {
	logger *zap.Logger
	db     sql.Executor

	edVerifier   *signing.EdVerifier
	postVerifier PostVerifier
}

func NewInvalidPostIndexHandler(
	db sql.Executor,
	logger *zap.Logger,
	edVerifier *signing.EdVerifier,
	postVerifier PostVerifier,
) *InvalidPostIndexHandler {
	return &InvalidPostIndexHandler{
		db:     db,
		logger: logger,

		edVerifier:   edVerifier,
		postVerifier: postVerifier,
	}
}

func (mh *InvalidPostIndexHandler) Validate(ctx context.Context, data wire.ProofData) (types.NodeID, error) {
	proof, ok := data.(*wire.InvalidPostIndexProof)
	if !ok {
		return types.EmptyNodeID, errors.New("wrong message type for invalid post index")
	}
	atx := &proof.Atx

	if !mh.edVerifier.Verify(signing.ATX, atx.SmesherID, atx.SignedBytes(), atx.Signature) {
		return types.EmptyNodeID, errors.New("invalid signature")
	}
	commitmentAtx := atx.CommitmentATXID
	if commitmentAtx == nil {
		atx, err := atxs.CommitmentATX(mh.db, atx.SmesherID)
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
	if err := mh.postVerifier.Verify(
		ctx,
		post,
		meta,
		verifying.SelectedIndex(int(proof.InvalidIdx)),
	); err != nil {
		return atx.SmesherID, nil
	}
	return types.EmptyNodeID, errors.New("invalid post index malfeasance proof - POST is valid")
}

func (mh *InvalidPostIndexHandler) ReportProof(numProofs *prometheus.CounterVec) {
	numProofs.WithLabelValues(invalidPostIndex).Inc()
}

func (mh *InvalidPostIndexHandler) ReportInvalidProof(numInvalidProofs *prometheus.CounterVec) {
	numInvalidProofs.WithLabelValues(invalidPostIndex).Inc()
}

type InvalidPrevATXHandler struct {
	logger *zap.Logger
	db     sql.Executor

	edVerifier *signing.EdVerifier
}

func NewInvalidPrevATXHandler(
	db sql.Executor,
	logger *zap.Logger,
	edVerifier *signing.EdVerifier,
) *InvalidPrevATXHandler {
	return &InvalidPrevATXHandler{
		db:     db,
		logger: logger,

		edVerifier: edVerifier,
	}
}

func (mh *InvalidPrevATXHandler) Validate(ctx context.Context, data wire.ProofData) (types.NodeID, error) {
	proof, ok := data.(*wire.InvalidPrevATXProof)
	if !ok {
		return types.EmptyNodeID, errors.New("wrong message type for invalid previous ATX")
	}

	atx1 := proof.Atx1
	ok, err := atxs.IdentityExists(mh.db, atx1.SmesherID)
	if err != nil {
		return types.EmptyNodeID, fmt.Errorf("check identity %v in invalid previous ATX: %w", atx1.SmesherID, err)
	}
	if !ok {
		return types.EmptyNodeID, fmt.Errorf("identity does not exist: %v", atx1.SmesherID)
	}

	if !mh.edVerifier.Verify(signing.ATX, atx1.SmesherID, atx1.SignedBytes(), atx1.Signature) {
		return types.EmptyNodeID, errors.New("atx1: invalid signature")
	}

	atx2 := proof.Atx2
	if atx1.SmesherID != atx2.SmesherID {
		return types.EmptyNodeID, errors.New("invalid old prev ATX malfeasance proof: smesher IDs are different")
	}

	if !mh.edVerifier.Verify(signing.ATX, atx2.SmesherID, atx2.SignedBytes(), atx2.Signature) {
		return types.EmptyNodeID, errors.New("atx2: invalid signature")
	}

	if atx1.ID() == atx2.ID() {
		return types.EmptyNodeID, errors.New("invalid old prev ATX malfeasance proof: ATX IDs are the same")
	}
	if atx1.PrevATXID != atx2.PrevATXID {
		return types.EmptyNodeID, errors.New("invalid old prev ATX malfeasance proof: prev ATX IDs are different")
	}
	return atx1.SmesherID, nil
}

func (mh *InvalidPrevATXHandler) ReportProof(numProofs *prometheus.CounterVec) {
	numProofs.WithLabelValues(invalidPrevATX).Inc()
}

func (mh *InvalidPrevATXHandler) ReportInvalidProof(numInvalidProofs *prometheus.CounterVec) {
	numInvalidProofs.WithLabelValues(invalidPrevATX).Inc()
}
