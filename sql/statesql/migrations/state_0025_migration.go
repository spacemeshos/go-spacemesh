package migrations

import (
	"encoding/hex"
	"errors"
	"time"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

type migration0025 struct {
	edVerifier *signing.EdVerifier
}

var _ sql.Migration = &migration0025{}

func New0025Migration(config config.Config) *migration0025 {
	return &migration0025{
		edVerifier: signing.NewEdVerifier(
			signing.WithVerifierPrefix(config.Genesis.GenesisID().Bytes()),
		),
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

		if proof.Proof.Type != wire.InvalidPrevATX {
			// we only care about invalid prev ATX proofs
			return true
		}

		id, err := m.Validate(proof.Proof.Data)
		if err == nil && id == nodeID {
			logger.Debug("Proof is valid", log.ZShortStringer("smesherID", nodeID))
			return true
		}
		proof.Proof.Data.(*wire.InvalidPrevATXProof).Atx1.VRFNonce = nil
		id, err = m.Validate(proof.Proof.Data)
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

func (m *migration0025) Validate(data wire.ProofData) (types.NodeID, error) {
	proof, ok := data.(*wire.InvalidPrevATXProof)
	if !ok {
		return types.EmptyNodeID, errors.New("wrong message type for invalid previous ATX")
	}

	atx1 := proof.Atx1
	if !m.edVerifier.Verify(signing.ATX, atx1.SmesherID, atx1.SignedBytes(), atx1.Signature) {
		return types.EmptyNodeID, errors.New("atx1: invalid signature")
	}

	atx2 := proof.Atx2
	if atx1.SmesherID != atx2.SmesherID {
		return types.EmptyNodeID, errors.New("invalid old prev ATX malfeasance proof: smesher IDs are different")
	}

	if !m.edVerifier.Verify(signing.ATX, atx2.SmesherID, atx2.SignedBytes(), atx2.Signature) {
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
