package sim

import (
	"context"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// Frac is a shortcut for creating Fraction object.
func Frac(nominator, denominator int) Fraction {
	return Fraction{Nominator: nominator, Denominator: denominator}
}

// Fraction of something.
type Fraction struct {
	Nominator, Denominator int
}

func (f Fraction) String() string {
	return fmt.Sprintf("%d/%d", f.Nominator, f.Denominator)
}

// SplitOpt is for configuring partition.
type SplitOpt func(*splitConf)

// WithPartitions configures number of miners in each partition
// relative to original Generator.
func WithPartitions(parts ...Fraction) SplitOpt {
	return func(c *splitConf) {
		c.Partitions = parts
	}
}

func splitConfDefaults() splitConf {
	return splitConf{
		Partitions: []Fraction{Frac(1, 2), Frac(1, 2)},
	}
}

type splitConf struct {
	Partitions []Fraction
	// TODO(dshulyak) support both transient and full partitions
	// in transient partition miners still have the same atxs and beacons
	//
	// transient partitions can be configured without splitting state, in transient partitions
	// - votes are splitted
	// - hare will not reach consensus (or will reach only in one of the partition)
	// all of this can be done by option to Next
}

// Split generator into multiple partitions.
// First generator will use original tortoise state (mesh, activations).
func (g *Generator) Split(opts ...SplitOpt) []*Generator {
	conf := splitConfDefaults()
	for _, opt := range opts {
		opt(&conf)
	}
	if len(conf.Partitions) < 2 {
		panic("should be atlest two parititions")
	}
	gens := make([]*Generator, len(conf.Partitions))
	total := len(g.activations)

	for i := range gens {
		part := conf.Partitions[i]
		if part.Denominator == 0 {
			panic(fmt.Sprintf("denominator in fraction %v is zero", part))
		}
		share := total * int(part.Nominator) / int(part.Denominator)
		if i == 0 {
			gens[i] = g
			g.Setup(WithSetupMinerRange(share, share))
		} else {
			layers := make([]*types.Layer, len(g.layers))
			copy(layers, g.layers)
			gens[i] = New(
				withRng(g.rng),
				withConf(g.conf),
				withLayers(layers),
				WithLogger(g.logger),
			)
			g.Setup(WithSetupMinerRange(share, share))
		}
	}
	return gens
}

// Merge other Generator state into this Generator state. This is to simulate state exchange after partition.
func (g *Generator) Merge(other *Generator) {
	// don't merge beacon, as it is not supposed to be merged.

	g.activations = append(g.activations, other.activations...)
	for _, key := range other.keys {
		for _, existing := range g.keys {
			if key == existing {
				continue
			}
			g.keys = append(g.keys, key)
		}
	}

	for _, atxid := range other.activations {
		for _, existing := range g.activations {
			if atxid == existing {
				continue
			}
			atx, err := other.State.AtxDB.GetFullAtx(atxid)
			if err != nil {
				g.logger.With().Panic("failed to get atx", atxid, log.Err(err))
			}
			if err := g.State.AtxDB.StoreAtx(context.Background(), 0, atx); err != nil {
				g.logger.With().Panic("failed to save atx", atxid, log.Err(err))
			}
			g.activations = append(g.activations, atxid)
		}
	}

	for i, layer := range other.layers {
		for _, block := range layer.Blocks() {
			rst, _ := g.State.MeshDB.GetBlock(block.ID())
			if rst != nil {
				continue
			}
			if err := g.State.MeshDB.AddBlock(block); err != nil {
				g.logger.With().Panic("failed to save block", block.ID(), log.Err(err))
			}
			g.layers[i].AddBlock(block)
		}
	}
}
