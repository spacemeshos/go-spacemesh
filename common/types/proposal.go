package types

// ProposalID is a 20-byte sha256 sum of the serialized ballot used to identify a Proposal.
type ProposalID Hash20

// EmptyProposalID is a canonical empty ProposalID.
var EmptyProposalID = ProposalID{}

const (
	// ProposalIDSize in bytes.
	// FIXME(dshulyak) why do we cast to hash32 when returning bytes?
	ProposalIDSize = Hash32Length
)

// Proposal contains the smesher's signed content proposal for a given layer and vote on the mesh history.
// Proposal is ephemeral and will be discarded after the unified content block is created. the Ballot within
// the Proposal will remain in the mesh.
type Proposal struct {
	// the content proposal for a given layer and the votes on the mesh history
	InnerProposal
	// smesher's signature on InnerProposal
	Signature []byte

	// the following fields are kept private and from being serialized
	proposalID ProposalID
}

// InnerProposal contains a smesher's content proposal for layer and its votes on the mesh history.
// this structure is serialized and signed to produce the signature in Proposal.
type InnerProposal struct {
	// smesher's votes on the mesh history
	Ballot
	// smesher's content proposal for a layer
	TxIDs []TransactionID
}

// ID returns the ProposalID.
func (p *Proposal) ID() ProposalID {
	return p.proposalID
}

// Bytes returns the ProposalID as a byte slice.
func (id ProposalID) Bytes() []byte {
	return id.AsHash32().Bytes()
}

// AsHash32 returns a Hash32 whose first 20 bytes are the bytes of this ProposalID, it is right-padded with zeros.
func (id ProposalID) AsHash32() Hash32 {
	return Hash20(id).ToHash32()
}
