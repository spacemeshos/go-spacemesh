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
	log    log.Log
	poetDb poetValidatorPersistor
}

// HandlePoetProofMessage is a receiver for broadcast messages.
func (l *PoetListener) HandlePoetProofMessage(ctx context.Context, _ p2p.Peer, msg []byte) pubsub.ValidationResult {
	var proofMessage types.PoetProofMessage
	if err := types.BytesToInterface(msg, &proofMessage); err != nil {
		l.log.WithContext(ctx).With().Error("failed to unmarshal poet membership proof", log.Err(err))
		return pubsub.ValidationIgnore
	}

	ref, err := proofMessage.Ref()
	if err != nil {
		l.log.WithContext(ctx).With().Error("failed to get poet proof", log.Err(err))
		return pubsub.ValidationIgnore
	}

	if l.poetDb.HasProof(ref) {
		// don't spam the network
		return pubsub.ValidationIgnore
	}

	if err := l.poetDb.Validate(proofMessage.PoetProof, proofMessage.PoetServiceID,
		proofMessage.RoundID, proofMessage.Signature); err != nil {
		if types.IsProcessingError(err) {
			l.log.WithContext(ctx).With().Error("failed to validate poet proof", log.Err(err))
		} else {
			l.log.WithContext(ctx).With().Warning("poet proof not valid", log.Err(err))
		}
		return pubsub.ValidationIgnore
	}

	if err := l.poetDb.StoreProof(ref, &proofMessage); err != nil {
		if errors.Is(err, sql.ErrObjectExists) {
			// don't spam the network
			return pubsub.ValidationIgnore
		}
		l.log.WithContext(ctx).With().Error("failed to store poet proof", log.Err(err))
	}
	return pubsub.ValidationAccept
}

// NewPoetListener returns a new PoetListener.
func NewPoetListener(poetDb poetValidatorPersistor, logger log.Log) *PoetListener {
	return &PoetListener{
		log:    logger,
		poetDb: poetDb,
	}
}
