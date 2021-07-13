package activation

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
)

// PoetProofProtocol is the name of the PoetProof gossip protocol.
const PoetProofProtocol = "PoetProof"

type poetValidatorPersistor interface {
	Validate(proof types.PoetProof, poetID []byte, roundID string, signature []byte) error
	storeProof(proofMessage *types.PoetProofMessage) error
}

// PoetListener handles PoET gossip messages.
type PoetListener struct {
	Log               log.Log
	net               service.Service
	poetDb            poetValidatorPersistor
	poetProofMessages chan service.GossipMessage
	started           bool
	exit              chan struct{}
}

// Start starts listening to PoET gossip messages.
func (l *PoetListener) Start(ctx context.Context) {
	if l.started {
		return
	}
	go l.loop(ctx)
	l.started = true
}

// Close performs graceful shutdown of the PoET listener.
func (l *PoetListener) Close() {
	close(l.exit)
	l.started = false
}

func (l *PoetListener) loop(ctx context.Context) {
	for {
		select {
		case poetProof := <-l.poetProofMessages:
			if poetProof == nil {
				l.Log.WithContext(ctx).Error("nil poet message received")
				continue
			}
			go l.handlePoetProofMessage(ctx, poetProof)
		case <-l.exit:
			l.Log.WithContext(ctx).Info("listening stopped")
			return
		}
	}
}

func (l *PoetListener) handlePoetProofMessage(ctx context.Context, gossipMessage service.GossipMessage) {
	// ⚠️ IMPORTANT: We must not ensure that the node is synced! PoET messages must be propagated regardless.
	var proofMessage types.PoetProofMessage
	if err := types.BytesToInterface(gossipMessage.Bytes(), &proofMessage); err != nil {
		l.Log.Error("failed to unmarshal PoET membership proof: %v", err)
		return
	}
	if err := l.poetDb.Validate(proofMessage.PoetProof, proofMessage.PoetServiceID,
		proofMessage.RoundID, proofMessage.Signature); err != nil {

		if types.IsProcessingError(err) {
			l.Log.Error("failed to validate PoET proof: %v", err)
		} else {
			l.Log.Warning("PoET proof not valid: %v", err)
		}
		return
	}

	gossipMessage.ReportValidation(ctx, PoetProofProtocol)

	if err := l.poetDb.storeProof(&proofMessage); err != nil {
		l.Log.Error("failed to store PoET proof: %v", err)
	}
}

// NewPoetListener returns a new PoetListener.
func NewPoetListener(net service.Service, poetDb poetValidatorPersistor, logger log.Log) *PoetListener {
	return &PoetListener{
		Log:               logger,
		net:               net,
		poetDb:            poetDb,
		poetProofMessages: net.RegisterGossipProtocol(PoetProofProtocol, priorityq.Low),
		exit:              make(chan struct{}),
	}
}
