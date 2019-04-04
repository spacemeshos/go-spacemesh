package mesh

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/davecgh/go-xdr/xdr2"
	"github.com/spacemeshos/go-spacemesh/common"
	"sort"
)

func (b BlockID) ToBytes() []byte { return common.Uint64ToBytes(uint64(b)) }

func (l LayerID) ToBytes() []byte { return common.Uint64ToBytes(uint64(l)) }

func BlockAsBytes(block Block) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &block); err != nil {
		return nil, fmt.Errorf("error marshalling block ids %v", err)
	}
	return w.Bytes(), nil
}

func BytesAsBlock(buf []byte) (Block, error) {
	b := Block{}
	_, err := xdr.Unmarshal(bytes.NewReader(buf), &b)
	if err != nil {
		return b, err
	}
	return b, nil
}

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
		return nil, errors.New("error marshaling layer ")
	}
	return ids, nil
}

func BlockHeaderToBytes(bheader *BlockHeader) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, bheader); err != nil {
		return nil, fmt.Errorf("error marshalling block ids %v", err)
	}
	return w.Bytes(), nil
}

func BytesAsBlockHeader(buf []byte) (BlockHeader, error) {
	b := BlockHeader{}
	_, err := xdr.Unmarshal(bytes.NewReader(buf), &b)
	if err != nil {
		return b, err
	}
	return b, nil
}

func TransactionAsBytes(tx *SerializableTransaction) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &tx); err != nil {
		return nil, fmt.Errorf("error marshalling block ids %v", err)
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

func MiniBlockToBytes(mini MiniBlock) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &mini); err != nil {
		return nil, fmt.Errorf("error marshalling block ids %v", err)
	}
	return w.Bytes(), nil
}

func BytesAsMiniBlock(buf []byte) (*MiniBlock, error) {
	b := MiniBlock{}
	_, err := xdr.Unmarshal(bytes.NewReader(buf), &b)
	if err != nil {
		return &b, err
	}
	return &b, nil
}
