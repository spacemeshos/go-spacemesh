package activation

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/sha256-simd"
)

type IdentityStore struct {
	//todo: think about whether we need one db or several
	ids database.DB
}

func NewIdentityStore(db database.DB) *IdentityStore {
	return &IdentityStore{db}
}

func getKey(key string) [32]byte {
	return sha256.Sum256(util.Hex2Bytes(key))
}

func (s *IdentityStore) StoreNodeIdentity(id types.NodeId) error {
	key := getKey(id.Key)
	err := s.ids.Put(key[:], id.VRFPublicKey)
	return err
}

func (s *IdentityStore) GetIdentity(id string) (types.NodeId, error) {
	key := getKey(id)
	bytes, err := s.ids.Get(key[:])
	return types.NodeId{id, bytes}, err
}
