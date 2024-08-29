package malfeasance2

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/malfeasance"
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

func (p *Publisher) PublishV2ATXProof(
	ctx context.Context,
	smesherIDs []types.NodeID,
	domain ProofDomain,
	proof []byte,
) error {
	// Combine IDs from the present equivocation set for atx.SmesherID and IDs in atx.Marriages.
	allMalicious := make(map[types.NodeID]struct{})

	for _, id := range smesherIDs {
		set, err := identities.EquivocationSet(p.cdb, id)
		if err != nil {
			return fmt.Errorf("getting equivocation set: %w", err)
		}
		for _, id := range set {
			allMalicious[id] = struct{}{}
		}
		for _, id := range smesherIDs {
			allMalicious[id] = struct{}{}
		}
	}

	for id := range allMalicious {
		if err := malfeasance.Add(p.cdb, id, byte(domain), proof, time.Now()); err != nil {
			return fmt.Errorf("setting malfeasance proof: %w", err)
		}
		// TODO(mafa): cache proof
		// p.cdb.CacheMalfeasanceProof(id, proof)
		p.tortoise.OnMalfeasance(id)
	}

	// TODO(mafa): check if we are in sync before publishing, if not just return

	malfeasanceProof := &MalfeasanceProof{
		Version: 0,
		Domain:  domain,
		Proof:   proof,
	}
	if err := p.publisher.Publish(ctx, pubsub.MalfeasanceProof2, codec.MustEncode(malfeasanceProof)); err != nil {
		p.logger.Error("failed to broadcast malfeasance proof", zap.Error(err))
		return fmt.Errorf("broadcast atx malfeasance proof: %w", err)
	}

	return nil
}
