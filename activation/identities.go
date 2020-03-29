package activation

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/sha256-simd"
)

// IdentityStore stores couples of identities and used to retrieve bls identity by provided ed25519 identity
type IdentityStore struct {
	//todo: think about whether we need one db or several(#1922)
	ids database.Database
}

// NewIdentityStore creates a new identity store
func NewIdentityStore(db database.Database) *IdentityStore {
	return &IdentityStore{db}
}

func getKey(key string) [32]byte {
	return sha256.Sum256(util.Hex2Bytes(key))
}

// StoreNodeIdentity stores a NodeId type, which consists of 2 identities: BLS and ed25519
func (s *IdentityStore) StoreNodeIdentity(id types.NodeId) error {
	key := getKey(id.Key)
	err := s.ids.Put(key[:], id.VRFPublicKey)
	return err
}

// GetIdentity gets the identity by the provided ed25519 string id, it returns a NodeId struct or an error if id
// was not found
func (s *IdentityStore) GetIdentity(id string) (types.NodeId, error) {
	key := getKey(id)
	bytes, err := s.ids.Get(key[:])
	return types.NodeId{Key: id, VRFPublicKey: bytes}, err
}
