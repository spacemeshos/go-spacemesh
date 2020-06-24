package types

import (
	"encoding/hex"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/poet/shared"
	"github.com/spacemeshos/post/proving"
	"github.com/spacemeshos/sha256-simd"
	"strings"
)

// EpochID is the running epoch number. It's zero-based, so the genesis epoch has EpochID == 0.
type EpochID uint64

// ToBytes returns a byte-slice representation of the EpochID, using little endian encoding.
func (l EpochID) ToBytes() []byte { return util.Uint64ToBytes(uint64(l)) }

// IsGenesis returns true if this epoch is in genesis. The first two epochs are considered genesis epochs.
func (l EpochID) IsGenesis() bool {
	return l < 2
}

// FirstLayer returns the layer ID of the first layer in the epoch.
func (l EpochID) FirstLayer(layersPerEpoch uint16) LayerID {
	return LayerID(uint64(l) * uint64(layersPerEpoch))
}

// Field returns a log field. Implements the LoggableField interface.
func (l EpochID) Field() log.Field { return log.Uint64("epoch_id", uint64(l)) }

// ATXID is a 32-bit hash used to identify an activation transaction.
type ATXID Hash32

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
func (t ATXID) Field() log.Field { return t.Hash32().Field("atx_id") }

// EmptyATXID is a canonical empty ATXID.
var EmptyATXID = &ATXID{}

// ActivationTxHeader is the header of an activation transaction. It includes all fields from the NIPSTChallenge, as
// well as the coinbase address and active set size.
type ActivationTxHeader struct {
	NIPSTChallenge
	id            *ATXID // non-exported cache of the ATXID
	Coinbase      Address
	ActiveSetSize uint32
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

// TargetEpoch returns the target epoch of the activation transaction. This is the epoch in which the miner is eligible
// to participate thanks to the ATX.
func (atxh *ActivationTxHeader) TargetEpoch(layersPerEpoch uint16) EpochID {
	return atxh.PubLayerID.GetEpoch(layersPerEpoch) + 1
}

// SetID sets the ATXID in this ATX's cache.
func (atxh *ActivationTxHeader) SetID(id *ATXID) {
	atxh.id = id
}

// NIPSTChallenge is the set of fields that's serialized, hashed and submitted to the PoET service to be included in the
// PoET membership proof. It includes the node ID, ATX sequence number, the previous ATX's ID (for all but the first in
// the sequence), the intended publication layer ID, the PoET's start and end ticks, the positioning ATX's ID and for
// the first ATX in the sequence also the commitment Merkle root.
type NIPSTChallenge struct {
	NodeID               NodeID
	Sequence             uint64
	PrevATXID            ATXID
	PubLayerID           LayerID
	StartTick            uint64
	EndTick              uint64
	PositioningATX       ATXID
	CommitmentMerkleRoot []byte
}

// Hash serializes the NIPSTChallenge and returns its hash.
func (challenge *NIPSTChallenge) Hash() (*Hash32, error) {
	ncBytes, err := NIPSTChallengeToBytes(challenge)
	if err != nil {
		return nil, err
	}
	hash := CalcHash32(ncBytes)
	return &hash, nil
}

// String returns a string representation of the NIPSTChallenge, for logging purposes.
// It implements the Stringer interface.
func (challenge *NIPSTChallenge) String() string {
	return fmt.Sprintf("<id: [vrf: %v ed: %v], seq: %v, prevATX: %v, PubLayer: %v, s tick: %v, e tick: %v, "+
		"posATX: %v>",
		util.Bytes2Hex(challenge.NodeID.VRFPublicKey)[:5],
		challenge.NodeID.Key[:5],
		challenge.Sequence,
		challenge.PrevATXID.ShortString(),
		challenge.PubLayerID,
		challenge.StartTick,
		challenge.EndTick,
		challenge.PositioningATX.ShortString())
}

// InnerActivationTx is a set of all of an ATX's fields, except the signature. To generate the ATX signature, this
// structure is serialized and signed. It includes the header fields, as well as the larger fields that are only used
// for validation: the NIPST, view and PoST proof.
type InnerActivationTx struct {
	*ActivationTxHeader
	Nipst      *NIPST
	View       []BlockID
	Commitment *PostProof
}

// ActivationTx is a full, signed activation transaction. It includes (or references) everything a miner needs to prove
// they are eligible to actively participate in the Spacemesh protocol in the next epoch.
type ActivationTx struct {
	*InnerActivationTx
	Sig []byte
}

// NewActivationTx returns a new activation transaction. The ATXID is calculated and cached.
func NewActivationTx(nipstChallenge NIPSTChallenge, coinbase Address, activeSetSize uint32, view []BlockID,
	nipst *NIPST, commitment *PostProof) *ActivationTx {

	atx := &ActivationTx{
		InnerActivationTx: &InnerActivationTx{
			ActivationTxHeader: &ActivationTxHeader{
				NIPSTChallenge: nipstChallenge,
				Coinbase:       coinbase,
				ActiveSetSize:  activeSetSize,
			},
			Nipst:      nipst,
			View:       view,
			Commitment: commitment,
		},
	}
	atx.CalcAndSetID()
	return atx
}

// InnerBytes returns a byte slice of the serialization of the inner ATX (excluding the signature field).
func (atx *ActivationTx) InnerBytes() ([]byte, error) {
	return InterfaceToBytes(atx.InnerActivationTx)
}

// Fields returns an array of LoggableFields for logging
func (atx *ActivationTx) Fields(layersPerEpoch uint16, size int) []log.LoggableField {
	commitmentStr := ""
	if atx.Commitment != nil {
		commitmentStr = atx.Commitment.String()
	}

	challenge := ""
	h, err := atx.NIPSTChallenge.Hash()
	if err == nil && h != nil {
		challenge = h.String()
	}

	return []log.LoggableField{
		atx.ID(),
		log.FieldNamed("sender_id", atx.NodeID),
		log.FieldNamed("prev_atx_id", atx.PrevATXID),
		log.FieldNamed("pos_atx_id", atx.PositioningATX),
		atx.PubLayerID,
		atx.PubLayerID.GetEpoch(layersPerEpoch),
		log.Uint32("active_set", atx.ActiveSetSize),
		log.Int("viewlen", len(atx.View)),
		log.Uint64("sequence_number", atx.Sequence),
		log.String("NIPSTChallenge", challenge),
		log.String("commitment", commitmentStr),
		log.Int("atx_size", size),
	}
}

// AtxIdsField returns a list of loggable fields for a given list of ATXIDs
func AtxIdsField(ids []ATXID) log.Field {
	strs := []string{}
	for _, a := range ids {
		strs = append(strs, a.ShortString())
	}
	return log.String("atx_ids", strings.Join(strs, ", "))
}

// CalcAndSetID calculates and sets the cached ID field. This field must be set before calling the ID() method.
func (atx *ActivationTx) CalcAndSetID() {
	id := ATXID(CalcATXHash32(atx))
	atx.SetID(&id)
}

// GetPoetProofRef returns the reference to the PoET proof.
func (atx *ActivationTx) GetPoetProofRef() []byte {
	return atx.Nipst.PostProof.Challenge
}

// GetShortPoetProofRef returns the first 5 characters of the PoET proof reference, for logging purposes.
func (atx *ActivationTx) GetShortPoetProofRef() []byte {
	return atx.Nipst.PostProof.Challenge[:util.Min(5, len(atx.Nipst.PostProof.Challenge))]
}

// PoetProof is the full PoET service proof of elapsed time. It includes the list of members, a leaf count declaration
// and the actual PoET Merkle proof.
type PoetProof struct {
	shared.MerkleProof
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
	poetProofBytes, err := InterfaceToBytes(&proofMessage.PoetProof)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal poet proof for poetId %x round %v: %v",
			proofMessage.PoetServiceID, proofMessage.RoundID, err)
	}

	ref := sha256.Sum256(poetProofBytes)
	return ref[:], nil
}

// PoetRound includes the PoET's round ID.
type PoetRound struct {
	ID string
}

// NIPST is Non-Interactive Proof of Space-Time.
// Given an id, a space parameter S, a duration D and a challenge C,
// it can convince a verifier that (1) the prover expended S * D space-time
// after learning the challenge C. (2) the prover did not know the NIPST until D time
// after the prover learned C.
type NIPST struct {
	// space is the amount of storage which the prover
	// requires to dedicate for generating the NIPST.
	Space uint64

	// nipstChallenge is the challenge for PoET which is
	// constructed from fields in the activation transaction.
	NipstChallenge *Hash32

	// postProof is the proof that the prover data
	// is still stored (or was recomputed).
	PostProof *PostProof
}

// PostProof is an alias to the PoST proof.
type PostProof proving.Proof

// String returns a string representation of the PostProof, for logging purposes.
// It implements the Stringer interface.
func (p PostProof) String() string {
	return fmt.Sprintf("challenge: %v, root: %v",
		bytesToShortString(p.Challenge), bytesToShortString(p.MerkleRoot))
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
