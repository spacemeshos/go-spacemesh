package sim

import (
	"math/rand"
	"time"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
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

// WithStates creates n states.
func WithStates(n int) GenOpt {
	if n == 0 {
		panic("generator without attached state is not supported")
	}
	return func(g *Generator) {
		g.conf.StateInstances = n
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

type config struct {
	Path           string
	LayerSize      uint32
	LayersPerEpoch uint32
	StateInstances int
}

func defaults() config {
	return config{
		LayerSize:      30,
		LayersPerEpoch: types.GetLayersPerEpoch(),
		StateInstances: 1,
	}
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
	// TODO support multiple persist states.
	for i := 0; i < g.conf.StateInstances; i++ {
		g.states = append(g.states, newState(g.logger, g.conf))
	}
	return g
}

// Generator for layers of blocks.
type Generator struct {
	logger log.Log
	rng    *rand.Rand
	conf   config

	states []State

	nextLayer types.LayerID
	// key is when to return => value is the layer to return
	reordered map[types.LayerID]types.LayerID
	layers    []*types.Layer
	units     [2]int

	activations []types.ATXID
	ticksRange  [2]int
	ticks       []uint64
	prevHeight  []uint64

	keys []*signing.EdSigner
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
		conf.UnitsRange = [2]int{low, high}
	}
}

// WithSetupTicksRange configures range of atxs, that will be randomly chosen by atxs.
func WithSetupTicksRange(low, high int) SetupOpt {
	return func(conf *setupConf) {
		conf.TicksRange = [2]int{low, high}
	}
}

// WithSetupTicks configures ticks for every atx.
func WithSetupTicks(ticks ...uint64) SetupOpt {
	return func(conf *setupConf) {
		if len(ticks) > 0 {
			conf.Ticks = ticks
		}
	}
}

type setupConf struct {
	Miners     [2]int
	UnitsRange [2]int
	TicksRange [2]int
	Ticks      []uint64
}

func defaultSetupConf() setupConf {
	return setupConf{
		Miners:     [2]int{30, 30},
		UnitsRange: [2]int{10, 10},
		TicksRange: [2]int{10, 10},
	}
}

// GetState at index.
func (g *Generator) GetState(i int) State {
	return g.states[i]
}

func (g *Generator) addState(state State) {
	g.states = append(g.states, state)
}

func (g *Generator) popState(i int) State {
	state := g.states[i]
	copy(g.states[i:], g.states[i+1:])
	g.states[len(g.states)-1] = State{}
	g.states = g.states[:len(g.states)-1]
	return state
}

// Setup should be called before running Next.
func (g *Generator) Setup(opts ...SetupOpt) {
	conf := defaultSetupConf()
	for _, opt := range opts {
		opt(&conf)
	}
	if conf.Ticks != nil && conf.Miners[0] != conf.Miners[1] && len(conf.Ticks) != conf.Miners[0] {
		g.logger.Panic("if conf.Ticks is provided it should be equal to the constant number of conf.Miners")
	}
	g.units = conf.UnitsRange
	g.ticks = conf.Ticks
	g.ticksRange = conf.TicksRange
	if len(g.layers) == 0 {
		genesis := types.NewLayer(types.GetEffectiveGenesis())
		ballot := &types.Ballot{}
		ballot.Layer = genesis.Index()
		genesis.AddBallot(ballot)
		g.layers = append(g.layers, genesis)
	}
	last := g.layers[len(g.layers)-1]
	g.nextLayer = last.Index().Add(1)

	miners := intInRange(g.rng, conf.Miners)
	g.activations = make([]types.ATXID, miners)
	g.prevHeight = make([]uint64, miners)

	for i := uint32(0); i < miners; i++ {
		sig, err := signing.NewEdSigner(signing.WithKeyFromRand(g.rng))
		if err != nil {
			panic(err)
		}
		g.keys = append(g.keys, sig)
	}
}

func (g *Generator) generateAtxs() {
	for i := range g.activations {
		units := intInRange(g.rng, g.units)
		sig, err := signing.NewEdSigner()
		if err != nil {
			panic(err)
		}
		address := types.GenerateAddress(sig.PublicKey().Bytes())

		nipost := types.NIPostChallenge{
			PubLayerID: g.nextLayer.Sub(1),
		}
		atx := types.NewActivationTx(nipost, address, nil, units, nil, nil)
		var ticks uint64
		if g.ticks != nil {
			ticks = g.ticks[i]
		} else {
			ticks = uint64(intInRange(g.rng, g.ticksRange))
		}
		if err := activation.SignAndFinalizeAtx(sig, atx); err != nil {
			panic(err)
		}
		atx.SetEffectiveNumUnits(atx.NumUnits)
		atx.SetReceived(time.Now())
		vAtx, err := atx.Verify(g.prevHeight[i], ticks)
		if err != nil {
			panic(err)
		}

		g.prevHeight[i] += ticks
		g.activations[i] = atx.ID()

		for _, state := range g.states {
			state.OnActivationTx(vAtx)
		}
	}
}

// Layer returns generated layer.
func (g *Generator) Layer(i int) *types.Layer {
	return g.layers[i+1]
}
