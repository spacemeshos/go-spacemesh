package activation

import (
	"context"
	"encoding/hex"
	"fmt"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/spacemeshos/merkle-tree"
	"github.com/spacemeshos/poet/hash"
	"github.com/spacemeshos/poet/shared"
	"github.com/spacemeshos/poet/verifier"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/poets"
)

// PoetDbOptions are options for PoetDb.
type PoetDbOptions struct {
	cacheSize int
}

type PoetDbOption func(*PoetDbOptions)

// WithCacheSize sets the cache size for PoetDb.
func WithCacheSize(size int) PoetDbOption {
	return func(opts *PoetDbOptions) {
		opts.cacheSize = size
	}
}

// PoetDb is a database for PoET proofs.
type PoetDb struct {
	sqlDB               sql.StateDatabase
	poetProofsDbRequest singleflight.Group
	poetProofsLru       *lru.Cache[types.PoetProofRef, *types.PoetProofMessage]
	logger              *zap.Logger
}

// NewPoetDb returns a new PoET handler.
func NewPoetDb(db sql.StateDatabase, log *zap.Logger, opts ...PoetDbOption) *PoetDb {
	options := PoetDbOptions{
		// in last epochs there are 45 proofs per epoch, with each of them nearly 140KB
		// 200 is set not to keep multiple epochs, but to account for unexpected growth
		// select round_id, count(*), max(length(poet)) from poets group by round_id;
		// 25|42|146200
		// 26|45|145936
		// 27|45|145738
		// 28|45|145903
		cacheSize: 200,
	}
	for _, opt := range opts {
		opt(&options)
	}
	poetProofsLru, err := lru.New[types.PoetProofRef, *types.PoetProofMessage](options.cacheSize)
	if err != nil {
		log.Panic("failed to create PoET proofs LRU cache", zap.Error(err))
	}
	return &PoetDb{
		sqlDB:         db,
		poetProofsLru: poetProofsLru,
		logger:        log,
	}
}

// HasProof returns true if the database contains a proof with the given reference, or false otherwise.
func (db *PoetDb) HasProof(proofRef types.PoetProofRef) bool {
	if db.poetProofsLru.Contains(proofRef) {
		return true
	}
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

	if err := db.Validate(
		proofMessage.Statement[:],
		proofMessage.PoetProof,
		proofMessage.PoetServiceID,
		proofMessage.RoundID,
		proofMessage.Signature,
	); err != nil {
		return err
	}

	return db.StoreProof(ctx, ref, proofMessage)
}

// ValidateAndStoreMsg validates and stores a new PoET proof.
func (db *PoetDb) ValidateAndStoreMsg(ctx context.Context, expHash types.Hash32, _ p2p.Peer, data []byte) error {
	var proofMessage types.PoetProofMessage
	if err := codec.Decode(data, &proofMessage); err != nil {
		return fmt.Errorf("parse message: %w", err)
	}
	ref, err := proofMessage.Ref()
	if err != nil {
		return err
	}
	if types.Hash32(ref) != expHash {
		return fmt.Errorf(
			"fetched wrong poet proof hash. want %s, got %s",
			expHash.ShortString(),
			types.Hash32(ref).ShortString(),
		)
	}

	return db.ValidateAndStore(ctx, &proofMessage)
}

// Validate validates a new PoET proof.
func (db *PoetDb) Validate(
	root []byte,
	proof types.PoetProof,
	poetID []byte,
	roundID string,
	signature types.EdSignature,
) error {
	shortID := poetID[:min(5, len(poetID))]
	if err := validatePoet(root, proof.MerkleProof, proof.LeafCount); err != nil {
		return fmt.Errorf("failed to validate poet proof for poetID %x round %s: %w", shortID, roundID, err)
	}
	// TODO(noamnelke): validate signature (or extract public key and use for salting merkle hashes)
	return nil
}

// StoreProof saves the poet proof in local db.
func (db *PoetDb) StoreProof(ctx context.Context, ref types.PoetProofRef, proofMessage *types.PoetProofMessage) error {
	db.poetProofsLru.Add(ref, proofMessage)
	messageBytes, err := codec.Encode(proofMessage)
	if err != nil {
		return fmt.Errorf("could not marshal proof message: %w", err)
	}

	if err := poets.Add(db.sqlDB, ref, messageBytes, proofMessage.PoetServiceID, proofMessage.RoundID); err != nil {
		return fmt.Errorf(
			"failed to store poet proof for poetId %x round %s: %w",
			proofMessage.PoetServiceID[:5], proofMessage.RoundID, err,
		)
	}

	db.logger.Debug("stored poet proof",
		log.ZContext(ctx),
		log.ZShortStringer("poet_proof_id", types.Hash32(ref)),
		zap.String("round_id", proofMessage.RoundID),
		zap.String("poet_service_id", hex.EncodeToString(proofMessage.PoetServiceID[:5])),
	)

	return nil
}

func (db *PoetDb) GetProofRef(poetID []byte, roundID string) (types.PoetProofRef, error) {
	proofRef, err := poets.GetRef(db.sqlDB, poetID, roundID)
	if err != nil {
		return types.PoetProofRef{}, fmt.Errorf(
			"could not fetch poet proof for poet ID %x in round %v: %w",
			poetID[:5],
			roundID,
			err,
		)
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

// Proof returns full proof.
func (db *PoetDb) Proof(proofRef types.PoetProofRef) (*types.PoetProof, *types.Hash32, error) {
	response, err, _ := db.poetProofsDbRequest.Do(string(proofRef[:]), func() (any, error) {
		cachedProof, ok := db.poetProofsLru.Get(proofRef)
		if ok && cachedProof != nil {
			return cachedProof, nil
		}
		proofMessageBytes, err := db.GetProofMessage(proofRef)
		if err != nil {
			return nil, fmt.Errorf("could not fetch poet proof for ref %x: %w", proofRef, err)
		}
		var proofMessage types.PoetProofMessage
		if err := codec.Decode(proofMessageBytes, &proofMessage); err != nil {
			return nil, fmt.Errorf("failed to unmarshal poet proof for ref %x: %w", proofRef, err)
		}
		db.poetProofsLru.Add(proofRef, &proofMessage)
		return &proofMessage, nil
	})
	if err != nil {
		return nil, nil, err
	}
	proof := response.(*types.PoetProofMessage)
	return &proof.PoetProof, &proof.Statement, nil
}

func (db *PoetDb) ProofForRound(poetID []byte, roundID string) (*types.PoetProof, error) {
	proofRef, err := db.GetProofRef(poetID, roundID)
	if err != nil {
		return nil, err
	}

	proof, _, err := db.Proof(proofRef)
	return proof, err
}

func calcRoot(leaves []types.Hash32) ([]byte, error) {
	tree, err := merkle.NewTreeBuilder().WithHashFunc(shared.HashMembershipTreeNode).Build()
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
