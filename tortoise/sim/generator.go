package sim

import (
	"context"
	"math/rand"
	"time"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// GenOpt for configuring Generator.
type GenOpt func(*Generator)

// WithSeed configures seed for Generator. By default current time used as a seed.
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

// WithPath configures path for persist storage.
func WithPath(path string) GenOpt {
	return func(g *Generator) {
		g.conf.Path = path
	}
}

type config struct {
	FirstLayer     types.LayerID
	LayerSize      uint32
	LayersPerEpoch uint32
	Path           string
}

func defaults() config {
	return config{
		LayerSize:      30,
		FirstLayer:     types.GetEffectiveGenesis().Add(1),
		LayersPerEpoch: types.GetLayersPerEpoch(),
	}
}

// State is state that can be used by tortoise.
type State struct {
	MeshDB *mesh.DB
	AtxDB  *activation.DB
}

// New creates Generator instance.
func New(opts ...GenOpt) *Generator {
	g := &Generator{
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
		conf:   defaults(),
		logger: log.NewNop(),
	}
	for _, opt := range opts {
		opt(g)
	}
	mdb := mesh.NewMemMeshDB(g.logger)
	atxdb := newAtxDB(g.logger, mdb, g.conf)

	g.State = State{MeshDB: mdb, AtxDB: atxdb}
	g.layers = append(g.layers, mesh.GenesisLayer())
	g.nextLayer = g.conf.FirstLayer
	return g
}

// Generator for layers of blocks.
type Generator struct {
	logger log.Log
	rng    *rand.Rand
	conf   config

	State State

	nextLayer   types.LayerID
	layers      []*types.Layer
	activations []types.ATXID
	keys        []*signing.EdSigner
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

type setupConf struct {
	Miners [2]int
	Units  [2]int
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

// Next generates next layer.
func (g *Generator) Next() types.LayerID {
	half := int(g.conf.LayerSize) / 2
	numblocks := intInRange(g.rng, [2]int{half, half * 3})
	layer := types.NewLayer(g.nextLayer)
	for i := 0; i < numblocks; i++ {
		miner := g.rng.Intn(len(g.activations))
		atxid := g.activations[miner]
		signer := g.keys[miner]

		// TODO base block selection algorithm must be selected as an option
		// so that erroneous base block selection can be defined as a failure condition

		baseLayer := g.layers[len(g.layers)-1]
		support := g.layers[len(g.layers)-1].BlocksIDs()
		base := baseLayer.Blocks()[g.rng.Intn(len(baseLayer.Blocks()))]

		data := make([]byte, 20)
		g.rng.Read(data)
		block := &types.Block{
			MiniBlock: types.MiniBlock{
				BlockHeader: types.BlockHeader{
					LayerIndex: g.nextLayer,
					ATXID:      atxid,
					BaseBlock:  base.ID(),
					ForDiff:    support,
					Data:       data,
				},
			},
		}
		block.Signature = signer.Sign(block.Bytes())
		block.Initialize()
		if err := g.State.MeshDB.AddBlock(block); err != nil {
			g.logger.With().Panic("failed to add block", log.Err(err))
		}
		layer.AddBlock(block)
	}
	// TODO saving layer input should be skipped as one of the error conditions
	if err := g.State.MeshDB.SaveLayerInputVectorByID(context.Background(), layer.Index(), layer.BlocksIDs()); err != nil {
		g.logger.With().Panic("failed to save layer input vecotor", log.Err(err))
	}
	g.layers = append(g.layers, layer)
	g.nextLayer = g.nextLayer.Add(1)
	return layer.Index()
}
