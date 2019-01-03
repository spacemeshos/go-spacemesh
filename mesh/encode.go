package mesh

import (
	"bytes"
	"encoding/binary"
	"errors"
	"github.com/davecgh/go-xdr/xdr2"
)

func (b BlockID) ToBytes() []byte { return uint32ToBytes(uint32(b)) }
func (l LayerID) ToBytes() []byte { return uint32ToBytes(uint32(l)) }

func BytesToUint32(i []byte) uint32 { return binary.LittleEndian.Uint32(i) }

func uint32ToBytes(i uint32) []byte {
	a := make([]byte, 4)
	binary.LittleEndian.PutUint32(a, i)
	return a
}

func blockIdsAsBytes(ids map[BlockID]bool) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &ids); err != nil {
		return nil, errors.New("error marshalling block ids ")
	}
	return w.Bytes(), nil
}

func blockAsBytes(block Block) ([]byte, error) {
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

func bytesToBlock(b []byte) (*Block, error) {
	var block Block
	if _, err := xdr.Unmarshal(bytes.NewReader(b), &block); err != nil {
		return nil, errors.New("could not unmarshal block")
	}
	return &block, nil
}
