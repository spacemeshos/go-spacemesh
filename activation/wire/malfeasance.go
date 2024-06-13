package wire

// ATXVersion is an identifier to allow for different versions of ATXs to be part of a proof.
type ATXVersion uint8

// TODO(mafa): this is for proofs that involve ATXs from different versions. This is not yet implemented.
const (
	ATXVersion1 ATXVersion = 1
	ATXVersion2 ATXVersion = 2
)

type ProofType byte

const (
	DoublePublish ProofType = iota + 1
	DoubleMarry
)

type ATXProof struct {
	ProofType ProofType

	// TODO(mafa): add field with certificates for IDs that share the marriage set with the ID proven to be malfeasant

	Proof []byte `scale:"max=1048576"` // max size of proof is 1MiB
}
