package types

import (
	"encoding/hex"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/util"
)

// BytesToNodeID is a helper to copy buffer into a NodeID.
func BytesToNodeID(buf []byte) (id NodeID) {
	copy(id[:], buf)
	return id
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

// ShortString returns a the first 3 hex-encoded bytes of the ID, for logging purposes.
func (id NodeID) ShortString() string {
	return hex.EncodeToString(id[:3])
}

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

func (id NodeID) MarshalText() ([]byte, error) {
	return util.Base64Encode(id[:]), nil
}

func (id *NodeID) UnmarshalText(buf []byte) error {
	return util.Base64Decode(id[:], buf)
}

// NodeIDsToHashes turns a list of NodeID into their Hash32 representation.
func NodeIDsToHashes(ids []NodeID) []Hash32 {
	hashes := make([]Hash32, 0, len(ids))
	for _, id := range ids {
		hashes = append(hashes, Hash32(id))
	}
	return hashes
}
