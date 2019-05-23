package activation

import (
	"bytes"
	"encoding/binary"
	"fmt"
	xdr "github.com/nullstyle/go-xdr/xdr3"
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

type PoetDb struct {
	store database.Database
	log   log.Log
}

func NewPoetDb(store database.Database, log log.Log) *PoetDb {
	return &PoetDb{store: store, log: log}
}

func (db *PoetDb) ValidateAndStorePoetProof(proof types.PoetProof, poetId []byte, roundId uint64, signature []byte) error {
	root, err := calcRoot(proof.Members)
	if err != nil {
		return db.logError(fmt.Errorf("failed to calculate membership root for poetId %x round %d: %v",
			poetId, roundId, err))
	}
	if err := validatePoet(root, proof.MerkleProof, proof.LeafCount); err != nil {
		return db.logWarning(fmt.Errorf("failed to validate poet proof for poetId %x round %d: %v",
			poetId, roundId, err))
	}

	var poetProof bytes.Buffer
	if _, err := xdr.Marshal(&poetProof, proof); err != nil {
		return db.logError(fmt.Errorf("failed to marshal poet proof for poetId %x round %d: %v",
			poetId, roundId, err))
	}

	// TODO(noamnelke): validate signature (or extract public key and use for salting merkle hashes)

	ref := sha256.Sum256(poetProof.Bytes())

	batch := db.store.NewBatch()
	if err := batch.Put(ref[:], poetProof.Bytes()); err != nil {
		return db.logError(fmt.Errorf("failed to store poet proof for poetId %x round %d: %v",
			poetId, roundId, err))
	}
	if err := batch.Put(makeKey(poetId, roundId), ref[:]); err != nil {
		return db.logError(fmt.Errorf("failed to store poet proof index entry for poetId %x round %d: %v",
			poetId, roundId, err))
	}
	if err := batch.Write(); err != nil {
		return db.logError(fmt.Errorf("failed to store poet proof and index for poetId %x round %d: %v",
			poetId, roundId, err))
	}

	return nil
}

func (db *PoetDb) GetPoetProofRef(poetId []byte, roundId uint64) ([]byte, error) {
	poetRef, err := db.store.Get(makeKey(poetId, roundId))
	if err != nil {
		return nil, db.logInfo(fmt.Errorf("could not fetch poet proof for poetId %x round %d: %v",
			poetId, roundId, err))
	}
	return poetRef, nil
}

func (db *PoetDb) GetMembershipByPoetProofRef(poetRef []byte) (map[common.Hash]bool, error) {
	poetProofBytes, err := db.store.Get(poetRef)
	if err != nil {
		return nil, db.logWarning(fmt.Errorf("could not fetch poet proof for ref %x: %v", poetRef, err))
	}
	var poetProof types.PoetProof
	if _, err := xdr.Unmarshal(bytes.NewReader(poetProofBytes), &poetProof); err != nil {
		return nil, db.logError(fmt.Errorf("failed to unmarshal poet proof for ref %x: %v", poetRef, err))
	}
	return membershipSliceToMap(poetProof.Members), nil
}

func (db *PoetDb) logInfo(err error) error {
	db.log.Info(err.Error())
	return err
}

func (db *PoetDb) logWarning(err error) error {
	db.log.Warning(err.Error())
	return err
}

func (db *PoetDb) logError(err error) error {
	db.log.Error(err.Error())
	return err
}

func makeKey(poetId []byte, roundId uint64) []byte {
	roundIdBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(roundIdBytes, roundId)
	sum := sha256.Sum256(append(poetId, roundIdBytes...))
	return sum[:]
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
