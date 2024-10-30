package system

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/core"
)

//go:generate mockgen -typed -package=mocks -destination=./mocks/vm.go -source=./vm.go

// ValidationRequest parses transaction and verifies it.
type ValidationRequest interface {
	Parse(*core.StagedCache) (*types.TxHeader, error)
	Verify() bool
	Cache() *core.StagedCache
}
