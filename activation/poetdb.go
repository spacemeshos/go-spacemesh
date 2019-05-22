package activation

import (
	"bytes"
	"encoding/binary"
	"fmt"
	xdr "github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spacemeshos/merkle-tree"
	"github.com/spacemeshos/poet/hash"
	"github.com/spacemeshos/poet/shared"
	"github.com/spacemeshos/poet/verifier"
	"github.com/spacemeshos/sha256-simd"
)

type PoetDb struct {
	store database.Database
}

func NewPoetDb(store database.Database) *PoetDb {
	return &PoetDb{store: store}
}

func (db *PoetDb) ValidateAndStorePoetProof(proof types.PoetProof) error {
	root, err := calcRoot(proof.Members)
	if err != nil {
		return fmt.Errorf("failed to calculate membership root: %v", err)
	}
	if err := validatePoet(root, proof.MerkleProof, proof.LeafCount); err != nil {
		return fmt.Errorf("failed to validate poet proof: %v", err)
	}

	var poetProof bytes.Buffer
	if _, err := xdr.Marshal(&poetProof, proof); err != nil {
		return fmt.Errorf("failed to marshal poet proof: %v", err)
	}

	ref := sha256.Sum256(poetProof.Bytes())

	batch := db.store.NewBatch()
	if err := batch.Put(ref[:], poetProof.Bytes()); err != nil {
		return fmt.Errorf("failed to store poet proof: %v", err)
	}
	if err := batch.Put(makeKey(proof.PoetId, proof.RoundId), ref[:]); err != nil {
		return fmt.Errorf("failed to store poet proof index entry: %v", err)
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to store poet proof and index: %v", err)
	}

	return nil
}

func (db *PoetDb) GetPoetProofRef(poetId []byte, round *types.PoetRound) ([]byte, error) {
	poetRef, err := db.store.Get(makeKey(poetId, round.Id))
	if err != nil {
		return nil, err
	}
	return poetRef, nil
}

func (db *PoetDb) GetMembershipByPoetProofRef(poetRef []byte) (map[common.Hash]bool, error) {
	poetProofBytes, err := db.store.Get(poetRef)
	if err != nil {
		return nil, err
	}
	var poetProof types.PoetProof
	if _, err := xdr.Unmarshal(bytes.NewReader(poetProofBytes), &poetProof); err != nil {
		return nil, err
	}
	return membershipSliceToMap(poetProof.Members), nil
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
