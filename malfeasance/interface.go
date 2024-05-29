package malfeasance

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -typed -package=malfeasance -destination=./mocks.go -source=./interface.go

type tortoise interface {
	OnMalfeasance(types.NodeID)
}
