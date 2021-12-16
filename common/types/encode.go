package types

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/util"
)

// Bytes returns the byte representation of the LayerID, using little endian encoding.
func (l LayerID) Bytes() []byte { return util.Uint32ToBytes(l.Value) }

// BytesToAtx deserializes an ActivationTx.
func BytesToAtx(b []byte) (*ActivationTx, error) {
	var atx ActivationTx
	err := codec.Decode(b, &atx)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &atx, nil
}

// NIPostChallengeToBytes serializes a NIPostChallenge.
func NIPostChallengeToBytes(challenge *NIPostChallenge) ([]byte, error) {
	buf, err := codec.Encode(challenge)
	if err != nil {
		return nil, fmt.Errorf("error marshaling NIPost Challenge: %v", err)
	}
	return buf, nil
}

// BytesToTransaction deserializes a Transaction.
func BytesToTransaction(buf []byte) (*Transaction, error) {
	b := Transaction{}
	err := codec.Decode(buf, &b)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	return &b, nil
}

// BytesToLayerID return uint64 layer IO.
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
