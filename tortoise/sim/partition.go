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
	//
	// transient partitions can be configured without splitting state, in transient partitions
	// - votes are splitted
	// - hare will not reach consensus (or will reach only in one of the partition)
	// - but atxs and beacons remain the same
	//
	// for now transient can be configured by options to Next
}

// Split generator into multiple partitions.
// First generator will use original tortoise state (mesh, activations).
//
// This has an issue, original beacon is preserved only in the first generator
// therefore rerun after merge will work only in the first generator.
func (g *Generator) Split(opts ...SplitOpt) []*Generator {
	// Split everything that would be changed after long network partition
	// - activations
	// - beacons
	// - number of miners in each partition
	//
	//  things that should remain the same:
	// - blocks, contextually valid blocks and input vectors before partition
	// - activations before partition
	// - beacons before partition

	conf := splitConfDefaults()
	for _, opt := range opts {
		opt(&conf)
	}
	if len(conf.Partitions) < 2 {
		panic("should be at least two parititions")
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
			g.Setup(
				WithSetupMinerRange(share, share),
				WithSetupUnitsRange(g.units[0], g.units[1]),
			)
		} else {
			gens[i] = New(
				withRng(g.rng),
				withConf(g.conf),
				WithLogger(g.logger),
			)
			gens[i].mergeAll(g) // merging *all* previous history
			gens[i].Setup(
				WithSetupMinerRange(share, share),
				WithSetupUnitsRange(g.units[0], g.units[1]),
			)
		}
	}
	return gens
}

func (g *Generator) mergeAll(other *Generator) {
	g.Merge(other)
	g.beacons.Copy(other.beacons)
	for _, layer := range g.layers {
		bids, err := other.State.MeshDB.GetLayerInputVectorByID(layer.Index())
		if err != nil {
			g.logger.With().Panic("load layer input vector", log.Err(err))
		}
		err = g.State.MeshDB.SaveLayerInputVectorByID(context.Background(), layer.Index(), bids)
		if err != nil {
			g.logger.With().Panic("save layer input vector", log.Err(err))
		}
		bidsmap, err := other.State.MeshDB.LayerContextuallyValidBlocks(context.Background(), layer.Index())
		if err != nil {
			g.logger.With().Panic("load contextually valid blocks", log.Err(err))
		}
		for bid := range bidsmap {
			if err := g.State.MeshDB.SaveContextualValidity(bid, layer.Index(), true); err != nil {
				g.logger.With().Panic("save contextual validity", bid, log.Err(err))
			}
		}
	}
}

// Merge other Generator state into this Generator state.
func (g *Generator) Merge(other *Generator) {
	for _, key := range other.keys {
		var exists bool
		for _, existing := range g.keys {
			exists = key == existing
			if exists {
				break
			}
		}
		if !exists {
			g.keys = append(g.keys, key)
		}
	}

	for _, atxid := range other.activations {
		var exists bool
		for _, existing := range g.activations {
			exists = atxid == existing
			if exists {
				break
			}
		}
		if !exists {
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
		if len(g.layers)-1 < i {
			g.layers = append(g.layers, types.NewLayer(layer.Index()))
		}
		for _, block := range layer.Blocks() {
			var exists bool
			for _, existing := range g.layers[i].BlocksIDs() {
				exists = block.ID() == existing
				if exists {
					break
				}
			}
			if !exists {
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
}
