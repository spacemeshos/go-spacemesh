package fetch

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

//go:generate scalegen

// RequestMessage is sent to the peer for hash query.
type RequestMessage struct {
	Hint datastore.Hint
	Hash types.Hash32
}

// ResponseMessage is sent to the node as a response.
type ResponseMessage struct {
	Hash types.Hash32
	Data []byte
}

// RequestBatch is a batch of requests and a hash of all requests as ID.
type RequestBatch struct {
	ID       types.Hash32
	Requests []RequestMessage
}

// ResponseBatch is the response struct send for a RequestBatch. the ResponseBatch ID must be the same
// as stated in RequestBatch even if not all Data is present.
type ResponseBatch struct {
	ID        types.Hash32
	Responses []ResponseMessage
}

type MeshHashRequest struct {
	From, To     types.LayerID
	Delta, Steps uint32
}

func (r *MeshHashRequest) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddUint32("from", r.From.Uint32())
	encoder.AddUint32("to", r.To.Uint32())
	encoder.AddUint32("delta", r.Delta)
	encoder.AddUint32("steps", r.Steps)
	return nil
}

type MeshHashes struct {
	Layers []types.LayerID
	Hashes []types.Hash32
}

type EpochData struct {
	Beacon types.Beacon
	Weight uint64
	AtxIDs []types.ATXID
}

// LayerData is the data response for a given layer ID.
type LayerData struct {
	Ballots []types.BallotID
	Blocks  []types.BlockID
}

// LayerOpinion is the response for opinion for a given layer.
type LayerOpinion struct {
	EpochWeight    uint64
	PrevAggHash    types.Hash32
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
	encoder.AddString("prev_hash", lo.PrevAggHash.String())
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
