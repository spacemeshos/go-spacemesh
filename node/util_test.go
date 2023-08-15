package node

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
)

// NewTestNetwork creates a network of fully connected nodes.
func NewTestNetwork(t *testing.T, conf config.Config, l log.Log, size int) []*TestApp {
	// We need to set this global state
	types.SetLayersPerEpoch(conf.LayersPerEpoch)
	types.SetNetworkHRP(conf.NetworkHRP) // set to generate coinbase

	// To save an epoch of startup time, we bootstrap (meaning we manually set
	// it) the beacon for epoch 2 so that in epoch 3 hare can start.
	bootstrapEpoch := (types.GetEffectiveGenesis() + 1).GetEpoch()
	bootstrapBeacon := types.Beacon{}
	genesis := conf.Genesis.GenesisID()
	copy(bootstrapBeacon[:], genesis[:])

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	g := errgroup.Group{}
	var apps []*TestApp

	t.Cleanup(func() {
		cancel()
		// Wait for nodes to shutdown
		g.Wait()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()
		for _, a := range apps {
			a.Cleanup(ctx)
		}
	})

	// We encode and decode the config in order to deep copy it.
	var buf bytes.Buffer
	err := gob.NewEncoder(&buf).Encode(conf)
	require.NoError(t, err)
	marshaled := buf.Bytes()

	for i := 0; i < size; i++ {
		var c config.Config
		err := gob.NewDecoder(bytes.NewBuffer(marshaled)).Decode(&c)
		require.NoError(t, err)

		dir := t.TempDir()
		c.DataDirParent = dir
		c.SMESHING.Opts.DataDir = dir
		c.SMESHING.CoinbaseAccount = types.GenerateAddress([]byte(strconv.Itoa(i))).String()
		c.FileLock = filepath.Join(c.DataDirParent, "LOCK")

		app := NewApp(t, &c, l)
		g.Go(func() error {
			err := app.Start(ctx)
			if !errors.Is(err, context.Canceled) {
				t.Logf("failed to start instance %d: %v", i, err)
			}
			return err
		})
		<-app.Started()
		err = app.beaconProtocol.UpdateBeacon(bootstrapEpoch, bootstrapBeacon)
		require.NoError(t, err, "failed to bootstrap beacon for node %q", i)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		conn, err := grpc.DialContext(ctx, app.grpcPublicService.BoundAddress,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
		)
		require.NoError(t, err)
		apps = append(apps, NewTestApp(app, conn))
	}

	// Connect all nodes to each other
	for i := 0; i < size; i++ {
		for j := i + 1; j < size; j++ {
			err = apps[i].Host().Connect(context.Background(), peer.AddrInfo{
				ID:    apps[j].Host().ID(),
				Addrs: apps[j].Host().Addrs(),
			})
			require.NoError(t, err)
		}
	}
	return apps
}

func NewApp(t *testing.T, conf *config.Config, l log.Log) *App {
	app := New(
		WithConfig(conf),
		WithLog(l),
	)

	err := app.Initialize()
	require.NoError(t, err)

	/* Create or load miner identity */
	app.edSgn, err = app.LoadOrCreateEdSigner()
	require.NoError(t, err, "could not retrieve identity")

	return app
}

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
