package activation

import (
	"context"
	"fmt"

	"github.com/spacemeshos/merkle-tree"
	"github.com/spacemeshos/poet/hash"
	"github.com/spacemeshos/poet/shared"
	"github.com/spacemeshos/poet/verifier"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/poets"
)

var ErrObjectExists = sql.ErrObjectExists

// PoetDb is a database for PoET proofs.
type PoetDb struct {
	sqlDB *sql.Database
	log   log.Log
}

// NewPoetDb returns a new PoET handler.
func NewPoetDb(db *sql.Database, log log.Log) *PoetDb {
	return &PoetDb{sqlDB: db, log: log}
}

// HasProof returns true if the database contains a proof with the given reference, or false otherwise.
func (db *PoetDb) HasProof(proofRef types.PoetProofRef) bool {
	has, err := poets.Has(db.sqlDB, proofRef)
	return err == nil && has
}

// ValidateAndStore validates and stores a new PoET proof.
func (db *PoetDb) ValidateAndStore(ctx context.Context, proofMessage *types.PoetProofMessage) error {
	ref, err := proofMessage.Ref()
	if err != nil {
		return err
	}

	if db.HasProof(ref) {
		return nil
	}

	if err := db.Validate(proofMessage.Statement[:], proofMessage.PoetProof, proofMessage.PoetServiceID, proofMessage.RoundID, proofMessage.Signature); err != nil {
		return err
	}

	return db.StoreProof(ctx, ref, proofMessage)
}

// ValidateAndStoreMsg validates and stores a new PoET proof.
func (db *PoetDb) ValidateAndStoreMsg(ctx context.Context, _ p2p.Peer, data []byte) error {
	var proofMessage types.PoetProofMessage
	if err := codec.Decode(data, &proofMessage); err != nil {
		return fmt.Errorf("parse message: %w", err)
	}
	return db.ValidateAndStore(ctx, &proofMessage)
}

// Validate validates a new PoET proof.
func (db *PoetDb) Validate(root []byte, proof types.PoetProof, poetID []byte, roundID string, signature types.EdSignature) error {
	const shortIDlth = 5 // check the length to prevent a panic in the errors
	if len(poetID) < shortIDlth {
		return types.ProcessingError{Err: fmt.Sprintf("invalid poet id %x", poetID)}
	}

	if err := validatePoet(root, proof.MerkleProof, proof.LeafCount); err != nil {
		return fmt.Errorf("failed to validate poet proof for poetID %x round %s: %w", poetID[:shortIDlth], roundID, err)
	}
	// TODO(noamnelke): validate signature (or extract public key and use for salting merkle hashes)

	return nil
}

// StoreProof saves the poet proof in local db.
func (db *PoetDb) StoreProof(ctx context.Context, ref types.PoetProofRef, proofMessage *types.PoetProofMessage) error {
	messageBytes, err := codec.Encode(proofMessage)
	if err != nil {
		return fmt.Errorf("could not marshal proof message: %w", err)
	}

	if err := poets.Add(db.sqlDB, ref, messageBytes, proofMessage.PoetServiceID, proofMessage.RoundID); err != nil {
		return fmt.Errorf("failed to store poet proof for poetId %x round %s: %w",
			proofMessage.PoetServiceID[:5], proofMessage.RoundID, err)
	}

	db.log.WithContext(ctx).With().Info("stored poet proof",
		log.String("poet_proof_id", fmt.Sprintf("%x", ref[:5])),
		log.String("round_id", proofMessage.RoundID),
		log.String("poet_service_id", fmt.Sprintf("%x", proofMessage.PoetServiceID[:5])),
	)

	return nil
}

func (db *PoetDb) GetProofRef(poetID []byte, roundID string) (types.PoetProofRef, error) {
	proofRef, err := poets.GetRef(db.sqlDB, poetID, roundID)
	if err != nil {
		return types.PoetProofRef{}, fmt.Errorf("could not fetch poet proof for poet ID %x in round %v: %w", poetID[:5], roundID, err)
	}
	return proofRef, nil
}

// GetProofMessage returns the originally received PoET proof message.
func (db *PoetDb) GetProofMessage(proofRef types.PoetProofRef) ([]byte, error) {
	proof, err := poets.Get(db.sqlDB, proofRef)
	if err != nil {
		return proof, fmt.Errorf("get proof from store: %w", err)
	}

	return proof, nil
}

// GetProof returns full proof.
func (db *PoetDb) GetProof(proofRef types.PoetProofRef) (*types.PoetProof, error) {
	proofMessageBytes, err := db.GetProofMessage(proofRef)
	if err != nil {
		return nil, fmt.Errorf("could not fetch poet proof for ref %x: %w", proofRef, err)
	}
	var proofMessage types.PoetProofMessage
	if err := codec.Decode(proofMessageBytes, &proofMessage); err != nil {
		return nil, fmt.Errorf("failed to unmarshal poet proof for ref %x: %w", proofRef, err)
	}
	return &proofMessage.PoetProof, nil
}

func calcRoot(leaves []types.Member) ([]byte, error) {
	tree, err := merkle.NewTree()
	if err != nil {
		return nil, fmt.Errorf("failed to generate tree: %w", err)
	}
	for _, member := range leaves {
		err := tree.AddLeaf(member[:])
		if err != nil {
			return nil, fmt.Errorf("failed to add leaf: %w", err)
		}
	}
	return tree.Root(), nil
}

func validatePoet(membershipRoot []byte, merkleProof shared.MerkleProof, leafCount uint64) error {
	labelHashFunc := hash.GenLabelHashFunc(membershipRoot)
	merkleHashFunc := hash.GenMerkleHashFunc(membershipRoot)
	if err := verifier.Validate(merkleProof, labelHashFunc, merkleHashFunc, leafCount, shared.T); err != nil {
		return fmt.Errorf("validate PoET: %w", err)
	}

	return nil
}
