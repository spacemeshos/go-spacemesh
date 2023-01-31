package types

import (
	"encoding/hex"

	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/log"
)

// BytesToNodeID is a helper to copy buffer into NodeID struct.
func BytesToNodeID(buf []byte) (id NodeID) {
	copy(id[:], buf)
	return id
}

// ExtractNodeIDFromSig extracts the NodeID from a signature.
//
// TODO(mafa): this is a hack because the common/types package is not able to import the signing package.
// The proper fix for this would be to move code that calls this function out of the common/types package.
var ExtractNodeIDFromSig = func(msg, sig []byte) (NodeID, error) {
	pub, err := ed25519.ExtractPublicKey(msg, sig)
	if err != nil {
		return NodeID{}, err
	}
	return BytesToNodeID(pub), nil
}

// NodeID contains a miner's public key.
type NodeID Hash32

const (
	// NodeIDSize in bytes.
	NodeIDSize = Hash32Length
)

// String returns a string representation of the NodeID, for logging purposes.
// It implements the Stringer interface.
func (id NodeID) String() string {
	return hex.EncodeToString(id.Bytes())
}

// Bytes returns the byte representation of the Edwards public key.
func (id NodeID) Bytes() []byte {
	return id[:]
}

// ShortString returns a the first 5 characters of the ID, for logging purposes.
func (id NodeID) ShortString() string {
	return Shorten(id.String(), 5)
}

// Field returns a log field. Implements the LoggableField interface.
func (id NodeID) Field() log.Field { return log.Stringer("node_id", id) }

// EmptyNodeID is a canonical empty NodeID.
var EmptyNodeID NodeID

// EncodeScale implements scale codec interface.
func (id *NodeID) EncodeScale(e *scale.Encoder) (int, error) {
	return scale.EncodeByteArray(e, id[:])
}

// DecodeScale implements scale codec interface.
func (id *NodeID) DecodeScale(d *scale.Decoder) (int, error) {
	return scale.DecodeByteArray(d, id[:])
}

// NodeIDsToHashes turns a list of NodeID into their Hash32 representation.
func NodeIDsToHashes(ids []NodeID) []Hash32 {
	hashes := make([]Hash32, 0, len(ids))
	for _, id := range ids {
		hashes = append(hashes, Hash32(id))
	}
	return hashes
}
