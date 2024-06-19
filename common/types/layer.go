// Package types defines the types used by go-spacemesh consensus algorithms and structs
package types

import (
	"fmt"
	"strconv"
	"sync/atomic"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/log"
)

var (
	layersPerEpoch uint32
	// effectiveGenesis marks when actual proposals would start being created in the network. It takes into account
	// the first genesis epoch and the following epoch in which ATXs are published.
	effectiveGenesis uint32

	// EmptyLayerHash is the layer hash for an empty layer.
	EmptyLayerHash = EmptyHash32
)

// SetLayersPerEpoch sets global parameter of layers per epoch, all conversions from layer to epoch use this param.
func SetLayersPerEpoch(layers uint32) {
	atomic.StoreUint32(&layersPerEpoch, layers)
	SetEffectiveGenesis(layers*2 - 1)
}

func SetEffectiveGenesis(layer uint32) {
	atomic.StoreUint32(&effectiveGenesis, layer)
}

// GetLayersPerEpoch returns number of layers per epoch.
func GetLayersPerEpoch() uint32 {
	return atomic.LoadUint32(&layersPerEpoch)
}

// FirstEffectiveGenesis returns the first effective genesis layer.
func FirstEffectiveGenesis() LayerID {
	return LayerID(GetLayersPerEpoch()*2 - 1)
}

// GetEffectiveGenesis returns the last layer of genesis.
// this value can change after a checkpoint recovery.
func GetEffectiveGenesis() LayerID {
	return LayerID(atomic.LoadUint32(&effectiveGenesis))
}

// LayerID is representing a layer number. Zero value is safe to use, and means 0.
// Internally it is a simple wrapper over uint32 and should be considered immutable
// the same way as any integer.
type LayerID uint32

// EncodeScale implements scale codec interface.
func (l LayerID) EncodeScale(e *scale.Encoder) (int, error) {
	n, err := scale.EncodeCompact32(e, uint32(l))
	if err != nil {
		return 0, err
	}
	return n, nil
}

// DecodeScale implements scale codec interface.
func (l *LayerID) DecodeScale(d *scale.Decoder) (int, error) {
	value, n, err := scale.DecodeCompact32(d)
	if err != nil {
		return 0, err
	}
	*l = LayerID(value)
	return n, nil
}

// GetEpoch returns the epoch number of this LayerID.
func (l LayerID) GetEpoch() EpochID {
	return EpochID(l.Uint32() / GetLayersPerEpoch())
}

// Add layers to the layer. Panics on wraparound.
func (l LayerID) Add(layers uint32) LayerID {
	nl := l + LayerID(layers)
	if nl < l {
		panic("layer_id wraparound")
	}
	return nl
}

// Sub layers from the layer. Panics on wraparound.
func (l LayerID) Sub(layers uint32) LayerID {
	if layers > l.Uint32() {
		panic("layer_id wraparound")
	}
	nl := l - LayerID(layers)
	return nl
}

// OrdinalInEpoch returns layer ordinal in epoch.
func (l LayerID) OrdinalInEpoch() uint32 {
	return l.Uint32() % GetLayersPerEpoch()
}

// FirstInEpoch returns whether this LayerID is first in epoch.
func (l LayerID) FirstInEpoch() bool {
	return l.OrdinalInEpoch() == 0
}

// Mul layer by the layers. Panics on wraparound.
func (l LayerID) Mul(layers uint32) LayerID {
	if l == 0 {
		return l
	}
	nl := l * LayerID(layers)
	if nl/l != LayerID(layers) {
		panic("layer_id wraparound")
	}
	return nl
}

// Uint32 returns the LayerID as a uint32.
func (l LayerID) Uint32() uint32 {
	return uint32(l)
}

// Before returns true if this layer is lower than the other.
func (l LayerID) Before(other LayerID) bool {
	return l < other
}

// After returns true if this layer is higher than the other.
func (l LayerID) After(other LayerID) bool {
	return l > other
}

// Difference returns the difference between current and other layer.
func (l LayerID) Difference(other LayerID) uint32 {
	if other > l {
		panic(fmt.Sprintf("other (%d) must be before or equal to this layer (%d)", other, l))
	}
	return (l - other).Uint32()
}

// Field returns a log field. Implements the LoggableField interface.
func (l LayerID) Field() log.Field { return log.Uint32("layer_id", l.Uint32()) }

// String returns string representation of the layer id numeric value.
func (l LayerID) String() string {
	return strconv.FormatUint(uint64(l), 10)
}

// Layer contains a list of proposals and their corresponding LayerID.
type Layer struct {
	index   LayerID
	ballots []*Ballot
	blocks  []*Block
}

// Field returns a log field. Implements the LoggableField interface.
func (l *Layer) Field() log.Field {
	return log.String(
		"layer",
		fmt.Sprintf("layer_id %d num_ballot %d num_blocks %d", l.index, len(l.ballots), len(l.blocks)),
	)
}

// Index returns the layer's ID.
func (l *Layer) Index() LayerID {
	return l.index
}

// Blocks returns the list of Block in this layer.
func (l *Layer) Blocks() []*Block {
	return l.blocks
}

// BlocksIDs returns the list of IDs for blocks in this layer.
func (l *Layer) BlocksIDs() []BlockID {
	return ToBlockIDs(l.blocks)
}

// Ballots returns the list of ballots in this layer.
func (l *Layer) Ballots() []*Ballot {
	return l.ballots
}

// BallotIDs returns the list of IDs for ballots in this layer.
func (l *Layer) BallotIDs() []BallotID {
	return ToBallotIDs(l.ballots)
}

// AddBallot adds a ballot to this layer. Panics if the ballot's index doesn't match the layer.
func (l *Layer) AddBallot(b *Ballot) {
	if b.Layer != l.index {
		log.Panic("add ballot with wrong layer number act %v exp %v", b.Layer, l.index)
	}
	l.ballots = append(l.ballots, b)
}

// AddBlock adds a block to this layer. Panics if the block's index doesn't match the layer.
func (l *Layer) AddBlock(b *Block) {
	if b.LayerIndex != l.index {
		log.Panic("add block with wrong layer number act %v exp %v", b.LayerIndex, l.index)
	}
	l.blocks = append(l.blocks, b)
}

// SetBallots sets the list of ballots for the layer without validation.
func (l *Layer) SetBallots(ballots []*Ballot) {
	l.ballots = ballots
}

// SetBlocks sets the list of blocks for the layer without validation.
func (l *Layer) SetBlocks(blocks []*Block) {
	l.blocks = blocks
}

// NewExistingLayer returns a new layer with the given list of blocks without validation.
func NewExistingLayer(idx LayerID, ballots []*Ballot, blocks []*Block) *Layer {
	return &Layer{
		index:   idx,
		ballots: ballots,
		blocks:  blocks,
	}
}

// NewLayer returns a layer with no proposals.
func NewLayer(layerIndex LayerID) *Layer {
	return &Layer{
		index:   layerIndex,
		ballots: make([]*Ballot, 0, 10),
		blocks:  make([]*Block, 0, 3),
	}
}
