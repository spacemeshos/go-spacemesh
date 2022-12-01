package types

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"

	"github.com/spacemeshos/go-scale"
	poetShared "github.com/spacemeshos/poet/shared"
	postShared "github.com/spacemeshos/post/shared"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
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

// Less returns true if other (the given ATXID) is less than this ATXID, by lexicographic comparison.
func (t ATXID) Less(other ATXID) bool {
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

// NIPostChallenge is the set of fields that's serialized, hashed and submitted to the PoET service to be included in the
// PoET membership proof. It includes ATX sequence number, the previous ATX's ID (for all but the first in the sequence),
// the intended publication layer ID, the PoET's start and end ticks, the positioning ATX's ID and for
// the first ATX in the sequence also the commitment Merkle root.
type NIPostChallenge struct {
	// Sequence number counts the number of ancestors of the ATX. It sequentially increases for each ATX in the chain.
	// Two ATXs with the same sequence number from the same miner can be used as the proof of malfeasance against that miner.
	Sequence       uint64
	PrevATXID      ATXID
	PubLayerID     LayerID
	PositioningATX ATXID

	// CommitmentATX is the ATX used in the commitment for initializing the PoST of the node.
	CommitmentATX      *ATXID
	InitialPostIndices []byte
}

func (c *NIPostChallenge) MarshalLogObject(encoder log.ObjectEncoder) error {
	if c == nil {
		return nil
	}
	encoder.AddUint64("Sequence", c.Sequence)
	encoder.AddString("PrevATXID", c.PrevATXID.String())
	encoder.AddUint32("PubLayerID", c.PubLayerID.Value)
	encoder.AddString("PositioningATX", c.PositioningATX.String())
	if c.CommitmentATX != nil {
		encoder.AddString("CommitmentATX", c.CommitmentATX.String())
	}
	encoder.AddBinary("InitialPostIndices", c.InitialPostIndices)
	return nil
}

type PoetChallenge struct {
	*NIPostChallenge
	InitialPost         *Post
	InitialPostMetadata *PostMetadata
	NumUnits            uint32
}

func (c *PoetChallenge) MarshalLogObject(encoder log.ObjectEncoder) error {
	if c == nil {
		return nil
	}
	if err := encoder.AddObject("NIPostChallenge", c.NIPostChallenge); err != nil {
		return err
	}
	if err := encoder.AddObject("InitialPost", c.InitialPost); err != nil {
		return err
	}
	if err := encoder.AddObject("InitialPostMetadata", c.InitialPostMetadata); err != nil {
		return err
	}
	encoder.AddUint32("NumUnits", c.NumUnits)
	return nil
}

// Hash serializes the NIPostChallenge and returns its hash.
func (challenge *NIPostChallenge) Hash() (*Hash32, error) {
	ncBytes, err := codec.Encode(challenge)
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

// TargetEpoch returns the target epoch of the NIPostChallenge. This is the epoch in which the miner is eligible
// to participate thanks to the ATX.
func (challenge *NIPostChallenge) TargetEpoch() EpochID {
	return challenge.PubLayerID.GetEpoch() + 1
}

// PublishEpoch returns the publishing epoch of the NIPostChallenge.
func (challenge *NIPostChallenge) PublishEpoch() EpochID {
	return challenge.PubLayerID.GetEpoch()
}

// InnerActivationTx is a set of all of an ATX's fields, except the signature. To generate the ATX signature, this
// structure is serialized and signed. It includes the header fields, as well as the larger fields that are only used
// for validation: the NIPost and the initial Post.
type InnerActivationTx struct {
	NIPostChallenge
	Coinbase Address
	NumUnits uint32

	NIPost      *NIPost
	InitialPost *Post
	VRFNonce    *VRFPostIndex

	// the following fields are kept private and from being serialized
	id *ATXID // non-exported cache of the ATXID

	nodeID *NodeID // the id of the Node that created the ATX (public key)
}

// ActivationTx is a full, signed activation transaction. It includes (or references) everything a miner needs to prove
// they are eligible to actively participate in the Spacemesh protocol in the next epoch.
type ActivationTx struct {
	InnerActivationTx
	Sig []byte
}

// NewActivationTx returns a new activation transaction. The ATXID is calculated and cached.
func NewActivationTx(challenge NIPostChallenge, coinbase Address, nipost *NIPost, numUnits uint32, initialPost *Post) *ActivationTx {
	atx := &ActivationTx{
		InnerActivationTx: InnerActivationTx{
			NIPostChallenge: challenge,
			Coinbase:        coinbase,
			NumUnits:        numUnits,

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
	if atx.CommitmentATX != nil {
		encoder.AddString("commitment_atx_id", atx.CommitmentATX.String())
	}
	encoder.AddString("coinbase", atx.Coinbase.String())
	encoder.AddUint32("pub_layer_id", atx.PubLayerID.Value)
	encoder.AddUint32("epoch", uint32(atx.PublishEpoch()))
	encoder.AddUint64("num_units", uint64(atx.NumUnits))
	encoder.AddUint64("sequence_number", atx.Sequence)
	return nil
}

// CalcAndSetID calculates and sets the cached ID field. This field must be set before calling the ID() method.
func (atx *ActivationTx) CalcAndSetID() error {
	if atx.Sig == nil {
		return fmt.Errorf("cannot calculate ATX ID: sig is nil")
	}
	id := ATXID(CalcObjectHash32(atx))
	atx.id = &id
	return nil
}

// CalcAndSetNodeID calculates and sets the cached Node ID field. This field must be set before calling the NodeID() method.
func (atx *ActivationTx) CalcAndSetNodeID() error {
	b, err := atx.InnerBytes()
	if err != nil {
		return fmt.Errorf("failed to derive NodeID: %w", err)
	}
	pub, err := signing.ExtractPublicKey(b, atx.Sig)
	if err != nil {
		return fmt.Errorf("failed to derive NodeID: %w", err)
	}
	nodeID := BytesToNodeID(pub)
	atx.nodeID = &nodeID
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

// ShortString returns the first 5 characters of the ID, for logging purposes.
func (atx *ActivationTx) ShortString() string {
	return atx.ID().ShortString()
}

// ID returns the ATX's ID.
func (atx *ActivationTx) ID() ATXID {
	if atx.id == nil {
		panic("id field must be set")
	}
	return *atx.id
}

// NodeID returns the ATX's Node ID.
func (atx *ActivationTx) NodeID() NodeID {
	if atx.nodeID == nil {
		panic("nodeID field must be set")
	}
	return *atx.nodeID
}

// SetID sets the ATXID in this ATX's cache.
func (atx *ActivationTx) SetID(id *ATXID) {
	atx.id = id
}

// SetNodeID sets the Node ID in the ATX's cache.
func (atx *ActivationTx) SetNodeID(nodeID *NodeID) {
	atx.nodeID = nodeID
}

// Verify an ATX for a given base TickHeight and TickCount.
func (atx *ActivationTx) Verify(baseTickHeight, tickCount uint64) (*VerifiedActivationTx, error) {
	if atx.id == nil {
		if err := atx.CalcAndSetID(); err != nil {
			return nil, err
		}
	}
	if atx.nodeID == nil {
		if err := atx.CalcAndSetNodeID(); err != nil {
			return nil, err
		}
	}
	vAtx := &VerifiedActivationTx{
		ActivationTx: atx,

		baseTickHeight: baseTickHeight,
		tickCount:      tickCount,
	}
	return vAtx, nil
}

type PoetProofRef []byte

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
func (proofMessage PoetProofMessage) Ref() (PoetProofRef, error) {
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
	ID            string
	ChallengeHash []byte
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

// VRFPostIndex is the nonce generated using Pow during post initialization. It is used as a mitigation for
// grinding of identities for VRF eligibility.
type VRFPostIndex uint64

func (v *VRFPostIndex) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact64(enc, uint64(*v))
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (v *VRFPostIndex) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		value, n, err := scale.DecodeCompact64(dec)
		if err != nil {
			return total, err
		}
		total += n
		*v = VRFPostIndex(value)
	}
	return total, nil
}

// Post is an alias to postShared.Proof.
type Post postShared.Proof

// EncodeScale implements scale codec interface.
func (p *Post) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeCompact32(enc, uint32(p.Nonce))
		if err != nil {
			return total, err
		}
		total += n
	}
	{
		n, err := scale.EncodeByteSlice(enc, p.Indices)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// DecodeScale implements scale codec interface.
func (p *Post) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeCompact32(dec)
		if err != nil {
			return total, err
		}
		total += n
		p.Nonce = uint32(field)
	}
	{
		field, n, err := scale.DecodeByteSlice(dec)
		if err != nil {
			return total, err
		}
		total += n
		p.Indices = field
	}
	return total, nil
}

func (p *Post) MarshalLogObject(encoder log.ObjectEncoder) error {
	if p == nil {
		return nil
	}
	encoder.AddUint32("Nonce", p.Nonce)
	encoder.AddString("Indicies", util.Bytes2Hex(p.Indices))
	return nil
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

// PostMetadata is similar postShared.ProofMetadata, but without the fields which can be derived elsewhere in a given ATX (ID, NumUnits).
type PostMetadata struct {
	Challenge     []byte
	BitsPerLabel  uint8
	LabelsPerUnit uint64
	K1            uint32
	K2            uint32
}

func (m *PostMetadata) MarshalLogObject(encoder log.ObjectEncoder) error {
	if m == nil {
		return nil
	}
	encoder.AddString("Challenge", util.Bytes2Hex(m.Challenge))
	encoder.AddUint8("BitsPerLabel", m.BitsPerLabel)
	encoder.AddUint64("LabelsPerUnit", m.LabelsPerUnit)
	encoder.AddUint32("K1", m.K1)
	encoder.AddUint32("K2", m.K2)
	return nil
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
	sort.Slice(ids, func(i, j int) bool { return ids[i].Less(ids[j]) })
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
