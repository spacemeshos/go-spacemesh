package hare3

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

func (mh *MalfeasanceHandler) HandleHareEquivocation(ctx context.Context, data scale.Type) (types.NodeID, error) {
	var (
		firstNid types.NodeID
		firstMsg wire.HareProofMsg
	)
	hp, ok := data.(*wire.HareProof)
	if !ok {
		return types.EmptyNodeID, errors.New("wrong message type for hare equivocation")
	}
	for _, msg := range hp.Messages {
		if !mh.edVerifier.Verify(signing.HARE, msg.SmesherID, msg.SignedBytes(), msg.Signature) {
			return types.EmptyNodeID, errors.New("invalid signature")
		}
		if firstNid == types.EmptyNodeID {
			ok, err := atxs.IdentityExists(mh.db, msg.SmesherID)
			if err != nil {
				return types.EmptyNodeID, fmt.Errorf("check identity in hare malfeasance %v: %w", msg.SmesherID, err)
			}
			if !ok {
				return types.EmptyNodeID, fmt.Errorf("identity does not exist: %v", msg.SmesherID)
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
	mh.logger.Warn("received invalid hare malfeasance proof",
		log.ZContext(ctx),
		zap.Stringer("first_smesher", hp.Messages[0].SmesherID),
		zap.Object("first_proof", &hp.Messages[0].InnerMsg),
		zap.Stringer("second_smesher", hp.Messages[1].SmesherID),
		zap.Object("second_proof", &hp.Messages[1].InnerMsg),
	)
	// TODO(mafa): add metrics
	// numInvalidProofsHare.Inc()
	return types.EmptyNodeID, errors.New("invalid hare malfeasance proof")
}
