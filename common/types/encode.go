package types

import (
	"fmt"
	"time"

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
	atx.SetReceived(time.Now())
	return &atx, nil
}

// BytesToLayerID return uint64 layer IO.
func BytesToLayerID(b []byte) LayerID {
	return NewLayerID(util.BytesToUint32(b))
}
