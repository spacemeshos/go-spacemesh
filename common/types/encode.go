package types

import (
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/util"
)

// Bytes returns the BlockID as a byte slice.
func (id BlockID) Bytes() []byte { return id.AsHash32().Bytes() }

// Bytes returns the byte representation of the LayerID, using little endian encoding.
func (l LayerID) Bytes() []byte { return util.Uint32ToBytes(l.Value) }

// BlockIdsToBytes serializes a slice of BlockIDs.
func BlockIdsToBytes(ids []BlockID) ([]byte, error) {
	SortBlockIDs(ids)
	buf, err := codec.Encode(&ids)
	if err != nil {
		return nil, errors.New("error marshalling block ids ")
	}
	return buf, nil
}

// BytesToBlockIds deserializes a slice of BlockIDs.
func BytesToBlockIds(blockIds []byte) ([]BlockID, error) {
	var ids []BlockID
	if err := codec.Decode(blockIds, &ids); err != nil {
		return nil, fmt.Errorf("error marshaling layer: %v", err)
	}
	return ids, nil
}

// BytesToAtx deserializes an ActivationTx.
func BytesToAtx(b []byte) (*ActivationTx, error) {
	var atx ActivationTx
	err := codec.Decode(b, &atx)
	if err != nil {
		return nil, err
	}
	return &atx, nil
}

// NIPostChallengeToBytes serializes a NIPostChallenge.
func NIPostChallengeToBytes(challenge *NIPostChallenge) ([]byte, error) {
	buf, err := codec.Encode(challenge)
	if err != nil {
		return nil, fmt.Errorf("error marshalling NIPost Challenge: %v", err)
	}
	return buf, nil
}

// BytesToTransaction deserializes a Transaction.
func BytesToTransaction(buf []byte) (*Transaction, error) {
	b := Transaction{}
	err := codec.Decode(buf, &b)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// ATXIdsToBytes serializes a slice of atx ids.
func ATXIdsToBytes(ids []ATXID) ([]byte, error) {
	SortAtxIDs(ids)
	buf, err := codec.Encode(&ids)
	if err != nil {
		return nil, errors.New("error marshalling block ids ")
	}
	return buf, nil
}

// BytesToLayerID return uint64 layer IO
func BytesToLayerID(b []byte) LayerID {
	return NewLayerID(util.BytesToUint32(b))
}

var (
	// FIXME(dshulyak) refactor rest of the code to use codec module.

	// InterfaceToBytes is an alias to codec.Encode.
	InterfaceToBytes = codec.Encode
	// BytesToInterface is an alias to codec.Decode.
	BytesToInterface = codec.Decode
)
