package fetch

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

//go:generate scalegen

// LayerData is the data response for a given layer ID.
type LayerData struct {
	Ballots []types.BallotID
	Blocks  []types.BlockID
}

// LayerOpinion is the response for opinion for a given layer.
type LayerOpinion struct {
	EpochWeight    uint64
	AggregatedHash types.Hash32
	Verified       types.LayerID
	Valid, Invalid []types.BlockID
	Cert           *types.Certificate

	peer p2p.Peer
}

// SetPeer ...
func (lo *LayerOpinion) SetPeer(p p2p.Peer) {
	lo.peer = p
}

// Peer ...
func (lo *LayerOpinion) Peer() p2p.Peer {
	return lo.peer
}

// MarshalLogObject implements logging encoder for LayerOpinion.
func (lo *LayerOpinion) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddString("peer", lo.peer.String())
	encoder.AddUint64("epoch_weight", lo.EpochWeight)
	encoder.AddUint32("verified", lo.Verified.Uint32())
	if lo.Cert != nil {
		encoder.AddString("cert_block_id", lo.Cert.BlockID.String())
	}
	encoder.AddArray("valid", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
		for _, bid := range lo.Valid {
			encoder.AppendString(bid.String())
		}
		return nil
	}))
	encoder.AddArray("invalid", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
		for _, bid := range lo.Invalid {
			encoder.AppendString(bid.String())
		}
		return nil
	}))
	return nil
}
