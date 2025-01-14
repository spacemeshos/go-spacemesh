package malfeasance2

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/malfeasance"
	"github.com/spacemeshos/go-spacemesh/sql/marriage"
)

type Publisher struct {
	logger    *zap.Logger
	cdb       *datastore.CachedDB
	sync      syncer
	tortoise  tortoise
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
		sync:      sync,
		tortoise:  tortoise,
		publisher: publisher,
	}
}

func (p *Publisher) PublishATXProof(ctx context.Context, nodeID types.NodeID, proof []byte) error {
	marriageID, err := marriage.FindIDByNodeID(p.cdb, nodeID)
	switch {
	case errors.Is(err, sql.ErrNotFound): // smesher is not married
		malicious, err := malfeasance.IsMalicious(p.cdb, nodeID)
		if err != nil {
			return fmt.Errorf("check if smesher is malicious: %w", err)
		}
		if malicious {
			p.logger.Debug("smesher is already marked as malicious", zap.String("smesher_id", nodeID.ShortString()))
			return nil
		}
		if err := malfeasance.AddProof(p.cdb, nodeID, nil, proof, int(InvalidActivation), time.Now()); err != nil {
			return fmt.Errorf("setting malfeasance proof: %w", err)
		}
		atxID, err := atxs.GetFirstIDByNodeID(p.cdb, nodeID)
		if err != nil {
			return fmt.Errorf("getting atx id: %w", err)
		}
		return p.publish(ctx, nodeID, []types.ATXID{atxID}, proof, InvalidActivation)
	case err != nil:
		return fmt.Errorf("getting equivocation set: %w", err)
	default: // smesher is married
	}

	// Combine IDs from the present equivocation set for atx.SmesherID and IDs in atx.Marriages.
	set, err := marriage.NodeIDsByID(p.cdb, marriageID)
	if err != nil {
		return fmt.Errorf("getting equivocation set: %w", err)
	}

	publish := false // whether to publish the proof
	malicious, err := malfeasance.IsMalicious(p.cdb, nodeID)
	if err != nil {
		return fmt.Errorf("check if smesher is malicious: %w", err)
	}
	if !malicious {
		err := malfeasance.AddProof(p.cdb, nodeID, &marriageID, proof, int(InvalidActivation), time.Now())
		if err != nil {
			return fmt.Errorf("setting malfeasance proof: %w", err)
		}
		publish = true
	}

	mATXs := make(map[types.ATXID]struct{})
	for _, id := range set {
		info, err := marriage.FindByNodeID(p.cdb, id)
		if err != nil {
			return fmt.Errorf("getting marriage info: %w", err)
		}
		mATXs[info.ATX] = struct{}{}
		if id == nodeID {
			// already handled
			continue
		}
		malicious, err := malfeasance.IsMalicious(p.cdb, id)
		if err != nil {
			return fmt.Errorf("check if smesher is malicious: %w", err)
		}
		if malicious {
			p.logger.Debug("smesher is already marked as malicious", zap.String("smesher_id", id.ShortString()))
			continue
		}
		publish = true
		if err := malfeasance.SetMalicious(p.cdb, id, marriageID, time.Now()); err != nil {
			return fmt.Errorf("setting malicious: %w", err)
		}
	}

	if !publish {
		// all smeshers were already marked as malicious - no gossip to void spamming the network
		return nil
	}
	return p.publish(ctx, nodeID, maps.Keys(mATXs), proof, ProofDomain(InvalidActivation))
}

func (p *Publisher) Regossip(ctx context.Context, nodeID types.NodeID) error {
	marriageID, err := marriage.FindIDByNodeID(p.cdb, nodeID)
	switch {
	case errors.Is(err, sql.ErrNotFound): // smesher is not married
		malicious, err := malfeasance.IsMalicious(p.cdb, nodeID)
		if err != nil {
			return fmt.Errorf("check if smesher is malicious: %w", err)
		}
		if malicious {
			p.logger.Debug("smesher is already marked as malicious", zap.String("smesher_id", nodeID.ShortString()))
			return nil
		}
		proof, domain, err := malfeasance.NodeIDProof(p.cdb, nodeID)
		if err != nil {
			return fmt.Errorf("getting malfeasance proof: %w", err)
		}
		atxID, err := atxs.GetFirstIDByNodeID(p.cdb, nodeID)
		if err != nil {
			return fmt.Errorf("getting atx id: %w", err)
		}
		return p.publish(ctx, nodeID, []types.ATXID{atxID}, proof, ProofDomain(domain))
	case err != nil:
		return fmt.Errorf("getting equivocation set: %w", err)
	default: // smesher is married
	}

	proof, domain, err := malfeasance.MarriageProof(p.cdb, marriageID)
	if err != nil {
		return fmt.Errorf("getting malfeasance proof: %w", err)
	}

	atxs, err := marriage.MarriageATXs(p.cdb, marriageID)
	if err != nil {
		return fmt.Errorf("getting equivocation set: %w", err)
	}

	return p.publish(ctx, nodeID, atxs, proof, ProofDomain(domain))
}

func (p *Publisher) publish(
	ctx context.Context,
	nodeID types.NodeID,
	marriageATXs []types.ATXID,
	proof []byte,
	domain ProofDomain,
) error {
	p.tortoise.OnMalfeasance(nodeID)

	// Only gossip the proof if we are synced (to not spam the network with proofs others probably already have).
	if !p.sync.ListenToATXGossip() {
		p.logger.Debug("not in sync, not broadcasting malfeasance proof",
			zap.String("smesher_id", nodeID.ShortString()),
		)
		return nil
	}

	malfeasanceProof := &MalfeasanceProof{
		Version:      0,
		MarriageATXs: marriageATXs,
		Domain:       domain,
		Proof:        proof,
	}
	if err := p.publisher.Publish(ctx, pubsub.MalfeasanceProof2, codec.MustEncode(malfeasanceProof)); err != nil {
		p.logger.Error("failed to broadcast malfeasance proof", zap.Error(err))
		return fmt.Errorf("broadcast atx malfeasance proof: %w", err)
	}

	return nil
}
