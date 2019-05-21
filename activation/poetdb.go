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
	store database.DB
}

func NewPoetDb(store database.DB) *PoetDb {
	return &PoetDb{store: store}
}

func (db *PoetDb) AddMembershipProof(proof types.PoetMembershipProof) error {
	root, err := calcRoot(proof.Members)
	if err != nil {
		return fmt.Errorf("failed to calculate membership root: %v", err)
	}

	var members bytes.Buffer
	if _, err := xdr.Marshal(&members, proof.Members); err != nil {
		return fmt.Errorf("failed to marshal members: %v", err)
	}

	if err := db.store.Put(root, members.Bytes()); err != nil {
		return fmt.Errorf("failed to store membership proof: %v", err)
	}

	return nil
}

func (db *PoetDb) AddPoetProof(proof types.PoetProof) error {
	if _, err := db.store.Get(proof.MembershipRoot); err != nil {
		return fmt.Errorf("failed to fetch matching membership proof: %v", err)
	}
	if err := validatePoet(proof.MembershipRoot, proof.MerkleProof, proof.LeafCount); err != nil {
		return fmt.Errorf("failed to validate poet proof: %v", err)
	}

	var poetProof bytes.Buffer
	if _, err := xdr.Marshal(&poetProof, proof); err != nil {
		return fmt.Errorf("failed to marshal poet proof: %v", err)
	}

	if err := db.store.Put(proof.Root, poetProof.Bytes()); err != nil {
		return fmt.Errorf("failed to store poet proof: %v", err)
	}

	if err := db.store.Put(makeKey(proof.PoetId, proof.RoundId), proof.Root); err != nil {
		return fmt.Errorf("failed to store poet proof index entry: %v", err)
	}

	return nil
}

func (db *PoetDb) GetPoetProofRoot(poetId []byte, round *types.PoetRound) ([]byte, error) {
	poetRoot, err := db.store.Get(makeKey(poetId, round.Id))
	if err != nil {
		return nil, err
	}
	return poetRoot, nil
}

func (db *PoetDb) GetMembershipByPoetProofRoot(poetRoot []byte) (map[common.Hash]bool, error) {
	poetProofBytes, err := db.store.Get(poetRoot)
	if err != nil {
		return nil, err
	}
	var poetProof types.PoetProof
	if _, err := xdr.Unmarshal(bytes.NewReader(poetProofBytes), &poetProof); err != nil {
		return nil, err
	}
	membershipBytes, err := db.store.Get(poetProof.MembershipRoot)
	if err != nil {
		return nil, err
	}
	var membership [][]byte
	if _, err := xdr.Unmarshal(bytes.NewReader(membershipBytes), &membership); err != nil {
		return nil, err
	}
	return membershipSliceToMap(membership), nil
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
