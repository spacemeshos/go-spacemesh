package beacon

import (
	"encoding/hex"

	"go.uber.org/zap/zapcore"
	"golang.org/x/exp/maps"
)

type proposalSet map[Proposal]struct{}

func (vs proposalSet) sort() proposalList {
	return proposalList(maps.Keys(vs)).sort()
}

func (p proposalSet) MarshalLogArray(enc zapcore.ArrayEncoder) error {
	for proposal := range p {
		enc.AppendString(hex.EncodeToString(proposal[:]))
	}
	return nil
}
