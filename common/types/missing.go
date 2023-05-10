package types

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/log"
)

type ErrorMissing struct {
	MissingData
}

func (e *ErrorMissing) Error() string {
	return e.MissingData.String()
}

type MissingData struct {
	Blocks []BlockID
}

func (m *MissingData) String() string {
	return fmt.Sprintf("missing: blocks %v", m.Blocks)
}

func (m *MissingData) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddArray("blocks", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
		for _, block := range m.Blocks {
			encoder.AppendString(block.String())
		}
		return nil
	}))
	return nil
}
