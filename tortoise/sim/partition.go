package sim

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
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
		Partitions: []Fraction{Frac(1, 2)},
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
func (g *Generator) Split(opts ...SplitOpt) []*Generator {
	// Split everything that would be changed after long network partition
	// - activations
	// - beacons
	// - number of miners in each partition
	//
	//  things that should remain the same:
	// - blocks, contextually valid blocks and hare output before partition
	// - activations before partition
	// - beacons before partition

	conf := splitConfDefaults()
	for _, opt := range opts {
		opt(&conf)
	}
	if len(g.states) == 1 {
		panic("initailize with more then one state")
	}
	if len(conf.Partitions) > len(g.states) {
		panic("can partition only only less then existing states")
	}

	gens := make([]*Generator, len(conf.Partitions))
	total := len(g.activations)
	leftover := total

	for i := range gens {
		part := conf.Partitions[i]
		share := total * part.Nominator / part.Denominator

		gens[i] = New(
			withRng(g.rng),
			withConf(g.conf),
			WithLogger(g.logger),
		)
		gens[i].addState(g.popState(i + 1))
		gens[i].mergeLayers(g)
		gens[i].Setup(
			WithSetupMinerRange(share, share),
			WithSetupUnitsRange(g.units[0], g.units[1]),
			WithSetupTicksRange(g.ticksRange[0], g.ticksRange[1]),
			WithSetupTicks(g.ticks...),
		)

		leftover -= share
	}
	g.Setup(
		WithSetupMinerRange(leftover, leftover),
		WithSetupUnitsRange(g.units[0], g.units[1]),
		WithSetupTicksRange(g.ticksRange[0], g.ticksRange[1]),
		WithSetupTicks(g.ticks...),
	)
	return append([]*Generator{g}, gens...)
}

func (g *Generator) mergeKeys(other *Generator) {
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
}

func (g *Generator) mergeActivations(other *Generator) {
	for _, atxid := range other.activations {
		var exists bool
		for _, existing := range g.activations {
			exists = atxid == existing
			if exists {
				break
			}
		}
		if !exists {
			atx, err := other.GetState(0).DB.GetFullAtx(atxid)
			if err != nil {
				g.logger.With().Panic("failed to get atx", atxid, log.Err(err))
			}
			for _, state := range g.states {
				state.OnActivationTx(atx)
			}
			g.activations = append(g.activations, atxid)
			g.prevHeight = append(g.prevHeight, atx.TickHeight())
		}
	}
}

func (g *Generator) mergeLayers(other *Generator) {
	for i, layer := range other.layers {
		if len(g.layers)-1 < i {
			g.layers = append(g.layers, types.NewLayer(layer.Index()))
		}
		for _, ballot := range layer.Ballots() {
			var exists bool
			for _, existing := range g.layers[i].BallotIDs() {
				exists = ballot.ID() == existing
				if exists {
					break
				}
			}
			if !exists {
				rst, _ := ballots.Get(g.GetState(0).DB, ballot.ID())
				if rst != nil {
					continue
				}
				for _, state := range g.states {
					state.OnBallot(ballot)
				}
				g.layers[i].AddBallot(ballot)
			}
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
				rst, _ := blocks.Get(g.GetState(0).DB, block.ID())
				if rst != nil {
					continue
				}
				for _, state := range g.states {
					state.OnBlock(block)
				}
				g.layers[i].AddBlock(block)
			}
		}
	}
}

// Merge other Generator state into this Generator state.
func (g *Generator) Merge(other *Generator) {
	g.mergeKeys(other)
	g.mergeActivations(other)
	other.mergeActivations(g)
	g.mergeLayers(other)
	other.mergeLayers(g)

	for _, state := range other.states {
		g.addState(state)
	}
	other.states = nil
}
