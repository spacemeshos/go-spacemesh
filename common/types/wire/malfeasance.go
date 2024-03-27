package wire

//go:generate scalegen

type InvalidPostIndexProofV1 struct {
	Atx ActivationTxV1

	// Which index in POST is invalid
	InvalidIdx uint32
}
