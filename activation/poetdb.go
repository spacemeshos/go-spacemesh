package activation

import (
	"encoding/binary"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spacemeshos/merkle-tree"
	"github.com/spacemeshos/poet/hash"
	"github.com/spacemeshos/poet/shared"
	"github.com/spacemeshos/poet/verifier"
	"github.com/spacemeshos/sha256-simd"
	"sync"
)

type poetProofKey [sha256.Size]byte

type PoetDb struct {
	store                     database.Database
	poetProofRefSubscriptions map[poetProofKey][]chan []byte
	log                       log.Log
	mu                        sync.Mutex
}

func NewPoetDb(store database.Database, log log.Log) *PoetDb {
	return &PoetDb{store: store, poetProofRefSubscriptions: make(map[poetProofKey][]chan []byte), log: log}
}

func (db *PoetDb) HasProof(proofRef []byte) bool {
	_, err := db.GetProofMessage(proofRef)
	return err == nil
}

func (db *PoetDb) ValidateAndStore(proofMessage *types.PoetProofMessage) error {
	if err := db.Validate(proofMessage.PoetProof, proofMessage.PoetId,
		proofMessage.RoundId, proofMessage.Signature); err != nil {

		return err
	}

	err := db.storeProof(proofMessage)
	return err
}

func (db *PoetDb) Validate(proof types.PoetProof, poetId [types.PoetIdLength]byte, roundId uint64,
	signature []byte) error {

	root, err := calcRoot(proof.Members)
	if err != nil {
		return types.ProcessingError(fmt.Sprintf("failed to calculate membership root for poetId %x round %d: %v",
			poetId[:5], roundId, err))
	}
	if err := validatePoet(root, proof.MerkleProof, proof.LeafCount); err != nil {
		return fmt.Errorf("failed to validate poet proof for poetId %x round %d: %v",
			poetId[:5], roundId, err)
	}
	// TODO(noamnelke): validate signature (or extract public key and use for salting merkle hashes)

	return nil
}

func (db *PoetDb) storeProof(proofMessage *types.PoetProofMessage) error {
	ref, err := proofMessage.Ref()
	if err != nil {
		return fmt.Errorf("failed to get PoET proof message refference: %v", err)
	}

	messageBytes, err := types.InterfaceToBytes(proofMessage)
	if err != nil {
		return fmt.Errorf("could not marshal proof message: %v", err)
	}

	batch := db.store.NewBatch()
	if err := batch.Put(ref, messageBytes); err != nil {
		return fmt.Errorf("failed to store poet proof for poetId %x round %d: %v",
			proofMessage.PoetId[:5], proofMessage.RoundId, err)
	}
	key := makeKey(proofMessage.PoetId, proofMessage.RoundId)
	if err := batch.Put(key[:], ref); err != nil {
		return fmt.Errorf("failed to store poet proof index entry for poetId %x round %d: %v",
			proofMessage.PoetId[:5], proofMessage.RoundId, err)
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to store poet proof and index for poetId %x round %d: %v",
			proofMessage.PoetId[:5], proofMessage.RoundId, err)
	}
	db.log.Debug("stored proof (id: %x) for round %d PoET id %x", ref[:5], proofMessage.RoundId, proofMessage.PoetId[:5])
	db.publishProofRef(key, ref)
	return nil
}

func (db *PoetDb) SubscribeToProofRef(poetId [types.PoetIdLength]byte, roundId uint64) chan []byte {
	key := makeKey(poetId, roundId)
	ch := make(chan []byte)

	db.addSubscription(key, ch)

	poetProofRef, err := db.getProofRef(key)
	if err == nil {
		db.publishProofRef(key, poetProofRef)
	}

	return ch
}

func (db *PoetDb) addSubscription(key poetProofKey, ch chan []byte) {
	db.mu.Lock()
	defer db.mu.Unlock()

	db.poetProofRefSubscriptions[key] = append(db.poetProofRefSubscriptions[key], ch)
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

func (db *PoetDb) GetProofMessage(proofRef []byte) ([]byte, error) {
	return db.store.Get(proofRef)
}

func (db *PoetDb) GetMembershipMap(proofRef []byte) (map[common.Hash]bool, error) {
	proofMessageBytes, err := db.GetProofMessage(proofRef)
	if err != nil {
		return nil, fmt.Errorf("could not fetch poet proof for ref %x: %v", proofRef[:5], err)
	}
	var proofMessage types.PoetProofMessage
	if err := types.BytesToInterface(proofMessageBytes, &proofMessage); err != nil {
		return nil, fmt.Errorf("failed to unmarshal poet proof for ref %x: %v", proofRef[:5], err)
	}
	return membershipSliceToMap(proofMessage.Members), nil
}

func makeKey(poetId [types.PoetIdLength]byte, roundId uint64) poetProofKey {
	roundIdBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(roundIdBytes, roundId)
	sum := sha256.Sum256(append(poetId[:], roundIdBytes...))
	return sum
}

func membershipSliceToMap(membership [][]byte) map[common.Hash]bool {
	res := make(map[common.Hash]bool)
	for _, member := range membership {
		res[common.BytesToHash(member)] = true
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
