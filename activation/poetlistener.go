package activation

import (
	"context"

	"github.com/libp2p/go-libp2p-core/peer"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/lp2p/pubsub"
)

// PoetProofProtocol is the name of the PoetProof gossip protocol.
const PoetProofProtocol = "PoetProof"

type poetValidatorPersistor interface {
	Validate(proof types.PoetProof, poetID []byte, roundID string, signature []byte) error
	storeProof(proofMessage *types.PoetProofMessage) error
}

// PoetListener handles PoET gossip messages.
type PoetListener struct {
	Log    log.Log
	poetDb poetValidatorPersistor
}

func (l *PoetListener) HandlePoetProofMessage(ctx context.Context, _ peer.ID, msg []byte) pubsub.ValidationResult {
	var proofMessage types.PoetProofMessage
	if err := types.BytesToInterface(msg, &proofMessage); err != nil {
		l.Log.Error("failed to unmarshal poet membership proof: %v", err)
		return pubsub.ValidationIgnore
	}

	if err := l.poetDb.Validate(proofMessage.PoetProof, proofMessage.PoetServiceID,
		proofMessage.RoundID, proofMessage.Signature); err != nil {
		if types.IsProcessingError(err) {
			l.Log.Error("failed to validate poet proof: %v", err)
		} else {
			l.Log.Warning("poet proof not valid: %v", err)
		}
		return pubsub.ValidationIgnore
	}

	if err := l.poetDb.storeProof(&proofMessage); err != nil {
		l.Log.Error("failed to store poet proof: %v", err)
	}
	return pubsub.ValidationAccept
}

// NewPoetListener returns a new PoetListener.
func NewPoetListener(poetDb poetValidatorPersistor, logger log.Log) *PoetListener {
	return &PoetListener{
		Log:    logger,
		poetDb: poetDb,
	}
}
