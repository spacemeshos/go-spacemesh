package types

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/crypto/sha3"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/spacemeshos/poet/shared"
	"sort"
)

func (b BlockID) ToBytes() []byte { return common.Uint64ToBytes(uint64(b)) }

func (l LayerID) ToBytes() []byte { return common.Uint64ToBytes(uint64(l)) }

func BlockIdsAsBytes(ids []BlockID) ([]byte, error) {
	var w bytes.Buffer
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if _, err := xdr.Marshal(&w, &ids); err != nil {
		return nil, errors.New("error marshalling block ids ")
	}
	return w.Bytes(), nil
}

func BytesToBlockIds(blockIds []byte) ([]BlockID, error) {
	var ids []BlockID
	if _, err := xdr.Unmarshal(bytes.NewReader(blockIds), &ids); err != nil {
		return nil, fmt.Errorf("error marshaling layer: %v", err)
	}
	return ids, nil
}

func ViewAsBytes(ids []BlockID) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &ids); err != nil {
		return nil, fmt.Errorf("error marshalling view: %v", err)
	}
	return w.Bytes(), nil
}

func BytesToView(blockIds []byte) ([]BlockID, error) {
	var ids []BlockID
	if _, err := xdr.Unmarshal(bytes.NewReader(blockIds), &ids); err != nil {
		return nil, errors.New("error marshaling layer ")
	}
	return ids, nil
}

func (t ActivationTx) ActivesetValid() bool {
	if t.VerifiedActiveSet > 0 {
		return t.VerifiedActiveSet >= t.ActiveSetSize
	}
	return false
}

func AtxHeaderAsBytes(tx *ActivationTxHeader) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &tx); err != nil {
		return nil, fmt.Errorf("error marshalling atx header: %v", err)
	}
	return w.Bytes(), nil
}

func AtxAsBytes(tx *ActivationTx) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &tx); err != nil {
		return nil, fmt.Errorf("error marshalling atx: %v", err)
	}
	return w.Bytes(), nil
}

func BytesAsAtx(b []byte) (*ActivationTx, error) {
	buf := bytes.NewReader(b)
	atx := ActivationTx{
		ActivationTxHeader: ActivationTxHeader{
			NIPSTChallenge: NIPSTChallenge{
				NodeId: NodeId{
					Key:          "",
					VRFPublicKey: nil,
				},
				Sequence: 0,
				PrevATXId: AtxId{
					Hash: common.Hash{},
				},
				PubLayerIdx: 0,
				StartTick:   0,
				EndTick:     0,
				PositioningAtx: AtxId{
					Hash: common.Hash{},
				},
			},
			ActiveSetSize: 0,
			View:          nil,
		},
		Nipst: &nipst.NIPST{
			Id:             nil,
			Space:          0,
			Duration:       0,
			NipstChallenge: nil,
			PoetRound: &nipst.PoetRound{
				Id: 0,
			},
			PoetMembershipProof: &nipst.MembershipProof{
				Index: 0,
				Root:  common.Hash{},
				Proof: nil,
			},
			PoetProof: &nipst.PoetProof{
				Commitment: nil,
				N:          0,
				Proof: &shared.MerkleProof{
					Root:         nil,
					ProvenLeaves: nil,
					ProofNodes:   nil,
				},
			},
			PostChallenge: nil,
			PostProof: &nipst.PostProof{
				Identity:     nil,
				Challenge:    nil,
				MerkleRoot:   nil,
				ProofNodes:   nil,
				ProvenLeaves: nil,
			},
		},
	}
	_, err := xdr.Unmarshal(buf, &atx)
	if err != nil {
		return nil, err
	}
	return &atx, nil
}

func NIPSTChallengeAsBytes(challenge *NIPSTChallenge) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, challenge); err != nil {
		return nil, fmt.Errorf("error marshalling NIPST Challenge: %v", err)
	}
	return w.Bytes(), nil
}

func TransactionAsBytes(tx *SerializableTransaction) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &tx); err != nil {
		return nil, fmt.Errorf("error marshalling transaction: %v", err)
	}
	return w.Bytes(), nil
}

func BytesAsTransaction(buf []byte) (*SerializableTransaction, error) {
	b := SerializableTransaction{}
	_, err := xdr.Unmarshal(bytes.NewReader(buf), &b)
	if err != nil {
		return &b, err
	}
	return &b, nil
}

//!!! Pass the interface by reference
func BytesToInterface(buf []byte, i interface{}) error {
	_, err := xdr.Unmarshal(bytes.NewReader(buf), i)
	if err != nil {
		return err
	}
	return nil
}

func InterfaceToBytes(i interface{}) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &i); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

//todo standardized transaction id across project
//todo replace panic
func GetTransactionId(t *SerializableTransaction) TransactionId {
	tx, err := TransactionAsBytes(t)
	if err != nil {
		panic("could not Serialize transaction")
	}

	return sha3.Sum256(tx)
}
