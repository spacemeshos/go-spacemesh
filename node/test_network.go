package node

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
)

// NewTestNetwork creates a network of fully connected nodes.
func NewTestNetwork(tb testing.TB, conf config.Config, l log.Log, size int) []*TestApp {
	// We need to set this global state
	types.SetLayersPerEpoch(conf.LayersPerEpoch)
	types.SetNetworkHRP(conf.NetworkHRP) // set to generate coinbase

	// To save an epoch of startup time, we bootstrap (meaning we manually set
	// it) the beacon for epoch 2 so that in epoch 3 hare can start.
	bootstrapEpoch := (types.GetEffectiveGenesis() + 1).GetEpoch()
	bootstrapBeacon := types.Beacon{}
	genesis := conf.Genesis.GenesisID()
	copy(bootstrapBeacon[:], genesis[:])

	// This context is used to call Start on a node and canceling it will
	// shutdown the node. (Hence no timeout has been set).
	ctx, cancel := context.WithCancel(context.Background())
	g, grpContext := errgroup.WithContext(ctx)
	var apps []*TestApp

	for i := 0; i < size; i++ {
		// Copy config, services don't modify their config, so we just need to
		// be careful here when we modify any pointer values in the config.
		c := conf
		dir := tb.TempDir()
		c.DataDirParent = dir
		c.SMESHING.Opts.DataDir = dir
		c.SMESHING.CoinbaseAccount = types.GenerateAddress([]byte(strconv.Itoa(i))).String()

		c.FileLock = filepath.Join(c.DataDirParent, "LOCK")

		app := NewApp(tb, &c, l)
		g.Go(func() error {
			err := app.Start(grpContext)
			if err != nil && !errors.Is(err, context.Canceled) {
				tb.Logf("failed to start instance %d: %v", i, err)
			}
			return err
		})
		<-app.Started()
		err := app.beaconProtocol.UpdateBeacon(bootstrapEpoch, bootstrapBeacon)
		require.NoError(tb, err, "failed to bootstrap beacon for node %q", i)

		conn, err := grpc.NewClient(
			app.grpcPublicServer.BoundAddress,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		require.NoError(tb, err)
		tb.Cleanup(func() { assert.NoError(tb, conn.Close()) })
		apps = append(apps, &TestApp{app, conn})
	}

	// Note that we must call cleanup after all calls to t.TempDir since calls
	// to cleanup are executed in LIFO fashion (similar to defer) and t.TempDir
	// internally calls Cleanup to delete the dir. By calling Cleanup here
	// we ensure that the apps have been shut-down before attempting to delete
	// the temp dirs.
	tb.Cleanup(func() {
		cancel()
		// Wait for nodes to shutdown
		g.Wait()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()
		for _, a := range apps {
			a.Cleanup(ctx)
		}
	})

	// Connect all nodes to each other
	for i := 0; i < size; i++ {
		for j := i + 1; j < size; j++ {
			err := apps[i].Host().Connect(context.Background(), peer.AddrInfo{
				ID:    apps[j].Host().ID(),
				Addrs: apps[j].Host().Addrs(),
			})
			require.NoError(tb, err)
		}
	}
	return apps
}

func NewApp(tb testing.TB, conf *config.Config, l log.Log) *App {
	app := New(
		WithConfig(conf),
		WithLog(l),
	)

	err := app.Initialize()
	require.NoError(tb, err)

	err = app.NewIdentity()
	require.NoError(tb, err, "could not create identity")

	return app
}

type TestApp struct {
	*App
	Conn *grpc.ClientConn
}
