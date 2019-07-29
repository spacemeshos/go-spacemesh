package types

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/crypto/sha3"
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

func BytesAsAtx(b []byte, id *AtxId) (*ActivationTx, error) {
	buf := bytes.NewReader(b)
	var atx ActivationTx
	_, err := xdr.Unmarshal(buf, &atx)
	if err != nil {
		return nil, err
	}
	if id == nil {
		atx.CalcAndSetId()
	} else {
		atx.SetId(id)
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

func AddressableTransactionAsBytes(tx *AddressableSignedTransaction) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, tx); err != nil {
		return nil, fmt.Errorf("error marshalling transaction: %v", err)
	}
	return w.Bytes(), nil
}

func BytesAsAddressableTransaction(buf []byte) (*AddressableSignedTransaction, error) {
	b := AddressableSignedTransaction{}
	_, err := xdr.Unmarshal(bytes.NewReader(buf), &b)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func SignedTransactionAsBytes(tx *SerializableSignedTransaction) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &tx); err != nil {
		return nil, fmt.Errorf("error marshalling transaction: %v", err)
	}
	return w.Bytes(), nil
}

func BytesAsSignedTransaction(buf []byte) (*SerializableSignedTransaction, error) {
	b := SerializableSignedTransaction{}
	_, err := xdr.Unmarshal(bytes.NewReader(buf), &b)
	if err != nil {
		return nil, err
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

//!!! Pass the interface by reference
func InterfaceToBytes(i interface{}) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &i); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

//todo standardized transaction id across project
//todo replace panic
func GetTransactionId(t *SerializableSignedTransaction) TransactionId {
	tx, err := InterfaceToBytes(t)
	if err != nil {
		panic("could not Serialize transaction")
	}
	res := sha3.Sum256(tx)
	return res
}
