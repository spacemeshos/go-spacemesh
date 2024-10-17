package malfeasance2

import "github.com/spacemeshos/go-spacemesh/common/types"

//go:generate scalegen

// ProofDomain encodes the type of malfeasance proof. It is used to decide which domain generated the proof.
type ProofDomain byte

const (
	InvalidActivation ProofDomain = iota
	InvalidBallot
	InvalidHareMsg
)

// ProofVersion encodes the version of the malfeasance proof.
// At the moment this will always be 0.
type ProofVersion byte

type MalfeasanceProof struct {
	// Version is the version identifier of the proof. This can be used to extend the malfeasance proof in the future.
	Version ProofVersion

	// Certificates is a slice of marriage certificates showing which identities belong to the same marriage set as
	// the one proven to be malfeasant. Up to 1024 can be put into a single proof, since by repeatedly marrying other
	// identities there can be much more than 256 in a malfeasant marriage set. Beyond that a second proof could be
	// provided to show that additional identities are part of the same malfeasant marriage set.
	Certificates []ProofCertificate `scale:"max=1024"`

	// Domain encodes the domain for which the proof was created
	Domain ProofDomain
	// Proof is the domain specific proof. Its type depends on the ProofDomain.
	Proof []byte `scale:"max=1048576"` // max size of proof is 1MiB
}

type ProofCertificate struct {
	// TargetID is the identity that was married to by the smesher.
	TargetID types.NodeID
	// SmesherID is the identity that signed the certificate.
	SmesherID types.NodeID
	// Signature is the signature of the certificate.
	Signature types.EdSignature
}
