package node

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"sync"
	"time"

	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/spacemeshos/post/initialization"

	"github.com/spacemeshos/go-spacemesh/activation"
	apiConfig "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/beacon"
	"github.com/spacemeshos/go-spacemesh/collector"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

// ManualClock is a clock that releases ticks on demand and not according to a real world clock.
type ManualClock struct {
	subs          map[timesync.LayerTimer]struct{}
	layerChannels map[types.LayerID]chan struct{}
	m             sync.Mutex
	currentLayer  types.LayerID
	genesisTime   time.Time
}

// LayerToTime returns the time of the provided layer.
func (clk *ManualClock) LayerToTime(types.LayerID) time.Time {
	return time.Now().Add(1000 * time.Hour) // hack so this wont take affect in the mock
}

// NewManualClock creates a new manual clock struct.
func NewManualClock(genesisTime time.Time) *ManualClock {
	t := &ManualClock{
		subs:          make(map[timesync.LayerTimer]struct{}),
		layerChannels: make(map[types.LayerID]chan struct{}),
		genesisTime:   genesisTime,
	}
	return t
}

// Unsubscribe removes this channel ch from channels notified on tick.
func (clk *ManualClock) Unsubscribe(ch timesync.LayerTimer) {
	clk.m.Lock()
	delete(clk.subs, ch)
	clk.m.Unlock()
}

// StartNotifying is empty because this clock is manual.
func (clk *ManualClock) StartNotifying() {
}

var closedChannel chan struct{}

func init() {
	closedChannel = make(chan struct{})
	close(closedChannel)
}

// AwaitLayer implement the ability to notify a subscriber when a layer has ticked.
func (clk *ManualClock) AwaitLayer(layerID types.LayerID) chan struct{} {
	clk.m.Lock()
	defer clk.m.Unlock()
	if !layerID.After(clk.currentLayer) {
		return closedChannel
	}
	if ch, found := clk.layerChannels[layerID]; found {
		return ch
	}
	ch := make(chan struct{})
	clk.layerChannels[layerID] = ch
	return ch
}

// Subscribe allow subscribes to be notified when a layer ticks.
func (clk *ManualClock) Subscribe() timesync.LayerTimer {
	ch := make(timesync.LayerTimer)
	clk.m.Lock()
	clk.subs[ch] = struct{}{}
	clk.m.Unlock()
	return ch
}

// Tick notifies all subscribers to this clock.
func (clk *ManualClock) Tick() {
	clk.m.Lock()
	defer clk.m.Unlock()

	clk.currentLayer = clk.currentLayer.Add(1)
	if ch, found := clk.layerChannels[clk.currentLayer]; found {
		close(ch)
		delete(clk.layerChannels, clk.currentLayer)
	}
	for s := range clk.subs {
		s <- clk.currentLayer
	}
}

// GetCurrentLayer gets the last ticked layer.
func (clk *ManualClock) GetCurrentLayer() types.LayerID {
	clk.m.Lock()
	defer clk.m.Unlock()

	return clk.currentLayer
}

// GetGenesisTime returns the set genesis time for this clock.
func (clk *ManualClock) GetGenesisTime() time.Time {
	clk.m.Lock()
	defer clk.m.Unlock()
	return clk.genesisTime
}

// Close does nothing because this clock is manual.
func (clk *ManualClock) Close() {}

func getTestDefaultConfig() *config.Config {
	cfg, err := LoadConfigFromFile()
	if err != nil {
		log.Error("cannot load config from file")
		return nil
	}
	// is set to 0 to make sync start immediately when node starts
	cfg.P2P.TargetOutbound = 0

	cfg.POST = activation.DefaultPostConfig()
	cfg.POST.MinNumUnits = 2
	cfg.POST.MaxNumUnits = 4
	cfg.POST.LabelsPerUnit = 32
	cfg.POST.BitsPerLabel = 8
	cfg.POST.K2 = 4

	cfg.SMESHING = config.DefaultSmeshingConfig()
	cfg.SMESHING.Start = true
	cfg.SMESHING.Opts.NumUnits = cfg.POST.MinNumUnits + 1
	cfg.SMESHING.Opts.NumFiles = 1
	cfg.SMESHING.Opts.ComputeProviderID = initialization.CPUProviderID()

	// note: these need to be set sufficiently low enough that turbohare finishes well before the LayerDurationSec
	cfg.HARE.RoundDuration = 2
	cfg.HARE.WakeupDelta = 1
	cfg.HARE.N = 5
	cfg.HARE.F = 2
	cfg.HARE.ExpectedLeaders = 5
	cfg.HARE.SuperHare = true
	cfg.LayerAvgSize = 5
	cfg.LayersPerEpoch = 3
	cfg.TxsPerBlock = 100
	cfg.Tortoise.Hdist = 5
	cfg.Tortoise.Zdist = 5

	cfg.LayerDurationSec = 20
	cfg.HareEligibility.ConfidenceParam = 4
	cfg.HareEligibility.EpochOffset = 0
	cfg.SyncRequestTimeout = 500
	cfg.SyncInterval = 2

	cfg.FETCH.RequestTimeout = 10
	cfg.FETCH.MaxRetriesForPeer = 5
	cfg.FETCH.BatchSize = 5
	cfg.FETCH.BatchTimeout = 5
	cfg.GoldenATXID = "0x5678"

	cfg.Beacon = beacon.NodeSimUnitTestConfig()

	cfg.Genesis = apiConfig.DefaultTestGenesisConfig()

	types.SetLayersPerEpoch(cfg.LayersPerEpoch)

	return cfg
}

// ActivateGrpcServer starts a grpc server on the provided node.
func ActivateGrpcServer(smApp *App) {
	// Activate the API services used by app_test
	smApp.Config.API.StartGatewayService = true
	smApp.Config.API.StartGlobalStateService = true
	smApp.Config.API.StartTransactionService = true
	smApp.Config.API.GrpcServerPort = 9094
	smApp.grpcAPIService = grpcserver.NewServerWithInterface(smApp.Config.API.GrpcServerPort, smApp.Config.API.GrpcServerInterface)
	smApp.gatewaySvc = grpcserver.NewGatewayService(smApp.host)
	smApp.globalstateSvc = grpcserver.NewGlobalStateService(smApp.mesh, smApp.txPool)
	smApp.txService = grpcserver.NewTransactionService(smApp.host, smApp.mesh, smApp.txPool, smApp.syncer)
	smApp.gatewaySvc.RegisterService(smApp.grpcAPIService)
	smApp.globalstateSvc.RegisterService(smApp.grpcAPIService)
	smApp.txService.RegisterService(smApp.grpcAPIService)
	smApp.grpcAPIService.Start()
}

// GracefulShutdown stops the current services running in apps.
func GracefulShutdown(apps []*App) {
	log.Info("graceful shutdown begin")

	var wg sync.WaitGroup
	for _, app := range apps {
		wg.Add(1)
		go func(app *App) {
			app.stopServices()
			wg.Done()
		}(app)
	}
	wg.Wait()

	log.Info("graceful shutdown end")
}

// InitSingleInstance initializes a node instance with given
// configuration and parameters, it does not stop the instance.
func InitSingleInstance(lg log.Log, cfg config.Config, i int, genesisTime string, storePath string, rolacle *eligibility.FixedRolacle,
	poetClient *activation.HTTPPoetClient, clock TickProvider, host *p2p.Host, edSgn *signing.EdSigner) (*App, error) {
	smApp := New(WithLog(lg))
	smApp.Config = &cfg
	smApp.Config.GenesisTime = genesisTime

	smApp.Config.SMESHING.CoinbaseAccount = strconv.Itoa(i + 1)
	smApp.Config.SMESHING.Opts.DataDir, _ = ioutil.TempDir("", "sm-app-test-post-datadir")

	smApp.host = host
	smApp.edSgn = edSgn

	pub := edSgn.PublicKey()
	vrfSigner, vrfPub, err := signing.NewVRFSigner(pub.Bytes())
	if err != nil {
		return nil, fmt.Errorf("create VRF signer: %w", err)
	}

	nodeID := types.NodeID{Key: pub.String(), VRFPublicKey: vrfPub}

	dbStorepath := storePath

	hareOracle := newLocalOracle(rolacle, 5, nodeID)
	hareOracle.Register(true, pub.String())

	err = smApp.initServices(context.TODO(), nodeID, dbStorepath, edSgn, false, hareOracle,
		uint32(smApp.Config.LayerAvgSize), poetClient, vrfSigner, smApp.Config.LayersPerEpoch, clock)
	if err != nil {
		return nil, err
	}

	return smApp, err
}

// StartMultiNode Starts the run of a number of nodes, running in process consensus between them.
// this also runs a single transaction between the nodes.
func StartMultiNode(logger log.Log, numOfInstances, layerAvgSize int, runTillLayer uint32, dbPath string) {
	cfg := getTestDefaultConfig()
	cfg.LayerAvgSize = layerAvgSize
	mesh, err := mocknet.FullMeshLinked(context.Background(), numOfInstances)
	if err != nil {
		logger.With().Panic("failed to initialize mocknet", log.Err(err))
	}
	path := dbPath + time.Now().Format(time.RFC3339)

	genesisTime := time.Now().Add(20 * time.Second).Format(time.RFC3339)

	poetHarness, err := activation.NewHTTPPoetHarness(false)
	if err != nil {
		logger.With().Panic("failed creating poet client harness", log.Err(err))
	}
	defer func() {
		err := poetHarness.Teardown(true)
		if err != nil {
			logger.With().Error("failed to tear down poet harness", log.Err(err))
		}
	}()

	rolacle := eligibility.New(logger)
	gTime, err := time.Parse(time.RFC3339, genesisTime)
	if err != nil {
		logger.With().Error("cannot parse genesis time", log.Err(err))
	}
	events.CloseEventPubSub()
	pubsubAddr := "tcp://localhost:55666"
	if err := events.InitializeEventReporter(pubsubAddr); err != nil {
		logger.With().Error("error initializing event reporter", log.Err(err))
	}
	clock := NewManualClock(gTime)

	apps := make([]*App, 0, numOfInstances)
	name := 'a'
	for i := 0; i < numOfInstances; i++ {
		dbStorepath := path + string(name)
		edSgn := signing.NewEdSigner()
		host, err := p2p.Upgrade(mesh.Hosts()[i], p2p.WithLog(logger), p2p.WithConfig(cfg.P2P))
		if err != nil {
			logger.With().Error("failed to initialize host", log.Err(err))
			return
		}
		smApp, err := InitSingleInstance(logger, *cfg, i, genesisTime, dbStorepath, rolacle, poetHarness.HTTPPoetClient, clock, host, edSgn)
		if err != nil {
			logger.With().Error("cannot run multi node", log.Err(err))
			return
		}
		apps = append(apps, smApp)
		name++
	}
	mesh.ConnectAllButSelf()

	eventDb := collector.NewMemoryCollector()
	collect := collector.NewCollector(eventDb, pubsubAddr)
	for _, a := range apps {
		a.startServices(context.TODO())
	}
	collect.Start(false)
	ActivateGrpcServer(apps[0])

	go func() {
		r := bufio.NewReader(poetHarness.Stdout)
		for {
			line, _, err := r.ReadLine()
			if errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) {
				return
			}
			if err != nil {
				logger.Error("failed to read poet stdout: %v", err)
				return
			}

			logger.Info("[poet stdout] %v\n", string(line))
		}
	}()
	go func() {
		r := bufio.NewReader(poetHarness.Stderr)
		for {
			line, _, err := r.ReadLine()
			if errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) {
				return
			}
			if err != nil {
				logger.Error("failed to read poet stderr: %v", err)
				return
			}

			logger.Info("[poet stderr] %v", string(line))
		}
	}()

	if err := poetHarness.Start(context.TODO(), []string{"127.0.0.1:9094"}); err != nil {
		logger.Panic("failed to start poet server: %v", err)
	}

	defer GracefulShutdown(apps)

	timeout := time.After(time.Duration(runTillLayer*60) * time.Second)

	startLayer := time.Now()
	clock.Tick()
	errors := 0
loop:
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			logger.Panic("run timed out", err)
			return
		default:
			if errors > 100 {
				logger.Panic("too many errors and retries")
				break loop
			}
			layer := clock.GetCurrentLayer()

			if layer.GetEpoch().IsGenesis() {
				time.Sleep(20 * time.Second)
				clock.Tick()
				continue
			}

			if eventDb.GetBlockCreationDone(layer) < numOfInstances {
				logger.Warning("blocks done in layer %v: %v", layer, eventDb.GetBlockCreationDone(layer))
				time.Sleep(500 * time.Millisecond)
				errors++
				continue
			}
			log.Info("all miners tried to create block in %v", layer)
			if eventDb.GetNumOfCreatedBlocks(layer)*numOfInstances != eventDb.GetReceivedBlocks(layer) {
				logger.Warning("finished: %v, block received %v layer %v", eventDb.GetNumOfCreatedBlocks(layer), eventDb.GetReceivedBlocks(layer), layer)
				time.Sleep(500 * time.Millisecond)
				errors++
				continue
			}
			logger.Info("all miners got blocks for layer: %v created: %v received: %v", layer, eventDb.GetNumOfCreatedBlocks(layer), eventDb.GetReceivedBlocks(layer))
			epoch := layer.GetEpoch()
			if !(eventDb.GetAtxCreationDone(epoch) >= numOfInstances && eventDb.GetAtxCreationDone(epoch)%numOfInstances == 0) {
				logger.Warning("atx not created %v in epoch %v, created only %v atxs", numOfInstances-eventDb.GetAtxCreationDone(epoch), epoch, eventDb.GetAtxCreationDone(epoch))
				time.Sleep(500 * time.Millisecond)
				errors++
				continue
			}
			logger.Info("all miners finished reading %v atxs, layer %v done in %v", eventDb.GetAtxCreationDone(epoch), layer, time.Since(startLayer))
			for _, atxID := range eventDb.GetCreatedAtx(epoch) {
				if !eventDb.AtxIDExists(atxID) {
					logger.Warning("atx %v not propagated", atxID)
					errors++
					continue
				}
			}
			beacons := eventDb.GetBeacon(epoch)
			logger.Info("all miners finished calculating %v beacons, epoch %v done in %v", len(beacons), epoch, time.Since(startLayer))
			if len(beacons) != 0 {
				first := beacons[0]
				for _, beacon := range beacons {
					if first != beacon {
						logger.Info("beacons %v and %v differ", first, beacon)
						errors++
						continue
					}
				}
			}

			errors = 0

			startLayer = time.Now()
			clock.Tick()

			if !apps[0].mesh.LatestLayer().Before(types.NewLayerID(runTillLayer)) {
				break loop
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	events.CloseEventReporter()
	events.CloseEventPubSub()
	collect.Stop()
}
