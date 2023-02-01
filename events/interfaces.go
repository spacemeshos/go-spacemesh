package events

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type LayerClock interface {
	AwaitLayer(layerID types.LayerID) chan struct{}
	CurrentLayer() types.LayerID
}
