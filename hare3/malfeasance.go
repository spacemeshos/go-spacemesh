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

const (
	hareEquivocate = "hare_eq"
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

func (mh *MalfeasanceHandler) HandleHareEquivocation(
	ctx context.Context,
	data scale.Type,
) (types.NodeID, []string, error) {
	hp, ok := data.(*wire.HareProof)
	if !ok {
		return types.EmptyNodeID, []string{hareEquivocate}, errors.New("wrong message type for hare equivocation")
	}
	for _, msg := range hp.Messages {
		if !mh.edVerifier.Verify(signing.HARE, msg.SmesherID, msg.SignedBytes(), msg.Signature) {
			return types.EmptyNodeID, []string{hareEquivocate}, errors.New("invalid signature")
		}
	}
	msg1, msg2 := hp.Messages[0], hp.Messages[1]
	ok, err := atxs.IdentityExists(mh.db, msg1.SmesherID)
	if err != nil {
		return types.EmptyNodeID, []string{hareEquivocate},
			fmt.Errorf("check identity in hare malfeasance %v: %w", msg1.SmesherID, err)
	}
	if !ok {
		return types.EmptyNodeID, []string{hareEquivocate},
			fmt.Errorf("identity does not exist: %v", msg1.SmesherID)
	}

	if msg1.SmesherID == msg2.SmesherID &&
		msg1.InnerMsg.Layer == msg2.InnerMsg.Layer &&
		msg1.InnerMsg.Round == msg2.InnerMsg.Round &&
		msg1.InnerMsg.MsgHash != msg2.InnerMsg.MsgHash {
		return msg1.SmesherID, []string{hareEquivocate}, nil
	}
	mh.logger.Warn("received invalid hare malfeasance proof",
		log.ZContext(ctx),
		zap.Stringer("first_smesher", hp.Messages[0].SmesherID),
		zap.Object("first_proof", &hp.Messages[0].InnerMsg),
		zap.Stringer("second_smesher", hp.Messages[1].SmesherID),
		zap.Object("second_proof", &hp.Messages[1].InnerMsg),
	)
	return types.EmptyNodeID, []string{hareEquivocate}, errors.New("invalid hare malfeasance proof")
}
