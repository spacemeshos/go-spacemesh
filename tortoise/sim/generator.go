package sim

import (
	"context"
	"math/rand"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/blocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// GenOpt for configuring Generator.
type GenOpt func(*Generator)

// WithSeed configures seed for Generator. By default 0 is used.
func WithSeed(seed int64) GenOpt {
	return func(g *Generator) {
		g.rng = rand.New(rand.NewSource(seed))
	}
}

// WithLayerSize configures average layer size.
func WithLayerSize(size uint32) GenOpt {
	return func(g *Generator) {
		g.conf.LayerSize = size
	}
}

// WithLogger configures logger.
func WithLogger(logger log.Log) GenOpt {
	return func(g *Generator) {
		g.logger = logger
	}
}

// WithPath configures path for persistent databases.
func WithPath(path string) GenOpt {
	return func(g *Generator) {
		g.conf.Path = path
	}
}

func withRng(rng *rand.Rand) GenOpt {
	return func(g *Generator) {
		g.rng = rng
	}
}

func withConf(conf config) GenOpt {
	return func(g *Generator) {
		g.conf = conf
	}
}

func withLayers(layers []*types.Layer) GenOpt {
	return func(g *Generator) {
		g.layers = layers
	}
}

type config struct {
	Path           string
	LayerSize      uint32
	LayersPerEpoch uint32
}

func defaults() config {
	return config{
		LayerSize:      30,
		LayersPerEpoch: types.GetLayersPerEpoch(),
	}
}

// State is state that can be used by tortoise.
type State struct {
	MeshDB  *mesh.DB
	AtxDB   *activation.DB
	Beacons blocks.BeaconGetter
}

// New creates Generator instance.
func New(opts ...GenOpt) *Generator {
	g := &Generator{
		rng:       rand.New(rand.NewSource(0)),
		conf:      defaults(),
		logger:    log.NewNop(),
		reordered: map[types.LayerID]types.LayerID{},
	}
	for _, opt := range opts {
		opt(g)
	}
	mdb := newMeshDB(g.logger, g.conf)
	atxdb := newAtxDB(g.logger, mdb, g.conf)

	g.beacons = &beaconStore{}
	g.State = State{MeshDB: mdb, AtxDB: atxdb, Beacons: g.beacons}
	return g
}

// Generator for layers of blocks.
type Generator struct {
	logger log.Log
	rng    *rand.Rand
	conf   config

	State State

	nextLayer types.LayerID
	// key is when to return => value is the layer to return
	reordered   map[types.LayerID]types.LayerID
	layers      []*types.Layer
	activations []types.ATXID

	beacons *beaconStore
	keys    []*signing.EdSigner
}

// SetupOpt configures setup.
type SetupOpt func(g *setupConf)

// WithSetupMinerRange number of miners will be selected between low and high values.
func WithSetupMinerRange(low, high int) SetupOpt {
	return func(conf *setupConf) {
		conf.Miners = [2]int{low, high}
	}
}

// WithSetupUnitsRange adjusts units of the ATXs, which will directly affect block weight.
func WithSetupUnitsRange(low, high int) SetupOpt {
	return func(conf *setupConf) {
		conf.Units = [2]int{low, high}
	}
}

func withBeacon(beacon []byte) SetupOpt {
	return func(conf *setupConf) {
		conf.Beacon = beacon
	}
}

type setupConf struct {
	Miners [2]int
	Units  [2]int
	Beacon []byte
}

func defaultSetupConf() setupConf {
	return setupConf{
		Miners: [2]int{30, 100},
		Units:  [2]int{1, 10},
	}
}

// Setup should be called before running Next.
func (g *Generator) Setup(opts ...SetupOpt) {
	conf := defaultSetupConf()
	for _, opt := range opts {
		opt(&conf)
	}

	if len(g.layers) == 0 {
		g.layers = append(g.layers, mesh.GenesisLayer())
	}
	last := g.layers[len(g.layers)-1]
	g.nextLayer = last.Index().Add(1)

	// TODO(dshulyak) this needs to be improved to store multiple beacons
	// after partition - the previous beacon should remain in the store
	if conf.Beacon != nil {
		g.beacons.beacon = conf.Beacon
	} else {
		g.beacons.beacon = make([]byte, 32)
		g.rng.Read(g.beacons.beacon)
	}

	miners := intInRange(g.rng, conf.Miners)
	g.activations = make([]types.ATXID, miners)

	for i := range g.activations {
		units := intInRange(g.rng, conf.Units)
		address := types.Address{}
		_, _ = g.rng.Read(address[:])
		atx := types.NewActivationTx(types.NIPostChallenge{
			NodeID:    types.NodeID{Key: address.Hex()},
			StartTick: 1,
			EndTick:   2,
		}, address, nil, uint(units), nil)
		g.activations[i] = atx.ID()
		if err := g.State.AtxDB.StoreAtx(context.Background(), g.nextLayer.GetEpoch(), atx); err != nil {
			g.logger.With().Panic("failed to store atx", log.Err(err))
		}

		g.keys = append(g.keys, signing.NewEdSignerFromRand(g.rng))
	}
}
