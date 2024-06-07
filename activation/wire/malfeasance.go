package wire

// ATXVersion is an identifier to allow for different versions of ATXs to be part of a proof.
type ATXVersion uint8

const (
	ATXVersion1 ATXVersion = 1
	ATXVersion2 ATXVersion = 2
)
