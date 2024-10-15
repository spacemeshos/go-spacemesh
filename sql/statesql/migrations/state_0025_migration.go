package migrations

import (
	"context"
	"encoding/hex"
	"time"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

type migration0025 struct {
	handler malfeasanceValidator
}

var _ sql.Migration = &migration0025{}

func New0025Migration(handler malfeasanceValidator) *migration0025 {
	return &migration0025{
		handler: handler,
	}
}

func (*migration0025) Name() string {
	return "check DB for invalid malfeasance proofs"
}

func (*migration0025) Order() int {
	return 25
}

func (*migration0025) Rollback() error {
	return nil
}

func (m *migration0025) Apply(db sql.Executor, logger *zap.Logger) error {
	updates := map[types.NodeID][]byte{}

	err := identities.IterateOps(db, builder.Operations{}, func(nodeID types.NodeID, bytes []byte, t time.Time) bool {
		proof := &wire.MalfeasanceProof{}
		codec.MustDecode(bytes, proof)

		id, err := m.handler.Validate(context.Background(), proof)
		if err == nil && id == nodeID {
			logger.Debug("Proof is valid", log.ZShortStringer("smesherID", nodeID))
			return true
		}

		if proof.Proof.Type != wire.InvalidPrevATX {
			logger.Warn("Found invalid proof during migration that cannot be fixed",
				log.ZShortStringer("smesherID", nodeID),
				zap.String("proof", hex.EncodeToString(bytes)),
				zap.Error(err),
			)
			return true
		}

		proof.Proof.Data.(*wire.InvalidPrevATXProof).Atx1.VRFNonce = nil
		id, err = m.handler.Validate(context.Background(), proof)
		if err == nil && id == nodeID {
			updates[nodeID] = codec.MustEncode(proof)
			return true
		}
		logger.Error("Failed to fix invalid malfeasance proof during migration",
			log.ZShortStringer("smesherID", nodeID),
			zap.String("proof", hex.EncodeToString(bytes)),
			zap.Error(err),
		)
		return true
	})
	if err != nil {
		return err
	}
	for nodeID, proofBytes := range updates {
		if _, err := db.Exec(`
				UPDATE identities
				SET proof = ?2
				WHERE pubkey = ?1
			`, func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
			stmt.BindBytes(2, proofBytes)
		}, nil); err != nil {
			logger.Error("Failed to update invalid proof",
				log.ZShortStringer("smesherID", nodeID),
				zap.Error(err),
			)
		}
		logger.Info("Fixed invalid proof during migration",
			log.ZShortStringer("smesherID", nodeID),
		)
	}
	return nil
}
