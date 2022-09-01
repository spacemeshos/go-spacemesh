package types

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"

	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-scale"
	poetShared "github.com/spacemeshos/poet/shared"
	postShared "github.com/spacemeshos/post/shared"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
)

//go:generate scalegen

// EpochID is the running epoch number. It's zero-based, so the genesis epoch has EpochID == 0.
type EpochID uint32

// ToBytes returns a byte-slice representation of the EpochID, using little endian encoding.
func (l EpochID) ToBytes() []byte { return util.Uint32ToBytes(uint32(l)) }

// IsGenesis returns true if this epoch is in genesis. The first two epochs are considered genesis epochs.
func (l EpochID) IsGenesis() bool {
	return l < 2
}

// NeedsGoldenPositioningATX returns true if ATXs in this epoch require positioning ATX to be equal to the Golden ATX.
// All ATXs in epoch 1 must have the Golden ATX as positioning ATX.
func (l EpochID) NeedsGoldenPositioningATX() bool {
	return l == 1
}

// FirstLayer returns the layer ID of the first layer in the epoch.
func (l EpochID) FirstLayer() LayerID {
	return NewLayerID(uint32(l)).Mul(GetLayersPerEpoch())
}

// Field returns a log field. Implements the LoggableField interface.
func (l EpochID) Field() log.Field { return log.Uint32("epoch_id", uint32(l)) }

// String returns string representation of the epoch id numeric value.
func (l EpochID) String() string {
	return strconv.FormatUint(uint64(l), 10)
}

// ATXID is a 32-bit hash used to identify an activation transaction.
type ATXID Hash32

const (
	// ATXIDSize in bytes.
	ATXIDSize = Hash32Length
)

// String implements stringer interface.
func (t ATXID) String() string {
	return t.ShortString()
}

// ShortString returns the first few characters of the ID, for logging purposes.
func (t ATXID) ShortString() string {
	return t.Hash32().ShortString()
}

// Hash32 returns the ATXID as a Hash32.
func (t ATXID) Hash32() Hash32 {
	return Hash32(t)
}

// Bytes returns the ATXID as a byte slice.
func (t ATXID) Bytes() []byte {
	return Hash32(t).Bytes()
}

// Field returns a log field. Implements the LoggableField interface.
func (t ATXID) Field() log.Field { return log.FieldNamed("atx_id", t.Hash32()) }

// Compare returns true if other (the given ATXID) is less than this ATXID, by lexicographic comparison.
func (t ATXID) Compare(other ATXID) bool {
	return bytes.Compare(t.Bytes(), other.Bytes()) < 0
}

// EncodeScale implements scale codec interface.
func (t *ATXID) EncodeScale(e *scale.Encoder) (int, error) {
	return scale.EncodeByteArray(e, t[:])
}

// DecodeScale implements scale codec interface.
func (t *ATXID) DecodeScale(d *scale.Decoder) (int, error) {
	return scale.DecodeByteArray(d, t[:])
}

// EmptyATXID is a canonical empty ATXID.
var EmptyATXID = &ATXID{}

// ActivationTxHeader is the header of an activation transaction. It includes all fields from the NIPostChallenge, as
// well as the coinbase address and total weight.
type ActivationTxHeader struct {
	NIPostChallenge
	Coinbase Address
	NumUnits uint32

	// the following fields are kept private and from being serialized
	id *ATXID // non-exported cache of the ATXID

	nodeID *NodeID // the id of the Node that created the ATX (public key)

	// TODO(dshulyak) this is important to prevent accidental state reads
	// before this field is set. reading empty data could lead to disastrous bugs.
	verified       bool
	baseTickHeight uint64
	tickCount      uint64
}

// ShortString returns the first 5 characters of the ID, for logging purposes.
func (atxh *ActivationTxHeader) ShortString() string {
	return atxh.ID().ShortString()
}

// Hash32 returns the ATX's ID as a Hash32.
func (atxh *ActivationTxHeader) Hash32() Hash32 {
	return atxh.ID().Hash32()
}

// ID returns the ATX's ID.
func (atxh *ActivationTxHeader) ID() ATXID {
	if atxh.id == nil {
		panic("id field must be set")
	}
	return *atxh.id
}

// NodeID returns the ATX's Node ID.
func (atxh *ActivationTxHeader) NodeID() NodeID {
	if atxh.nodeID == nil {
		panic("nodeID field must be set")
	}
	return *atxh.nodeID
}

// TargetEpoch returns the target epoch of the activation transaction. This is the epoch in which the miner is eligible
// to participate thanks to the ATX.
func (atxh *ActivationTxHeader) TargetEpoch() EpochID {
	return atxh.PubLayerID.GetEpoch() + 1
}

// SetID sets the ATXID in this ATX's cache.
func (atxh *ActivationTxHeader) SetID(id *ATXID) {
	atxh.id = id
}

// SetNodeID sets the Node ID in the ATX's cache.
func (atxh *ActivationTxHeader) SetNodeID(nodeID *NodeID) {
	atxh.nodeID = nodeID
}

// GetWeight of the atx.
func (atxh *ActivationTxHeader) GetWeight() uint64 {
	return uint64(atxh.NumUnits) * (atxh.tickCount)
}

// BaseTickHeight is a tick height of the positional atx.
func (atxh *ActivationTxHeader) BaseTickHeight() uint64 {
	return atxh.baseTickHeight
}

// TickCount returns tick count from from poet proof attached to the atx.
func (atxh *ActivationTxHeader) TickCount() uint64 {
	return atxh.tickCount
}

// TickHeight returns a sum of base tick height and tick count.
func (atxh *ActivationTxHeader) TickHeight() uint64 {
	return atxh.baseTickHeight + atxh.tickCount
}

// Verify sets field extract after verification.
func (atxh *ActivationTxHeader) Verify(baseTickHeight, tickCount uint64) {
	atxh.verified = true
	atxh.baseTickHeight = baseTickHeight
	atxh.tickCount = tickCount
}

// NIPostChallenge is the set of fields that's serialized, hashed and submitted to the PoET service to be included in the
// PoET membership proof. It includes ATX sequence number, the previous ATX's ID (for all but the first in the sequence),
// the intended publication layer ID, the PoET's start and end ticks, the positioning ATX's ID and for
// the first ATX in the sequence also the commitment Merkle root.
type NIPostChallenge struct {
	Sequence           uint64
	PrevATXID          ATXID
	PubLayerID         LayerID
	PositioningATX     ATXID
	InitialPostIndices []byte
}

// Hash serializes the NIPostChallenge and returns its hash.
func (challenge *NIPostChallenge) Hash() (*Hash32, error) {
	ncBytes, err := NIPostChallengeToBytes(challenge)
	if err != nil {
		return nil, err
	}
	hash := CalcHash32(ncBytes)
	return &hash, nil
}

// String returns a string representation of the NIPostChallenge, for logging purposes.
// It implements the Stringer interface.
func (challenge *NIPostChallenge) String() string {
	return fmt.Sprintf("<seq: %v, prevATX: %v, PubLayer: %v, posATX: %s>",
		challenge.Sequence,
		challenge.PrevATXID.ShortString(),
		challenge.PubLayerID,
		challenge.PositioningATX.ShortString())
}

// InnerActivationTx is a set of all of an ATX's fields, except the signature. To generate the ATX signature, this
// structure is serialized and signed. It includes the header fields, as well as the larger fields that are only used
// for validation: the NIPost and the initial Post.
type InnerActivationTx struct {
	ActivationTxHeader
	NIPost      *NIPost
	InitialPost *Post
}

// ActivationTx is a full, signed activation transaction. It includes (or references) everything a miner needs to prove
// they are eligible to actively participate in the Spacemesh protocol in the next epoch.
type ActivationTx struct {
	InnerActivationTx
	Sig []byte
}

// NewActivationTx returns a new activation transaction. The ATXID is calculated and cached.
func NewActivationTx(challenge NIPostChallenge, coinbase Address, nipost *NIPost, numUnits uint, initialPost *Post) *ActivationTx {
	atx := &ActivationTx{
		InnerActivationTx: InnerActivationTx{
			ActivationTxHeader: ActivationTxHeader{
				NIPostChallenge: challenge,
				Coinbase:        coinbase,
				NumUnits:        uint32(numUnits),
			},
			NIPost:      nipost,
			InitialPost: initialPost,
		},
	}
	return atx
}

// InnerBytes returns a byte slice of the serialization of the inner ATX (excluding the signature field).
func (atx *ActivationTx) InnerBytes() ([]byte, error) {
	return codec.Encode(&atx.InnerActivationTx)
}

// MarshalLogObject implements logging interface.
func (atx *ActivationTx) MarshalLogObject(encoder log.ObjectEncoder) error {
	if atx.InitialPost != nil {
		encoder.AddString("nipost", atx.InitialPost.String())
	}
	h, err := atx.NIPostChallenge.Hash()
	if err == nil && h != nil {
		encoder.AddString("challenge", h.String())
	}
	encoder.AddString("id", atx.id.String())
	encoder.AddString("sender_id", atx.nodeID.String())
	encoder.AddString("prev_atx_id", atx.PrevATXID.String())
	encoder.AddString("pos_atx_id", atx.PositioningATX.String())
	encoder.AddString("coinbase", atx.Coinbase.String())
	encoder.AddUint32("pub_layer_id", atx.PubLayerID.Value)
	encoder.AddUint32("epoch", uint32(atx.PubLayerID.GetEpoch()))
	encoder.AddUint64("num_units", uint64(atx.NumUnits))
	encoder.AddUint64("sequence_number", atx.Sequence)
	if atx.verified {
		encoder.AddUint64("base_tick_height", atx.baseTickHeight)
		encoder.AddUint64("tick_count", atx.tickCount)
		encoder.AddUint64("weight", atx.GetWeight())
	}
	return nil
}

// CalcAndSetID calculates and sets the cached ID field. This field must be set before calling the ID() method.
func (atx *ActivationTx) CalcAndSetID() {
	if atx.Sig == nil {
		panic("cannot calculate ATX ID: sig is nil")
	}
	id := ATXID(CalcATXHash32(atx))
	atx.SetID(&id)
}

// CalcAndSetNodeID calculates and sets the cached Node ID field. This field must be set before calling the NodeID() method.
func (atx *ActivationTx) CalcAndSetNodeID() error {
	b, err := atx.InnerBytes()
	if err != nil {
		return fmt.Errorf("failed to derive NodeID: %w", err)
	}
	pub, err := ed25519.ExtractPublicKey(b, atx.Sig)
	if err != nil {
		return fmt.Errorf("failed to derive NodeID: %w", err)
	}
	nodeID := BytesToNodeID(pub)
	atx.SetNodeID(&nodeID)
	return nil
}

// GetPoetProofRef returns the reference to the PoET proof.
func (atx *ActivationTx) GetPoetProofRef() Hash32 {
	return BytesToHash(atx.NIPost.PostMetadata.Challenge)
}

// GetShortPoetProofRef returns the first 5 characters of the PoET proof reference, for logging purposes.
func (atx *ActivationTx) GetShortPoetProofRef() []byte {
	ref := atx.GetPoetProofRef()
	return ref[:util.Min(5, len(ref))]
}

// PoetProof is the full PoET service proof of elapsed time. It includes the list of members, a leaf count declaration
// and the actual PoET Merkle proof.
type PoetProof struct {
	poetShared.MerkleProof
	Members   [][]byte
	LeafCount uint64
}

// PoetProofMessage is the envelope which includes the PoetProof, service ID, round ID and signature.
type PoetProofMessage struct {
	PoetProof
	PoetServiceID []byte
	RoundID       string
	Signature     []byte
}

// Ref returns the reference to the PoET proof message. It's the sha256 sum of the entire proof message.
func (proofMessage PoetProofMessage) Ref() ([]byte, error) {
	poetProofBytes, err := codec.Encode(&proofMessage.PoetProof)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal poet proof for poetId %x round %v: %v",
			proofMessage.PoetServiceID, proofMessage.RoundID, err)
	}
	ref := hash.Sum(poetProofBytes)
	h := CalcHash32(ref[:])
	return h.Bytes(), nil
}

// PoetRound includes the PoET's round ID.
type PoetRound struct {
	ID string
}

// NIPost is Non-Interactive Proof of Space-Time.
// Given an id, a space parameter S, a duration D and a challenge C,
// it can convince a verifier that (1) the prover expended S * D space-time
// after learning the challenge C. (2) the prover did not know the NIPost until D time
// after the prover learned C.
type NIPost struct {
	// Challenge is the challenge for the PoET which is
	// constructed from fields in the activation transaction.
	Challenge *Hash32

	// Post is the proof that the prover data is still stored (or was recomputed) at
	// the time he learned the challenge constructed from the PoET.
	Post *Post

	// PostMetadata is the Post metadata, associated with the proof.
	// The proof should be verified upon the metadata during the syntactic validation,
	// while the metadata should be verified during the contextual validation.
	PostMetadata *PostMetadata
}

// Post is an alias to postShared.Proof.
type Post postShared.Proof

// EncodeScale implements scale codec interface.
func (p *Post) EncodeScale(enc *scale.Encoder) (total int, err error) {
	n, err := scale.EncodeCompact32(enc, uint32(p.Nonce))
	if err != nil {
		return total, err
	}
	total += n
	n, err = scale.EncodeByteSlice(enc, p.Indices)
	if err != nil {
		return total, err
	}
	total += n
	return total, nil
}

// DecodeScale implements scale codec interface.
func (p *Post) DecodeScale(dec *scale.Decoder) (total int, err error) {
	if field, n, err := scale.DecodeCompact32(dec); err != nil {
		return total, err
	} else { // nolint
		total += n
		p.Nonce = uint32(field)
	}
	if field, n, err := scale.DecodeByteSlice(dec); err != nil {
		return total, err
	} else { // nolint
		total += n
		p.Indices = field
	}
	return total, nil
}

// PostMetadata is similar postShared.ProofMetadata, but without the fields which can be derived elsewhere in a given ATX (ID, NumUnits).
type PostMetadata struct {
	Challenge     []byte
	BitsPerLabel  uint8
	LabelsPerUnit uint64
	K1            uint32
	K2            uint32
}

// String returns a string representation of the PostProof, for logging purposes.
// It implements the Stringer interface.
func (p *Post) String() string {
	return fmt.Sprintf("nonce: %v, indices: %v",
		p.Nonce, bytesToShortString(p.Indices))
}

func bytesToShortString(b []byte) string {
	l := len(b)
	if l == 0 {
		return "empty"
	}
	return fmt.Sprintf("\"%s…\"", hex.EncodeToString(b)[:util.Min(l, 5)])
}

// ProcessingError is a type of error (implements the error interface) that is used to differentiate processing errors
// from validation errors.
type ProcessingError string

// Error returns the processing error as a string. It implements the error interface.
func (s ProcessingError) Error() string {
	return string(s)
}

// IsProcessingError returns true if the given error is a processing error.
func IsProcessingError(err error) bool {
	_, ok := err.(ProcessingError)
	return ok
}

// ToATXIDs returns a slice of ATXID corresponding to the given activation tx.
func ToATXIDs(atxs []*ActivationTx) []ATXID {
	ids := make([]ATXID, 0, len(atxs))
	for _, atx := range atxs {
		ids = append(ids, atx.ID())
	}
	return ids
}

// SortAtxIDs sorts a list of atx IDs in lexicographic order, in-place.
func SortAtxIDs(ids []ATXID) []ATXID {
	sort.Slice(ids, func(i, j int) bool { return ids[i].Compare(ids[j]) })
	return ids
}

// ATXIDsToHashes turns a list of ATXID into their Hash32 representation.
func ATXIDsToHashes(ids []ATXID) []Hash32 {
	hashes := make([]Hash32, 0, len(ids))
	for _, id := range ids {
		hashes = append(hashes, id.Hash32())
	}
	return hashes
}
