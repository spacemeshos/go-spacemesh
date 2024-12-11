package malfeasance

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
)

//go:generate mockgen -typed -package=malfeasance -destination=./mocks.go -source=./interface.go

type tortoise interface {
	OnMalfeasance(types.NodeID)
}

type syncer interface {
	ListenToATXGossip() bool
}

type MalfeasanceHandler interface {
	// Validate the proof and return the node ID of the malicious node if the proof is valid
	Validate(ctx context.Context, data wire.ProofData) (types.NodeID, error)

	// Info returns a map of key-value pairs that serve as metadata for the proof
	Info(data wire.ProofData) (map[string]string, error)

	// ReportLabel returns the label for the prometheus counter of the given proof type
	ReportLabel() string
}
