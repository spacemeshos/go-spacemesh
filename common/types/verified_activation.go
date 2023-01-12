package types

import "github.com/spacemeshos/go-spacemesh/log"

type VerifiedActivationTx struct {
	*ActivationTx

	baseTickHeight uint64
	tickCount      uint64
}

// GetWeight of the ATX. The total weight of the epoch is expected to fit in a uint64 and is
// sum(atx.NumUnits * atx.TickCount for each ATX in a given epoch).
// Space Units sizes are chosen such that NumUnits for all ATXs in an epoch is expected to be < 10^6.
// PoETs should produce ~10k ticks at genesis, but are expected due to technological advances
// to produce more over time. A uint64 should be large enough to hold the total weight of an epoch,
// for at least the first few years.
func (vatx *VerifiedActivationTx) GetWeight() uint64 {
	return getWeight(uint64(vatx.NumUnits), vatx.tickCount)
}

// BaseTickHeight is a tick height of the positional atx.
func (vatx *VerifiedActivationTx) BaseTickHeight() uint64 {
	return vatx.baseTickHeight
}

// TickCount returns tick count from poet proof attached to the atx.
func (vatx *VerifiedActivationTx) TickCount() uint64 {
	return vatx.tickCount
}

// TickHeight returns a sum of base tick height and tick count.
func (vatx *VerifiedActivationTx) TickHeight() uint64 {
	return vatx.baseTickHeight + vatx.tickCount
}

// MarshalLogObject implements logging interface.
func (vatx *VerifiedActivationTx) MarshalLogObject(encoder log.ObjectEncoder) error {
	if vatx.InitialPost != nil {
		encoder.AddString("nipost", vatx.InitialPost.String())
	}
	encoder.AddString("challenge", vatx.NIPostChallenge.Hash().String())
	encoder.AddString("id", vatx.id.String())
	encoder.AddString("sender_id", vatx.nodeID.String())
	encoder.AddString("prev_atx_id", vatx.PrevATXID.String())
	encoder.AddString("pos_atx_id", vatx.PositioningATX.String())
	encoder.AddString("coinbase", vatx.Coinbase.String())
	encoder.AddUint32("pub_layer_id", vatx.PubLayerID.Value)
	encoder.AddUint32("epoch", uint32(vatx.PublishEpoch()))
	encoder.AddUint64("num_units", uint64(vatx.NumUnits))
	encoder.AddUint64("sequence_number", vatx.Sequence)
	encoder.AddUint64("base_tick_height", vatx.baseTickHeight)
	encoder.AddUint64("tick_count", vatx.tickCount)
	encoder.AddUint64("weight", vatx.GetWeight())
	return nil
}
