package mesh

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-scale"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

const multiBallots = "ballot"

type MalfeasanceHandler struct {
	logger *zap.Logger
	db     sql.Executor

	edVerifier *signing.EdVerifier
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
	opt ...MalfeasanceOpt,
) *MalfeasanceHandler {
	mh := &MalfeasanceHandler{
		logger: zap.NewNop(),
		db:     db,

		edVerifier: edVerifier,
	}
	for _, o := range opt {
		o(mh)
	}
	return mh
}

func (mh *MalfeasanceHandler) HandleMultipleBallots(
	ctx context.Context,
	data scale.Type,
) (types.NodeID, []string, error) {
	var (
		firstNid types.NodeID
		firstMsg wire.BallotProofMsg
	)
	bp, ok := data.(*wire.BallotProof)
	if !ok {
		return types.EmptyNodeID, []string{multiBallots}, errors.New("wrong message type for multi ballots")
	}
	for _, msg := range bp.Messages {
		if !mh.edVerifier.Verify(signing.BALLOT, msg.SmesherID, msg.SignedBytes(), msg.Signature) {
			return types.EmptyNodeID, []string{multiBallots}, errors.New("invalid signature")
		}
		if firstNid == types.EmptyNodeID {
			ok, err := atxs.IdentityExists(mh.db, msg.SmesherID)
			if err != nil {
				return types.EmptyNodeID, []string{multiBallots},
					fmt.Errorf("check identity in ballot malfeasance %v: %w", msg.SmesherID, err)
			}
			if !ok {
				return types.EmptyNodeID, []string{multiBallots}, errors.New("identity does not exist")
			}
			firstNid = msg.SmesherID
			firstMsg = msg
		} else if msg.SmesherID == firstNid {
			if msg.InnerMsg.Layer == firstMsg.InnerMsg.Layer &&
				msg.InnerMsg.MsgHash != firstMsg.InnerMsg.MsgHash {
				return msg.SmesherID, []string{multiBallots}, nil
			}
		}
	}
	mh.logger.Warn("received invalid ballot malfeasance proof",
		log.ZContext(ctx),
		zap.Stringer("first_smesher", bp.Messages[0].SmesherID),
		zap.Object("first_proof", &bp.Messages[0].InnerMsg),
		zap.Stringer("second_smesher", bp.Messages[1].SmesherID),
		zap.Object("second_proof", &bp.Messages[1].InnerMsg),
	)
	return types.EmptyNodeID, []string{multiBallots}, errors.New("invalid ballot malfeasance proof")
}
