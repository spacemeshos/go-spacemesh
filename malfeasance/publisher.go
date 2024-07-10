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
	publisher pubsub.Publisher
}

func NewPublisher(
	logger *zap.Logger,
	cdb *datastore.CachedDB,
	tortoise tortoise,
	publisher pubsub.Publisher,
) *Publisher {
	return &Publisher{
		logger:    logger,
		cdb:       cdb,
		tortoise:  tortoise,
		publisher: publisher,
	}
}

// Publishes a malfeasance proof to the network.
func (p *Publisher) PublishProof(ctx context.Context, smesherID types.NodeID, proof *wire.MalfeasanceProof) error {
	err := identities.SetMalicious(p.cdb, smesherID, codec.MustEncode(proof), time.Now())
	if err != nil {
		return fmt.Errorf("adding malfeasance proof: %w", err)
	}
	p.cdb.CacheMalfeasanceProof(smesherID, proof)
	p.tortoise.OnMalfeasance(smesherID)

	gossip := wire.MalfeasanceGossip{
		MalfeasanceProof: *proof,
	}
	if err = p.publisher.Publish(ctx, pubsub.MalfeasanceProof, codec.MustEncode(&gossip)); err != nil {
		p.logger.Error("failed to broadcast malfeasance proof", zap.Error(err))
		return fmt.Errorf("broadcast atx malfeasance proof: %w", err)
	}
	return nil
}
