package wire

// ATXVersion is an identifier to allow for different versions of ATXs to be part of a proof.
type ATXVersion uint8

// TODO(mafa): this is for proofs that involve ATXs from different versions. This is not yet implemented.
const (
	ATXVersion1 ATXVersion = 1
	ATXVersion2 ATXVersion = 2
)
