package malfeasance2

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/malfeasance"
	"github.com/spacemeshos/go-spacemesh/sql/marriage"
)

type Publisher struct {
	logger    *zap.Logger
	db        sql.StateDatabase
	sync      syncer
	tortoise  tortoise
	publisher pubsub.Publisher
}

func NewPublisher(
	logger *zap.Logger,
	db sql.StateDatabase,
	sync syncer,
	tortoise tortoise,
	publisher pubsub.Publisher,
) *Publisher {
	return &Publisher{
		logger:    logger,
		db:        db,
		sync:      sync,
		tortoise:  tortoise,
		publisher: publisher,
	}
}

func (p *Publisher) PublishATXProof(ctx context.Context, nodeID types.NodeID, proof []byte) error {
	publish := false // whether to publish the proof
	var set []types.NodeID
	var refATXs []types.ATXID
	err := p.db.WithTxImmediate(ctx, func(tx sql.Transaction) error {
		marriageID, err := marriage.FindIDByNodeID(tx, nodeID)
		switch {
		case errors.Is(err, sql.ErrNotFound): // smesher is not married
			malicious, err := malfeasance.IsMalicious(tx, nodeID)
			if err != nil {
				return fmt.Errorf("check if smesher is malicious: %w", err)
			}
			if malicious {
				p.logger.Debug("smesher is already marked as malicious", zap.String("smesher_id", nodeID.ShortString()))
				return nil
			}
			if err := malfeasance.AddProof(tx, nodeID, nil, proof, int(InvalidActivation), time.Now()); err != nil {
				return fmt.Errorf("setting malfeasance proof: %w", err)
			}
			atxID, err := atxs.GetFirstIDByNodeID(tx, nodeID)
			if err != nil {
				return fmt.Errorf("getting atx id: %w", err)
			}
			publish = true
			set = []types.NodeID{nodeID}
			refATXs = []types.ATXID{atxID}
			return nil
		case err != nil:
			return fmt.Errorf("getting equivocation set: %w", err)
		default: // smesher is married
		}

		// Combine IDs from the present equivocation set for atx.SmesherID and IDs in atx.Marriages.
		set, err = marriage.NodeIDsByID(tx, marriageID)
		if err != nil {
			return fmt.Errorf("getting equivocation set: %w", err)
		}

		malicious, err := malfeasance.IsMalicious(tx, nodeID)
		if err != nil {
			return fmt.Errorf("check if smesher is malicious: %w", err)
		}
		if !malicious {
			err := malfeasance.AddProof(tx, nodeID, &marriageID, proof, int(InvalidActivation), time.Now())
			if err != nil {
				return fmt.Errorf("setting malfeasance proof: %w", err)
			}
			publish = true
		} else {
			p.logger.Debug("smesher is already marked as malicious", zap.String("smesher_id", nodeID.ShortString()))
		}

		mATXs := make(map[types.ATXID]struct{})
		for _, id := range set {
			info, err := marriage.FindByNodeID(tx, id)
			if err != nil {
				return fmt.Errorf("getting marriage info: %w", err)
			}
			mATXs[info.ATX] = struct{}{}
			if id == nodeID {
				// already handled
				continue
			}
			malicious, err := malfeasance.IsMalicious(tx, id)
			if err != nil {
				return fmt.Errorf("check if smesher is malicious: %w", err)
			}
			if malicious {
				p.logger.Debug("smesher is already marked as malicious", zap.String("smesher_id", id.ShortString()))
				continue
			}
			publish = true
			if err := malfeasance.SetMalicious(tx, id, marriageID, time.Now()); err != nil {
				return fmt.Errorf("setting malicious: %w", err)
			}
		}
		refATXs = maps.Keys(mATXs)
		return nil
	})
	if err != nil {
		return err
	}
	if !publish {
		// all smeshers were already marked as malicious - no gossip to void spamming the network
		return nil
	}
	for _, nodeID := range set {
		p.tortoise.OnMalfeasance(nodeID)
	}
	return p.publish(ctx, set, refATXs, proof, ProofDomain(InvalidActivation))
}

func (p *Publisher) Regossip(ctx context.Context, nodeID types.NodeID) error {
	tx, err := p.db.TxImmediate(ctx)
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	defer tx.Release()
	marriageID, err := marriage.FindIDByNodeID(tx, nodeID)
	switch {
	case errors.Is(err, sql.ErrNotFound): // smesher is not married
		proof, domain, err := malfeasance.NodeIDProof(tx, nodeID)
		if err != nil {
			return fmt.Errorf("getting malfeasance proof: %w", err)
		}
		atxID, err := atxs.GetFirstIDByNodeID(tx, nodeID)
		if err != nil {
			return fmt.Errorf("getting first atx of identity %s: %w", nodeID.ShortString(), err)
		}
		return p.publish(ctx, []types.NodeID{nodeID}, []types.ATXID{atxID}, proof, ProofDomain(domain))
	case err != nil:
		return fmt.Errorf("getting equivocation set: %w", err)
	default: // smesher is married
	}

	proof, domain, err := malfeasance.MarriageProof(tx, marriageID)
	if err != nil {
		return fmt.Errorf("getting malfeasance proof: %w", err)
	}

	nodeIDs, err := marriage.NodeIDsByID(tx, marriageID)
	if err != nil {
		return fmt.Errorf("getting equivocation set: %w", err)
	}

	atxs, err := marriage.MarriageATXs(tx, marriageID)
	if err != nil {
		return fmt.Errorf("getting equivocation info: %w", err)
	}

	return p.publish(ctx, nodeIDs, atxs, proof, ProofDomain(domain))
}

func (p *Publisher) publish(
	ctx context.Context,
	nodeID []types.NodeID,
	refATXs []types.ATXID,
	proof []byte,
	domain ProofDomain,
) error {
	// Only gossip the proof if we are synced (to not spam the network with proofs others probably already have).
	if !p.sync.ListenToATXGossip() {
		p.logger.Debug("not in sync, not broadcasting malfeasance proof",
			zap.Array("smesher_ids", zapcore.ArrayMarshalerFunc(func(enc zapcore.ArrayEncoder) error {
				for _, nodeID := range nodeID {
					enc.AppendString(nodeID.ShortString())
				}
				return nil
			})),
		)
		return nil
	}

	malfeasanceProof := &MalfeasanceProof{
		Version: 0,
		RefATXs: refATXs,
		Domain:  domain,
		Proof:   proof,
	}
	if err := p.publisher.Publish(ctx, pubsub.MalfeasanceProof2, codec.MustEncode(malfeasanceProof)); err != nil {
		p.logger.Error("failed to broadcast malfeasance proof", zap.Error(err))
		return fmt.Errorf("broadcast atx malfeasance proof: %w", err)
	}
	return nil
}

func (p *Publisher) ProofByID(ctx context.Context, nodeID types.NodeID) ([]byte, error) {
	tx, err := p.db.TxImmediate(ctx)
	if err != nil {
		return nil, fmt.Errorf("starting transaction: %w", err)
	}
	defer tx.Release()
	info, err := marriage.FindByNodeID(tx, nodeID)
	switch {
	case errors.Is(err, sql.ErrNotFound): // smesher is not married
		proof, domain, err := malfeasance.NodeIDProof(tx, nodeID)
		if err != nil {
			return nil, fmt.Errorf("getting malfeasance proof: %w", err)
		}
		atxID, err := atxs.GetFirstIDByNodeID(tx, nodeID)
		if err != nil {
			return nil, fmt.Errorf("getting first atx of identity %s: %w", nodeID.ShortString(), err)
		}
		malfeasanceProof := &MalfeasanceProof{
			Version: 0,
			RefATXs: []types.ATXID{atxID},
			Domain:  ProofDomain(domain),
			Proof:   proof,
		}
		return codec.MustEncode(malfeasanceProof), nil
	case err != nil:
		return nil, fmt.Errorf("getting equivocation set: %w", err)
	default: // smesher is married
	}

	set, err := marriage.NodeIDsByID(tx, info.ID)
	if err != nil {
		return nil, fmt.Errorf("getting equivocation set: %w", err)
	}

	refATXs := make(map[types.ATXID]struct{})
	for _, id := range set {
		info, err := marriage.FindByNodeID(tx, id)
		if err != nil {
			return nil, fmt.Errorf("getting marriage info: %w", err)
		}
		refATXs[info.ATX] = struct{}{}
	}

	proof, domain, err := malfeasance.MarriageProof(tx, info.ID)
	if err != nil {
		return nil, fmt.Errorf("getting malfeasance proof: %w", err)
	}
	malfeasanceProof := &MalfeasanceProof{
		Version: 0,
		RefATXs: maps.Keys(refATXs),
		Domain:  ProofDomain(domain),
		Proof:   proof,
	}
	return codec.MustEncode(malfeasanceProof), nil
}
