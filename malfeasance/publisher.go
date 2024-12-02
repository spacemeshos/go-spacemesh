package malfeasance

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

type Publisher struct {
	logger    *zap.Logger
	cdb       *datastore.CachedDB
	tortoise  tortoise
	sync      syncer
	publisher pubsub.Publisher
}

func NewPublisher(
	logger *zap.Logger,
	cdb *datastore.CachedDB,
	sync syncer,
	tortoise tortoise,
	publisher pubsub.Publisher,
) *Publisher {
	return &Publisher{
		logger:    logger,
		cdb:       cdb,
		tortoise:  tortoise,
		sync:      sync,
		publisher: publisher,
	}
}

// Publishes a malfeasance proof to the network.
func (p *Publisher) PublishProof(ctx context.Context, smesherID types.NodeID, proof *wire.MalfeasanceProof) error {
	malicious, err := identities.IsMalicious(p.cdb, smesherID)
	if err != nil {
		return fmt.Errorf("check if smesher is malicious: %w", err)
	}
	if malicious {
		p.logger.Debug("smesher is already marked as malicious", zap.String("smesher_id", smesherID.ShortString()))
		return nil
	}

	if err := identities.SetMalicious(p.cdb, smesherID, codec.MustEncode(proof), time.Now()); err != nil {
		return fmt.Errorf("adding malfeasance proof: %w", err)
	}

	p.cdb.CacheMalfeasanceProof(smesherID, codec.MustEncode(proof))
	p.tortoise.OnMalfeasance(smesherID)

	// Only gossip the proof if we are synced (to not spam the network with proofs others probably already have).
	if !p.sync.ListenToATXGossip() {
		p.logger.Debug("not synced, not broadcasting malfeasance proof",
			zap.String("smesher_id", smesherID.ShortString()),
		)
		return nil
	}
	gossip := wire.MalfeasanceGossip{
		MalfeasanceProof: *proof,
	}
	if err := p.publisher.Publish(ctx, pubsub.MalfeasanceProof, codec.MustEncode(&gossip)); err != nil {
		return fmt.Errorf("broadcast atx malfeasance proof: %w", err)
	}
	return nil
}
