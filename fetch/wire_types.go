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
	Hint datastore.Hint `scale:"max=256"`
	Hash types.Hash32
}

// ResponseMessage is sent to the node as a response.
type ResponseMessage struct {
	Hash types.Hash32
	Data []byte `scale:"max=1024"` // TODO(mafa): check if this is the right max size
}

// RequestBatch is a batch of requests and a hash of all requests as ID.
type RequestBatch struct {
	ID       types.Hash32
	Requests []RequestMessage `scale:"max=32"`
}

// ResponseBatch is the response struct send for a RequestBatch. the ResponseBatch ID must be the same
// as stated in RequestBatch even if not all Data is present.
type ResponseBatch struct {
	ID        types.Hash32
	Responses []ResponseMessage `scale:"max=32"`
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
	Layers []types.LayerID `scale:"max=32"`
	Hashes []types.Hash32  `scale:"max=32"`
}

type MaliciousIDs struct {
	NodeIDs []types.NodeID `scale:"max=32"`
}

type EpochData struct {
	AtxIDs []types.ATXID `scale:"max=32"`
}

// LayerData is the data response for a given layer ID.
type LayerData struct {
	Ballots []types.BallotID `scale:"max=32"`
	Blocks  []types.BlockID  `scale:"max=32"`
}

// LayerOpinion is the response for opinion for a given layer.
type LayerOpinion struct {
	PrevAggHash types.Hash32
	Cert        *types.Certificate

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
	encoder.AddString("prev_hash", lo.PrevAggHash.String())
	if lo.Cert != nil {
		encoder.AddString("cert_block_id", lo.Cert.BlockID.String())
	}
	return nil
}
