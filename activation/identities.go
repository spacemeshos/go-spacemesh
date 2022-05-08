package activation

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
)

// IdentityStore stores couples of identities and used to retrieve bls identity by provided ed25519 identity.
type IdentityStore struct {
	// todo: think about whether we need one db or several(#1922)
	ids database.Database
}

// NewIdentityStore creates a new identity store.
func NewIdentityStore(db database.Database) *IdentityStore {
	return &IdentityStore{db}
}

// GetIdentity gets the identity by the provided ed25519 string id, it returns a NodeID struct or an error if id
// was not found.
func (s *IdentityStore) GetIdentity(id string) types.NodeID {
	return types.NodeID{Key: id}
}
