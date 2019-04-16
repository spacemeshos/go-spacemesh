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
	return l == 0
}

type AtxId struct {
	common.Hash
}

var EmptyAtxId = AtxId{common.Hash{0}}

type ActivationTxHeader struct {
	NIPSTChallenge
	VerifiedActiveSet uint32
	ActiveSetSize     uint32
	View              []BlockID
	Valid             bool
}

type NIPSTChallenge struct {
	NodeId         NodeId
	Sequence       uint64
	PrevATXId      AtxId
	LayerIdx       LayerID
	StartTick      uint64
	EndTick        uint64
	PositioningAtx AtxId
}

func (challenge *NIPSTChallenge) Hash() (*common.Hash, error) {
	ncBytes, err := NIPSTChallengeAsBytes(challenge)
	if err != nil {
		return nil, fmt.Errorf("marshaling NIPST Challenge failed: %v: ", err)
	}
	hash := common.Hash(sha256.Sum256(ncBytes))
	return &hash, nil
}

type ActivationTx struct {
	ActivationTxHeader
	Nipst *nipst.NIPST
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
				LayerIdx:       LayerIndex,
				StartTick:      StartTick,
				PositioningAtx: PositioningATX,
			},
			ActiveSetSize: ActiveSetSize,
			View:          View,
			Valid:         isValid,
		},

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
			Valid:          isValid,
		},

		Nipst: nipst,
	}

}

func (t ActivationTx) Id() AtxId {
	//todo: revise id function, add cache
	tx, err := AtxHeaderAsBytes(&t.ActivationTxHeader)
	if err != nil {
		panic("could not Serialize atx")
	}

	return AtxId{crypto.Keccak256Hash(tx)}
}
