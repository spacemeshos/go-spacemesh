package fetch

import (
	"fmt"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

//go:generate scalegen

const MaxHashesInReq = 100

var (
	maxEpochDataAtxIDs = scale.MustGetMaxElements[EpochData]("AtxIDs")
	maxMaliciousIDs    = scale.MustGetMaxElements[MaliciousIDs]("NodeIDs")
	maxMeshHashes      = scale.MustGetMaxElements[MeshHashes]("Hashes")
)

func init() {
	if maxEpochDataAtxIDs != types.MaxEpochActiveSetSize {
		panic("MaxEpochDataAtxIDs and MaxEpochActiveSetSize differ")
	}
}

// RequestMessage is sent to the peer for hash query.
type RequestMessage struct {
	Hint datastore.Hint `scale:"max=256"` // TODO(mafa): covert to an enum
	Hash types.Hash32
}

// ResponseMessage is sent to the node as a response.
type ResponseMessage struct {
	Hash types.Hash32
	// keep in line with limit of Response.Data in `p2p/server/server.go`
	Data []byte `scale:"max=235929600"` // 225 MiB > 7.0 mio ATX * 32 bytes per ID
}

// RequestBatch is a batch of requests and a hash of all requests as ID.
type RequestBatch struct {
	ID types.Hash32
	// depends on fetch config `BatchSize` which defaults to 10, more than 100 seems unlikely
	Requests []RequestMessage `scale:"max=100"`
}

// ResponseBatch is the response struct send for a RequestBatch. the ResponseBatch ID must be the same
// as stated in RequestBatch even if not all Data is present.
type ResponseBatch struct {
	ID types.Hash32
	// depends on fetch config `BatchSize` which defaults to 10, more than 100 seems unlikely
	Responses []ResponseMessage `scale:"max=100"`
}

// MeshHashRequest is used by ForkFinder to request the hashes of layers from
// a peer to find the layer at which a divergence occurred in the local mesh of
// the node.
//
// From and To define the beginning and end layer to request. By defines the
// increment between layers to limit the number of hashes in the response,
// e.g. 2 means only every other hash is requested. The number of hashes in
// the response is limited by `fetch.MaxHashesInReq`.
type MeshHashRequest struct {
	From, To types.LayerID
	Step     uint32
}

func NewMeshHashRequest(from, to types.LayerID) *MeshHashRequest {
	diff := to.Difference(from)
	delta := diff/uint32(MaxHashesInReq-1) + 1
	return &MeshHashRequest{
		From: from,
		To:   to,
		Step: delta,
	}
}

func (r *MeshHashRequest) Count() uint {
	diff := r.To.Difference(r.From)
	count := uint(diff/r.Step + 1)
	if diff%r.Step != 0 {
		// last layer is not a multiple of Step size, so we need to add it
		count++
	}
	return count
}

func (r *MeshHashRequest) Validate() error {
	if r.Step == 0 {
		return fmt.Errorf("%w: By must not be zero", errBadRequest)
	}

	if r.To.Before(r.From) {
		return fmt.Errorf("%w: To before From", errBadRequest)
	}

	if r.Count() > MaxHashesInReq {
		return fmt.Errorf("%w: number of layers requested exceeds maximum for one request", errBadRequest)
	}
	return nil
}

func (r *MeshHashRequest) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddUint32("from", r.From.Uint32())
	encoder.AddUint32("to", r.To.Uint32())
	encoder.AddUint32("by", r.Step)
	return nil
}

type MeshHashes struct {
	// depends on syncer Config `MaxHashesInReq`, defaults to 100, 1000 is a safe upper bound
	Hashes []types.Hash32 `scale:"max=1000"`
}

type MaliciousIDs struct {
	NodeIDs []types.NodeID `scale:"max=7000000"` // to be in line with `EpochData.AtxIDs` below
}

type EpochData struct {
	// When changing this value also check
	// - the size of `ResponseMessage` above
	// - the size of `NodeIDs` in `MaliciousIDs` above
	// - the size of `Set` in `EpochActiveSet` in common/types/activation.go
	// - the size of `EligibilityProofs` in the type `Ballot` in common/types/ballot.go
	// - the size of `Rewards` in the type `InnerBlock` in common/types/block.go
	// - the size of `Ballots` in the type `LayerData` below
	// - the size of `Proposals` in the type `Value` in hare3/types.go
	AtxIDs []types.ATXID `scale:"max=7000000"`
}

// LayerData is the data response for a given layer ID.
type LayerData struct {
	// Ballots contains the ballots for the given layer.
	//
	// Worst case scenario is that a single smesher identity has > 99.97% of the total weight of the network.
	// In this case they will get all 50 available slots in all 4032 layers of the epoch.
	// Additionally every other identity on the network that successfully published an ATX will get 1 slot.
	//
	// If we expect 7.0 Mio ATXs that would be a total of 7.0 Mio + 50 * 4032 = 7 201 600 slots.
	// Since these are randomly distributed across the epoch, we can expect an average of n * p =
	// 7 201 600 / 4032 = 1786.1 ballots in a layer with a standard deviation of sqrt(n * p * (1 - p)) =
	// sqrt(7 201 600 * 1/4032 * 4031/4032) = 42.3
	//
	// This means that we can expect a maximum of 1786.1 + 6*42.3 = 2039.7 ballots per layer with
	// > 99.9997% probability.
	Ballots []types.BallotID `scale:"max=2050"`
}

type OpinionRequest struct {
	Layer types.LayerID
	Block *types.BlockID
}

type LayerOpinion struct {
	PrevAggHash types.Hash32
	Certified   *types.BlockID

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
	encoder.AddString("prev hash", lo.PrevAggHash.String())
	encoder.AddBool("has cert", lo.Certified != nil)
	if lo.Certified != nil {
		encoder.AddString("cert block", lo.Certified.String())
	}
	return nil
}
