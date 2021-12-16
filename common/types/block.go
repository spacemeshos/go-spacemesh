package types

import (
	"sort"

	"github.com/spacemeshos/go-spacemesh/log"
)

type (
	// BlockID is an alias for ProposalID until unified content block is implemented.
	BlockID ProposalID
	// Block is an alias for Proposal until unified content block is implemented.
	Block Proposal
)

// Bytes returns the BlockID as a byte slice.
func (id BlockID) Bytes() []byte {
	return ProposalID(id).Bytes()
}

// AsHash32 returns a Hash32 whose first 20 bytes are the bytes of this ProposalID, it is right-padded with zeros.
func (id BlockID) AsHash32() Hash32 {
	return ProposalID(id).AsHash32()
}

// Field returns a log field. Implements the LoggableField interface.
func (id BlockID) Field() log.Field {
	return ProposalID(id).Field()
}

// String implements the Stringer interface.
func (id BlockID) String() string {
	return ProposalID(id).String()
}

// Compare returns true if other (the given ProposalID) is less than this ProposalID, by lexicographic comparison.
func (id BlockID) Compare(other BlockID) bool {
	return ProposalID(id).Compare(ProposalID(other))
}

// BlockIDsToHashes turns a list of BlockID into their Hash32 representation.
func BlockIDsToHashes(ids []BlockID) []Hash32 {
	hashes := make([]Hash32, 0, len(ids))
	for _, id := range ids {
		hashes = append(hashes, id.AsHash32())
	}
	return hashes
}

type blockIDs []BlockID

func (ids blockIDs) MarshalLogArray(encoder log.ArrayEncoder) error {
	for i := range ids {
		encoder.AppendString(ids[i].String())
	}
	return nil
}

// SortBlockIDs sorts a list of ProposalID in lexicographic order, in-place.
func SortBlockIDs(ids blockIDs) []BlockID {
	sort.Slice(ids, func(i, j int) bool { return ids[i].Compare(ids[j]) })
	return ids
}

// BlockIdsField returns a list of loggable fields for a given list of ProposalID.
func BlockIdsField(ids blockIDs) log.Field {
	return log.Array("block_ids", ids)
}

// ID returns the ID of the block.
func (b *Block) ID() BlockID {
	return BlockID((*Proposal)(b).ID())
}

// BlockIDs returns a slice of BlockID corresponding to the given proposals.
func BlockIDs(blocks []*Block) []BlockID {
	ids := make([]BlockID, 0, len(blocks))
	for _, b := range blocks {
		ids = append(ids, b.ID())
	}
	return ids
}

// ToProposals turns a list of Block into a list of Proposal.
func ToProposals(blocks []*Block) []*Proposal {
	proposals := make([]*Proposal, 0, len(blocks))
	for _, b := range blocks {
		proposals = append(proposals, (*Proposal)(b))
	}
	return proposals
}

// SortBlocks sort blocks by their IDs.
func SortBlocks(blks []*Block) []*Block {
	sort.Slice(blks, func(i, j int) bool { return blks[i].ID().Compare(blks[j].ID()) })
	return blks
}
