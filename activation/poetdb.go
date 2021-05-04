package activation

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/merkle-tree"
	"github.com/spacemeshos/poet/hash"
	"github.com/spacemeshos/poet/shared"
	"github.com/spacemeshos/poet/verifier"
	"github.com/spacemeshos/sha256-simd"
	"sync"
)

type poetProofKey [sha256.Size]byte

// PoetDb is a database for PoET proofs.
type PoetDb struct {
	store                     database.Database
	poetProofRefSubscriptions map[poetProofKey][]chan []byte
	log                       log.Log
	mu                        sync.Mutex
}

// NewPoetDb returns a new PoET DB.
func NewPoetDb(store database.Database, log log.Log) *PoetDb {
	return &PoetDb{store: store, poetProofRefSubscriptions: make(map[poetProofKey][]chan []byte), log: log}
}

// HasProof returns true if the database contains a proof with the given reference, or false otherwise.
func (db *PoetDb) HasProof(proofRef []byte) bool {
	_, err := db.GetProofMessage(proofRef)
	return err == nil
}

// ValidateAndStore validates and stores a new PoET proof.
func (db *PoetDb) ValidateAndStore(proofMessage *types.PoetProofMessage) error {
	if err := db.Validate(proofMessage.PoetProof, proofMessage.PoetServiceID,
		proofMessage.RoundID, proofMessage.Signature); err != nil {

		return err
	}

	err := db.storeProof(proofMessage)
	return err
}

// ValidateAndStoreMsg validates and stores a new PoET proof.
func (db *PoetDb) ValidateAndStoreMsg(data []byte) error {
	var proofMessage types.PoetProofMessage
	err := types.BytesToInterface(data, &proofMessage)
	if err != nil {
		return err
	}
	return db.ValidateAndStore(&proofMessage)
}

// Validate validates a new PoET proof.
func (db *PoetDb) Validate(proof types.PoetProof, poetID []byte, roundID string, signature []byte) error {

	root, err := calcRoot(proof.Members)
	if err != nil {
		return types.ProcessingError(fmt.Sprintf("failed to calculate membership root for poetID %x round %s: %v",
			poetID[:5], roundID, err))
	}
	if err := validatePoet(root, proof.MerkleProof, proof.LeafCount); err != nil {
		return fmt.Errorf("failed to validate poet proof for poetID %x round %s: %v",
			poetID[:5], roundID, err)
	}
	// TODO(noamnelke): validate signature (or extract public key and use for salting merkle hashes)

	return nil
}

func (db *PoetDb) storeProof(proofMessage *types.PoetProofMessage) error {
	ref, err := proofMessage.Ref()
	if err != nil {
		return fmt.Errorf("failed to get PoET proof message reference: %v", err)
	}

	messageBytes, err := types.InterfaceToBytes(proofMessage)
	if err != nil {
		return fmt.Errorf("could not marshal proof message: %v", err)
	}

	batch := db.store.NewBatch()
	if err := batch.Put(ref, messageBytes); err != nil {
		return fmt.Errorf("failed to store poet proof for poetId %x round %s: %v",
			proofMessage.PoetServiceID[:5], proofMessage.RoundID, err)
	}
	key := makeKey(proofMessage.PoetServiceID, proofMessage.RoundID)
	if err := batch.Put(key[:], ref); err != nil {
		return fmt.Errorf("failed to store poet proof index entry for poetId %x round %s: %v",
			proofMessage.PoetServiceID[:5], proofMessage.RoundID, err)
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to store poet proof and index for poetId %x round %s: %v",
			proofMessage.PoetServiceID[:5], proofMessage.RoundID, err)
	}
	db.log.With().Info("stored PoET proof",
		log.String("poet_proof_id", fmt.Sprintf("%x", ref)[:5]),
		log.String("round_id", proofMessage.RoundID),
		log.String("poet_service_id", fmt.Sprintf("%x", proofMessage.PoetServiceID)[:5]),
	)
	db.publishProofRef(key, ref)
	return nil
}

// SubscribeToProofRef returns a channel that PoET proof ref for the requested PoET ID and round ID will be sent. If the
// proof is already available it will be sent immediately, otherwise it will be sent when available.
func (db *PoetDb) SubscribeToProofRef(poetID []byte, roundID string) chan []byte {
	key := makeKey(poetID, roundID)
	ch := make(chan []byte)

	db.addSubscription(key, ch)

	if poetProofRef, err := db.getProofRef(key); err == nil {
		db.publishProofRef(key, poetProofRef)
	}

	return ch
}

func (db *PoetDb) addSubscription(key poetProofKey, ch chan []byte) {
	db.mu.Lock()
	db.poetProofRefSubscriptions[key] = append(db.poetProofRefSubscriptions[key], ch)
	db.mu.Unlock()
}

// UnsubscribeFromProofRef removes all subscriptions from a given poetID and roundID. This method should be used with
// caution since any subscribers still waiting will now hang forever. TODO: only cancel specific subscription.
func (db *PoetDb) UnsubscribeFromProofRef(poetID []byte, roundID string) {
	db.mu.Lock()
	delete(db.poetProofRefSubscriptions, makeKey(poetID, roundID))
	db.mu.Unlock()
}

func (db *PoetDb) getProofRef(key poetProofKey) ([]byte, error) {
	proofRef, err := db.store.Get(key[:])
	if err != nil {
		return nil, fmt.Errorf("could not fetch poet proof for key %x: %v", key[:5], err)
	}
	return proofRef, nil
}

func (db *PoetDb) publishProofRef(key poetProofKey, poetProofRef []byte) {
	db.mu.Lock()
	defer db.mu.Unlock()

	for _, ch := range db.poetProofRefSubscriptions[key] {
		go func(c chan []byte) {
			c <- poetProofRef
			close(c)
		}(ch)
	}
	delete(db.poetProofRefSubscriptions, key)
}

// GetProofMessage returns the originally received PoET proof message.
func (db *PoetDb) GetProofMessage(proofRef []byte) ([]byte, error) {
	return db.store.Get(proofRef)
}

// GetMembershipMap returns the map of memberships in the requested PoET proof.
func (db *PoetDb) GetMembershipMap(proofRef []byte) (map[types.Hash32]bool, error) {
	proofMessageBytes, err := db.GetProofMessage(proofRef)
	if err != nil {
		return nil, fmt.Errorf("could not fetch poet proof for ref %x: %v", proofRef[:3], err)
	}
	var proofMessage types.PoetProofMessage
	if err := types.BytesToInterface(proofMessageBytes, &proofMessage); err != nil {
		return nil, fmt.Errorf("failed to unmarshal poet proof for ref %x: %v", proofRef[:5], err)
	}
	return membershipSliceToMap(proofMessage.Members), nil
}

func makeKey(poetID []byte, roundID string) poetProofKey {
	sum := sha256.Sum256(append(poetID[:], []byte(roundID)...))
	return sum
}

func membershipSliceToMap(membership [][]byte) map[types.Hash32]bool {
	res := make(map[types.Hash32]bool)
	for _, member := range membership {
		res[types.BytesToHash(member)] = true
	}
	return res
}

func calcRoot(leaves [][]byte) ([]byte, error) {
	tree, err := merkle.NewTree()
	if err != nil {
		return nil, fmt.Errorf("failed to generate tree: %v", err)
	}
	for _, member := range leaves {
		err := tree.AddLeaf(member)
		if err != nil {
			return nil, fmt.Errorf("failed to add leaf: %v", err)
		}
	}
	return tree.Root(), nil
}

func validatePoet(membershipRoot []byte, merkleProof shared.MerkleProof, leafCount uint64) error {
	labelHashFunc := hash.GenLabelHashFunc(membershipRoot)
	merkleHashFunc := hash.GenMerkleHashFunc(membershipRoot)
	return verifier.Validate(merkleProof, labelHashFunc, merkleHashFunc, leafCount, shared.T)
}
