package main

import (
	"context"
	"errors"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/poet/server"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"time"

	"github.com/spacemeshos/go-spacemesh/activation"
	apiConfig "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/p2p"
	node2 "github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon"
	"github.com/spacemeshos/post/initialization"
	"github.com/testground/sdk-go/sync"

	"github.com/spacemeshos/go-spacemesh/config"
	p2pcfg "github.com/spacemeshos/go-spacemesh/p2p/config"
	poetconfig "github.com/spacemeshos/poet/config"

	"github.com/spacemeshos/go-spacemesh/cmd/node"
	"github.com/testground/sdk-go/network"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

/*
	default: poets=1 default gateways=1
	testground plan import --from ./plans/network --name network
	testground run single --plan=network --testcase=start --builder=docker:generic --runner=local:docker --instances=5
*/

var testcases = map[string]interface{}{
	"start": run.InitializedTestCaseFn(Start),
}



// MustSetupNetworking activates the required networking configuration for the network
//  this is still very simple and eventually will control latency and such.
//  panics if there's an error
func MustSetupNetworking(ctx context.Context, netclient *network.Client)  {
	netcfg := network.Config{
		// Control the "default" network. At the moment, this is the only network.
		Network: "default",

		// Enable this network. Setting this to false will disconnect this test
		// instance from this network. You probably don't want to do that.
		Enable:  true,

		RoutingPolicy: network.AllowAll,
		// Set what state the sidecar should signal back to you when it's done.
		CallbackState: "network-configured",
	}

	netclient.MustConfigureNetwork(ctx, &netcfg)

	if err := netclient.WaitNetworkInitialized(ctx); err != nil {
		panic(err)
	}
}

// Start creates a basic spacemesh layout of instances.
// 	it first creates poet instances shares their addresses with gateways and miners
// 	gateway nodes function as bootstrap nodes and gateways for poets, their addresses
//  are distributed to miners.
//
// 	gateways - the number of bootstrap nodes and poet gateways
// 	poets - the number of poets
// 	the rest of the instances are miners
func Start(env *runtime.RunEnv, initCtx *run.InitContext) error {
	// TODO: extract a lot of constants/params

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	client := initCtx.SyncClient
	netclient := initCtx.NetClient

	MustSetupNetworking(ctx, netclient)


	poets_topic := sync.NewTopic("poets", string(""))
	gateways_topic := sync.NewTopic("gateways", node2.Info{})
	genesisTimeTopic := sync.NewTopic("genesis_time", "")

	// role-allocation assigns a sequence number to each instance and distributes roles
	poets := env.IntParam("poets")
	gateways := env.IntParam("gateways")
	genesisOffset := env.IntParam("genesis_timestamp_offset")


	seq := int(client.MustSignalAndWait(ctx, "role-allocation", env.TestInstanceCount))

	var genesisTime string

	if seq == 1 {
		genesisTime = time.Now().Add(time.Second * time.Duration(genesisOffset)).Format(time.RFC3339)
		client.MustPublish(ctx, genesisTimeTopic, genesisTime)
	} else {
		gench := make(chan string)
		client.MustSubscribe(ctx, genesisTimeTopic, gench)
		genesisTime = <-gench
	}



	ra := &roleAllocator{}

	ra.Add("poet", poets, func(r *role) {
		env.RecordMessage("Getting poet ip")
		poet_ip := netclient.MustGetDataNetworkIP()
		client.MustPublish(ctx, poets_topic, string(poet_ip.String()+":8080"))
		env.RecordMessage("Published poet data ", string(poet_ip.String()+":8080"))

		gatewaych := make(chan node2.Info)
		client.MustSubscribe(ctx, gateways_topic, gatewaych)
		gatewaylist := make([]string, 0)

		for g := 0; g < gateways; g++ {
			gw := <-gatewaych
			env.RecordMessage("Added poet gatweway %v", gw.IP.String()+":9092")
			gatewaylist = append(gatewaylist, gw.IP.String()+":9092")
		}

		var err error
		cfg := poetconfig.DefaultConfig()
		cfg.RawRPCListener = ":50002"
		cfg.RawRESTListener = ":8080"
		cfg.Service.N = 19
		cfg.Service.GatewayAddresses = gatewaylist
		cfg, err = poetconfig.SetupConfig(cfg)
		if err != nil {
			env.RecordFailure(err)
		}

		env.RecordMessage("starting poet")

		go func(cfg2 *poetconfig.Config) {
		if err := server.StartServer(cfg2); err != nil {
			env.RecordFailure(err)
		}
		env.RecordSuccess()
		}(cfg)

	}, func(obj interface{}) {
		env.RecordMessage("done starting poet")
	})

	ra.Add("gateway", gateways, func(r *role) {
		env.RecordMessage("hello im gateway")
		nd, err := InitNode(ctx, createMinerConfig())

		if err != nil {
			env.RecordFailure(err)
			return
		}

		ip := netclient.MustGetDataNetworkIP()


		env.RecordMessage("gateway waiting for poet message")
		poetch := make(chan string)
		client.MustSubscribe(ctx, poets_topic, poetch)

		// TODO(y0sher): multiple poets
		poetaddr := <-poetch

		nd.Config.PoETServer = poetaddr

		go func(app *node.App) {
			err := app.Start()
			if err != nil {
				env.RecordFailure(err)
				return
			}
		}(nd)

		for nd.P2P == nil {
			time.Sleep(1 * time.Second)
		}
		inch, _ := nd.P2P.SubscribePeerEvents()
		info := nd.P2P.(*p2p.Switch).LocalNode()
		env.RecordMessage("gateway P2P is ready, p2pid :", info.PublicKey().String())

		port := uint16(7513)
		bs_info := node2.NewNode(info.PublicKey(), ip, port, port)

		client.MustPublish(ctx, gateways_topic, bs_info)

		select {
		case <-inch:
			env.RecordSuccess()
		case <-ctx.Done():
			env.RecordFailure(errors.New("no peers within time"))
		}
		}, func(obj interface{}) {
		env.RecordMessage("done gateways")
	})

	ra.Add("miner", env.TestInstanceCount-poets-gateways, func(r *role) {
		env.RecordMessage("hello im miner")
		minercfg := createMinerConfig()
		minercfg.GenesisTime = genesisTime
		pcfg := &minercfg.P2P
		pcfg.SwarmConfig.Bootstrap = true

		env.RecordMessage("miner waiting for poet message")
		poetch := make(chan string)
		client.MustSubscribe(ctx, poets_topic, poetch)
		poetaddr := <-poetch

		minercfg.PoETServer = poetaddr

		env.RecordMessage("miner waiting for bootstrap message")
		gatewaych := make(chan node2.Info)
		client.MustSubscribe(ctx, gateways_topic, gatewaych)
		gatewaylist := make([]string, 0)

		for g := 0; g < gateways; g++ {
			gw := <-gatewaych
			env.RecordMessage("miner adding gateway %v", gw.String())
			gatewaylist = append(gatewaylist, gw.String())
		}

		pcfg.SwarmConfig.BootstrapNodes = gatewaylist

		minercfg.P2P = *pcfg
		env.RecordMessage("starting miner node")

		nd, err := InitNode(ctx, minercfg)

		if err != nil {
			env.RecordFailure(err)
			return
		}

		go func(app *node.App) {
			err := app.Start()
			if err != nil {
				env.RecordFailure(err)
				return
			}

			}(nd)

		for nd.P2P == nil {
			time.Sleep(1 * time.Second)
		}

		inch, _ := nd.P2P.SubscribePeerEvents()
		select {
			case <-inch:
				env.RecordSuccess()
				case <-ctx.Done():
				env.RecordFailure(errors.New("no peers within time"))
		}
	}, func(obj interface{}) {
		env.RecordMessage("done miner %v")
	})

	if err := ra.Allocate(seq); err != nil {
		env.RecordFailure(err)
	}

	<-ctx.Done()
	return nil
}

func createMinerConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.POST = activation.DefaultPostConfig()
	cfg.POST.LabelsPerUnit = 32
	cfg.POST.BitsPerLabel = 8
	cfg.POST.K2 = 4

	cfg.SMESHING = config.DefaultSmeshingConfig()
	cfg.SMESHING.Start = true
	cfg.SMESHING.Opts.NumUnits = cfg.POST.MinNumUnits + 1
	cfg.SMESHING.Opts.NumFiles = 1
	cfg.SMESHING.Opts.ComputeProviderID = int(initialization.CPUProviderID())

	cfg.HARE.N = 800
	cfg.HARE.F = 399
	cfg.HARE.RoundDuration = 7
	cfg.HARE.WakeupDelta = 20
	cfg.HARE.ExpectedLeaders = 10
	//cfg.HARE.SuperHare = true
	cfg.LayerAvgSize = 50
	cfg.LayersPerEpoch = 3
	cfg.TxsPerBlock = 100
	cfg.Hdist = 5

	cfg.LayerDurationSec = 60
	cfg.HareEligibility.ConfidenceParam = 6
	cfg.HareEligibility.EpochOffset = 0
	cfg.SyncRequestTimeout = 10000
	//cfg.SyncInterval = 2
	//cfg.SyncValidationDelta = 5
	//
	//cfg.FETCH.RequestTimeout = 10
	//cfg.FETCH.MaxRetiresForPeer = 5
	//cfg.FETCH.BatchSize = 5
	//cfg.FETCH.BatchTimeout = 5
	//
	cfg.LAYERS.RequestTimeout = 10
	cfg.SMESHING.CoinbaseAccount = "0x123"
	cfg.GoldenATXID = "0x5678"


	ppcfg := p2pcfg.DefaultConfig()
	mppcfg := &ppcfg
	mppcfg.SwarmConfig.RandomConnections = 1
	cfg.P2P = *mppcfg

	cfg.LOGGING.AppLoggerLevel = "info"
	cfg.LOGGING.P2PLoggerLevel = "info"
	cfg.LOGGING.PostLoggerLevel = "info"
	cfg.LOGGING.StateDbLoggerLevel = "info"
	cfg.LOGGING.StateLoggerLevel = "info"
	cfg.LOGGING.AtxDbStoreLoggerLevel = "info"
	cfg.LOGGING.TBeaconDbStoreLoggerLevel = "info"
	cfg.LOGGING.TBeaconDbLoggerLevel = "info"
	cfg.LOGGING.TBeaconLoggerLevel = "info"
	cfg.LOGGING.WeakCoinLoggerLevel = "info"
	cfg.LOGGING.PoetDbStoreLoggerLevel = "info"
	cfg.LOGGING.StoreLoggerLevel = "info"
	cfg.LOGGING.PoetDbLoggerLevel = "info"
	cfg.LOGGING.MeshDBLoggerLevel = "info"
	cfg.LOGGING.TrtlLoggerLevel = "info"
	cfg.LOGGING.AtxDbLoggerLevel = "info"
	cfg.LOGGING.BlkEligibilityLoggerLevel = "info"
	cfg.LOGGING.MeshLoggerLevel = "info"
	cfg.LOGGING.SyncLoggerLevel = "info"
	cfg.LOGGING.BlockOracleLevel = "info"
	cfg.LOGGING.HareOracleLoggerLevel = "info"
	cfg.LOGGING.HareLoggerLevel = "info"
	cfg.LOGGING.BlockBuilderLoggerLevel = "info"
	cfg.LOGGING.BlockListenerLoggerLevel = "info"
	cfg.LOGGING.PoetListenerLoggerLevel = "info"
	cfg.LOGGING.NipostBuilderLoggerLevel = "info"
	cfg.LOGGING.AtxBuilderLoggerLevel = "info"
	cfg.LOGGING.HareBeaconLoggerLevel = "info"
	cfg.LOGGING.TimeSyncLoggerLevel = "info"

	apicfg := apiConfig.DefaultConfig()
	papicfg := &apicfg
	papicfg.StartGatewayService = true
	papicfg.StartGlobalStateService = true
	papicfg.StartMeshService = true
	papicfg.StartNodeService = true
	papicfg.StartSmesherService = true
	papicfg.StartTransactionService = true

	cfg.API = *papicfg

	cfg.TortoiseBeacon = tortoisebeacon.DefaultConfig()
	return &cfg
}

// InitNode creates a spacemesh node instance and initializes it.
func InitNode(ctx context.Context, cfg *config.Config) (*node.App, error) {
	nd := node.New(node.WithConfig(cfg),
		node.WithLog(log.RegisterHooks(
			log.NewWithLevel("", zap.NewAtomicLevelAt(zapcore.DebugLevel)),
			events.EventHook())),
	)
	if err := nd.Initialize(); err != nil {
		return nil, err
	}
	return nd, nil
}


func main() {
	run.InvokeMap(testcases)
}
