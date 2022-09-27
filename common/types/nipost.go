package types

//go:generate scalegen -types NIPostBuilderState

// NIPostBuilderState is a builder state.
type NIPostBuilderState struct {
	Challenge Hash32

	NIPost *NIPost

	// PoetRound is the round of the PoET proving service in which the PoET challenge was included in.
	PoetRound *PoetRound

	// PoetServiceID returns the public key of the PoET proving service.
	PoetServiceID []byte

	// PoetProofRef is the root of the proof received from the PoET service.
	PoetProofRef []byte
}
