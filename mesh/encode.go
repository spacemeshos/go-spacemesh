package mesh

import (
	"bytes"
	"errors"
	"github.com/davecgh/go-xdr/xdr2"
)

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
