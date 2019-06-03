package types

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/spacemeshos/sha256-simd"
)

type EpochId uint64

func (l EpochId) ToBytes() []byte { return common.Uint64ToBytes(uint64(l)) }

func (l EpochId) IsGenesis() bool {
	return l < 2
}

type AtxId struct {
	common.Hash
}

func (t AtxId) ShortId() string {
	return t.String()[2:7]
}

var EmptyAtxId = &AtxId{common.Hash{0}}

type ActivationTxHeader struct {
	NIPSTChallenge
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
	VerifiedActiveSet uint32
	Valid             bool
	id                *AtxId
	Nipst             *nipst.NIPST
	//todo: add sig
}

func NewActivationTx(NodeId NodeId, Sequence uint64, PrevATX AtxId, LayerIndex LayerID, StartTick uint64,
	PositioningATX AtxId, ActiveSetSize uint32, View []BlockID, nipst *nipst.NIPST, isValid bool) *ActivationTx {
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
			ActiveSetSize: ActiveSetSize,
			View:          View,
		},
		Valid: isValid,
		Nipst: nipst,
	}

}

func NewActivationTxWithChallenge(poetChallenge NIPSTChallenge, ActiveSetSize uint32, View []BlockID, nipst *nipst.NIPST,
	isValid bool) *ActivationTx {

	return &ActivationTx{
		ActivationTxHeader: ActivationTxHeader{
			NIPSTChallenge: poetChallenge,
			ActiveSetSize:  ActiveSetSize,
			View:           View,
		},
		Valid: isValid,
		Nipst: nipst,
	}

}

func (t ActivationTx) Id() AtxId {
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
