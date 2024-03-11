package types

import "fmt"

// PostInfo contains information about the PoST as returned by the service.
type PostInfo struct {
	NodeID        NodeID
	CommitmentATX ATXID
	Nonce         *VRFPostIndex

	NumUnits      uint32
	LabelsPerUnit uint64
}

type PostState int

const (
	// PostStateIdle is the state of a PoST service that is not currently proving.
	PostStateIdle PostState = iota
	// PostStateProving is the state of a PoST service that is currently proving.
	PostStateProving
)

func (s PostState) String() string {
	switch s {
	case PostStateIdle:
		return "idle"
	case PostStateProving:
		return "proving"
	default:
		panic(fmt.Sprintf("unknown post state %d", s))
	}
}

type IdentityDescriptor interface {
	Name() string
	NodeID() NodeID
}
