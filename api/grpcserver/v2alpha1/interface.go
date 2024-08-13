package v2alpha1

import "github.com/spacemeshos/go-spacemesh/common/types"

type identityState interface {
	// IdentityStates returns the current state of all registered IDs.
	IdentityStates() map[types.IdentityDescriptor]types.IdentityState
}
