package types

import (
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/log"
)

// UCBlock contains the content of a layer on the mesh history.
type UCBlock struct {
	InnerBlock
	// the following fields are kept private and from being serialized
	blockID BlockID
}

// InnerBlock contains the transactions and rewards of a block.
type InnerBlock struct {
	// the layer ID in which this ballot is eligible for. this will be validated via EligibilityProof
	LayerIndex LayerID
	// the list of rewards
	Rewards []AnyReward
	// the content for a given layer.
	TxIDs []TransactionID
}

// AnyReward contains the rewards inforamtion.
type AnyReward struct {
	Address     Address
	SmesherID   NodeID
	Amount      uint64
	LayerReward uint64
}

// Initialize calculates and sets the Block's cached blockID.
func (b *UCBlock) Initialize() {
	b.blockID = BlockID(CalcHash32(b.Bytes()).ToHash20())
}

// Bytes returns the serialization of the InnerProposal.
func (b *UCBlock) Bytes() []byte {
	bytes, err := codec.Encode(b.InnerBlock)
	if err != nil {
		log.Panic("failed to serialize block: %v", err)
	}
	return bytes
}

// ID returns the BlockID.
func (b *UCBlock) ID() BlockID {
	return b.blockID
}

// MarshalLogObject implements logging encoder for Block.
func (b *UCBlock) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddString("id", b.ID().String())
	encoder.AddUint32("layer", b.LayerIndex.Value)
	encoder.AddInt("num_tx", len(b.TxIDs))
	encoder.AddInt("num_rewards", len(b.Rewards))
	return nil
}

// DBBlock is a Block structure stored in DB to skip ID hashing.
type DBBlock struct {
	ID         BlockID
	InnerBlock InnerBlock
}

// ToBlock creates a Block from data that is stored locally.
func (dbb *DBBlock) ToBlock() *UCBlock {
	return &UCBlock{
		InnerBlock: dbb.InnerBlock,
		blockID:    dbb.ID,
	}
}
