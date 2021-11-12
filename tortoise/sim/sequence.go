package sim

import "github.com/spacemeshos/go-spacemesh/common/types"

// Sequence of layers with same configuration.
type Sequence struct {
	Length int
	Opts   []NextOpt
}

// WithSequence creates Sequence object.
func WithSequence(lth int, opts ...NextOpt) Sequence {
	return Sequence{Length: lth, Opts: opts}
}

// GenLayers produces sequence of layers using all configurators.
func GenLayers(g *Generator, seqs ...Sequence) []types.LayerID {
	var rst []types.LayerID
	for _, seq := range seqs {
		for i := 0; i < seq.Length; i++ {
			rst = append(rst, g.Next(seq.Opts...))
		}
	}
	return rst
}
