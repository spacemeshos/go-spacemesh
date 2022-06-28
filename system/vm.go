package system

import "github.com/spacemeshos/go-spacemesh/common/types"

//go:generate mockgen -package=mocks -destination=./mocks/vm.go -source=./vm.go

// ValidationRequest parses transaction and verifies it.
type ValidationRequest interface {
	Parse() (*types.TxHeader, error)
	Verify() bool
}
