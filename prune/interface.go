package prune

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -typed -package=prune -destination=./mocks.go -source=./interface.go

type layerClock interface {
	CurrentLayer() types.LayerID
}
