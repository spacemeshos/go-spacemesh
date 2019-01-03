package mesh

import (
	"bytes"
	"errors"
	"github.com/davecgh/go-xdr/xdr2"
	"math/big"
)

func (b BlockID) ToBytes() []byte { return uint64ToBytes(uint64(b)) }
func (l LayerID) ToBytes() []byte { return uint64ToBytes(uint64(l)) }

func uint64ToBytes(i uint64) []byte { return new(big.Int).SetUint64(uint64(i)).Bytes() }

func boolAsBytes(b bool) []byte {
	var bitBool int8
	if b {
		bitBool = 1
	}
	return append(make([]byte, 0, 1), byte(bitBool))
}

func blockIdsAsBytes(ids map[BlockID]bool) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &ids); err != nil {
		return nil, errors.New("error marshalling block ids ")
	}
	return w.Bytes(), nil
}

func BlockAsBytes(block TortoiseBlock) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &block); err != nil {
		return nil, errors.New("error marshalling block ids ")
	}
	return w.Bytes(), nil
}

func bytesToBlockIds(blockIds []byte) (map[BlockID]bool, error) {
	var ids map[BlockID]bool
	if _, err := xdr.Unmarshal(bytes.NewReader(blockIds), &ids); err != nil {
		return nil, errors.New("error marshaling layer ")
	}
	return ids, nil
}

func bytesToBlock(b []byte) (*TortoiseBlock, error) {
	var block TortoiseBlock
	if _, err := xdr.Unmarshal(bytes.NewReader(b), &block); err != nil {
		return nil, errors.New("could not unmarshal block")
	}
	return &block, nil
}
