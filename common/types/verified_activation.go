package types

import "github.com/spacemeshos/go-spacemesh/log"

type VerifiedActivationTx struct {
	*ActivationTx

	baseTickHeight uint64
	tickCount      uint64
}

// GetWeight of the atx.
func (vatx *VerifiedActivationTx) GetWeight() uint64 {
	return uint64(vatx.NumUnits) * (vatx.tickCount)
}

// BaseTickHeight is a tick height of the positional atx.
func (vatx *VerifiedActivationTx) BaseTickHeight() uint64 {
	return vatx.baseTickHeight
}

// TickCount returns tick count from from poet proof attached to the atx.
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
	h, err := vatx.NIPostChallenge.Hash()
	if err == nil && h != nil {
		encoder.AddString("challenge", h.String())
	}
	encoder.AddString("id", vatx.id.String())
	encoder.AddString("sender_id", vatx.nodeID.String())
	encoder.AddString("prev_atx_id", vatx.PrevATXID.String())
	encoder.AddString("pos_atx_id", vatx.PositioningATX.String())
	encoder.AddString("coinbase", vatx.Coinbase.String())
	encoder.AddUint32("pub_layer_id", vatx.PubLayerID.Value)
	encoder.AddUint32("epoch", uint32(vatx.PubLayerID.GetEpoch()))
	encoder.AddUint64("num_units", uint64(vatx.NumUnits))
	encoder.AddUint64("sequence_number", vatx.Sequence)
	encoder.AddUint64("base_tick_height", vatx.baseTickHeight)
	encoder.AddUint64("tick_count", vatx.tickCount)
	encoder.AddUint64("weight", vatx.GetWeight())
	return nil
}
