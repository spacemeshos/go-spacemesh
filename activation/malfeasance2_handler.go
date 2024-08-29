package activation

import (
	"context"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/signing"
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
	malPublisher malfeasancePublisher,
	cdb *datastore.CachedDB,
	tortoise system.Tortoise,
	edVerifier *signing.EdVerifier,
) *MalfeasanceHandlerV2 {
	return &MalfeasanceHandlerV2{
		syncer:     syncer,
		clock:      layerClock,
		publisher:  malPublisher, // TODO(mafa): implement malfeasancePublisher in `malfeasance` package
		cdb:        cdb,
		tortoise:   tortoise,
		edVerifier: edVerifier,
	}
}

// TODO(mafa): call this validate in the handler for publish/gossip.
// TODO(mafa): extend this validate to return nil if `peer` == self.
func (mh *MalfeasanceHandlerV2) Validate(ctx context.Context, data []byte) ([]types.NodeID, error) {
	var decoded wire.ATXProof
	if err := codec.Decode(data, &decoded); err != nil {
		return nil, fmt.Errorf("decoding ATX malfeasance proof: %w", err)
	}

	var proof wire.Proof
	switch decoded.ProofType {
	case wire.DoubleMarry:
		p := &wire.ProofDoubleMarry{}
		if err := codec.Decode(decoded.Proof, p); err != nil {
			return nil, fmt.Errorf("decoding ATX double marry proof: %w", err)
		}
		proof = p
	case wire.InvalidPost:
		// TODO(mafa): implement this
	default:
		return nil, fmt.Errorf("unknown ATX malfeasance proof type: %d", decoded.ProofType)
	}

	id, err := proof.Valid(mh.edVerifier)
	if err != nil {
		return nil, fmt.Errorf("validating ATX malfeasance proof: %w", err)
	}

	// TODO(mafa): do this in the general handler
	// validIDs := make([]types.NodeID, 0, len(decoded.Certificates)+1)
	// validIDs = append(validIDs, id) // id has already been proven to be malfeasant

	// // check certificates provided with the proof
	// // TODO(mafa): only works if the main identity becomes malfeasant - try different approach with merkle proofs
	// for _, cert := range decoded.Certificates {
	// 	if id != cert.Target {
	// 		continue
	// 	}
	// 	if !mh.edVerifier.Verify(signing.MARRIAGE, cert.Target, cert.ID.Bytes(), cert.Signature) {
	// 		continue
	// 	}
	// 	validIDs = append(validIDs, cert.ID)
	// }
	// return validIDs, nil
	return []types.NodeID{id}, nil
}

// TODO(mafa): this roughly how the general publisher looks like
//
// func Publish(ctx context.Context, smesherID types.NodeID, data []byte) error {
// 	// Combine IDs from the present equivocation set for atx.SmesherID and IDs in atx.Marriages.
// 	set, err := identities.EquivocationSet(mh.cdb, nodeID)
// 	if err != nil {
// 		return fmt.Errorf("getting equivocation set: %w", err)
// 	}
// 	for _, id := range set {
// 		if err := identities.SetMalicious(mh.cdb, id, encoded, time.Now()); err != nil {
// 			return fmt.Errorf("adding malfeasance proof: %w", err)
// 		}

// 		mh.cdb.CacheMalfeasanceProof(id, proof)
// 		mh.tortoise.OnMalfeasance(id)
// 	}

// 	if !mh.syncer.ListenToATXGossip() {
//      // we are not gossiping proofs when we are not listening to ATX gossip
// 		return nil
// 	}

// 	gossip := mwire.MalfeasanceProofV2{
// 		Layer:     mh.clock.CurrentLayer(),
// 		ProofType: mwire.InvalidActivation,
// 		Proof:     data,
// 	}

// 	if err := mh.publisher.Publish(ctx, pubsub.MalfeasanceProof, codec.MustEncode(&gossip)); err != nil {
// 		mh.logger.Error("failed to broadcast malfeasance proof", zap.Error(err))
// 		return fmt.Errorf("broadcast atx malfeasance proof: %w", err)
// 	}
// 	return nil
// }
