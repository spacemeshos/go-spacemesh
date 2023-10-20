package beacon

import (
	"encoding/hex"

	"go.uber.org/zap/zapcore"
	"golang.org/x/exp/maps"
)

type proposalSet map[Proposal]struct{}

func (p proposalSet) sorted() proposalList {
	return proposalList(maps.Keys(p)).sort()
}

func (p proposalSet) MarshalLogArray(enc zapcore.ArrayEncoder) error {
	for proposal := range p {
		enc.AppendString(hex.EncodeToString(proposal[:]))
	}
	return nil
}
