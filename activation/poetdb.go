package activation

import (
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/types"
)

type PoetDb struct {

}

func (db *PoetDb) AddMembershipProof(proof types.PoetMembershipProof) error {
	// TODO(noamnelke)
	panic("implement me!")
}

func (db *PoetDb) AddPoetProof(proof types.PoetProof) error {
	// TODO(noamnelke)
	panic("implement me!")
}

func (db *PoetDb) GetPoetProofRoot(poetId []byte, round *types.PoetRound) ([]byte, error) {
	// TODO(noamnelke)
	panic("implement me!")
}

func (db *PoetDb) GetMembershipByPoetProofRoot(poetRoot []byte) (map[common.Hash]bool, error) {
	// TODO(noamnelke)
	panic("implement me!")
}

func NewPoetDb() *PoetDb {
	return &PoetDb{}
}
