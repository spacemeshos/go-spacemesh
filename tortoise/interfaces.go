package tortoise

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interfaces.go

type blockValidityUpdater interface {
	UpdateBlockValidity(types.BlockID, types.LayerID, bool) error
}
