package activation

import "github.com/spacemeshos/go-spacemesh/common/types"

//go:generate scalegen

// PoetRequest describes an in-flight challenge submission for a poet proof.
type PoetRequest struct {
	// PoetRound is the round of the PoET proving service in which the PoET challenge was included in.
	PoetRound *types.PoetRound
	// PoetServiceID returns the public key of the PoET proving service.
	PoetServiceID types.PoetServiceID
}

// NIPostBuilderState is a builder state.
type NIPostBuilderState struct {
	Challenge types.Hash32

	NIPost *types.NIPost

	PoetRequests []PoetRequest `scale:"max=100"` // max number of poets a node connects to

	// PoetProofRef is the root of the proof received from the PoET service.
	PoetProofRef types.PoetProofRef
}
