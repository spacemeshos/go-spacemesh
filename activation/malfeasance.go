package activation

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-scale"
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

type MalfeasanceHandler struct {
	logger *zap.Logger
	db     sql.Executor

	edVerifier   *signing.EdVerifier
	postVerifier PostVerifier
}

type MalfeasanceOpt func(*MalfeasanceHandler)

func WithMalfeasanceLogger(logger *zap.Logger) MalfeasanceOpt {
	return func(mh *MalfeasanceHandler) {
		mh.logger = logger
	}
}

func NewMalfeasanceHandler(
	db sql.Executor,
	edVerifier *signing.EdVerifier,
	postVerifier PostVerifier,
	opt ...MalfeasanceOpt,
) *MalfeasanceHandler {
	mh := &MalfeasanceHandler{
		logger: zap.NewNop(),
		db:     db,

		edVerifier:   edVerifier,
		postVerifier: postVerifier,
	}
	for _, o := range opt {
		o(mh)
	}
	return mh
}

func (mh *MalfeasanceHandler) HandleDoublePublish(ctx context.Context, data scale.Type) (types.NodeID, error) {
	var (
		firstNid types.NodeID
		firstMsg wire.AtxProofMsg
	)
	ap, ok := data.(*wire.AtxProof)
	if !ok {
		return types.EmptyNodeID, errors.New("wrong message type for multiple ATXs")
	}
	for _, msg := range ap.Messages {
		if !mh.edVerifier.Verify(signing.ATX, msg.SmesherID, msg.SignedBytes(), msg.Signature) {
			return types.EmptyNodeID, errors.New("invalid signature")
		}
		if firstNid == types.EmptyNodeID {
			if err := hasPublishedAtxs(mh.db, msg.SmesherID); err != nil {
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
	// TODO(mafa): add metrics
	// numInvalidProofsATX.Inc()
	mh.logger.Warn("received invalid atx malfeasance proof",
		log.ZContext(ctx),
		zap.Stringer("first_smesher", ap.Messages[0].SmesherID),
		zap.Object("first_proof", &ap.Messages[0].InnerMsg),
		zap.Stringer("second_smesher", ap.Messages[1].SmesherID),
		zap.Object("second_proof", &ap.Messages[1].InnerMsg),
	)
	return types.EmptyNodeID, errors.New("invalid atx malfeasance proof")
}

func (mh *MalfeasanceHandler) HandleInvalidPostIndex(ctx context.Context, data scale.Type) (types.NodeID, error) {
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
	// TODO(mafa): add metrics
	// numInvalidProofsPostIndex.Inc()
	return types.EmptyNodeID, errors.New("invalid post index malfeasance proof - POST is valid")
}

func (mh *MalfeasanceHandler) HandleInvalidPrevATX(ctx context.Context, data scale.Type) (types.NodeID, error) {
	proof, ok := data.(*wire.InvalidPrevATXProof)
	if !ok {
		return types.EmptyNodeID, errors.New("wrong message type for invalid previous ATX")
	}

	atx1 := proof.Atx1
	if err := hasPublishedAtxs(mh.db, atx1.SmesherID); err != nil {
		return types.EmptyNodeID, fmt.Errorf("check identity %v in invalid previous ATX: %w", atx1.SmesherID, err)
	}

	if !mh.edVerifier.Verify(signing.ATX, atx1.SmesherID, atx1.SignedBytes(), atx1.Signature) {
		return types.EmptyNodeID, errors.New("atx1: invalid signature")
	}

	atx2 := proof.Atx2
	if atx1.SmesherID != atx2.SmesherID {
		// TODO(mafa): add metrics
		// numInvalidProofsPrevATX.Inc()
		return types.EmptyNodeID, errors.New("invalid old prev ATX malfeasance proof: smesher IDs are different")
	}

	if !mh.edVerifier.Verify(signing.ATX, atx2.SmesherID, atx2.SignedBytes(), atx2.Signature) {
		return types.EmptyNodeID, errors.New("atx2: invalid signature")
	}

	if atx1.ID() == atx2.ID() {
		// TODO(mafa): add metrics
		// numInvalidProofsPrevATX.Inc()
		return types.EmptyNodeID, errors.New("invalid old prev ATX malfeasance proof: ATX IDs are the same")
	}
	if atx1.PrevATXID != atx2.PrevATXID {
		// TODO(mafa): add metrics
		// numInvalidProofsPrevATX.Inc()
		return types.EmptyNodeID, errors.New("invalid old prev ATX malfeasance proof: prev ATX IDs are different")
	}
	return atx1.SmesherID, nil
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
