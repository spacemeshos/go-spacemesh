package types

// EpochStatus ...
type EpochStatus uint8

const (
	// EpochStatusUnspecified just in case if we cant calc actual epoch status.
	EpochStatusUnspecified EpochStatus = iota
	// EpochStatusPostNotFound is a status when node does not have generated post file.
	EpochStatusPostNotFound
	// EpochStatusInitializing when node start create post file.
	EpochStatusInitializing
	// EpochStatusGenerating when node is Proof-of-Space.
	EpochStatusGenerating
	// EpochStatusWaiting Waiting for PoET challenge.
	EpochStatusWaiting
	// EpochStatusSmeshing when node is smeshing.
	EpochStatusSmeshing
)

type EpochResult struct {
	Epoch  EpochID
	Status EpochStatus
}
