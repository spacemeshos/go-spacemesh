package activation

import (
	"context"
	"errors"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// PoetProofProtocol is the name of the PoetProof gossip protocol.
const PoetProofProtocol = "PoetProof"

// PoetListener handles PoET gossip messages.
type PoetListener struct {
	Log    log.Log
	poetDb poetValidatorPersistor
}

// HandlePoetProofMessage is a receiver for broadcast messages.
func (l *PoetListener) HandlePoetProofMessage(ctx context.Context, _ p2p.Peer, msg []byte) pubsub.ValidationResult {
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

	if err := l.poetDb.StoreProof(&proofMessage); err != nil {
		if errors.Is(err, sql.ErrObjectExists) {
			// don't spam the network
			return pubsub.ValidationIgnore
		}
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
