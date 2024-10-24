package v2alpha1

import (
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -typed -package=v2alpha1 -destination=./mocks.go -source=./interface.go

type malfeasanceInfo interface {
	Info(data []byte) (map[string]string, error)
}

type identityState interface {
	// IdentityStates returns the current state of all registered IDs.
	IdentityStates() map[types.IdentityDescriptor]activation.IdentityState
}
