package activation

import (
	"context"
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/system"
)

type MalfeasanceHandlerV2 struct {
	syncer     syncer
	clock      layerClock
	publisher  malfeasancePublisher
	cdb        *datastore.CachedDB
	tortoise   system.Tortoise
	edVerifier *signing.EdVerifier
}

func NewMalfeasanceHandlerV2(
	syncer syncer,
	layerClock layerClock,
	malfeasancePublisher malfeasancePublisher,
	cdb *datastore.CachedDB,
	tortoise system.Tortoise,
	edVerifier *signing.EdVerifier,
) *MalfeasanceHandlerV2 {
	return &MalfeasanceHandlerV2{
		syncer:     syncer,
		clock:      layerClock,
		publisher:  malfeasancePublisher, // TODO(mafa): implement malfeasancePublisher in `malfeasance` package
		cdb:        cdb,
		tortoise:   tortoise,
		edVerifier: edVerifier,
	}
}

// TODO(mafa): call this validate in the handler for publish/gossip.
func (mh *MalfeasanceHandlerV2) Validate(ctx context.Context, data []byte) ([]types.NodeID, error) {
	var decoded wire.ATXProof
	if err := codec.Decode(data, &decoded); err != nil {
		return nil, fmt.Errorf("decoding ATX malfeasance proof: %w", err)
	}

	var proof wire.Proof
	switch decoded.ProofType {
	case wire.DoublePublish:
		var p wire.ProofDoublePublish
		if err := codec.Decode(decoded.Proof, &p); err != nil {
			return nil, fmt.Errorf("decoding ATX double publish proof: %w", err)
		}
		proof = p
	case wire.DoubleMarry:
		var p wire.ProofDoubleMarry
		if err := codec.Decode(decoded.Proof, &p); err != nil {
			return nil, fmt.Errorf("decoding ATX double marry proof: %w", err)
		}
		proof = p
	default:
		return nil, fmt.Errorf("unknown ATX malfeasance proof type: %d", decoded.ProofType)
	}

	id, err := proof.Valid(mh.edVerifier)
	if err != nil {
		return nil, fmt.Errorf("validating ATX malfeasance proof: %w", err)
	}

	validIDs := make([]types.NodeID, 0, len(decoded.Certificates)+1)
	validIDs = append(validIDs, id) // id has already been proven to be malfeasant

	// check certificates provided with the proof
	// TODO(mafa): this only works if the main identity becomes malfeasant - try different approach with merkle proofs
	for _, cert := range decoded.Certificates {
		if id != cert.Target {
			continue
		}
		if !mh.edVerifier.Verify(signing.MARRIAGE, cert.Target, cert.ID.Bytes(), cert.Signature) {
			continue
		}
		validIDs = append(validIDs, cert.ID)
	}
	return validIDs, nil
}

func (mh *MalfeasanceHandlerV2) publishATXProof(ctx context.Context, nodeID types.NodeID, proof *wire.ATXProof) error {
	// Combine IDs from the present equivocation set for atx.SmesherID and IDs in atx.Marriages.
	set, err := identities.EquivocationSet(mh.cdb, nodeID)
	if err != nil {
		return fmt.Errorf("getting equivocation set: %w", err)
	}

	encoded := codec.MustEncode(proof)

	for _, id := range set {
		if err := identities.SetMalicious(mh.cdb, id, encoded, time.Now()); err != nil {
			return fmt.Errorf("setting malfeasance proof: %w", err)
		}

		// TODO(mafa): update malfeasance proof caching
		// mh.cdb.CacheMalfeasanceProof(id, proof)

		mh.tortoise.OnMalfeasance(id)
	}

	return mh.publisher.Publish(ctx, codec.MustEncode(proof))
}

func (mp *MalfeasanceHandlerV2) PublishDoublePublishProof(ctx context.Context, atx1, atx2 *wire.ActivationTxV2) error {
	proof, err := wire.NewDoublePublishProof(atx1, atx2)
	if err != nil {
		return fmt.Errorf("create double publish proof: %w", err)
	}

	if !mp.syncer.ListenToATXGossip() {
		// we are not gossiping proofs when we are not listening to ATX gossip
		return nil
	}

	atxProof := &wire.ATXProof{
		Layer:     mp.clock.CurrentLayer(),
		ProofType: wire.DoublePublish,
		Proof:     codec.MustEncode(proof),
	}
	return mp.publishATXProof(ctx, atx1.SmesherID, atxProof)
}
