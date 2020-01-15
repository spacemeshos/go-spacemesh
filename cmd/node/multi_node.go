package node

import (
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/amcl"
	"github.com/spacemeshos/go-spacemesh/amcl/BLS381"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/collector"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/oracle"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/poet/integration"
	"strconv"
	"sync"
	"time"
)

func getTestDefaultConfig() *config.Config {
	cfg, err := LoadConfigFromFile()
	if err != nil {
		log.Error("cannot load config from file")
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
	cfg.Hdist = 5

	cfg.LayerDurationSec = 20
	cfg.HareEligibility.ConfidenceParam = 4
	cfg.HareEligibility.EpochOffset = 0
	cfg.StartMining = true
	cfg.SyncRequestTimeout = 2000
	return cfg
}

// ActivateGrpcServer starts a grpc server on the provided node
func ActivateGrpcServer(smApp *SpacemeshApp) {
	smApp.Config.API.StartGrpcServer = true
	layerDuration := smApp.Config.LayerDurationSec
	smApp.grpcAPIService = api.NewGrpcService(smApp.Config.API.GrpcServerPort, smApp.P2P, smApp.state, smApp.mesh, smApp.txPool, smApp.atxBuilder, smApp.oracle, smApp.clock, nil, layerDuration, nil)
	smApp.grpcAPIService.StartService()
}

// NewRPCPoetHarnessClient returns a new instance of RPCPoetClient
// which utilizes a local self-contained poet server instance
// in order to exercise functionality.
func NewRPCPoetHarnessClient() (*activation.RPCPoetClient, error) {
	cfg, err := integration.DefaultConfig()
	if err != nil {
		return nil, err
	}

	cfg.N = 10
	cfg.InitialRoundDuration = time.Duration(3 * time.Second).String()

	h, err := integration.NewHarness(cfg)
	if err != nil {
		return nil, err
	}

	return activation.NewRPCPoetClient(h.PoetClient, h.TearDown), nil
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

//initialize a network mock object to simulate network between nodes.
var net = service.NewSimulator()

// InitSingleInstance initializes a node instance with given
// configuration and parameters, it does not stop the instance.
func InitSingleInstance(cfg config.Config, i int, genesisTime string, rng *amcl.RAND, storePath string, rolacle *eligibility.FixedRolacle, poetClient *activation.RPCPoetClient, fastHare bool, clock TickProvider) (*SpacemeshApp, error) {

	smApp := NewSpacemeshApp()
	smApp.Config = &cfg
	smApp.Config.CoinbaseAccount = strconv.Itoa(i + 1)
	smApp.Config.GenesisTime = genesisTime
	edSgn := signing.NewEdSigner()
	pub := edSgn.PublicKey()

	vrfPriv, vrfPub := BLS381.GenKeyPair(rng)
	vrfSigner := BLS381.NewBlsSigner(vrfPriv)
	nodeID := types.NodeId{Key: pub.String(), VRFPublicKey: vrfPub}

	swarm := net.NewNode()
	dbStorepath := storePath

	hareOracle := oracle.NewLocalOracle(rolacle, 5, nodeID)
	hareOracle.Register(true, pub.String())

	postClient, err := activation.NewPostClient(&smApp.Config.POST, util.Hex2Bytes(nodeID.Key))
	if err != nil {
		return nil, err
	}

	err = smApp.initServices(nodeID, swarm, dbStorepath, edSgn, false, hareOracle, uint32(smApp.Config.LayerAvgSize), postClient, poetClient, vrfSigner, uint16(smApp.Config.LayersPerEpoch), clock)
	if err != nil {
		return nil, err
	}
	smApp.setupGenesis()

	return smApp, err
}

// Starts the run of a number of nodes, running in process consensus between them.
// this also runs a single transaction between the nodes.
func StartMultiNode(numOfinstances, layerAvgSize int, runTillLayer uint32, dbPath string) {
	cfg := getTestDefaultConfig()
	cfg.LayerAvgSize = layerAvgSize
	numOfInstances := numOfinstances

	path := dbPath + time.Now().Format(time.RFC3339)

	genesisTime := time.Now().Add(20 * time.Second).Format(time.RFC3339)

	poetClient, err := NewRPCPoetHarnessClient()
	if err != nil {
		log.Panic("failed creating poet client harness: %v", err)
	}
	defer poetClient.Teardown(true)

	rolacle := eligibility.New()
	rng := BLS381.DefaultSeed()
	gTime, err := time.Parse(time.RFC3339, genesisTime)
	if err != nil {
		log.Error("cannot parse genesis time %v", err)
	}
	pubsubAddr := "tcp://localhost:56565"
	events.InitializeEventPubsub(pubsubAddr)
	clock := timesync.NewManualClock(gTime)

	apps := make([]*SpacemeshApp, 0, numOfInstances)
	name := 'a'
	for i := 0; i < numOfInstances; i++ {
		dbStorepath := path + string(name)
		smApp, err := InitSingleInstance(*cfg, i, genesisTime, rng, dbStorepath, rolacle, poetClient, true, clock)
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

	if err := poetClient.Start("127.0.0.1:9091"); err != nil {
		log.Panic("failed to start poet server: %v", err)
	}

	//startInLayer := 5 // delayed pod will start in this layer
	defer GracefulShutdown(apps)

	timeout := time.After(time.Duration(runTillLayer*60) * time.Second)

	//stickyClientsDone := 0
	startLayer := time.Now()
	clock.Tick()
	errors := 0
loop:
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			log.Error("run timed out", err)
			return
		default:
			if errors > 100 {
				log.Error("too many errors and retries")
				break loop
			}
			layer := clock.GetCurrentLayer()
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
			epoch := layer.GetEpoch(uint16(cfg.LayersPerEpoch))
			if !(eventDb.GetAtxCreationDone(epoch) >= numOfInstances && eventDb.GetAtxCreationDone(epoch)%numOfInstances == 0) {
				log.Info("atx not created %v in epoch %v, created only %v atxs", numOfInstances-eventDb.GetAtxCreationDone(epoch), epoch, eventDb.GetAtxCreationDone(epoch))
				time.Sleep(500 * time.Millisecond)
				errors++
				continue
			}
			log.Info("all miners finished reading %v atxs, layer %v done in %v", eventDb.GetAtxCreationDone(epoch), layer, time.Since(startLayer))
			for _, atxId := range eventDb.GetCreatedAtx(epoch) {
				if _, found := eventDb.Atxs[atxId]; !found {
					log.Info("atx %v not propagated", atxId)
					errors++
					continue
				}
			}

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
