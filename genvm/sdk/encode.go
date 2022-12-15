package sdk

import (
	"bytes"

	"github.com/spacemeshos/go-scale"
)

func Encode(fields ...scale.Encodable) []byte {
	buf := bytes.NewBuffer(nil)
	encoder := scale.NewEncoder(buf)
	for _, field := range fields {
		_, err := field.EncodeScale(encoder)
		if err != nil {
			panic(err)
		}
	}
	return buf.Bytes()
}

type LengthPrefixedStruct struct {
	scale.Encodable
}

func (lp LengthPrefixedStruct) EncodeScale(enc *scale.Encoder) (int, error) {
	buf := bytes.NewBuffer(nil)
	n, err := lp.Encodable.EncodeScale(scale.NewEncoder(buf))
	if err != nil {
		return n, err
	}
	return scale.EncodeByteSlice(enc, buf.Bytes())
}
