package bootstrap

import "github.com/spacemeshos/go-spacemesh/common/types"

//go:generate mockgen -typed -package=bootstrap -destination=./mocks.go -source=./interface.go

type layerClock interface {
	CurrentLayer() types.LayerID
}
