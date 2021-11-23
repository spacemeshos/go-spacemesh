package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/plans/network/api"
	"github.com/spacemeshos/poet/server"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"math/big"
	"strings"
	"time"

	"github.com/spacemeshos/go-spacemesh/activation"
	apiConfig "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon"
	"github.com/spacemeshos/post/initialization"
	"github.com/testground/sdk-go/sync"

	"github.com/spacemeshos/go-spacemesh/config"
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
func MustSetupNetworking(ctx context.Context, netclient *network.Client) {
	netcfg := network.Config{
		// Control the "default" network. At the moment, this is the only network.
		Network: "default",

		// Enable this network. Setting this to false will disconnect this test
		// instance from this network. You probably don't want to do that.
		Enable: true,

		RoutingPolicy: network.AllowAll,
		// Set what state the sidecar should signal back to you when it's done.
		CallbackState: "network-configured",
	}

	netclient.MustConfigureNetwork(ctx, &netcfg)

	if err := netclient.WaitNetworkInitialized(ctx); err != nil {
		panic(err)
	}
}

func NodeString(key, ip string, port uint16) string {
	return fmt.Sprintf("/ip4/%s/tcp/%d/p2p/%s", ip, port, key)
}

func  IPFromNodeString(str string) string {
	return strings.Split(str, "/")[2]
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

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*30)
	defer cancel()

	client := sync.MustBoundClient(ctx, env)
	defer client.Close()
	netclient := initCtx.NetClient


	MustSetupNetworking(ctx, netclient)
	initCtx.MustWaitAllInstancesInitialized(ctx)

	ip := netclient.MustGetDataNetworkIP()

	poets_topic := sync.NewTopic("poets", string(""))
	gateways_topic := sync.NewTopic("gateways", string(""))
	genesisTimeTopic := sync.NewTopic("genesis_time", "")

	// role-allocation assigns a sequence number to each instance and distributes roles
	poets := env.IntParam("poets")
	gateways := env.IntParam("gateways")
	genesisOffset := env.IntParam("genesis_timestamp_offset")
	genActiveSet := env.TestInstanceCount - poets

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
		client.MustPublish(ctx, poets_topic, string(ip.String()+":8080"))
		env.RecordMessage("Published poet data ", string(ip.String()+":8080"))

		gatewaych := make(chan string)
		client.MustSubscribe(ctx, gateways_topic, gatewaych)
		gatewaylist := make([]string, 0)

		for g := 0; g < gateways; g++ {
			gw := <-gatewaych
			env.RecordMessage("Added poet gatweway %v", IPFromNodeString(gw)+":9092")
			gatewaylist = append(gatewaylist, IPFromNodeString(gw)+":9092")
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

		errch := make(chan error)

		go func(conf *poetconfig.Config) {
			err := server.StartServer(conf)
			if err != nil {
				errch <- err
			}
		}(cfg)

		select {
			case err := <-errch:
			env.RecordFailure(err)
			case <-time.After(5* time.Second):
				break
		}

		env.RecordSuccess()
	}, func(obj interface{}) {
	})

	ra.Add("gateway", gateways, func(r *role) {
		env.RecordMessage("hello im gateway")
		nd, err := InitNode(ctx, createMinerConfig(genActiveSet))

		if err != nil {
			env.RecordFailure(err)
			return
		}

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

		time.Sleep(5 * time.Second)

		port := uint16(7513)
		id := nd.Host.ID().String()
		ipstr := ip.String()
		bsInfo := NodeString(id, ipstr, port)

		env.RecordMessage("gateway P2P is ready, %v", bsInfo)
		client.MustPublish(ctx, gateways_topic, bsInfo)

		env.RecordSuccess()

	}, func(obj interface{}) {

	})


	ra.Add("miner", env.TestInstanceCount-1-poets-gateways, func(r *role) {

		env.RecordMessage("hello im miner")
		minercfg := createMinerConfig(genActiveSet)
		minercfg.GenesisTime = genesisTime

		env.RecordMessage("miner waiting for poet message")
		poetch := make(chan string)
		client.MustSubscribe(ctx, poets_topic, poetch)
		poetaddr := <-poetch

		minercfg.PoETServer = poetaddr

		env.RecordMessage("miner waiting for bootstrap message")
		gatewaych := make(chan string)
		s := client.MustSubscribe(ctx, gateways_topic, gatewaych)

		gatewaylist := make([]string, 0)

		for g := 0; g < gateways; g++ {
			select {
			case gw := <-gatewaych:
				env.RecordMessage("miner adding gateway %v", gw)
				gatewaylist = append(gatewaylist, gw)
				case <-s.Done():
					break
			}
		}

		pcfg := &minercfg.P2P
		pcfg.Bootnodes = gatewaylist

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

		time.Sleep(30 * time.Second)

		env.RecordMessage("running miner test")

		acc, err := TestMiner(ctx)
		if err != nil {
			env.RecordFailure(fmt.Errorf("miner is not smeshing %v, err: %v", acc, err))
			return
		}
		env.RecordMessage("Miner started smeshing! %v", acc)
		env.RecordSuccess()

	}, func(obj interface{}) {
	})

	if err := ra.Allocate(seq); err != nil {
		env.RecordFailure(err)
	}

	return nil
}

func TestMiner(ctx context.Context) (string, error) {
	client, err := api.New("localhost:9092")

	if err != nil {
		return "", err
	}


	accresp, err := client.SmesherID(ctx, &empty.Empty{})
	if err != nil {
		return "", err
	}

	accstr := util.Bytes2Hex(accresp.AccountId.GetAddress())
	smhresp, err := client.IsSmeshing(ctx, &empty.Empty{})
	if err != nil {
		return accstr, err
	}


	if smhresp.IsSmeshing {
		return accstr, nil
	} else {
		return accstr, errors.New("not smeshing")
	}


}

func createMinerConfig(genActiveSet int) *config.Config {
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
	cfg.HARE.F = 320
	cfg.HARE.RoundDuration = 10
	cfg.HARE.WakeupDelta = 20
	cfg.HARE.ExpectedLeaders = 240
	cfg.HARE.LimitConcurrent = 5
	cfg.HARE.LimitIterations = 10
	cfg.GenesisActiveSet = genActiveSet
	//cfg.HARE.SuperHare = true

	cfg.LayerAvgSize = 30
	cfg.LayersPerEpoch = 3
	cfg.LayerDurationSec = 30
	cfg.SyncRequestTimeout = 1000
	cfg.SyncInterval = 3
	cfg.Hdist = 10
	cfg.TxsPerBlock = 100

	cfg.HareEligibility.ConfidenceParam = 25
	cfg.HareEligibility.EpochOffset = 0

	//
	//cfg.FETCH.RequestTimeout = 10
	//cfg.FETCH.MaxRetiresForPeer = 5
	//cfg.FETCH.BatchSize = 5
	//cfg.FETCH.BatchTimeout = 5
	//
	cfg.LAYERS.RequestTimeout = 10
	cfg.SMESHING.CoinbaseAccount = "0x123"
	cfg.GoldenATXID = "0x5678"

	ppcfg := p2p.DefaultConfig()
	p_ppcfg := &ppcfg

	p_ppcfg.TargetOutbound = 3
	p_ppcfg.NetworkID = 20
	cfg.P2P = *p_ppcfg

	cfg.LOGGING.AppLoggerLevel = "info"
	cfg.LOGGING.P2PLoggerLevel = "info"
	cfg.LOGGING.PostLoggerLevel = "info"
	cfg.LOGGING.StateDbLoggerLevel = "info"
	cfg.LOGGING.StateLoggerLevel = "info"
	cfg.LOGGING.AtxDbStoreLoggerLevel = "info"
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


	cfg.POST.BitsPerLabel = 8
	cfg.POST.LabelsPerUnit= 32
	cfg.POST.MinNumUnits = 2
	cfg.POST.MaxNumUnits = 4
	cfg.POST.K1 = 2000
	cfg.POST.K2 = 4


	cfg.TortoiseBeacon.FirstVotingRoundDuration = time.Second*6
	cfg.TortoiseBeacon.GracePeriodDuration = time.Second*1
	cfg.TortoiseBeacon.Kappa = 400000
	cfg.TortoiseBeacon.ProposalDuration = time.Second*3
	cfg.TortoiseBeacon.Q = big.NewRat(1, 3)
	cfg.TortoiseBeacon.RoundsNumber = 6
	cfg.TortoiseBeacon.Theta = big.NewRat(1,25000)
	cfg.TortoiseBeacon.VotesLimit = 20
	cfg.TortoiseBeacon.VotingRoundDuration = time.Second*5
	cfg.TortoiseBeacon.WeakCoinRoundDuration= time.Second*1

	cfg.SMESHING.Opts = activation.PostSetupOpts{
		DataDir: "/root/data/post",
		NumUnits: 2,
		NumFiles: 1,
		//ComputeProviderID: 0,
		Throttle: true,
	}

	apicfg := apiConfig.DefaultConfig()
	papicfg := &apicfg
	papicfg.StartGatewayService = true
	papicfg.StartGlobalStateService = true
	papicfg.StartDebugService = true
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
