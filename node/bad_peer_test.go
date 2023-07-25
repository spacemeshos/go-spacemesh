package node

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pubsubpb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	varint "github.com/multiformats/go-varint"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	poetconfig "github.com/spacemeshos/poet/config"
	"github.com/spacemeshos/poet/server"
	postCfg "github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/initialization"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	ps "github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

func TestPeerDisconnectForMessageResultValidationReject(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	l := logtest.New(t)

	// Make 2 node instances
	conf1 := config.DefaultTestConfig()
	conf1.DataDirParent = t.TempDir()
	conf1.FileLock = filepath.Join(conf1.DataDirParent, "LOCK")
	conf1.P2P.Listen = "/ip4/127.0.0.1/tcp/0"
	// Ensure grpc services don't clash on ports
	conf1.API.PublicListener = "0.0.0.0:0"
	conf1.API.PrivateListener = "0.0.0.0:0"
	conf1.API.JSONListener = "0.0.0.0:0"
	app1, err := NewApp(&conf1, l)
	require.NoError(t, err)
	conf2 := config.DefaultTestConfig()
	// We need to copy the genesis config to ensure that both nodes share the
	// same gnenesis ID, otherwise they will not be able to connect to each
	// other.
	*conf2.Genesis = *conf1.Genesis
	conf2.DataDirParent = t.TempDir()
	conf2.FileLock = filepath.Join(conf2.DataDirParent, "LOCK")
	conf2.P2P.Listen = "/ip4/127.0.0.1/tcp/0"
	// Ensure grpc services don't clash on ports
	conf2.API.PublicListener = "0.0.0.0:0"
	conf2.API.PrivateListener = "0.0.0.0:0"
	conf2.API.JSONListener = "0.0.0.0:0"
	app2, err := NewApp(&conf2, l)
	require.NoError(t, err)

	types.SetLayersPerEpoch(conf1.LayersPerEpoch)
	t.Cleanup(func() {
		app1.Cleanup(ctx)
		app2.Cleanup(ctx)
	})
	g := errgroup.Group{}
	g.Go(func() error {
		return app1.Start(ctx)
	})
	<-app1.Started()
	g.Go(func() error {
		return app2.Start(ctx)
	})
	<-app2.Started()

	// Connect app2 to app1
	err = app2.Host().Connect(context.Background(), peer.AddrInfo{
		ID:    app1.Host().ID(),
		Addrs: app1.Host().Addrs(),
	})
	require.NoError(t, err)

	conns := app2.Host().Network().ConnsToPeer(app1.Host().ID())
	require.Equal(t, 1, len(conns))

	// Wait for streams to be established, one outbound and one inbound.
	require.Eventually(t, func() bool {
		return len(conns[0].GetStreams()) == 2
	}, time.Second*5, time.Millisecond*50)

	s := getStream(conns[0], pubsub.GossipSubID_v11, network.DirOutbound)

	require.True(t, app1.syncer.IsSynced(ctx))
	require.True(t, app2.syncer.IsSynced(ctx))

	protocol := ps.ProposalProtocol
	// Send a message that doesn't result in ValidationReject.
	p := types.Proposal{}
	bytes, err := codec.Encode(&p)
	require.NoError(t, err)
	m := &pubsubpb.Message{
		Data:  bytes,
		Topic: &protocol,
	}
	err = writeRpc(rpcWithMessages(m), s)
	require.NoError(t, err)

	// Verify that connections remain up
	for i := 0; i < 5; i++ {
		conns := app2.Host().Network().ConnsToPeer(app1.Host().ID())
		require.Equal(t, 1, len(conns))
		time.Sleep(100 * time.Millisecond)
	}

	// Send message that results in ValidationReject
	m = &pubsubpb.Message{
		Data:  make([]byte, 20),
		Topic: &protocol,
	}
	err = writeRpc(rpcWithMessages(m), s)
	require.NoError(t, err)

	// Wait for connection to be dropped
	require.Eventually(t, func() bool {
		return len(app2.Host().Network().ConnsToPeer(app1.Host().ID())) == 0
	}, time.Second*15, time.Millisecond*200)

	// Stop the nodes by canceling the context
	cancel()
	// Wait for nodes to finish
	require.NoError(t, g.Wait())
}

func TestConsensus(t *testing.T) {
	spew.Config.DisableMethods = true
	// cfg := fastnet()
	cfg := conf()
	cfg.Genesis = config.DefaultGenesisConfig()
	cfg.SMESHING.Start = true
	// cfg := config.DefaultTestConfig()
	// cfg.LayerDuration = time.Second * 10
	// cfg.HARE.RoundDuration = time.Second
	// cfg.Beacon.GracePeriodDuration = 0
	// cfg.Beacon.ProposalDuration = time.Second * 5
	// cfg.Beacon.FirstVotingRoundDuration = time.Second * 5
	// cfg.Beacon.VotingRoundDuration = time.Second * 5
	// cfg.Beacon.WeakCoinRoundDuration = time.Second * 5
	l := logtest.New(t)
	networkSize := 2
	network, cleanup, err := NewNetwork(cfg, l, networkSize)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, cleanup())
	}()
	// println("boundaddress1", network[0].grpcPublicService.BoundAddress)
	// println("boundaddress2", network[1].grpcPublicService.BoundAddress)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Second)
	defer cancel()
	layerNotify := make(chan *pb.Layer)
	var eg errgroup.Group
	defer func() {
		err := eg.Wait()
		if status.Code(err) != codes.Canceled {
			require.NoError(t, err)
		}
	}()
	for _, app := range network {
		appCopy := app
		eg.Go(func() error {
			meshapi := pb.NewMeshServiceClient(appCopy.Conn)
			layers, err := meshapi.LayerStream(ctx, &pb.LayerStreamRequest{})
			if err != nil {
				return err
			}
			for {
				layer, err := layers.Recv()
				if err != nil {
					return err
				}
				// fmt.Printf("-------------------------------------------------------------------GotLayer num:%v stat:%v blocks:%v\n", layer.Layer.Number, layer.Layer.Number, layer.Layer.Blocks)
				select {
				case layerNotify <- layer.Layer:
				case <-ctx.Done():
					return nil
				}
			}
		})
	}
	results := make(map[uint32]int)
	for {
		select {
		case layer := <-layerNotify:
			fmt.Printf("got layer %v %v %v\n", layer.Number.Number, len(layer.Blocks), layer.Status.String())
			fmt.Printf("results %v\n", results)
			if layer.Status == pb.Layer_LAYER_STATUS_APPROVED && len(layer.Blocks) == 1 {
				results[layer.Number.GetNumber()]++
			}
			if checkResults(results, networkSize) {
				cancel() // shutdown goroutines
				return
			}
		case <-ctx.Done():

			t.Fatalf("timed out %v, results: %v", ctx.Err(), results)
		}
	}
}

func checkResults(results map[uint32]int, n int) bool {
	for i := 10; i < 14; i++ {
		if results[uint32(i)] < n {
			return false
		}
	}
	return true
}

func NewNetwork(conf config.Config, l log.Log, size int) ([]*TestApp, func() error, error) {
	// We need to set this global state
	types.SetLayersPerEpoch(conf.LayersPerEpoch)
	types.SetNetworkHRP(conf.NetworkHRP) // set to generate coinbase

	// To save an epoch of startup time, we bootstrap (meaning we manually set
	// it) the beacon for epoch 2 so that in epoch 3 hare can start.
	bootstrapEpoch := (types.GetEffectiveGenesis() + 1).GetEpoch()
	bootstrapBeacon := types.Beacon{}
	genesis := conf.Genesis.GenesisID()
	copy(bootstrapBeacon[:], genesis[:])

	ctx, cancel := context.WithCancel(context.Background())
	g := errgroup.Group{}
	var apps []*TestApp
	var datadirs []string
	cleanup := func() error {
		// cancel the context
		cancel()
		// Wait for nodes to shutdown
		g.Wait()
		// Clean their datadirs
		for _, d := range datadirs {
			err := os.RemoveAll(d)
			if err != nil {
				return err
			}
		}
		return nil
	}

	poet, poetDir, err := NewPoet(poetconfig.DefaultConfig(), &conf)
	if err != nil {
		return nil, nil, err
	}
	g.Go(func() error {
		return poet.Start(ctx)
	})
	datadirs = append(datadirs, poetDir)

	// Add the poet address to the config
	conf.PoETServers = []string{"http://" + poet.GrpcRestProxyAddr().String()}

	// We encode and decode the config in order to deep copy it.
	var buf bytes.Buffer
	err = gob.NewEncoder(&buf).Encode(conf)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	marshaled := buf.Bytes()

	for i := 0; i < size; i++ {
		var c config.Config
		err := gob.NewDecoder(bytes.NewBuffer(marshaled)).Decode(&c)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		dir, err := os.MkdirTemp("", "")
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		datadirs = append(datadirs, dir)

		c.DataDirParent = dir
		c.SMESHING.Opts.DataDir = dir
		c.SMESHING.CoinbaseAccount = types.GenerateAddress([]byte(strconv.Itoa(i))).String()
		c.FileLock = filepath.Join(c.DataDirParent, "LOCK")

		app, err := NewApp(&c, l)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		g.Go(func() error {
			return app.Start(ctx)
		})
		<-app.Started()
		if err := app.beaconProtocol.UpdateBeacon(bootstrapEpoch, bootstrapBeacon); err != nil {
			return nil, nil, fmt.Errorf("failed to bootstrap beacon for node %q: %w", i, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		conn, err := grpc.DialContext(ctx, app.grpcPublicService.BoundAddress,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
		)
		if err != nil {
			return nil, nil, err
		}
		apps = append(apps, NewTestApp(app, conn))
		// apps = append(apps, NewTestApp(app, nil))
	}

	// Connect all nodes to each other
	for i := 0; i < size; i++ {
		for j := i + 1; j < size; j++ {
			err = apps[i].Host().Connect(context.Background(), peer.AddrInfo{
				ID:    apps[j].Host().ID(),
				Addrs: apps[j].Host().Addrs(),
			})
			if err != nil {
				cleanup()
				return nil, nil, err
			}
		}
	}
	return apps, cleanup, nil
}

func NewApp(conf *config.Config, l log.Log) (*App, error) {
	app := New(
		WithConfig(conf),
		WithLog(l),
	)

	var err error
	if err = app.Initialize(); err != nil {
		return nil, err
	}

	/* Create or load miner identity */
	if app.edSgn, err = app.LoadOrCreateEdSigner(); err != nil {
		return app, fmt.Errorf("could not retrieve identity: %w", err)
	}

	// app.edSgn, err = signing.NewEdSigner()
	// if err != nil {
	// 	return nil, err
	// }
	return app, err
}

func NewPoet(cfg *poetconfig.Config, appConf *config.Config) (*server.Server, string, error) {
	dir, err := os.MkdirTemp("", "")
	if err != nil {
		return nil, "", err
	}
	cfg.PoetDir = dir
	cfg.DataDir = filepath.Join(dir, "data")
	cfg.LogDir = filepath.Join(dir, "logs")
	cfg.DebugLog = false

	cfg.RawRESTListener = "0.0.0.0:0"
	cfg.RawRPCListener = "0.0.0.0:0"
	cfg.Service.Genesis.UnmarshalFlag(appConf.Genesis.GenesisTime)
	cfg.Service.EpochDuration = appConf.LayerDuration * time.Duration(appConf.LayersPerEpoch)
	cfg.Service.CycleGap = appConf.POET.CycleGap
	cfg.Service.PhaseShift = appConf.POET.PhaseShift
	srv, err := server.New(context.Background(), *cfg)
	if err != nil {
		err := fmt.Errorf("poet init faiure: %w", err)
		errDir := os.RemoveAll(dir)
		if errDir != nil {
			return nil, "", fmt.Errorf("failed to remove poet dir %q after poet init failure: %w", errDir, err)
		}
		return nil, "", err
	}

	return srv, dir, nil
	// app.log.With().Warning("lauching poet in standalone mode", log.Any("config", cfg))

	// app.eg.Go(func() error {
	// 	if err := srv.Start(ctx); err != nil {
	// 		app.log.With().Error("poet server failed", log.Err(err))
	// 		return err
	// 	}
	// 	return nil
	// })
}

func getStream(c network.Conn, p protocol.ID, dir network.Direction) network.Stream {
	for _, s := range c.GetStreams() {
		if s.Protocol() == p && s.Stat().Direction == dir {
			return s
		}
	}
	return nil
}

func rpcWithMessages(msgs ...*pubsubpb.Message) *pubsub.RPC {
	return &pubsub.RPC{RPC: pubsubpb.RPC{Publish: msgs}}
}

func writeRpc(rpc *pubsub.RPC, s network.Stream) error {
	size := uint64(rpc.Size())

	buf := make([]byte, varint.UvarintSize(size)+int(size))

	n := binary.PutUvarint(buf, size)
	_, err := rpc.MarshalTo(buf[n:])
	if err != nil {
		return err
	}

	_, err = s.Write(buf)
	return err
}

func conf() config.Config {
	conf := config.DefaultConfig()
	conf.NetworkHRP = "stest"
	types.SetNetworkHRP(conf.NetworkHRP) // set to generate coinbase

	conf.TIME.Peersync.Disable = true
	// conf.Standalone = true
	conf.DataDirParent = filepath.Join(os.TempDir(), "spacemesh")
	conf.FileLock = filepath.Join(conf.DataDirParent, "LOCK")

	conf.HARE.N = 2
	conf.HARE.ExpectedLeaders = 2
	conf.HARE.LimitConcurrent = 2
	conf.HARE.LimitIterations = 3
	conf.HARE.RoundDuration = 500 * time.Millisecond
	conf.HARE.WakeupDelta = 1 * time.Second

	conf.Genesis = &config.GenesisConfig{
		ExtraData: "stest",
		Accounts:  map[string]uint64{},
	}

	conf.LayerAvgSize = 50
	conf.LayerDuration = 15 * time.Second
	conf.Sync.Interval = 3 * time.Second
	conf.LayersPerEpoch = 5

	conf.Tortoise.Hdist = 3
	conf.Tortoise.Zdist = 2

	conf.HareEligibility.ConfidenceParam = 2

	conf.POST.K1 = 12
	conf.POST.K2 = 4
	conf.POST.K3 = 4
	conf.POST.LabelsPerUnit = 64
	conf.POST.MaxNumUnits = 2
	conf.POST.MinNumUnits = 1

	conf.SMESHING.CoinbaseAccount = types.GenerateAddress([]byte("1")).String()
	conf.SMESHING.Start = true
	conf.SMESHING.Opts.ProviderID = int(initialization.CPUProviderID())
	conf.SMESHING.Opts.NumUnits = 1
	conf.SMESHING.Opts.Throttle = true
	conf.SMESHING.Opts.DataDir = conf.DataDirParent
	conf.SMESHING.ProvingOpts.Flags = postCfg.RecommendedPowFlags()

	conf.Beacon.Kappa = 40
	conf.Beacon.Theta = big.NewRat(1, 4)
	conf.Beacon.FirstVotingRoundDuration = 2 * time.Second
	conf.Beacon.GracePeriodDuration = 6 * time.Second
	conf.Beacon.ProposalDuration = 400 * time.Millisecond
	conf.Beacon.VotingRoundDuration = 400 * time.Millisecond
	conf.Beacon.WeakCoinRoundDuration = 400 * time.Millisecond
	conf.Beacon.RoundsNumber = 4
	conf.Beacon.BeaconSyncWeightUnits = 10
	conf.Beacon.VotesLimit = 100

	conf.PoETServers = []string{"http://0.0.0.0:10010"}
	conf.POET.GracePeriod = 5 * time.Second
	conf.POET.CycleGap = 30 * time.Second
	conf.POET.PhaseShift = 30 * time.Second

	conf.P2P.DisableNatPort = true
	conf.P2P.Listen = "/ip4/127.0.0.1/tcp/0"

	conf.API.PublicListener = "0.0.0.0:0"
	conf.API.PrivateListener = "0.0.0.0:0"
	conf.API.JSONListener = "0.0.0.0:0"

	return conf
}

// func fastnet() config.Config {
// 	conf := config.DefaultConfig()
// 	conf.Address = types.DefaultTestAddressConfig()

// 	conf.BaseConfig.OptFilterThreshold = 90

// 	conf.HARE.N = 800
// 	conf.HARE.ExpectedLeaders = 10
// 	conf.HARE.LimitConcurrent = 5
// 	conf.HARE.LimitIterations = 3
// 	conf.HARE.RoundDuration = 2 * time.Second
// 	conf.HARE.WakeupDelta = 3 * time.Second

// 	conf.P2P.MinPeers = 10

// 	conf.Genesis = &config.GenesisConfig{
// 		ExtraData: "fastnet",
// 	}

// 	conf.LayerAvgSize = 50
// 	conf.LayerDuration = 15 * time.Second
// 	conf.Sync.Interval = 5 * time.Second
// 	conf.LayersPerEpoch = 4

// 	conf.Tortoise.Hdist = 4
// 	conf.Tortoise.Zdist = 2
// 	conf.Tortoise.BadBeaconVoteDelayLayers = 2

// 	conf.HareEligibility.ConfidenceParam = 2

// 	conf.POST.K1 = 12
// 	conf.POST.K2 = 4
// 	conf.POST.K3 = 4
// 	conf.POST.LabelsPerUnit = 128
// 	conf.POST.MaxNumUnits = 4
// 	conf.POST.MinNumUnits = 2

// 	conf.SMESHING.CoinbaseAccount = types.GenerateAddress([]byte("1")).String()
// 	conf.SMESHING.Start = false
// 	conf.SMESHING.Opts.ProviderID = int(initialization.CPUProviderID())
// 	conf.SMESHING.Opts.NumUnits = 2
// 	conf.SMESHING.Opts.Throttle = true
// 	// Override proof of work flags to use light mode (less memory intensive)
// 	conf.SMESHING.ProvingOpts.Flags = postCfg.RecommendedPowFlags()

// 	conf.Beacon.Kappa = 40
// 	conf.Beacon.Theta = big.NewRat(1, 4)
// 	conf.Beacon.FirstVotingRoundDuration = 10 * time.Second
// 	conf.Beacon.GracePeriodDuration = 30 * time.Second
// 	conf.Beacon.ProposalDuration = 2 * time.Second
// 	conf.Beacon.VotingRoundDuration = 2 * time.Second
// 	conf.Beacon.WeakCoinRoundDuration = 2 * time.Second
// 	conf.Beacon.RoundsNumber = 4
// 	conf.Beacon.BeaconSyncWeightUnits = 10
// 	conf.Beacon.VotesLimit = 100

// 	return conf
// }

// func (app *App) launchStandalone(ctx context.Context) error {
// 	if !app.Config.Standalone {
// 		return nil
// 	}
// 	if len(app.Config.PoETServers) != 1 {
// 		return fmt.Errorf("to launch in a standalone mode provide single local address for poet: %v", app.Config.PoETServers)
// 	}
// 	value := types.Beacon{}
// 	genesis := app.Config.Genesis.GenesisID()
// 	copy(value[:], genesis[:])
// 	epoch := types.GetEffectiveGenesis().GetEpoch() + 1
// 	app.log.With().Warning("using standalone mode for bootstrapping beacon",
// 		log.Uint32("epoch", epoch.Uint32()),
// 		log.Stringer("beacon", value),
// 	)
// 	if err := app.beaconProtocol.UpdateBeacon(epoch, value); err != nil {
// 		return fmt.Errorf("update standalone beacon: %w", err)
// 	}
// 	cfg := poetconfig.DefaultConfig()
// 	cfg.PoetDir = filepath.Join(app.Config.DataDir(), "poet")
// 	cfg.DataDir = cfg.PoetDir
// 	cfg.LogDir = cfg.PoetDir
// 	parsed, err := url.Parse(app.Config.PoETServers[0])
// 	if err != nil {
// 		return err
// 	}
// 	cfg.RawRESTListener = parsed.Host
// 	cfg.Service.Genesis.UnmarshalFlag(app.Config.Genesis.GenesisTime)
// 	cfg.Service.EpochDuration = app.Config.LayerDuration * time.Duration(app.Config.LayersPerEpoch)
// 	cfg.Service.CycleGap = app.Config.POET.CycleGap
// 	cfg.Service.PhaseShift = app.Config.POET.PhaseShift
// 	srv, err := server.New(ctx, *cfg)
// 	if err != nil {
// 		return fmt.Errorf("init poet server: %w", err)
// 	}
// 	app.log.With().Warning("lauching poet in standalone mode", log.Any("config", cfg))
// 	app.eg.Go(func() error {
// 		if err := srv.Start(ctx); err != nil {
// 			app.log.With().Error("poet server failed", log.Err(err))
// 			return err
// 		}
// 		return nil
// 	})
// 	return nil
// }

type TestApp struct {
	*App
	Conn *grpc.ClientConn
}

func NewTestApp(app *App, conn *grpc.ClientConn) *TestApp {
	return &TestApp{
		App:  app,
		Conn: conn,
	}
}
