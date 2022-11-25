package types

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	// BlockIDSize in bytes.
	// FIXME(dshulyak) why do we cast to hash32 when returning bytes?
	// probably required for fetching by hash between peers.
	BlockIDSize = Hash32Length
)

//go:generate scalegen -types Block,InnerBlock,RatNum,AnyReward,Certificate,CertifyMessage,CertifyContent

// BlockID is a 20-byte sha256 sum of the serialized block used to identify a Block.
type BlockID Hash20

// EmptyBlockID is a canonical empty BlockID.
var EmptyBlockID = BlockID{}

// NewExistingBlock creates a block from existing data.
func NewExistingBlock(id BlockID, inner InnerBlock) *Block {
	return &Block{blockID: id, InnerBlock: inner}
}

// EncodeScale implements scale codec interface.
func (id *BlockID) EncodeScale(e *scale.Encoder) (int, error) {
	return scale.EncodeByteArray(e, id[:])
}

// DecodeScale implements scale codec interface.
func (id *BlockID) DecodeScale(d *scale.Decoder) (int, error) {
	return scale.DecodeByteArray(d, id[:])
}

// Block contains the content of a layer on the mesh history.
type Block struct {
	InnerBlock
	// the following fields are kept private and from being serialized
	blockID BlockID
}

// InnerBlock contains the transactions and rewards of a block.
type InnerBlock struct {
	LayerIndex LayerID
	TickHeight uint64
	Rewards    []AnyReward
	TxIDs      []TransactionID
}

// RatNum represents a rational number with the numerator and denominator.
// note: RatNum aims to be a generic representation of a rational number and parse-able by
// different programming languages.
// for doing math around weight inside go-spacemesh codebase, use util.Weight.
type RatNum struct {
	Num, Denom uint64
}

// String implements fmt.Stringer interface for RatNum.
func (r *RatNum) String() string {
	return fmt.Sprintf("%d/%d", r.Num, r.Denom)
}

// AnyReward contains the reward information.
type AnyReward struct {
	Coinbase Address
	Weight   RatNum
}

// Initialize calculates and sets the Block's cached blockID.
func (b *Block) Initialize() {
	b.blockID = BlockID(CalcHash32(b.Bytes()).ToHash20())
}

// Bytes returns the serialization of the InnerBlock.
func (b *Block) Bytes() []byte {
	data, err := codec.Encode(&b.InnerBlock)
	if err != nil {
		log.Panic("failed to serialize block: %v", err)
	}
	return data
}

// ID returns the BlockID.
func (b *Block) ID() BlockID {
	return b.blockID
}

// ToVote creates Vote struct from block.
func (b *Block) ToVote() Vote {
	return Vote{ID: b.ID(), LayerID: b.LayerIndex, Height: b.TickHeight}
}

// MarshalLogObject implements logging encoder for Block.
func (b *Block) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddString("block_id", b.ID().String())
	encoder.AddUint32("layer_id", b.LayerIndex.Value)
	encoder.AddUint64("tick_height", b.TickHeight)
	encoder.AddInt("num_tx", len(b.TxIDs))
	encoder.AddInt("num_rewards", len(b.Rewards))
	return nil
}

// Bytes returns the BlockID as a byte slice.
func (id BlockID) Bytes() []byte {
	return id.AsHash32().Bytes()
}

// AsHash32 returns a Hash32 whose first 20 bytes are the bytes of this BlockID, it is right-padded with zeros.
func (id BlockID) AsHash32() Hash32 {
	return Hash20(id).ToHash32()
}

// Field returns a log field. Implements the LoggableField interface.
func (id BlockID) Field() log.Field {
	return log.String("block_id", id.String())
}

// String implements the Stringer interface.
func (id BlockID) String() string {
	return id.AsHash32().ShortString()
}

// Compare returns true if other (the given BlockID) is less than this BlockID, by lexicographic comparison.
func (id BlockID) Compare(other BlockID) bool {
	return bytes.Compare(id.Bytes(), other.Bytes()) < 0
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

// SortBlockIDs sorts a list of BlockID in lexicographic order, in-place.
func SortBlockIDs(ids blockIDs) []BlockID {
	sort.Slice(ids, func(i, j int) bool { return ids[i].Compare(ids[j]) })
	return ids
}

// BlockIdsField returns a list of loggable fields for a given list of BlockID.
func BlockIdsField(ids blockIDs) log.Field {
	return log.Array("block_ids", ids)
}

// ToBlockIDs returns a slice of BlockID corresponding to the given list of Block.
func ToBlockIDs(blocks []*Block) []BlockID {
	ids := make([]BlockID, 0, len(blocks))
	for _, b := range blocks {
		ids = append(ids, b.ID())
	}
	return ids
}

// BlockContextualValidity represents the contextual validity of a block.
type BlockContextualValidity struct {
	ID       BlockID
	Layer    LayerID
	Validity bool
}

// Certificate represents a certified block.
type Certificate struct {
	BlockID    BlockID
	Signatures []CertifyMessage
}

// CertifyMessage is generated by a node that's eligible to certify the hare output and is gossiped to the network.
type CertifyMessage struct {
	CertifyContent
	Signature []byte
}

// CertifyContent is actual content the node would sign to certify a hare output.
type CertifyContent struct {
	LayerID LayerID
	BlockID BlockID
	// EligibilityCnt is the number of eligibility of a NodeID on the given Layer
	// as a hare output certifier.
	EligibilityCnt uint16
	// Proof is the role proof for being a hare output certifier on the given Layer.
	Proof []byte
}

// Bytes returns the actual data being signed in a CertifyMessage.
func (cm *CertifyMessage) Bytes() []byte {
	data, err := codec.Encode(&cm.CertifyContent)
	if err != nil {
		log.Panic("failed to serialize certify msg: %v", err)
	}
	return data
}
