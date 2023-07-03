package types

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/google/go-cmp/cmp"
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	// ProposalIDSize in bytes.
	// FIXME(dshulyak) why do we cast to hash32 when returning bytes?
	// probably required for fetching by hash between peers.
	ProposalIDSize = Hash32Length
)

//go:generate scalegen -types Proposal,InnerProposal,ProposalID

// ProposalID is a 20-byte blake3 sum of the serialized ballot used to identify a Proposal.
type ProposalID Hash20

// EmptyProposalID is a canonical empty ProposalID.
var EmptyProposalID = ProposalID{}

// EncodeScale implements scale codec interface.
func (id *ProposalID) EncodeScale(e *scale.Encoder) (int, error) {
	return scale.EncodeByteArray(e, id[:])
}

// DecodeScale implements scale codec interface.
func (id *ProposalID) DecodeScale(d *scale.Decoder) (int, error) {
	return scale.DecodeByteArray(d, id[:])
}

// Proposal contains the smesher's signed content proposal for a given layer and vote on the mesh history.
// Proposal is ephemeral and will be discarded after the unified content block is created. the Ballot within
// the Proposal will remain in the mesh.
type Proposal struct {
	// the content proposal for a given layer and the votes on the mesh history
	InnerProposal
	// the smesher's signature on the InnerProposal
	Signature EdSignature

	// the following fields are kept private and from being serialized
	proposalID ProposalID
}

func (p Proposal) Equal(other Proposal) bool {
	return cmp.Equal(p.InnerProposal, other.InnerProposal) && p.Signature == other.Signature
}

// InnerProposal contains a smesher's content proposal for layer and its votes on the mesh history.
// this structure is serialized and signed to produce the signature in Proposal.
type InnerProposal struct {
	// smesher's votes on the mesh history
	Ballot
	// smesher's content proposal for a layer
	TxIDs []TransactionID `scale:"max=100000"`
	// aggregated hash up to the layer before this proposal.
	MeshHash Hash32
	// TODO add this when a state commitment mechanism is implemented.
	// state root up to the layer before this proposal.
	// note: this is needed in addition to mesh hash to detect bug in SVM
	// StateHash Hash32
}

// Initialize calculates and sets the Proposal's cached proposalID.
// this should be called once all the other fields of the Proposal are set.
func (p *Proposal) Initialize() error {
	if p.proposalID != EmptyProposalID {
		return fmt.Errorf("proposal already initialized")
	}
	if err := p.Ballot.Initialize(); err != nil {
		return err
	}

	h := hash.Sum(p.SignedBytes())
	p.proposalID = ProposalID(Hash32(h).ToHash20())
	return nil
}

// SignedBytes returns the serialization of the InnerProposal.
func (p *Proposal) SignedBytes() []byte {
	bytes, err := codec.Encode(&p.InnerProposal)
	if err != nil {
		log.With().Fatal("failed to serialize proposal", log.Err(err))
	}
	return bytes
}

// ID returns the ProposalID.
func (p *Proposal) ID() ProposalID {
	return p.proposalID
}

// SetID set the ProposalID.
func (p *Proposal) SetID(pid ProposalID) {
	p.proposalID = pid
}

// MarshalLogObject implements logging interface.
func (p *Proposal) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddString("proposal_id", p.ID().String())
	encoder.AddArray("transactions", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
		for _, id := range p.TxIDs {
			encoder.AppendString(id.String())
		}
		return nil
	}))
	encoder.AddString("mesh_hash", p.MeshHash.String())
	p.Ballot.MarshalLogObject(encoder)
	return nil
}

// String returns a short prefix of the hex representation of the ID.
func (id ProposalID) String() string {
	return id.AsHash32().ShortString()
}

// Bytes returns the ProposalID as a byte slice.
func (id ProposalID) Bytes() []byte {
	return id.AsHash32().Bytes()
}

// AsHash32 returns a Hash32 whose first 20 bytes are the bytes of this ProposalID, it is right-padded with zeros.
func (id ProposalID) AsHash32() Hash32 {
	return Hash20(id).ToHash32()
}

// Field returns a log field. Implements the LoggableField interface.
func (id ProposalID) Field() log.Field {
	return log.String("proposal_id", id.String())
}

// Compare returns true if other (the given ProposalID) is less than this ProposalID, by lexicographic comparison.
func (id ProposalID) Compare(other ProposalID) bool {
	return bytes.Compare(id.Bytes(), other.Bytes()) < 0
}

// ToProposalIDs returns a slice of ProposalID corresponding to the given proposals.
func ToProposalIDs(proposals []*Proposal) []ProposalID {
	ids := make([]ProposalID, 0, len(proposals))
	for _, p := range proposals {
		ids = append(ids, p.ID())
	}
	return ids
}

// SortProposals sorts a list of Proposal in their ID's lexicographic order, in-place.
func SortProposals(proposals []*Proposal) []*Proposal {
	sort.Slice(proposals, func(i, j int) bool { return proposals[i].ID().Compare(proposals[j].ID()) })
	return proposals
}

// SortProposalIDs sorts a list of ProposalID in lexicographic order, in-place.
func SortProposalIDs(ids []ProposalID) []ProposalID {
	sort.Slice(ids, func(i, j int) bool { return ids[i].Compare(ids[j]) })
	return ids
}

// ProposalIDsToHashes turns a list of ProposalID into their Hash32 representation.
func ProposalIDsToHashes(ids []ProposalID) []Hash32 {
	hashes := make([]Hash32, 0, len(ids))
	for _, id := range ids {
		hashes = append(hashes, id.AsHash32())
	}
	return hashes
}

type ProposalEligibility struct {
	Layer LayerID `json:"layer"`
	Count uint32  `json:"count"`
}
