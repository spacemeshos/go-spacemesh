package node

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/shared"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/beacon"
	"github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/config/presets"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

const layersPerEpoch = 3

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)

	res := m.Run()
	os.Exit(res)
}

func newLogger(buf *bytes.Buffer) log.Log {
	lvl := zap.NewAtomicLevelAt(zapcore.InfoLevel)
	syncer := zapcore.AddSync(buf)
	encConfig := zap.NewDevelopmentEncoderConfig()
	encConfig.TimeKey = ""
	enc := zapcore.NewConsoleEncoder(encConfig)
	core := zapcore.NewCore(enc, syncer, lvl)
	return log.NewFromLog(zap.New(core))
}

func TestSpacemeshApp_SetLoggers(t *testing.T) {
	r := require.New(t)

	cfg := getTestDefaultConfig(t)
	app := New(WithConfig(cfg), WithLog(logtest.New(t)))

	var buf1, buf2 bytes.Buffer
	myLogger := "anton"
	myLog := newLogger(&buf1)
	myLog2 := newLogger(&buf2)

	app.log = app.addLogger(myLogger, myLog)
	msg1 := "hi there"
	app.log.Info(msg1)
	r.Equal(
		fmt.Sprintf("INFO\t%s\t%s\n", myLogger, msg1),
		buf1.String(),
	)
	r.NoError(app.SetLogLevel(myLogger, "warn"))
	r.Equal("warn", app.loggers[myLogger].String())
	buf1.Reset()

	msg1 = "other logger"
	myLog2.Info(msg1)
	msg2 := "hi again"
	msg3 := "be careful"
	// This one should not be printed
	app.log.Info(msg2)
	// This one should be printed
	app.log.Warning(msg3)
	r.Equal(
		fmt.Sprintf("WARN\t%s\t%s\n", myLogger, msg3),
		buf1.String(),
	)
	r.Equal(fmt.Sprintf("INFO\t%s\n", msg1), buf2.String())
	buf1.Reset()

	r.NoError(app.SetLogLevel(myLogger, "info"))

	msg4 := "你好"
	app.log.Info(msg4)
	r.Equal("info", app.loggers[myLogger].String())
	r.Equal(
		fmt.Sprintf("INFO\t%s\t%s\n", myLogger, msg4),
		buf1.String(),
	)

	// test bad logger name
	r.Error(app.SetLogLevel("anton3", "warn"))

	// test bad loglevel
	r.Error(app.SetLogLevel(myLogger, "lulu"))
	r.Equal("info", app.loggers[myLogger].String())
}

func TestSpacemeshApp_AddLogger(t *testing.T) {
	r := require.New(t)

	var buf bytes.Buffer
	lg := newLogger(&buf)

	cfg := getTestDefaultConfig(t)
	app := New(WithConfig(cfg), WithLog(logtest.New(t)))

	myLogger := "anton"
	subLogger := app.addLogger(myLogger, lg)
	subLogger.Debug("should not get printed")
	teststr := "should get printed"
	subLogger.Info(teststr)
	r.Equal(
		fmt.Sprintf("INFO\t%s\t%s\n", myLogger, teststr),
		buf.String(),
	)
}

func testArgs(ctx context.Context, root *cobra.Command, args ...string) (string, error) {
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	// Workaround: will fail if args is nil (defaults to os args)
	if args == nil {
		args = []string{""}
	}
	root.SetArgs(args)
	_, err := root.ExecuteContextC(ctx) // runs Run()
	return buf.String(), err
}

func cmdWithRun(run func(*cobra.Command, []string) error) *cobra.Command {
	c := GetCommand()
	c.RunE = run
	return c
}

func TestSpacemeshApp_Cmd(t *testing.T) {
	r := require.New(t)

	cfg := getTestDefaultConfig(t)
	app := New(WithConfig(cfg), WithLog(logtest.New(t)))

	expected := `unknown command "illegal" for "node"`
	expected2 := "Error: " + expected + "\nRun 'node --help' for usage.\n"
	r.Equal(config.ConsoleLogEncoder, app.Config.LOGGING.Encoder)

	// Test an illegal flag
	c := cmdWithRun(func(*cobra.Command, []string) error {
		// We don't expect this to be called at all
		r.Fail("Command.Run not expected to run")
		return nil
	})

	str, err := testArgs(context.Background(), c, "illegal")
	r.Error(err)
	r.Equal(expected, err.Error())
	r.Equal(expected2, str)
}

func marshalProto(t *testing.T, msg proto.Message) []byte {
	buf, err := protojson.Marshal(msg)
	require.NoError(t, err)
	return buf
}

func callEndpoint(t *testing.T, url string, payload []byte) ([]byte, int) {
	resp, err := http.Post(url, "application/json", bytes.NewReader(payload))
	require.NoError(t, err)
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	buf, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	return buf, resp.StatusCode
}

func TestSpacemeshApp_GrpcService(t *testing.T) {
	// Use a unique port
	listener := "127.0.0.1:1242"

	r := require.New(t)
	cfg := getTestDefaultConfig(t)
	cfg.API.PublicListener = listener
	app := New(WithConfig(cfg), WithLog(logtest.New(t)))
	err := app.NewIdentity()
	require.NoError(t, err)

	app.clock, err = timesync.NewClock(
		timesync.WithLayerDuration(cfg.LayerDuration),
		timesync.WithTickInterval(1*time.Second),
		timesync.WithGenesisTime(app.Config.Genesis.GenesisTime.Time()),
		timesync.WithLogger(zaptest.NewLogger(t)))
	require.NoError(t, err)

	run := func(c *cobra.Command, args []string) error {
		return app.startAPIServices(context.Background())
	}
	defer app.stopServices(context.Background())

	events.CloseEventReporter()

	// Test starting the server from the command line
	str, err := testArgs(context.Background(), cmdWithRun(run))
	r.Empty(str)
	r.NoError(err)
	r.Equal(listener, app.Config.API.PublicListener)

	conn, err := grpc.NewClient(
		listener,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(conn.Close()) })
	c := pb.NewNodeServiceClient(conn)

	// call echo and validate result
	// We expect this one to succeed
	const message = "Hello World"
	response, err := c.Echo(context.Background(), &pb.EchoRequest{
		Msg: &pb.SimpleString{Value: message},
	})
	r.NoError(err)
	r.Equal(message, response.Msg.Value)
}

func TestSpacemeshApp_JsonServiceNotRunning(t *testing.T) {
	r := require.New(t)
	cfg := getTestDefaultConfig(t)
	app := New(WithConfig(cfg), WithLog(logtest.New(t)))
	err := app.NewIdentity()
	require.NoError(t, err)

	app.clock, err = timesync.NewClock(
		timesync.WithLayerDuration(cfg.LayerDuration),
		timesync.WithTickInterval(1*time.Second),
		timesync.WithGenesisTime(app.Config.Genesis.GenesisTime.Time()),
		timesync.WithLogger(zaptest.NewLogger(t)))
	require.NoError(t, err)

	// Make sure the service is not running by default
	run := func(c *cobra.Command, args []string) error {
		return app.startAPIServices(context.Background())
	}

	str, err := testArgs(context.Background(), cmdWithRun(run))
	r.Empty(str)
	r.NoError(err)
	defer app.stopServices(context.Background())

	require.Nil(t, app.jsonAPIServer)
}

func TestSpacemeshApp_JsonService(t *testing.T) {
	r := require.New(t)

	const message = "你好世界"
	payload := marshalProto(t, &pb.EchoRequest{Msg: &pb.SimpleString{Value: message}})
	listener := "127.0.0.1:0"

	cfg := getTestDefaultConfig(t)
	cfg.API.JSONListener = listener
	cfg.API.PrivateServices = nil
	app := New(WithConfig(cfg), WithLog(logtest.New(t)))

	var err error
	app.clock, err = timesync.NewClock(
		timesync.WithLayerDuration(cfg.LayerDuration),
		timesync.WithTickInterval(1*time.Second),
		timesync.WithGenesisTime(app.Config.Genesis.GenesisTime.Time()),
		timesync.WithLogger(zaptest.NewLogger(t)))
	require.NoError(t, err)

	// Make sure the service is not running by default
	run := func(c *cobra.Command, args []string) error {
		return app.startAPIServices(context.Background())
	}

	// Test starting the JSON server from the command line
	// uses Cmd.Run from above

	str, err := testArgs(context.Background(), cmdWithRun(run))
	r.Empty(str)
	r.NoError(err)
	defer app.stopServices(context.Background())

	var (
		respBody   []byte
		respStatus int
	)
	endpoint := fmt.Sprintf("http://%s/v1/node/echo", app.jsonAPIServer.BoundAddress)
	require.Eventually(t, func() bool {
		respBody, respStatus = callEndpoint(t, endpoint, payload)
		return respStatus == http.StatusOK
	}, 2*time.Second, 100*time.Millisecond)
	var msg pb.EchoResponse
	require.NoError(t, protojson.Unmarshal(respBody, &msg))
	require.Equal(t, message, msg.Msg.Value)
	require.Equal(t, http.StatusOK, respStatus)
	require.NoError(t, protojson.Unmarshal(respBody, &msg))
	require.Equal(t, message, msg.Msg.Value)
}

type noopHook struct{}

func (f *noopHook) OnWrite(*zapcore.CheckedEntry, []zapcore.Field) {}

// E2E app test of the stream endpoints in the NodeService.
func TestSpacemeshApp_NodeService(t *testing.T) {
	logger := logtest.New(t)
	// errlog is used to simulate errors in the app
	errlog := log.NewFromLog(
		zaptest.NewLogger(t, zaptest.WrapOptions(zap.Hooks(events.EventHook()), zap.WithPanicHook(&noopHook{}))),
	)

	cfg := getTestDefaultConfig(t)
	app := New(WithConfig(cfg), WithLog(logger))

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	app.signers = []*signing.EdSigner{signer}

	mesh, err := mocknet.WithNPeers(1)
	require.NoError(t, err)
	h, err := p2p.Upgrade(mesh.Hosts()[0])
	require.NoError(t, err)
	app.host = h

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	run := func(c *cobra.Command, args []string) error {
		// Give the error channel a buffer
		events.CloseEventReporter()
		events.InitializeReporter()

		// Speed things up a little
		app.Config.Sync.Interval = time.Second
		app.Config.LayerDuration = 2 * time.Second
		app.Config.DataDirParent = t.TempDir()
		app.Config.API.PublicListener = "localhost:0"

		// This will block. We need to run the full app here to make sure that
		// the various services are reporting events correctly. This could probably
		// be done more surgically, and we don't need _all_ of the services.
		return app.Start(appCtx)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Run the app in a goroutine. As noted above, it blocks if it succeeds.
	// If there's an error in the args, it will return immediately.
	var eg errgroup.Group
	eg.Go(func() error {
		str, err := testArgs(ctx, cmdWithRun(run))
		assert.Empty(t, str)
		assert.NoError(t, err)
		return nil
	})

	<-app.Started()
	conn, err := grpc.NewClient(
		app.grpcPublicServer.BoundAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, conn.Close()) })
	c := pb.NewNodeServiceClient(conn)

	eg.Go(func() error {
		streamStatus, err := c.StatusStream(ctx, &pb.StatusStreamRequest{})
		require.NoError(t, err)

		// We don't really control the order in which these are received,
		// unlike the errorStream. So just loop and listen here while the
		// app is running, and make sure there are no errors.
		for {
			in, err := streamStatus.Recv()
			if err != nil {
				code := status.Code(err)
				if code == codes.Unavailable {
					return nil
				}
				assert.NoError(t, err)
				return nil
			}
			// Note that, for some reason, protobuf does not display fields
			// that still have their default value, so the output here will
			// only be partial.
			logger.Debug("Got status message: %s", in.Status)
		}
	})

	streamErr, err := c.ErrorStream(ctx, &pb.ErrorStreamRequest{})
	require.NoError(t, err)
	_, err = streamErr.Header()
	require.NoError(t, err)

	// Report two errors and make sure they're both received
	eg.Go(func() error {
		errlog.Error("test123")
		errlog.Error("test456")
		errlog.Panic("testPANIC")
		return nil
	})

	// We expect a specific series of errors in a specific order!
	expected := []string{
		"test123",
		"test456",
		"Fatal: goroutine panicked.",
		"testPANIC",
	}
	for _, errmsg := range expected {
		in, err := streamErr.Recv()
		require.NoError(t, err)
		current := in.Error
		require.Contains(t, current.Msg, errmsg)
	}

	// Cleanup stops all services and thereby the app
	<-app.Started() // prevents races when app is not started yet
	appCancel()
	app.Cleanup(context.Background())

	// Wait for everything to stop cleanly before ending test
	eg.Wait()
}

// E2E app test of the transaction service.
func TestSpacemeshApp_TransactionService(t *testing.T) {
	listener := "127.0.0.1:14236"

	cfg := config.DefaultTestConfig()
	cfg.DataDirParent = t.TempDir()
	app := New(WithConfig(&cfg), WithLog(logtest.New(t)))

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	app.signers = []*signing.EdSigner{signer}
	address := wallet.Address(signer.PublicKey().Bytes())

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	run := func(c *cobra.Command, args []string) error {
		require.NoError(t, app.Initialize())

		// GRPC configuration
		app.Config.API.PublicListener = listener
		app.Config.API.PublicServices = []grpcserver.Service{grpcserver.Transaction}
		app.Config.API.PrivateServices = nil

		// Prevent obnoxious warning in macOS
		app.Config.P2P.Listen = p2p.MustParseAddresses("/ip4/127.0.0.1/tcp/7073")

		// Avoid waiting for new connections.
		app.Config.P2P.MinPeers = 0

		// syncer will cause the node to go out of sync (and not listen to gossip)
		// since we are testing single-node transaction service, we don't need the syncer to run
		app.Config.Sync.Interval = 1000000 * time.Second
		app.Config.LayerDuration = 2 * time.Second
		app.Config.HARE3.PreroundDelay = 100 * time.Millisecond
		app.Config.HARE3.RoundDuration = 100 * time.Millisecond

		app.Config.Genesis = config.GenesisConfig{
			GenesisTime: config.Genesis(time.Now().Add(20 * time.Second)),
			Accounts: map[string]uint64{
				address.String(): 100_000_000,
			},
		}

		// This will block. We need to run the full app here to make sure that
		// the various services are reporting events correctly. This could probably
		// be done more surgically, and we don't need _all_ of the services.
		return app.Start(appCtx)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run the app in a goroutine. As noted above, it blocks if it succeeds.
	// If there's an error in the args, it will return immediately.
	var wg sync.WaitGroup
	wg.Add(1)
	// nolint:testifylint
	go func() {
		str, err := testArgs(ctx, cmdWithRun(run))
		require.Empty(t, str)
		require.NoError(t, err)
		wg.Done()
	}()

	<-app.Started()
	require.Eventually(
		t,
		func() bool { return app.syncer.IsSynced(ctx) },
		4*time.Second,
		10*time.Millisecond,
	)

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.NewClient(
		listener,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	c := pb.NewTransactionServiceClient(conn)

	tx1 := types.NewRawTx(
		wallet.SelfSpawn(signer.PrivateKey(), 0, sdk.WithGenesisID(cfg.Genesis.GenesisID())),
	)

	stream, err := c.TransactionsStateStream(ctx, &pb.TransactionsStateStreamRequest{
		TransactionId:       []*pb.TransactionId{{Id: tx1.ID.Bytes()}},
		IncludeTransactions: true,
	})
	require.NoError(t, err)
	_, err = stream.Header()
	require.NoError(t, err)

	// TODO(dshulyak) synchronization below is messed up
	var wg2 sync.WaitGroup
	wg2.Add(1)
	// nolint:testifylint
	go func() {
		defer wg2.Done()

		// Now listen on the stream
		res, err := stream.Recv()
		require.NoError(t, err)
		require.Equal(t, tx1.ID.Bytes(), res.TransactionState.Id.Id)

		// We expect the tx to go to the mempool
		inTx := res.Transaction
		require.Equal(t, pb.TransactionState_TRANSACTION_STATE_MEMPOOL, res.TransactionState.State)
		require.Equal(t, tx1.ID.Bytes(), inTx.Id)
		require.Equal(t, address.String(), inTx.Principal.Address)
	}()

	// Submit the txs
	res1, err := c.SubmitTransaction(context.Background(), &pb.SubmitTransactionRequest{
		Transaction: tx1.Raw,
	})
	require.NoError(t, err)
	require.Equal(t, int32(code.Code_OK), res1.Status.Code)
	require.Equal(t, tx1.ID.Bytes(), res1.Txstate.Id.Id)
	require.Equal(t, pb.TransactionState_TRANSACTION_STATE_MEMPOOL, res1.Txstate.State)

	// Wait for messages to be received
	wg2.Wait()

	// This stops the app
	// Cleanup stops all services and thereby the app
	appCancel()
	app.Cleanup(context.Background())

	// Wait for it to stop
	wg.Wait()
}

func TestConfig_Preset(t *testing.T) {
	const name = "testnet"

	t.Run("PresetApplied", func(t *testing.T) {
		preset, err := presets.Get(name)
		require.NoError(t, err)

		conf := config.Config{}
		require.NoError(t, loadConfig(&conf, name, ""))
		require.Equal(t, preset, conf)
	})

	t.Run("PresetOverwrittenByFlags", func(t *testing.T) {
		preset, err := presets.Get(name)
		require.NoError(t, err)

		conf := config.Config{}
		var flags pflag.FlagSet
		cmd.AddFlags(&flags, &conf)

		const lowPeers = 1234
		require.NoError(t, loadConfig(&conf, name, ""))
		require.NoError(t, flags.Parse([]string{"--low-peers=" + strconv.Itoa(lowPeers)}))
		preset.P2P.LowPeers = lowPeers
		require.Equal(t, preset, conf)
	})

	t.Run("PresetOverWrittenByConfigFile", func(t *testing.T) {
		preset, err := presets.Get(name)
		require.NoError(t, err)

		conf := config.Config{}
		const lowPeers = 1234
		content := fmt.Sprintf(`{"p2p": {"low-peers": %d}}`, lowPeers)
		path := filepath.Join(t.TempDir(), "config.json")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		require.NoError(t, loadConfig(&conf, name, path))
		preset.P2P.LowPeers = lowPeers
		require.Equal(t, preset, conf)
	})

	t.Run("LoadedFromConfigFile", func(t *testing.T) {
		preset, err := presets.Get(name)
		require.NoError(t, err)

		conf := config.Config{}
		content := fmt.Sprintf(`{"preset": "%s"}`, name)
		path := filepath.Join(t.TempDir(), "config.json")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		require.NoError(t, loadConfig(&conf, name, path))
		require.NoError(t, err)
		require.Equal(t, preset, conf)
	})
}

func TestConfig_CustomTypes(t *testing.T) {
	const name = "testnet"

	tt := []struct {
		name         string
		cli          string
		config       string
		updatePreset func(*testing.T, *config.Config)
	}{
		{
			name:   "smeshing-opts-provider",
			cli:    "--smeshing-opts-provider=1337",
			config: `{"smeshing": {"smeshing-opts": {"smeshing-opts-provider": 1337}}}`,
			updatePreset: func(t *testing.T, c *config.Config) {
				c.SMESHING.Opts.ProviderID.SetUint32(1337)
			},
		},
		{
			name: "post-pow-difficulty",
			cli:  "--post-pow-difficulty=00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
			config: `{"post": {
				"post-pow-difficulty": "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
				}}`,
			updatePreset: func(t *testing.T, c *config.Config) {
				diff, err := hex.DecodeString(
					"00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
				)
				require.NoError(t, err)
				copy(c.POST.PowDifficulty[:], diff)
			},
		},
		{
			name:   "address-list-single",
			cli:    "--listen=/ip4/0.0.0.0/tcp/5555 --advertise-address=/ip4/10.20.30.40/tcp/5555",
			config: `{"p2p":{"listen":"/ip4/0.0.0.0/tcp/5555","advertise-address":"/ip4/10.20.30.40/tcp/5555"}}`,
			updatePreset: func(t *testing.T, c *config.Config) {
				c.P2P.Listen = p2p.MustParseAddresses("/ip4/0.0.0.0/tcp/5555")
				c.P2P.AdvertiseAddress = p2p.MustParseAddresses("/ip4/10.20.30.40/tcp/5555")
			},
		},
		{
			name: "address-list-multiple",
			cli: "--listen=/ip4/0.0.0.0/tcp/5555 --listen=/ip4/0.0.0.0/udp/5555/quic-v1" +
				" --advertise-address=/ip4/10.20.30.40/tcp/5555" +
				" --advertise-address=/ip4/10.20.30.40/udp/5555/quic-v1",
			config: `{"p2p":{"listen":["/ip4/0.0.0.0/tcp/5555","/ip4/0.0.0.0/udp/5555/quic-v1"],
                                  "advertise-address":[
                                    "/ip4/10.20.30.40/tcp/5555","/ip4/10.20.30.40/udp/5555/quic-v1"]}}`,
			updatePreset: func(t *testing.T, c *config.Config) {
				c.P2P.Listen = p2p.MustParseAddresses(
					"/ip4/0.0.0.0/tcp/5555",
					"/ip4/0.0.0.0/udp/5555/quic-v1")
				c.P2P.AdvertiseAddress = p2p.MustParseAddresses(
					"/ip4/10.20.30.40/tcp/5555",
					"/ip4/10.20.30.40/udp/5555/quic-v1")
			},
		},
	}

	for _, tc := range tt {
		t.Run(fmt.Sprintf("%s_Flags", tc.name), func(t *testing.T) {
			mainnet := config.MainnetConfig()
			require.Nil(t, mainnet.SMESHING.Opts.ProviderID.Value())

			conf := config.MainnetConfig()
			var flags pflag.FlagSet
			cmd.AddFlags(&flags, &conf)

			require.NoError(t, loadConfig(&conf, "", ""))
			require.NoError(t, flags.Parse(strings.Fields(tc.cli)))
			tc.updatePreset(t, &mainnet)
			require.Equal(t, mainnet, conf)
		})

		t.Run(fmt.Sprintf("%s_ConfigFile", tc.name), func(t *testing.T) {
			mainnet := config.MainnetConfig()
			require.Nil(t, mainnet.SMESHING.Opts.ProviderID.Value())

			conf := config.MainnetConfig()
			path := filepath.Join(t.TempDir(), "config.json")
			require.NoError(t, os.WriteFile(path, []byte(tc.config), 0o600))

			require.NoError(t, loadConfig(&conf, "", path))
			tc.updatePreset(t, &mainnet)
			require.Equal(t, mainnet, conf)
		})

		t.Run(fmt.Sprintf("%s_PresetOverwrittenByFlags", tc.name), func(t *testing.T) {
			preset, err := presets.Get(name)
			require.NoError(t, err)

			conf := config.Config{}
			var flags pflag.FlagSet
			cmd.AddFlags(&flags, &conf)

			require.NoError(t, loadConfig(&conf, name, ""))
			require.NoError(t, flags.Parse(strings.Fields(tc.cli)))
			tc.updatePreset(t, &preset)
			require.Equal(t, preset, conf)
		})

		t.Run(fmt.Sprintf("%s_PresetOverWrittenByConfigFile", tc.name), func(t *testing.T) {
			preset, err := presets.Get(name)
			require.NoError(t, err)

			conf := config.Config{}
			path := filepath.Join(t.TempDir(), "config.json")
			require.NoError(t, os.WriteFile(path, []byte(tc.config), 0o600))

			require.NoError(t, loadConfig(&conf, name, path))
			tc.updatePreset(t, &preset)
			require.Equal(t, preset, conf)
		})
	}
}

func TestConfig_PostProviderID_InvalidValues(t *testing.T) {
	tt := []struct {
		name        string
		cliValue    string
		configValue string
	}{
		{
			name:        "not a number",
			cliValue:    "not-a-number",
			configValue: "\"not-a-number\"",
		},
		{
			name:        "negative number",
			cliValue:    "-1",
			configValue: "-1",
		},
		{
			name:        "number too large for uint32",
			cliValue:    "4294967296",
			configValue: "4294967296",
		},
	}

	for _, tc := range tt {
		t.Run(fmt.Sprintf("%s_Flags", tc.name), func(t *testing.T) {
			conf := config.Config{}
			var flags pflag.FlagSet
			cmd.AddFlags(&flags, &conf)

			err := flags.Parse([]string{fmt.Sprintf("--smeshing-opts-provider=%s", tc.cliValue)})
			require.ErrorContains(t, err, "failed to parse PoST Provider ID")
		})

		t.Run(fmt.Sprintf("%s_ConfigFile", tc.name), func(t *testing.T) {
			conf := config.Config{}

			path := filepath.Join(t.TempDir(), "config.json")
			cfg := fmt.Sprintf(`{"smeshing": {"smeshing-opts": {"smeshing-opts-provider": %s}}}`, tc.configValue)
			require.NoError(t, os.WriteFile(path, []byte(cfg), 0o600))
			err := loadConfig(&conf, "", path)
			require.ErrorContains(t, err, "invalid provider ID value")
		})
	}
}

func TestConfig_Load(t *testing.T) {
	t.Run("invalid fails to load", func(t *testing.T) {
		conf := config.Config{}

		path := filepath.Join(t.TempDir(), "config.json")
		require.NoError(t, os.WriteFile(path, []byte("}"), 0o600))

		err := loadConfig(&conf, "", path)
		require.ErrorContains(t, err, path)
	})
	t.Run("missing default doesn't fail", func(t *testing.T) {
		conf := config.Config{}
		var flags pflag.FlagSet
		cmd.AddFlags(&flags, &conf)

		require.NoError(t, loadConfig(&conf, "", ""))
		require.NoError(t, flags.Parse([]string{}))
	})
}

func TestConfig_GenesisAccounts(t *testing.T) {
	conf := config.Config{
		Genesis: config.GenesisConfig{
			Accounts: make(map[string]uint64),
		},
	}
	var flags pflag.FlagSet
	cmd.AddFlags(&flags, &conf)

	const value = 100
	keys := []string{"0x03", "0x04"}
	args := []string{}
	for _, key := range keys {
		args = append(args, fmt.Sprintf("-a %s=%d", key, value))
	}

	require.NoError(t, loadConfig(&conf, "", ""))
	require.NoError(t, flags.Parse(args))
	for _, key := range keys {
		require.EqualValues(t, value, conf.Genesis.Accounts[key])
	}
}

func TestHRP(t *testing.T) {
	conf := config.Config{}
	data := `{"main": {"network-hrp": "TEST"}}`
	cfg := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(cfg, []byte(data), 0o600))
	require.NoError(t, loadConfig(&conf, "", cfg))
	app := New(WithConfig(&conf))
	require.NotNil(t, app)
	require.Equal(t, "TEST", types.NetworkHRP())
}

func TestGenesisConfig(t *testing.T) {
	t.Run("config is written to a file", func(t *testing.T) {
		cfg := getTestDefaultConfig(t)
		app := New(WithConfig(cfg))

		require.NoError(t, app.Initialize())
		t.Cleanup(func() { app.Cleanup(context.Background()) })

		var existing config.GenesisConfig
		require.NoError(t, existing.LoadFromFile(filepath.Join(app.Config.DataDir(), genesisFileName)))
		require.Empty(t, existing.Diff(&app.Config.Genesis))
	})

	t.Run("no error if no diff", func(t *testing.T) {
		cfg := getTestDefaultConfig(t)
		app := New(WithConfig(cfg))

		require.NoError(t, app.Initialize())
		app.Cleanup(context.Background())

		require.NoError(t, app.Initialize())
		app.Cleanup(context.Background())
	})

	t.Run("fatal error on a diff", func(t *testing.T) {
		cfg := getTestDefaultConfig(t)
		app := New(WithConfig(cfg))

		require.NoError(t, app.Initialize())
		t.Cleanup(func() { app.Cleanup(context.Background()) })

		app.Config.Genesis.ExtraData = "changed"
		app.Cleanup(context.Background())
		err := app.Initialize()
		require.ErrorContains(t, err, "genesis config")
	})

	t.Run("long extra data", func(t *testing.T) {
		cfg := getTestDefaultConfig(t)
		cfg.Genesis.ExtraData = string(make([]byte, 256))
		app := New(WithConfig(cfg))

		require.ErrorContains(t, app.Initialize(), "extra-data")
	})
}

func TestFlock(t *testing.T) {
	t.Run("sanity", func(t *testing.T) {
		cfg := getTestDefaultConfig(t)
		app := New(WithConfig(cfg))

		require.NoError(t, app.Lock())
		t.Cleanup(app.Unlock)

		app1 := *app
		require.ErrorContains(t, app1.Lock(), "only one spacemesh instance")
		app.Unlock()
		require.NoError(t, app.Lock())
	})

	t.Run("dir doesn't exist", func(t *testing.T) {
		cfg := getTestDefaultConfig(t)
		cfg.FileLock = filepath.Join(t.TempDir(), "newdir", "LOCK")
		app := New(WithConfig(cfg))

		require.NoError(t, app.Lock())
		t.Cleanup(app.Unlock)
	})
}

func TestEmptyExtraData(t *testing.T) {
	cfg := getTestDefaultConfig(t)
	cfg.Genesis.ExtraData = ""
	app := New(WithConfig(cfg), WithLog(logtest.New(t)))
	require.Error(t, app.Initialize())
}

func TestAdminEvents(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	cfg, err := presets.Get("standalone")
	require.NoError(t, err)
	cfg.DataDirParent = t.TempDir()
	cfg.FileLock = filepath.Join(cfg.DataDirParent, "LOCK")
	cfg.SMESHING.Opts.DataDir = t.TempDir()
	cfg.SMESHING.Opts.Scrypt.N = 2
	cfg.SMESHING.Start = true
	cfg.POSTService.PostServiceCmd = activation.DefaultTestPostServiceConfig().PostServiceCmd

	cfg.Genesis.GenesisTime = config.Genesis(time.Now().Add(5 * time.Second))
	types.SetLayersPerEpoch(cfg.LayersPerEpoch)

	logger := logtest.New(t, zapcore.DebugLevel)
	app := New(WithConfig(&cfg), WithLog(logger))
	require.NoError(t, app.Initialize())
	require.NoError(t, app.NewIdentity())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var eg errgroup.Group
	eg.Go(func() error {
		if err := app.Start(ctx); err != nil {
			return err
		}
		app.Cleanup(context.Background())
		return nil
	})
	t.Cleanup(func() { assert.NoError(t, eg.Wait()) })

	select {
	case <-app.Started():
	case <-time.After(15 * time.Second):
		require.Fail(t, "app did not start in time")
	}

	conn, err := grpc.NewClient(
		"127.0.0.1:10093",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, conn.Close()) })
	client := pb.NewAdminServiceClient(conn)

	tctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	// 4 is arbitrary, if we received events once, they must be
	// cached and should be returned immediately
	for i := 0; i < 4; i++ {
		stream, err := client.EventsStream(tctx, &pb.EventStreamRequest{})
		require.NoError(t, err)
		success := []pb.IsEventDetails{
			&pb.Event_Beacon{},
			&pb.Event_InitStart{},
			&pb.Event_InitComplete{},
			&pb.Event_PostServiceStarted{},
			&pb.Event_PostStart{},
			&pb.Event_PostComplete{},
			&pb.Event_PoetWaitRound{},
			&pb.Event_PoetWaitProof{},
			&pb.Event_PostStart{},
			&pb.Event_PostComplete{},
			&pb.Event_AtxPublished{},
		}
		for idx, ev := range success {
			msg, err := stream.Recv()
			require.NoError(t, err, "stream %d", i)
			require.IsType(t, ev, msg.Details, "stream %d, event %d", i, idx)
		}
		require.NoError(t, stream.CloseSend())
	}
}

func TestAdminEvents_MultiSmesher(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	cfg, err := presets.Get("standalone")
	require.NoError(t, err)
	cfg.DataDirParent = t.TempDir()
	cfg.FileLock = filepath.Join(cfg.DataDirParent, "LOCK")
	cfg.SMESHING.Opts.Scrypt.N = 2
	cfg.SMESHING.Start = false
	cfg.API.PostListener = "0.0.0.0:10094"
	cfg.POSTService.PostServiceCmd = activation.DefaultTestPostServiceConfig().PostServiceCmd

	cfg.Genesis.GenesisTime = config.Genesis(time.Now().Add(5 * time.Second))
	types.SetLayersPerEpoch(cfg.LayersPerEpoch)

	logger := zaptest.NewLogger(t)
	app := New(WithConfig(&cfg), WithLog(log.NewFromLog(logger)))

	dir := filepath.Join(app.Config.DataDir(), keyDir)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	for i := 0; i < 2; i++ {
		signer, err := signing.NewEdSigner(
			signing.WithPrefix(app.Config.Genesis.GenesisID().Bytes()),
		)
		require.NoError(t, err)

		keyFile := filepath.Join(dir, fmt.Sprintf("node_%d.key", i))
		dst := make([]byte, hex.EncodedLen(len(signer.PrivateKey())))
		hex.Encode(dst, signer.PrivateKey())
		require.NoError(t, os.WriteFile(keyFile, dst, 0o600))
	}

	require.NoError(t, app.LoadIdentities())
	require.Len(t, app.signers, 2)
	require.NoError(t, app.Initialize())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var eg errgroup.Group
	eg.Go(func() error {
		if err := app.Start(ctx); err != nil {
			return err
		}
		app.Cleanup(context.Background())
		return nil
	})
	t.Cleanup(func() { assert.NoError(t, eg.Wait()) })

	select {
	case <-app.Started():
	case <-time.After(15 * time.Second):
		require.Fail(t, "app did not start in time")
	}

	for _, signer := range app.signers {
		mgr, err := activation.NewPostSetupManager(
			cfg.POST,
			logger,
			app.db,
			app.atxsdata,
			types.ATXID(app.Config.Genesis.GoldenATX()),
			app.syncer,
			app.validator,
		)
		require.NoError(t, err)

		cfg.SMESHING.Opts.DataDir = t.TempDir()
		t.Cleanup(launchPostSupervisor(t,
			logger,
			mgr,
			signer,
			"127.0.0.1:10094",
			cfg.POST,
			cfg.SMESHING.Opts,
		))
	}

	conn, err := grpc.NewClient(
		"127.0.0.1:10093",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, conn.Close()) })
	client := pb.NewAdminServiceClient(conn)

	tctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	// 4 is arbitrary, if we received events once, they must be
	// cached and should be returned immediately
	for i := 0; i < 4; i++ {
		stream, err := client.EventsStream(tctx, &pb.EventStreamRequest{})
		require.NoError(t, err)

		matchers := map[int]func(pb.IsEventDetails) bool{
			0: func(ev pb.IsEventDetails) bool {
				_, ok := ev.(*pb.Event_Beacon)
				return ok
			},
			1: func(ev pb.IsEventDetails) bool {
				startEv, ok := ev.(*pb.Event_PostStart)
				if !ok {
					return false
				}
				return bytes.Equal(startEv.PostStart.Smesher, app.signers[0].NodeID().Bytes()) &&
					bytes.Equal(startEv.PostStart.Challenge, shared.ZeroChallenge)
			},
			2: func(ev pb.IsEventDetails) bool {
				completeEv, ok := ev.(*pb.Event_PostComplete)
				if !ok {
					return false
				}
				return bytes.Equal(completeEv.PostComplete.Smesher, app.signers[0].NodeID().Bytes()) &&
					bytes.Equal(completeEv.PostComplete.Challenge, shared.ZeroChallenge)
			},
			3: func(ev pb.IsEventDetails) bool {
				startEv, ok := ev.(*pb.Event_PostStart)
				if !ok {
					return false
				}
				return bytes.Equal(startEv.PostStart.Smesher, app.signers[1].NodeID().Bytes()) &&
					bytes.Equal(startEv.PostStart.Challenge, shared.ZeroChallenge)
			},
			4: func(ev pb.IsEventDetails) bool {
				completeEv, ok := ev.(*pb.Event_PostComplete)
				if !ok {
					return false
				}
				return bytes.Equal(completeEv.PostComplete.Smesher, app.signers[1].NodeID().Bytes()) &&
					bytes.Equal(completeEv.PostComplete.Challenge, shared.ZeroChallenge)
			},
			5: func(ev pb.IsEventDetails) bool {
				// TODO(mafa): this event happens once for each NodeID, but should probably only happen once for all
				_, ok := ev.(*pb.Event_PoetWaitRound)
				return ok
			},
			6: func(ev pb.IsEventDetails) bool {
				// TODO(mafa): this event happens once for each NodeID, but should probably only happen once for all
				_, ok := ev.(*pb.Event_PoetWaitProof)
				return ok
			},
			7: func(ev pb.IsEventDetails) bool {
				startEv, ok := ev.(*pb.Event_PostStart)
				if !ok {
					return false
				}
				return bytes.Equal(startEv.PostStart.Smesher, app.signers[0].NodeID().Bytes()) &&
					!bytes.Equal(startEv.PostStart.Challenge, shared.ZeroChallenge)
			},
			8: func(ev pb.IsEventDetails) bool {
				completeEv, ok := ev.(*pb.Event_PostComplete)
				if !ok {
					return false
				}
				return bytes.Equal(completeEv.PostComplete.Smesher, app.signers[0].NodeID().Bytes()) &&
					!bytes.Equal(completeEv.PostComplete.Challenge, shared.ZeroChallenge)
			},
			9: func(ev pb.IsEventDetails) bool {
				startEv, ok := ev.(*pb.Event_PostStart)
				if !ok {
					return false
				}
				return bytes.Equal(startEv.PostStart.Smesher, app.signers[1].NodeID().Bytes()) &&
					!bytes.Equal(startEv.PostStart.Challenge, shared.ZeroChallenge)
			},
			10: func(ev pb.IsEventDetails) bool {
				completeEv, ok := ev.(*pb.Event_PostComplete)
				if !ok {
					return false
				}
				return bytes.Equal(completeEv.PostComplete.Smesher, app.signers[1].NodeID().Bytes()) &&
					!bytes.Equal(completeEv.PostComplete.Challenge, shared.ZeroChallenge)
			},
			11: func(ev pb.IsEventDetails) bool {
				_, ok := ev.(*pb.Event_AtxPublished)
				return ok
			},
			12: func(ev pb.IsEventDetails) bool {
				_, ok := ev.(*pb.Event_AtxPublished)
				return ok
			},
		}
		for {
			msg, err := stream.Recv()
			require.NoError(t, err, "stream %d", i)
			for idx, matcher := range matchers {
				if matcher(msg.Details) {
					t.Log("matched event", idx)
					delete(matchers, idx)
					break
				}
			}
			if len(matchers) == 0 {
				break
			}
		}
		require.NoError(t, stream.CloseSend())
	}
}

func launchPostSupervisor(
	tb testing.TB,
	log *zap.Logger,
	mgr *activation.PostSetupManager,
	sig *signing.EdSigner,
	address string,
	postCfg activation.PostConfig,
	postOpts activation.PostSetupOpts,
) func() {
	cmdCfg := activation.DefaultTestPostServiceConfig()
	cmdCfg.NodeAddress = fmt.Sprintf("http://%s", address)
	provingOpts := activation.DefaultPostProvingOpts()
	provingOpts.RandomXMode = activation.PostRandomXModeLight

	builder := activation.NewMockAtxBuilder(gomock.NewController(tb))
	builder.EXPECT().Register(sig)
	ps := activation.NewPostSupervisor(log, postCfg, provingOpts, mgr, builder)
	require.NoError(tb, ps.Start(cmdCfg, postOpts, sig))
	return func() { assert.NoError(tb, ps.Stop(false)) }
}

func getTestDefaultConfig(tb testing.TB) *config.Config {
	cfg := config.MainnetConfig()
	types.SetNetworkHRP(cfg.NetworkHRP)
	types.SetLayersPerEpoch(cfg.LayersPerEpoch)

	tmp := tb.TempDir()
	cfg.DataDirParent = tmp
	cfg.FileLock = filepath.Join(tmp, "LOCK")
	cfg.LayerDuration = 20 * time.Second

	// is set to 0 to make sync start immediately when node starts
	cfg.P2P.MinPeers = 0

	cfg.POST = activation.DefaultPostConfig()
	cfg.POST.MinNumUnits = 2
	cfg.POST.MaxNumUnits = 4
	cfg.POST.LabelsPerUnit = 32
	cfg.POST.K2 = 4

	cfg.SMESHING = config.DefaultSmeshingConfig()
	cfg.SMESHING.Start = false
	cfg.SMESHING.CoinbaseAccount = types.GenerateAddress([]byte{1}).String()
	cfg.SMESHING.Opts.DataDir = filepath.Join(tmp, "post")
	cfg.SMESHING.Opts.NumUnits = cfg.POST.MinNumUnits + 1
	cfg.SMESHING.Opts.Scrypt.N = 2
	cfg.SMESHING.Opts.ProviderID.SetUint32(initialization.CPUProviderID())

	cfg.HARE3.RoundDuration = 2
	cfg.HARE3.PreroundDelay = 1

	cfg.HARE4.RoundDuration = 2
	cfg.HARE4.PreroundDelay = 1

	cfg.LayerAvgSize = 5
	cfg.LayersPerEpoch = 3
	cfg.TxsPerProposal = 100
	cfg.Tortoise.Hdist = 5
	cfg.Tortoise.Zdist = 5

	cfg.HareEligibility.ConfidenceParam = 1
	cfg.Sync.Interval = 2 * time.Second

	cfg.FETCH.RequestTimeout = 10 * time.Second
	cfg.FETCH.RequestHardTimeout = 20 * time.Second
	cfg.FETCH.BatchSize = 5
	cfg.FETCH.BatchTimeout = 5 * time.Second

	cfg.Beacon = beacon.NodeSimUnitTestConfig()
	cfg.Genesis = config.DefaultTestGenesisConfig()
	cfg.POSTService = activation.DefaultTestPostServiceConfig()

	return &cfg
}
