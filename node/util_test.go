package node

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
)

// NewNetwork creates a network of fully connected nodes.
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

	// We encode and decode the config in order to deep copy it.
	var buf bytes.Buffer
	err := gob.NewEncoder(&buf).Encode(conf)
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
			err := app.Start(ctx)
			println(err)
			return err
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

	return app, err
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
