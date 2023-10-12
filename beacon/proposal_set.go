package beacon

import (
	"encoding/hex"

	"go.uber.org/zap/zapcore"
)

type proposalSet map[Proposal]struct{}

func (vs proposalSet) list() proposalList {
	votes := make(proposalList, 0)

	for vote := range vs {
		votes = append(votes, vote)
	}

	return votes
}

func (vs proposalSet) sort() proposalList {
	return vs.list().sort()
}

func (p proposalSet) MarshalLogArray(enc zapcore.ArrayEncoder) error {
	for proposal := range p {
		enc.AppendString(hex.EncodeToString(proposal[:]))
	}
	return nil
}
