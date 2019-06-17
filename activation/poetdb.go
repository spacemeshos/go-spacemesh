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
)

type poetProofKey [sha256.Size]byte

type PoetDb struct {
	store                     database.Database
	poetProofRefSubscriptions map[poetProofKey][]chan []byte
	log                       log.Log
}

func NewPoetDb(store database.Database, log log.Log) *PoetDb {
	return &PoetDb{store: store, poetProofRefSubscriptions: make(map[poetProofKey][]chan []byte), log: log}
}

func (db *PoetDb) ValidateAndStorePoetProof(proof types.PoetProof, poetId [types.PoetIdLength]byte, roundId uint64,
	signature []byte) (processingIssue bool, err error) {

	root, err := calcRoot(proof.Members)
	if err != nil {
		return true, fmt.Errorf("failed to calculate membership root for poetId %x round %d: %v",
			poetId, roundId, err)
	}
	if err := validatePoet(root, proof.MerkleProof, proof.LeafCount); err != nil {
		return false, fmt.Errorf("failed to validate poet proof for poetId %x round %d: %v",
			poetId, roundId, err)
	}

	poetProof, err := types.InterfaceToBytes(&proof)
	if err != nil {
		return true, fmt.Errorf("failed to marshal poet proof for poetId %x round %d: %v",
			poetId, roundId, err)
	}

	// TODO(noamnelke): validate signature (or extract public key and use for salting merkle hashes)

	ref := sha256.Sum256(poetProof)

	batch := db.store.NewBatch()
	if err := batch.Put(ref[:], poetProof); err != nil {
		return true, fmt.Errorf("failed to store poet proof for poetId %x round %d: %v",
			poetId, roundId, err)
	}
	key := makeKey(poetId, roundId)
	if err := batch.Put(key[:], ref[:]); err != nil {
		return true, fmt.Errorf("failed to store poet proof index entry for poetId %x round %d: %v",
			poetId, roundId, err)
	}
	if err := batch.Write(); err != nil {
		return true, fmt.Errorf("failed to store poet proof and index for poetId %x round %d: %v",
			poetId, roundId, err)
	}
	db.log.Info("stored proof for round %d PoET id %x", roundId, poetId)
	db.publishPoetProofRef(key, ref[:])
	return false, nil
}

func (db *PoetDb) SubscribeToPoetProofRef(poetId [types.PoetIdLength]byte, roundId uint64) chan []byte {
	ch := make(chan []byte)
	key := makeKey(poetId, roundId)
	db.poetProofRefSubscriptions[key] = append(db.poetProofRefSubscriptions[key], ch)

	go func() {
		poetProofRef, err := db.getPoetProofRef(key)
		if err != nil {
			return
		}
		db.publishPoetProofRef(key, poetProofRef)
	}()

	return ch
}

func (db *PoetDb) getPoetProofRef(key poetProofKey) ([]byte, error) {
	poetRef, err := db.store.Get(key[:])
	if err != nil {
		return nil, fmt.Errorf("could not fetch poet proof for key %x: %v", key, err)
	}
	return poetRef, nil
}

func (db *PoetDb) publishPoetProofRef(key poetProofKey, poetProofRef []byte) {
	for _, ch := range db.poetProofRefSubscriptions[key] {
		go func() {
			ch <- poetProofRef
			close(ch)
		}()
	}
	delete(db.poetProofRefSubscriptions, key)
}

func (db *PoetDb) GetMembershipByPoetProofRef(poetRef []byte) (map[common.Hash]bool, error) {
	poetProofBytes, err := db.store.Get(poetRef)
	if err != nil {
		return nil, fmt.Errorf("could not fetch poet proof for ref %x: %v", poetRef, err)
	}
	var poetProof types.PoetProof
	if err := types.BytesToInterface(poetProofBytes, &poetProof); err != nil {
		return nil, fmt.Errorf("failed to unmarshal poet proof for ref %x: %v", poetRef, err)
	}
	return membershipSliceToMap(poetProof.Members), nil
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
