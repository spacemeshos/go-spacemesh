package types

//go:generate scalegen -types NIPostBuilderState,PoetRequest,PoetServiceID

type PoetServiceID struct {
	ServiceID []byte `scale:"max=32"` // public key of the PoET service
}

// PoetRequest describes an in-flight challenge submission for a poet proof.
type PoetRequest struct {
	// PoetRound is the round of the PoET proving service in which the PoET challenge was included in.
	PoetRound *PoetRound
	// PoetServiceID returns the public key of the PoET proving service.
	PoetServiceID PoetServiceID
}

// NIPostBuilderState is a builder state.
type NIPostBuilderState struct {
	Challenge Hash32

	NIPost *NIPost

	PoetRequests []PoetRequest `scale:"max=100"` // max number of poets a node connects to

	// PoetProofRef is the root of the proof received from the PoET service.
	PoetProofRef PoetProofRef
}
