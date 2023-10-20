package types

// PostInfo contains information about the PoST as returned by the service.
type PostInfo struct {
	NodeID        NodeID
	CommitmentATX ATXID
	Nonce         *VRFPostIndex

	NumUnits      uint32
	LabelsPerUnit uint64
}
