package malfeasance2

import "github.com/spacemeshos/go-spacemesh/common/types"

//go:generate scalegen

// ProofDomain encodes the type of malfeasance proof. It is used to decide which domain generated the proof.
type ProofDomain byte

const (
	InvalidActivation ProofDomain = 0x01
	InvalidBallot     ProofDomain = 0x02
	InvalidHareMsg    ProofDomain = 0x03
)

// ProofVersion encodes the version of the malfeasance proof.
// At the moment this will always be 0.
type ProofVersion byte

type MalfeasanceProof struct {
	// Version is the version identifier of the proof. This can be used to extend the malfeasance proof in the future.
	Version ProofVersion

	// RefATXs is a list of ATXs that proof that identity that the proof shows to be malicious exists. For non-married
	// identities this should be just the (valid) initial ATX of that identity and for married identities it should be
	// the marriage ATX (or multiple if an identity married multiple times).
	//
	// Upon receiving the proof, the node will fetch those ATXs and verify that they are valid to update the nodes view
	// on the status of the marriage set for the smesher the proof is about, since this malfeasance proof might show
	// that smesher A is malicious when smesher B was requested and the marriage ATXs then show that they are married.
	RefATXs []types.ATXID `scale:"max=64"`

	// Domain encodes the domain for which the proof was created
	Domain ProofDomain
	// Proof is the domain specific proof. Its type depends on the ProofDomain.
	Proof []byte `scale:"max=1048576"` // max size of proof is 1MiB
}
