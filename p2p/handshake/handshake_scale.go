package handshake

import (
	"github.com/spacemeshos/go-scale"
)

func (t *HandshakeMessage) EncodeScale(enc *scale.Encoder) (total int, err error) {

	return total, nil
}

func (t *HandshakeMessage) DecodeScale(dec *scale.Decoder) (total int, err error) {

	return total, nil
}

func (t *HandshakeAck) EncodeScale(enc *scale.Encoder) (total int, err error) {

	return total, nil
}

func (t *HandshakeAck) DecodeScale(dec *scale.Decoder) (total int, err error) {

	return total, nil
}
