package mesh

import (
	"context"
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
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

func (mh *MalfeasanceHandler) Validate(ctx context.Context, data wire.ProofData) (types.NodeID, error) {
	bp, ok := data.(*wire.BallotProof)
	if !ok {
		return types.EmptyNodeID, errors.New("wrong message type for multi ballots")
	}
	for _, msg := range bp.Messages {
		if !mh.edVerifier.Verify(signing.BALLOT, msg.SmesherID, msg.SignedBytes(), msg.Signature) {
			return types.EmptyNodeID, errors.New("invalid signature")
		}
	}
	msg1, msg2 := bp.Messages[0], bp.Messages[1]
	ok, err := atxs.IdentityExists(mh.db, msg1.SmesherID)
	if err != nil {
		return types.EmptyNodeID, fmt.Errorf("check identity in ballot malfeasance %v: %w", msg1.SmesherID, err)
	}
	if !ok {
		return types.EmptyNodeID, errors.New("identity does not exist")
	}

	if msg1.SmesherID == msg2.SmesherID &&
		msg1.InnerMsg.Layer == msg2.InnerMsg.Layer &&
		msg1.InnerMsg.MsgHash != msg2.InnerMsg.MsgHash {
		return msg1.SmesherID, nil
	}
	mh.logger.Debug("received invalid ballot malfeasance proof",
		log.ZContext(ctx),
		zap.Stringer("first_smesher", bp.Messages[0].SmesherID),
		zap.Object("first_proof", &bp.Messages[0].InnerMsg),
		zap.Stringer("second_smesher", bp.Messages[1].SmesherID),
		zap.Object("second_proof", &bp.Messages[1].InnerMsg),
	)
	return types.EmptyNodeID, errors.New("invalid ballot malfeasance proof")
}

func (mh *MalfeasanceHandler) ReportProof(numProofs *prometheus.CounterVec) {
	numProofs.WithLabelValues(multiBallots).Inc()
}

func (mh *MalfeasanceHandler) ReportInvalidProof(numInvalidProofs *prometheus.CounterVec) {
	numInvalidProofs.WithLabelValues(multiBallots).Inc()
}
