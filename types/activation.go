package types

import (
	"encoding/hex"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/poet/shared"
	"github.com/spacemeshos/post/proving"
	"github.com/spacemeshos/sha256-simd"
)

const PoetIdLength = 32

type EpochId uint64

func (l EpochId) ToBytes() []byte { return common.Uint64ToBytes(uint64(l)) }

func (l EpochId) IsGenesis() bool {
	return l < 2
}

type AtxId struct {
	common.Hash
}

func (t AtxId) ShortId() string {
	return t.ShortString()
}

var EmptyAtxId = &AtxId{common.Hash{0}}

type ActivationTxHeader struct {
	NIPSTChallenge
	Coinbase      address.Address
	ActiveSetSize uint32
	View          []BlockID
}

type NIPSTChallenge struct {
	NodeId         NodeId
	Sequence       uint64
	PrevATXId      AtxId
	PubLayerIdx    LayerID
	StartTick      uint64
	EndTick        uint64
	PositioningAtx AtxId
}

func (challenge *NIPSTChallenge) Hash() (*common.Hash, error) {
	ncBytes, err := NIPSTChallengeAsBytes(challenge)
	if err != nil {
		return nil, err
	}
	hash := common.Hash(sha256.Sum256(ncBytes))
	return &hash, nil
}

func (challenge *NIPSTChallenge) String() string {
	return fmt.Sprintf("<id: [vrf: %v ed: %v], seq: %v, prevAtx: %v, PubLayer: %v, s tick: %v, e tick: %v, "+
		"posAtx: %v>",
		common.Bytes2Hex(challenge.NodeId.VRFPublicKey)[:5],
		challenge.NodeId.Key[:5],
		challenge.Sequence,
		challenge.PrevATXId.ShortId(),
		challenge.PubLayerIdx,
		challenge.StartTick,
		challenge.EndTick,
		challenge.PositioningAtx.ShortId())
}

type ActivationTx struct {
	ActivationTxHeader
	id    *AtxId
	Nipst *NIPST
	//todo: add sig
}

func NewActivationTx(NodeId NodeId,
	Coinbase address.Address,
	Sequence uint64,
	PrevATX AtxId,
	LayerIndex LayerID,
	StartTick uint64,
	PositioningATX AtxId,
	ActiveSetSize uint32,
	View []BlockID,
	nipst *NIPST) *ActivationTx {
	return &ActivationTx{
		ActivationTxHeader: ActivationTxHeader{
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
			View:          View,
		},
		Nipst: nipst,
	}

}

func NewActivationTxWithChallenge(poetChallenge NIPSTChallenge, coinbase address.Address, ActiveSetSize uint32,
	View []BlockID, nipst *NIPST) *ActivationTx {

	return &ActivationTx{
		ActivationTxHeader: ActivationTxHeader{
			NIPSTChallenge: poetChallenge,
			Coinbase:       coinbase,
			ActiveSetSize:  ActiveSetSize,
			View:           View,
		},
		Nipst: nipst,
	}

}

func (t *ActivationTx) Id() AtxId {
	if t.id != nil {
		return *t.id
	}
	//todo: revise id function, add cache
	tx, err := AtxHeaderAsBytes(&t.ActivationTxHeader)
	if err != nil {
		panic("could not Serialize atx")
	}

	t.id = &AtxId{crypto.Keccak256Hash(tx)}
	return *t.id
}

func (t ActivationTx) ShortId() string {
	return t.Id().ShortId()
}

func (t ActivationTx) TargetEpoch(layersPerEpoch uint16) EpochId {
	return t.PubLayerIdx.GetEpoch(layersPerEpoch) + 1
}

func (t *ActivationTx) GetPoetProofRef() []byte {
	return t.Nipst.PoetProofRef
}

type PoetProof struct {
	shared.MerkleProof
	Members   [][]byte
	LeafCount uint64
}

type PoetProofMessage struct {
	PoetProof
	PoetId    [PoetIdLength]byte
	RoundId   uint64
	Signature []byte
}

func (proofMessage PoetProofMessage) Ref() ([]byte, error) {
	poetProofBytes, err := InterfaceToBytes(&proofMessage.PoetProof)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal poet proof for poetId %x round %d: %v",
			proofMessage.PoetId, proofMessage.RoundId, err)
	}

	ref := sha256.Sum256(poetProofBytes)
	return ref[:], nil
}

type PoetRound struct {
	Id uint64
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
	NipstChallenge *common.Hash

	// postProof is the proof that the prover data
	// is still stored (or was recomputed).
	PostProof *PostProof

	// reference to the PoET proof, should be available to all since the PoET proof is propagated over gossip
	PoetProofRef []byte
}

type PostProof proving.Proof

func (p *PostProof) String() string {
	return fmt.Sprintf("id: %v, challenge: %v, root: %v",
		bytesToShortString(p.Identity), bytesToShortString(p.Challenge), bytesToShortString(p.MerkleRoot))
}

func bytesToShortString(b []byte) string {
	l := len(b)
	if l == 0 {
		return "empty"
	}
	return fmt.Sprintf("\"%s…\"", hex.EncodeToString(b)[:common.Min(l, 5)])
}

type ProcessingError string

func (s ProcessingError) Error() string {
	return string(s)
}

func IsProcessingError(err error) bool {
	_, ok := err.(ProcessingError)
	return ok
}
