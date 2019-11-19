package types

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/common/util"
)

func (b BlockID) ToBytes() []byte { return b.AsHash32().Bytes() }

func (l LayerID) ToBytes() []byte { return util.Uint64ToBytes(uint64(l)) }

func BlockIdsAsBytes(ids []BlockID) ([]byte, error) {
	var w bytes.Buffer
	SortBlockIds(ids)
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

func BytesAsAtx(b []byte) (*ActivationTx, error) {
	buf := bytes.NewReader(b)
	var atx ActivationTx
	_, err := xdr.Unmarshal(buf, &atx)
	if err != nil {
		return nil, err
	}
	return &atx, nil
}

func TxIdsAsBytes(ids []TransactionId) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &ids); err != nil {
		return nil, fmt.Errorf("error marshalling tx ids: %v", err)
	}
	return w.Bytes(), nil
}

func NIPSTChallengeAsBytes(challenge *NIPSTChallenge) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, challenge); err != nil {
		return nil, fmt.Errorf("error marshalling NIPST Challenge: %v", err)
	}
	return w.Bytes(), nil
}

func BytesAsTransaction(buf []byte) (*Transaction, error) {
	b := Transaction{}
	_, err := xdr.Unmarshal(bytes.NewReader(buf), &b)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// ⚠️ Pass the interface by reference
func BytesToInterface(buf []byte, i interface{}) error {
	_, err := xdr.Unmarshal(bytes.NewReader(buf), i)
	if err != nil {
		return err
	}
	return nil
}

// ⚠️ Pass the interface by reference
func InterfaceToBytes(i interface{}) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &i); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}
