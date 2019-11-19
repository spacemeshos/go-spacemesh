package types

import (
	"encoding/hex"
	"fmt"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/poet/shared"
	"github.com/spacemeshos/post/proving"
	"github.com/spacemeshos/sha256-simd"
)

type EpochId uint64

func (l EpochId) ToBytes() []byte { return util.Uint64ToBytes(uint64(l)) }

func (l EpochId) IsGenesis() bool {
	return l < 2
}

func (l EpochId) FirstLayer(layersPerEpoch uint16) LayerID {
	return LayerID(uint64(l) * uint64(layersPerEpoch))
}

type AtxId Hash32

func (t *AtxId) ShortString() string {
	return t.Hash32().ShortString()
}

func (t *AtxId) Hash32() Hash32 {
	return Hash32(*t)
}

func (t AtxId) Bytes() []byte {
	return Hash32(t).Bytes()
}

var EmptyAtxId = &AtxId{}

type ActivationTxHeader struct {
	NIPSTChallenge
	id            *AtxId
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

func (atxh *ActivationTxHeader) Id() AtxId {
	if atxh.id == nil {
		panic("id field must be set")
	}
	return *atxh.id
}

func (atxh *ActivationTxHeader) TargetEpoch(layersPerEpoch uint16) EpochId {
	return atxh.PubLayerIdx.GetEpoch(layersPerEpoch) + 1
}

func (atxh *ActivationTxHeader) SetId(id *AtxId) {
	atxh.id = id
}

type NIPSTChallenge struct {
	NodeId               NodeId
	Sequence             uint64
	PrevATXId            AtxId
	PubLayerIdx          LayerID
	StartTick            uint64
	EndTick              uint64
	PositioningAtx       AtxId
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
	return fmt.Sprintf("<id: [vrf: %v ed: %v], seq: %v, prevAtx: %v, PubLayer: %v, s tick: %v, e tick: %v, "+
		"posAtx: %v>",
		util.Bytes2Hex(challenge.NodeId.VRFPublicKey)[:5],
		challenge.NodeId.Key[:5],
		challenge.Sequence,
		challenge.PrevATXId.ShortString(),
		challenge.PubLayerIdx,
		challenge.StartTick,
		challenge.EndTick,
		challenge.PositioningAtx.ShortString())
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

func NewActivationTx(NodeId NodeId, Coinbase Address, Sequence uint64, PrevATX AtxId, LayerIndex LayerID,
	StartTick uint64, PositioningATX AtxId, ActiveSetSize uint32, View []BlockID, nipst *NIPST) *ActivationTx {

	atx := &ActivationTx{
		&InnerActivationTx{
			ActivationTxHeader: &ActivationTxHeader{
				NIPSTChallenge: NIPSTChallenge{
					NodeId:         NodeId,
					Sequence:       Sequence,
					PrevATXId:      PrevATX,
					PubLayerIdx:    LayerIndex,
					StartTick:      StartTick,
					PositioningAtx: PositioningATX,
				},
				Coinbase:      Coinbase,
				ActiveSetSize: ActiveSetSize,
			},
			Nipst: nipst,
			View:  View,
		},
		nil,
	}
	atx.CalcAndSetId()
	return atx
}

func NewActivationTxWithChallenge(poetChallenge NIPSTChallenge, coinbase Address, ActiveSetSize uint32,
	View []BlockID, nipst *NIPST, commitment *PostProof) *ActivationTx {

	atx := &ActivationTx{
		&InnerActivationTx{
			ActivationTxHeader: &ActivationTxHeader{
				NIPSTChallenge: poetChallenge,
				Coinbase:       coinbase,
				ActiveSetSize:  ActiveSetSize,
			},
			Nipst:      nipst,
			View:       View,
			Commitment: commitment,
		},
		nil,
	}
	atx.CalcAndSetId()
	return atx
}

func (atx *ActivationTx) AtxBytes() ([]byte, error) {
	return InterfaceToBytes(atx.InnerActivationTx)
}

func (atx *ActivationTx) CalcAndSetId() {
	id := AtxId(CalcAtxHash32(atx))
	atx.SetId(&id)
}

func (atx *ActivationTx) GetPoetProofRef() []byte {
	return atx.Nipst.PostProof.Challenge
}

func (atx *ActivationTx) GetShortPoetProofRef() []byte {
	return atx.Nipst.PostProof.Challenge[:util.Min(5, len(atx.Nipst.PostProof.Challenge))]
}

// ValidateSignedAtx extracts public key from message and verifies public key exists in IdStore, this is how we validate
// ATX signature. If this is the first ATX it is considered valid anyways and ATX syntactic validation will determine ATX validity
func ExtractPublicKey(signedAtx *ActivationTx) (*signing.PublicKey, error) {
	bts, err := signedAtx.AtxBytes()
	if err != nil {
		return nil, err
	}

	pubKey, err := ed25519.ExtractPublicKey(bts, signedAtx.Sig)
	if err != nil {
		return nil, err
	}

	pub := signing.NewPublicKey(pubKey)
	return pub, nil
}

func SignAtx(signer *signing.EdSigner, atx *ActivationTx) (*ActivationTx, error) {
	bts, err := atx.AtxBytes()
	if err != nil {
		return nil, err
	}
	atx.Sig = signer.Sign(bts)
	return atx, nil
}

type PoetProof struct {
	shared.MerkleProof
	Members   [][]byte
	LeafCount uint64
}

type PoetProofMessage struct {
	PoetProof
	PoetServiceId []byte
	RoundId       string
	Signature     []byte
}

func (proofMessage PoetProofMessage) Ref() ([]byte, error) {
	poetProofBytes, err := InterfaceToBytes(&proofMessage.PoetProof)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal poet proof for poetId %x round %v: %v",
			proofMessage.PoetServiceId, proofMessage.RoundId, err)
	}

	ref := sha256.Sum256(poetProofBytes)
	return ref[:], nil
}

type PoetRound struct {
	Id string
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
