package activation

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
)

// IdentityStore stores couples of identities and used to retrieve bls identity by provided ed25519 identity
type IdentityStore struct {
	// todo: think about whether we need one db or several(#1922)
	ids database.Database
}

// NewIdentityStore creates a new identity store
func NewIdentityStore(db database.Database) *IdentityStore {
	return &IdentityStore{db}
}

func getKey(key string) []byte {
	return util.Hex2Bytes(key)
}

// StoreNodeIdentity stores a NodeID type, which consists of 2 identities: BLS and ed25519
func (s *IdentityStore) StoreNodeIdentity(id types.NodeID) error {
	if err := s.ids.Put(getKey(id.Key), id.VRFPublicKey); err != nil {
		return fmt.Errorf("put ID: %w", err)
	}

	return nil
}

// GetIdentity gets the identity by the provided ed25519 string id, it returns a NodeID struct or an error if id
// was not found
func (s *IdentityStore) GetIdentity(id string) (types.NodeID, error) {
	key := getKey(id)
	bytes, err := s.ids.Get(key)
	nodeID := types.NodeID{Key: id, VRFPublicKey: bytes}
	if err != nil {
		return nodeID, fmt.Errorf("get node ID from store: %w", err)
	}

	return nodeID, nil
}
