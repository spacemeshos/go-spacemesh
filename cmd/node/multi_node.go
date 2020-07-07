package node

import (
	"github.com/spacemeshos/amcl"
	"github.com/spacemeshos/amcl/BLS381"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/collector"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"strconv"
	"sync"
	"time"
)

// ManualClock is a clock that releases ticks on demand and not according to a real world clock
type ManualClock struct {
	subs          map[timesync.LayerTimer]struct{}
	layerChannels map[types.LayerID]chan struct{}
	m             sync.Mutex
	currentLayer  types.LayerID
	genesisTime   time.Time
}

// LayerToTime returns the time of the provided layer
func (clk *ManualClock) LayerToTime(types.LayerID) time.Time {
	return time.Now().Add(1000 * time.Hour) // hack so this wont take affect in the mock
}

// NewManualClock creates a new manual clock struct
func NewManualClock(genesisTime time.Time) *ManualClock {
	t := &ManualClock{
		subs:          make(map[timesync.LayerTimer]struct{}),
		layerChannels: make(map[types.LayerID]chan struct{}),
		currentLayer:  0, // genesis
		genesisTime:   genesisTime,
	}
	return t
}

// Unsubscribe removes this channel ch from channels notified on tick
func (clk *ManualClock) Unsubscribe(ch timesync.LayerTimer) {
	clk.m.Lock()
	delete(clk.subs, ch)
	clk.m.Unlock()
}

// StartNotifying is empty because this clock is manual
func (clk *ManualClock) StartNotifying() {
}

var closedChannel chan struct{}

func init() {
	closedChannel = make(chan struct{})
	close(closedChannel)
}

// AwaitLayer implement the ability to notify a subscriber when a layer has ticked
func (clk *ManualClock) AwaitLayer(layerID types.LayerID) chan struct{} {
	clk.m.Lock()
	defer clk.m.Unlock()
	if layerID <= clk.currentLayer {
		return closedChannel
	}
	if ch, found := clk.layerChannels[layerID]; found {
		return ch
	}
	ch := make(chan struct{})
	clk.layerChannels[layerID] = ch
	return ch
}

// Subscribe allow subscribes to be notified when a layer ticks
func (clk *ManualClock) Subscribe() timesync.LayerTimer {
	ch := make(timesync.LayerTimer)
	clk.m.Lock()
	clk.subs[ch] = struct{}{}
	clk.m.Unlock()
	return ch
}

// Tick notifies all subscribers to this clock
func (clk *ManualClock) Tick() {
	clk.m.Lock()
	defer clk.m.Unlock()

	clk.currentLayer++
	if ch, found := clk.layerChannels[clk.currentLayer]; found {
		close(ch)
		delete(clk.layerChannels, clk.currentLayer)
	}
	for s := range clk.subs {
		s <- clk.currentLayer
	}
}

// GetCurrentLayer gets the last ticked layer
func (clk *ManualClock) GetCurrentLayer() types.LayerID {
	clk.m.Lock()
	defer clk.m.Unlock()

	return clk.currentLayer
}

// GetGenesisTime returns the set genesis time for this clock
func (clk *ManualClock) GetGenesisTime() time.Time {
	clk.m.Lock()
	defer clk.m.Unlock()
	return clk.genesisTime
}

// Close does nothing because this clock is manual
func (clk *ManualClock) Close() {}

func getTestDefaultConfig() *config.Config {
	cfg, err := LoadConfigFromFile()
	if err != nil {
		log.Error("cannot load config from file")
		return nil
	}

	cfg.POST = activation.DefaultConfig()
	cfg.POST.Difficulty = 5
	cfg.POST.NumProvenLabels = 10
	cfg.POST.SpacePerUnit = 1 << 10 // 1KB.
	cfg.POST.NumFiles = 1

	cfg.HARE.N = 5
	cfg.HARE.F = 2
	cfg.HARE.RoundDuration = 3
	cfg.HARE.WakeupDelta = 5
	cfg.HARE.ExpectedLeaders = 5
	cfg.HARE.SuperHare = true
	cfg.LayerAvgSize = 5
	cfg.LayersPerEpoch = 3
	cfg.TxsPerBlock = 100
	cfg.Hdist = 5

	cfg.LayerDurationSec = 20
	cfg.HareEligibility.ConfidenceParam = 4
	cfg.HareEligibility.EpochOffset = 0
	cfg.StartMining = true
	cfg.SyncRequestTimeout = 500
	cfg.SyncInterval = 2
	cfg.SyncValidationDelta = 5
	types.SetLayersPerEpoch(int32(cfg.LayersPerEpoch))
	return cfg
}

// ActivateGrpcServer starts a grpc server on the provided node
func ActivateGrpcServer(smApp *SpacemeshApp) {
	smApp.Config.API.StartGrpcServer = true
	layerDuration := smApp.Config.LayerDurationSec
	smApp.grpcAPIService = api.NewGrpcService(smApp.Config.API.GrpcServerPort, smApp.P2P, smApp.state, smApp.mesh, smApp.txPool, smApp.atxBuilder, smApp.oracle, smApp.clock, nil, layerDuration, nil, nil, nil)
	smApp.grpcAPIService.StartService()
}

// GracefulShutdown stops the current services running in apps
func GracefulShutdown(apps []*SpacemeshApp) {
	log.Info("Graceful shutdown begin")

	var wg sync.WaitGroup
	for _, app := range apps {
		wg.Add(1)
		go func(app *SpacemeshApp) {
			app.stopServices()
			wg.Done()
		}(app)
	}
	wg.Wait()

	log.Info("Graceful shutdown end")
}

type network interface {
	NewNode() *service.Node
}

// initialize a network mock object to simulate network between nodes.
// var net = service.NewSimulator()

// InitSingleInstance initializes a node instance with given
// configuration and parameters, it does not stop the instance.
func InitSingleInstance(cfg config.Config, i int, genesisTime string, rng *amcl.RAND, storePath string, rolacle *eligibility.FixedRolacle, poetClient *activation.HTTPPoetClient, clock TickProvider, net network) (*SpacemeshApp, error) {

	smApp := NewSpacemeshApp()
	smApp.Config = &cfg
	smApp.Config.CoinbaseAccount = strconv.Itoa(i + 1)
	smApp.Config.GenesisTime = genesisTime
	edSgn := signing.NewEdSigner()
	pub := edSgn.PublicKey()

	vrfPriv, vrfPub := BLS381.GenKeyPair(rng)
	vrfSigner := BLS381.NewBlsSigner(vrfPriv)
	nodeID := types.NodeID{Key: pub.String(), VRFPublicKey: vrfPub}

	swarm := net.NewNode()
	dbStorepath := storePath

	hareOracle := newLocalOracle(rolacle, 5, nodeID)
	hareOracle.Register(true, pub.String())

	postClient, err := activation.NewPostClient(&smApp.Config.POST, util.Hex2Bytes(nodeID.Key))
	if err != nil {
		return nil, err
	}

	err = smApp.initServices(nodeID, swarm, dbStorepath, edSgn, false, hareOracle, uint32(smApp.Config.LayerAvgSize), postClient, poetClient, vrfSigner, uint16(smApp.Config.LayersPerEpoch), clock)
	if err != nil {
		return nil, err
	}

	return smApp, err
}

// StartMultiNode Starts the run of a number of nodes, running in process consensus between them.
// this also runs a single transaction between the nodes.
func StartMultiNode(numOfinstances, layerAvgSize int, runTillLayer uint32, dbPath string) {
	cfg := getTestDefaultConfig()
	cfg.LayerAvgSize = layerAvgSize
	numOfInstances := numOfinstances
	net := service.NewSimulator()
	path := dbPath + time.Now().Format(time.RFC3339)

	genesisTime := time.Now().Add(20 * time.Second).Format(time.RFC3339)

	poetHarness, err := activation.NewHTTPPoetHarness(false)
	if err != nil {
		log.Panic("failed creating poet client harness: %v", err)
	}
	defer func() {
		err := poetHarness.Teardown(true)
		if err != nil {
			log.With().Error("failed to tear down poet harness", log.Err(err))
		}
	}()

	rolacle := eligibility.New()
	rng := BLS381.DefaultSeed()
	gTime, err := time.Parse(time.RFC3339, genesisTime)
	if err != nil {
		log.Error("cannot parse genesis time %v", err)
	}
	pubsubAddr := "tcp://localhost:55666"
	events.InitializeEventReporter(pubsubAddr)
	clock := NewManualClock(gTime)

	apps := make([]*SpacemeshApp, 0, numOfInstances)
	name := 'a'
	for i := 0; i < numOfInstances; i++ {
		dbStorepath := path + string(name)
		database.SwitchCreationContext(dbStorepath, string(name))
		smApp, err := InitSingleInstance(*cfg, i, genesisTime, rng, dbStorepath, rolacle, poetHarness.HTTPPoetClient, clock, net)
		if err != nil {
			log.Error("cannot run multi node %v", err)
			return
		}
		apps = append(apps, smApp)
		name++
	}

	eventDb := collector.NewMemoryCollector()
	collect := collector.NewCollector(eventDb, pubsubAddr)
	for _, a := range apps {
		a.startServices()
	}
	collect.Start(false)
	ActivateGrpcServer(apps[0])

	if err := poetHarness.Start([]string{"127.0.0.1:9091"}); err != nil {
		log.Panic("failed to start poet server: %v", err)
	}

	// startInLayer := 5 // delayed pod will start in this layer
	defer GracefulShutdown(apps)

	timeout := time.After(time.Duration(runTillLayer*60) * time.Second)

	// stickyClientsDone := 0
	startLayer := time.Now()
	clock.Tick()
	errors := 0
loop:
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			log.Panic("run timed out", err)
			return
		default:
			if errors > 100 {
				log.Panic("too many errors and retries")
				break loop
			}
			layer := clock.GetCurrentLayer()

			if layer.GetEpoch().IsGenesis() {
				time.Sleep(20 * time.Second)
				clock.Tick()
				continue
			}

			if eventDb.GetBlockCreationDone(layer) < numOfInstances {
				log.Info("blocks done in layer %v: %v", layer, eventDb.GetBlockCreationDone(layer))
				time.Sleep(500 * time.Millisecond)
				errors++
				continue
			}
			log.Info("all miners tried to create block in %v", layer)
			if eventDb.GetNumOfCreatedBlocks(layer)*numOfInstances != eventDb.GetReceivedBlocks(layer) {
				log.Info("finished: %v, block received %v layer %v", eventDb.GetNumOfCreatedBlocks(layer), eventDb.GetReceivedBlocks(layer), layer)
				time.Sleep(500 * time.Millisecond)
				errors++
				continue
			}
			log.Info("all miners got blocks for layer: %v created: %v received: %v", layer, eventDb.GetNumOfCreatedBlocks(layer), eventDb.GetReceivedBlocks(layer))
			epoch := layer.GetEpoch()
			if !(eventDb.GetAtxCreationDone(epoch) >= numOfInstances && eventDb.GetAtxCreationDone(epoch)%numOfInstances == 0) {
				log.Info("atx not created %v in epoch %v, created only %v atxs", numOfInstances-eventDb.GetAtxCreationDone(epoch), epoch, eventDb.GetAtxCreationDone(epoch))
				time.Sleep(500 * time.Millisecond)
				errors++
				continue
			}
			log.Info("all miners finished reading %v atxs, layer %v done in %v", eventDb.GetAtxCreationDone(epoch), layer, time.Since(startLayer))
			for _, atxID := range eventDb.GetCreatedAtx(epoch) {
				if _, found := eventDb.Atxs[atxID]; !found {
					log.Info("atx %v not propagated", atxID)
					errors++
					continue
				}
			}
			errors = 0

			startLayer = time.Now()
			clock.Tick()

			if apps[0].mesh.LatestLayer() >= types.LayerID(runTillLayer) {
				break loop
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	collect.Stop()
}
