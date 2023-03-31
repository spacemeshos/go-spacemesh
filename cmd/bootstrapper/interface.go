package main

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type LayerClock interface {
	AwaitLayer(types.LayerID) chan struct{}
	CurrentLayer() types.LayerID
}
