package hare3

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
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

func (mh *MalfeasanceHandler) Info(data wire.ProofData) (map[string]string, error) {
	hp, ok := data.(*wire.HareProof)
	if !ok {
		return nil, errors.New("wrong message type for hare equivocation")
	}
	return map[string]string{
		"msg1":       hp.Messages[0].InnerMsg.MsgHash.String(),
		"msg2":       hp.Messages[1].InnerMsg.MsgHash.String(),
		"layer":      hp.Messages[0].InnerMsg.Layer.String(),
		"round":      strconv.FormatUint(uint64(hp.Messages[0].InnerMsg.Round), 10),
		"smesher_id": hp.Messages[0].SmesherID.String(),
	}, nil
}

func (mh *MalfeasanceHandler) Validate(ctx context.Context, data wire.ProofData) (types.NodeID, error) {
	hp, ok := data.(*wire.HareProof)
	if !ok {
		return types.EmptyNodeID, errors.New("wrong message type for hare equivocation")
	}
	for _, msg := range hp.Messages {
		if !mh.edVerifier.Verify(signing.HARE, msg.SmesherID, msg.SignedBytes(), msg.Signature) {
			return types.EmptyNodeID, errors.New("invalid signature")
		}
	}
	msg1, msg2 := hp.Messages[0], hp.Messages[1]
	ok, err := atxs.IdentityExists(mh.db, msg1.SmesherID)
	if err != nil {
		return types.EmptyNodeID, fmt.Errorf("check identity in hare malfeasance %v: %w", msg1.SmesherID, err)
	}
	if !ok {
		return types.EmptyNodeID, fmt.Errorf("identity does not exist: %v", msg1.SmesherID)
	}

	if msg1.SmesherID == msg2.SmesherID &&
		msg1.InnerMsg.Layer == msg2.InnerMsg.Layer &&
		msg1.InnerMsg.Round == msg2.InnerMsg.Round &&
		msg1.InnerMsg.MsgHash != msg2.InnerMsg.MsgHash {
		return msg1.SmesherID, nil
	}
	mh.logger.Debug("received invalid hare malfeasance proof",
		log.ZContext(ctx),
		zap.Stringer("first_smesher", hp.Messages[0].SmesherID),
		zap.Object("first_proof", &hp.Messages[0].InnerMsg),
		zap.Stringer("second_smesher", hp.Messages[1].SmesherID),
		zap.Object("second_proof", &hp.Messages[1].InnerMsg),
	)
	return types.EmptyNodeID, errors.New("invalid hare malfeasance proof")
}

func (mh *MalfeasanceHandler) ReportProof(numProofs *prometheus.CounterVec) {
	numProofs.WithLabelValues(hareEquivocate).Inc()
}

func (mh *MalfeasanceHandler) ReportInvalidProof(numInvalidProofs *prometheus.CounterVec) {
	numInvalidProofs.WithLabelValues(hareEquivocate).Inc()
}
