package malfeasance2

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -typed -package=malfeasance2 -destination=./mocks.go -source=./interface.go

type tortoise interface {
	OnMalfeasance(types.NodeID)
}

type MalfeasanceHandler interface {
	// Info returns a map of key-value pairs that serve as metadata for the proof
	Info(data []byte) (map[string]string, error)
}
