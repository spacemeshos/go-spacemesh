package types

import (
	"encoding/hex"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/poet/shared"
	"github.com/spacemeshos/post/proving"
	"github.com/spacemeshos/sha256-simd"
)

type EpochID uint64

func (l EpochID) ToBytes() []byte { return util.Uint64ToBytes(uint64(l)) }

func (l EpochID) IsGenesis() bool {
	return l < 2
}

func (l EpochID) FirstLayer(layersPerEpoch uint16) LayerID {
	return LayerID(uint64(l) * uint64(layersPerEpoch))
}

func (l EpochID) Field() log.Field { return log.Uint64("epoch_id", uint64(l)) }

type ATXID Hash32

func (t *ATXID) ShortString() string {
	return t.Hash32().ShortString()
}

func (t *ATXID) Hash32() Hash32 {
	return Hash32(*t)
}

func (t ATXID) Bytes() []byte {
	return Hash32(t).Bytes()
}

func (t ATXID) Field() log.Field { return t.Hash32().Field("atx_id") }

var EmptyATXID = &ATXID{}

type ActivationTxHeader struct {
	NIPSTChallenge
	id            *ATXID
	Coinbase      Address
	ActiveSetSize uint32
}

func (atxh *ActivationTxHeader) ShortString() string {
	return atxh.id.ShortString()
}

func (atxh *ActivationTxHeader) Hash32() Hash32 {
	return Hash32(*atxh.id)
}

func (atxh *ActivationTxHeader) Bytes() []byte {
	return atxh.id.Bytes()
}

func (atxh *ActivationTxHeader) ID() ATXID {
	if atxh.id == nil {
		panic("id field must be set")
	}
	return *atxh.id
}

func (atxh *ActivationTxHeader) TargetEpoch(layersPerEpoch uint16) EpochID {
	return atxh.PubLayerID.GetEpoch(layersPerEpoch) + 1
}

func (atxh *ActivationTxHeader) SetID(id *ATXID) {
	atxh.id = id
}

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

func (challenge *NIPSTChallenge) Hash() (*Hash32, error) {
	ncBytes, err := NIPSTChallengeAsBytes(challenge)
	if err != nil {
		return nil, err
	}
	hash := CalcHash32(ncBytes)
	return &hash, nil
}

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

type InnerActivationTx struct {
	*ActivationTxHeader
	Nipst      *NIPST
	View       []BlockID
	Commitment *PostProof
}

type ActivationTx struct {
	*InnerActivationTx
	Sig []byte
}

func NewActivationTxForTests(nodeID NodeID, sequence uint64, prevATX ATXID, pubLayerID LayerID, startTick uint64,
	positioningATX ATXID, coinbase Address, activeSetSize uint32, view []BlockID, nipst *NIPST) *ActivationTx {

	nipstChallenge := NIPSTChallenge{
		NodeID:         nodeID,
		Sequence:       sequence,
		PrevATXID:      prevATX,
		PubLayerID:     pubLayerID,
		StartTick:      startTick,
		PositioningATX: positioningATX,
	}
	return NewActivationTx(nipstChallenge, coinbase, activeSetSize, view, nipst, nil)
}

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

func (atx *ActivationTx) ATXBytes() ([]byte, error) {
	return InterfaceToBytes(atx.InnerActivationTx)
}

func (atx *ActivationTx) CalcAndSetID() {
	id := ATXID(CalcATXHash32(atx))
	atx.SetID(&id)
}

func (atx *ActivationTx) GetPoetProofRef() []byte {
	return atx.Nipst.PostProof.Challenge
}

func (atx *ActivationTx) GetShortPoetProofRef() []byte {
	return atx.Nipst.PostProof.Challenge[:util.Min(5, len(atx.Nipst.PostProof.Challenge))]
}

type PoetProof struct {
	shared.MerkleProof
	Members   [][]byte
	LeafCount uint64
}

type PoetProofMessage struct {
	PoetProof
	PoetServiceID []byte
	RoundID       string
	Signature     []byte
}

func (proofMessage PoetProofMessage) Ref() ([]byte, error) {
	poetProofBytes, err := InterfaceToBytes(&proofMessage.PoetProof)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal poet proof for poetId %x round %v: %v",
			proofMessage.PoetServiceID, proofMessage.RoundID, err)
	}

	ref := sha256.Sum256(poetProofBytes)
	return ref[:], nil
}

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

type PostProof proving.Proof

func (p *PostProof) String() string {
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

type ProcessingError string

func (s ProcessingError) Error() string {
	return string(s)
}

func IsProcessingError(err error) bool {
	_, ok := err.(ProcessingError)
	return ok
}
