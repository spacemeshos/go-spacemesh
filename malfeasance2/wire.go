package malfeasance2

// ProofDomain encodes the type of malfeasance proof. It is used to decide which domain generated the proof.
type ProofDomain byte

const (
	InvalidActivation ProofDomain = 0x01
	InvalidBallot     ProofDomain = 0x02
	InvalidHareMsg    ProofDomain = 0x03
)
