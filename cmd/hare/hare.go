// package hare is the tester executable running instances of hare consensus algorithm
package main

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/layerpatrol"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/lp2p"
	"github.com/spacemeshos/go-spacemesh/monitoring"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

// Cmd the command of the hare app.
var Cmd = &cobra.Command{
	Use:   "hare",
	Short: "start hare",
	Run: func(cmd *cobra.Command, args []string) {
		log.JSONLog(true)
		hareApp := NewHareApp()
		defer hareApp.Cleanup()

		// monitor app
		hareApp.updater = monitoring.NewMemoryUpdater()
		hareApp.monitor = monitoring.NewMonitor(1*time.Second, 5*time.Second, hareApp.updater, make(chan struct{}))
		hareApp.monitor.Start()

		// start app
		hareApp.Initialize(cmd)
		hareApp.Start(cmd, args)
		<-hareApp.ha.CloseChannel()
	},
}

func init() {
	cmdp.AddCommands(Cmd)
}

type mockBlockProvider struct{}

func (mbp *mockBlockProvider) HandleValidatedLayer(context.Context, types.LayerID, []types.BlockID) {
}

func (mbp *mockBlockProvider) InvalidateLayer(context.Context, types.LayerID) {
}

func (mbp *mockBlockProvider) RecordCoinflip(context.Context, types.LayerID, bool) {
}

func (mbp *mockBlockProvider) LayerBlocks(lyr types.LayerID) ([]*types.Block, error) {
	s := make([]*types.Block, 200)
	for i := uint64(0); i < 200; i++ {
		blk := types.NewExistingBlock(lyr, util.Uint64ToBytes(i), nil)
		blk.TortoiseBeacon = lyr.GetEpoch().ToBytes()
		s[i] = blk
	}
	return s, nil
}

func (mbp *mockBlockProvider) GetBlock(types.BlockID) (*types.Block, error) {
	return nil, nil
}

type mockBeaconGetter struct{}

func (mbg *mockBeaconGetter) GetBeacon(id types.EpochID) ([]byte, error) {
	return id.ToBytes(), nil
}

// HareApp represents an Hare application.
type HareApp struct {
	*cmdp.BaseApp
	host    *lp2p.Host
	oracle  *oracleClient
	sgn     hare.Signer
	ha      *hare.Hare
	clock   *timesync.TimeClock
	updater *monitoring.MemoryUpdater
	monitor *monitoring.Monitor
}

// IsSynced returns true always as we assume the node is synced.
func IsSynced(context.Context) bool {
	return true
}

// NewHareApp returns a new instance.
func NewHareApp() *HareApp {
	return &HareApp{BaseApp: cmdp.NewBaseApp(), sgn: signing.NewEdSigner()}
}

// Cleanup just unregisters the oracle.
func (app *HareApp) Cleanup() {
	// TODO: move to array of cleanup functions and execute all here
	app.oracle.Unregister(true, app.sgn.PublicKey().String())
}

type mockIDProvider struct{}

func (mip *mockIDProvider) GetIdentity(edID string) (types.NodeID, error) {
	return types.NodeID{Key: edID, VRFPublicKey: []byte{}}, nil
}

type mockStateQuerier struct{}

func (msq mockStateQuerier) IsIdentityActiveOnConsensusView(ctx context.Context, edID string, layer types.LayerID) (bool, error) {
	return true, nil
}

// Start the app.
func (app *HareApp) Start(cmd *cobra.Command, args []string) {
	log.Info("starting hare main")

	log.JSONLog(true)
	logger := log.NewWithLevel("HARE_TEST", zap.NewAtomicLevelAt(zap.DebugLevel))
	log.SetupGlobal(logger)

	if app.Config.PprofHTTPServer {
		log.Info("starting pprof server")
		go func() {
			err := http.ListenAndServe(":6060", nil)
			if err != nil {
				log.With().Error("cannot start http server", log.Err(err))
			}
		}()
	}
	types.SetLayersPerEpoch(app.Config.LayersPerEpoch)
	log.Info("initializing P2P services")

	pub := app.sgn.PublicKey()

	cfg := app.Config.P2P
	cfg.DataDir = filepath.Join(app.Config.DataDir(), "p2p")
	host, err := lp2p.New(cmdp.Ctx, logger, cfg)
	if err != nil {
		log.With().Panic("error starting p2p services", log.Err(err))
	}
	app.host = host

	setServerAddress(app.Config.OracleServer)
	app.oracle = newClientWithWorldID(uint64(app.Config.OracleServerWorldID))
	app.oracle.Register(true, pub.String()) // todo: configure no faulty nodes
	hareOracle := newHareOracleFromClient(app.oracle)

	gTime, err := time.Parse(time.RFC3339, app.Config.GenesisTime)
	if err != nil {
		log.With().Error("Failed to parse genesis time as RFC3339",
			log.String("genesis_time", app.Config.GenesisTime))
	}
	// TODO: consider uncommenting
	/*if err != nil {
		log.Panic("error parsing config err=%v", err)
	}*/
	//ld := time.Duration(app.Config.LayerDurationSec) * time.Second
	//app.clock = timesync.NewClock(timesync.RealClock{}, ld, gTime, lg)
	lt := make(timesync.LayerTimer)

	hareI := hare.New(app.Config.HARE, host.ID(), host, app.sgn, types.NodeID{Key: app.sgn.PublicKey().String(), VRFPublicKey: []byte{}},
		IsSynced, &mockBlockProvider{}, &mockBeaconGetter{}, hareOracle,
		layerpatrol.New(), uint16(app.Config.LayersPerEpoch), &mockIDProvider{}, &mockStateQuerier{}, lt, logger)
	log.Info("starting hare service")
	app.ha = hareI
	if err = app.ha.Start(cmdp.Ctx); err != nil {
		log.With().Panic("error starting hare", log.Err(err))
	}
	if gTime.After(time.Now()) {
		log.Info("sleeping until %v", gTime)
		time.Sleep(gTime.Sub(time.Now()))
	}
	lt <- types.GetEffectiveGenesis().Add(1)
}

func main() {
	if err := Cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
