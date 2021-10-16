package hare

import "github.com/spacemeshos/go-spacemesh/common/types"

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interfaces.go

type layerPatrol interface {
	SetHareInCharge(types.LayerID)
}
