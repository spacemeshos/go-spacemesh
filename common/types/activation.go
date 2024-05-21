package types

import (
	"encoding/hex"
	"errors"
	"time"

	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/post/shared"

	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
)

//go:generate scalegen -types ATXMetadata,MerkleProof,EpochActiveSet

// BytesToATXID is a helper to copy buffer into a ATXID.
func BytesToATXID(buf []byte) (id ATXID) {
	copy(id[:], buf)
	return id
}

type Validity int

const (
	Unknown Validity = iota
	Valid
	Invalid
)

// ATXID is a 32 byte hash used to identify an activation transaction.
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

// EncodeScale implements scale codec interface.
func (t *ATXID) EncodeScale(e *scale.Encoder) (int, error) {
	return scale.EncodeByteArray(e, t[:])
}

// DecodeScale implements scale codec interface.
func (t *ATXID) DecodeScale(d *scale.Decoder) (int, error) {
	return scale.DecodeByteArray(d, t[:])
}

func (t *ATXID) MarshalText() ([]byte, error) {
	return util.Base64Encode(t[:]), nil
}

func (t *ATXID) UnmarshalText(buf []byte) error {
	return util.Base64Decode(t[:], buf)
}

// EmptyATXID is a canonical empty ATXID.
var EmptyATXID = ATXID{}

type ATXIDs []ATXID

// impl zap's ArrayMarshaler interface.
func (ids ATXIDs) MarshalLogArray(enc log.ArrayEncoder) error {
	for _, id := range ids {
		enc.AppendString(id.String())
	}
	return nil
}

// NIPostChallenge is the set of fields that's serialized, hashed and submitted to the PoET service
// to be included in the PoET membership proof.
type NIPostChallenge struct {
	PublishEpoch EpochID
	// Sequence number counts the number of ancestors of the ATX. It sequentially increases for each ATX in the chain.
	// Two ATXs with the same sequence number from the same miner can be used as the proof of malfeasance against
	// that miner.
	Sequence uint64
	// the previous ATX's ID (for all but the first in the sequence)
	PrevATXID      ATXID
	PositioningATX ATXID

	// CommitmentATX is the ATX used in the commitment for initializing the PoST of the node.
	CommitmentATX *ATXID
	InitialPost   *Post
}

func (c *NIPostChallenge) MarshalLogObject(encoder log.ObjectEncoder) error {
	if c == nil {
		return nil
	}
	encoder.AddUint32("PublishEpoch", c.PublishEpoch.Uint32())
	encoder.AddUint64("Sequence", c.Sequence)
	encoder.AddString("PrevATXID", c.PrevATXID.String())
	encoder.AddUint32("PublishEpoch", c.PublishEpoch.Uint32())
	encoder.AddString("PositioningATX", c.PositioningATX.String())
	if c.CommitmentATX != nil {
		encoder.AddString("CommitmentATX", c.CommitmentATX.String())
	}
	encoder.AddObject("InitialPost", c.InitialPost)
	return nil
}

// TargetEpoch returns the target epoch of the NIPostChallenge. This is the epoch in which the miner is eligible
// to participate thanks to the ATX.
func (challenge *NIPostChallenge) TargetEpoch() EpochID {
	return challenge.PublishEpoch + 1
}

type InnerActivationTx struct {
	NIPostChallenge
	Coinbase Address
	NumUnits uint32

	NIPost   *NIPost
	NodeID   *NodeID
	VRFNonce *VRFPostIndex

	// the following fields are kept private and from being serialized
	id                ATXID     // non-exported cache of the ATXID
	effectiveNumUnits uint32    // the number of effective units in the ATX (minimum of this ATX and the previous ATX)
	received          time.Time // time received by node, gossiped or synced
	validity          Validity  // whether the chain is fully verified and OK
}

// ATXMetadata is the data of ActivationTx that is signed.
// It is also used for Malfeasance proofs.
type ATXMetadata struct {
	PublishEpoch EpochID
	MsgHash      Hash32 // Hash of InnerActivationTx (returned by HashInnerBytes)
}

func (m *ATXMetadata) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddUint32("epoch", uint32(m.PublishEpoch))
	encoder.AddString("hash", m.MsgHash.ShortString())
	return nil
}

// ActivationTx is a full, signed activation transaction. It includes (or references) everything a miner needs to prove
// they are eligible to actively participate in the Spacemesh protocol in the next epoch.
type ActivationTx struct {
	InnerActivationTx

	SmesherID NodeID
	Signature EdSignature

	golden bool
}

// NewActivationTx returns a new activation transaction. The ATXID is calculated and cached.
func NewActivationTx(
	challenge NIPostChallenge,
	coinbase Address,
	nipost *NIPost,
	numUnits uint32,
	nonce *VRFPostIndex,
) *ActivationTx {
	atx := &ActivationTx{
		InnerActivationTx: InnerActivationTx{
			NIPostChallenge: challenge,
			Coinbase:        coinbase,
			NumUnits:        numUnits,

			NIPost:   nipost,
			VRFNonce: nonce,
		},
	}
	return atx
}

// TargetEpoch returns the target epoch of the ATX. This is the epoch in which the miner is eligible
// to participate thanks to the ATX.
func (atx *ActivationTx) TargetEpoch() EpochID {
	return atx.PublishEpoch + 1
}

// Golden returns true if atx is from a checkpoint snapshot.
// a golden ATX is not verifiable, and is only allowed to be prev atx or positioning atx.
func (atx *ActivationTx) Golden() bool {
	return atx.golden
}

// SetGolden set atx to golden.
func (atx *ActivationTx) SetGolden() {
	atx.golden = true
}

// MarshalLogObject implements logging interface.
func (atx *ActivationTx) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddString("atx_id", atx.id.String())
	// encoder.AddString("challenge", atx.NIPostChallenge.Hash().String())
	encoder.AddString("smesher", atx.SmesherID.String())
	encoder.AddString("prev_atx_id", atx.PrevATXID.String())
	encoder.AddString("pos_atx_id", atx.PositioningATX.String())
	if atx.CommitmentATX != nil {
		encoder.AddString("commitment_atx_id", atx.CommitmentATX.String())
	}
	if atx.VRFNonce != nil {
		encoder.AddUint64("vrf_nonce", uint64(*atx.VRFNonce))
	}
	encoder.AddString("coinbase", atx.Coinbase.String())
	encoder.AddUint32("epoch", atx.PublishEpoch.Uint32())
	encoder.AddUint64("num_units", uint64(atx.NumUnits))
	if atx.effectiveNumUnits != 0 {
		encoder.AddUint64("effective_num_units", uint64(atx.effectiveNumUnits))
	}
	encoder.AddUint64("sequence_number", atx.Sequence)
	return nil
}

// GetPoetProofRef returns the reference to the PoET proof.
func (atx *ActivationTx) GetPoetProofRef() Hash32 {
	return BytesToHash(atx.NIPost.PostMetadata.Challenge)
}

// ShortString returns the first 5 characters of the ID, for logging purposes.
func (atx *ActivationTx) ShortString() string {
	return atx.ID().ShortString()
}

// ID returns the ATX's ID.
func (atx *ActivationTx) ID() ATXID {
	return atx.id
}

func (atx *ActivationTx) EffectiveNumUnits() uint32 {
	if atx.effectiveNumUnits == 0 {
		panic("effectiveNumUnits field must be set")
	}
	return atx.effectiveNumUnits
}

// SetID sets the ATXID in this ATX's cache.
func (atx *ActivationTx) SetID(id ATXID) {
	atx.id = id
}

func (atx *ActivationTx) SetEffectiveNumUnits(numUnits uint32) {
	atx.effectiveNumUnits = numUnits
}

func (atx *ActivationTx) SetReceived(received time.Time) {
	atx.received = received
}

func (atx *ActivationTx) Received() time.Time {
	return atx.received
}

func (atx *ActivationTx) Validity() Validity {
	return atx.validity
}

func (atx *ActivationTx) SetValidity(validity Validity) {
	atx.validity = validity
}

// Verify an ATX for a given base TickHeight and TickCount.
func (atx *ActivationTx) Verify(baseTickHeight, tickCount uint64) (*VerifiedActivationTx, error) {
	if atx.effectiveNumUnits == 0 {
		return nil, errors.New("effective num units not set")
	}
	if !atx.Golden() && atx.received.IsZero() {
		return nil, errors.New("received time not set")
	}
	vAtx := &VerifiedActivationTx{
		ActivationTx: atx,

		baseTickHeight: baseTickHeight,
		tickCount:      tickCount,
	}
	return vAtx, nil
}

// Merkle proof proving that a given leaf is included in the root of merkle tree.
type MerkleProof struct {
	// Nodes on path from leaf to root (not including leaf)
	Nodes     []Hash32 `scale:"max=32"`
	LeafIndex uint64
}

// NIPost is Non-Interactive Proof of Space-Time.
// Given an id, a space parameter S, a duration D and a challenge C,
// it can convince a verifier that (1) the prover expended S * D space-time
// after learning the challenge C. (2) the prover did not know the NIPost until D time
// after the prover learned C.
type NIPost struct {
	// Membership proves that the challenge for the PoET, which is
	// constructed from fields in the activation transaction,
	// is a member of the poet's proof.
	// Proof.Root must match the Poet's POSW statement.
	Membership MerkleProof

	// Post is the proof that the prover data is still stored (or was recomputed) at
	// the time he learned the challenge constructed from the PoET.
	Post *Post

	// PostMetadata is the Post metadata, associated with the proof.
	// The proof should be verified upon the metadata during the syntactic validation,
	// while the metadata should be verified during the contextual validation.
	PostMetadata *PostMetadata
}

// VRFPostIndex is the nonce generated using Pow during post initialization. It is used as a mitigation for
// grinding of identities for VRF eligibility.
type VRFPostIndex uint64

// Field returns a log field. Implements the LoggableField interface.
func (v VRFPostIndex) Field() log.Field { return log.Uint64("vrf_nonce", uint64(v)) }

// Post is an alias to postShared.Proof.
type Post shared.Proof

func (p *Post) MarshalLogObject(encoder log.ObjectEncoder) error {
	if p == nil {
		return nil
	}
	encoder.AddUint32("nonce", p.Nonce)
	encoder.AddString("indices", hex.EncodeToString(p.Indices))
	return nil
}

// PostMetadata is similar postShared.ProofMetadata, but without the fields which can be derived elsewhere
// in a given ATX (eg. NodeID, NumUnits).
type PostMetadata struct {
	Challenge     []byte
	LabelsPerUnit uint64
}

func (m *PostMetadata) MarshalLogObject(encoder log.ObjectEncoder) error {
	if m == nil {
		return nil
	}
	encoder.AddString("Challenge", hex.EncodeToString(m.Challenge))
	encoder.AddUint64("LabelsPerUnit", m.LabelsPerUnit)
	return nil
}

// ToATXIDs returns a slice of ATXID corresponding to the given activation tx.
func ToATXIDs(atxs []*ActivationTx) []ATXID {
	ids := make([]ATXID, 0, len(atxs))
	for _, atx := range atxs {
		ids = append(ids, atx.ID())
	}
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

type EpochActiveSet struct {
	Epoch EpochID
	Set   []ATXID `scale:"max=5500000"` // to be in line with `EpochData` in fetch/wire_types.go
}

var MaxEpochActiveSetSize = scale.MustGetMaxElements[EpochActiveSet]("Set")
