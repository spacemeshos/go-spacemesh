// package hare is the tester executable running instances of hare consensus algorithm
package main

import (
	"context"
	"fmt"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/monitoring"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"net/http"
	"os"
	"time"
)

import _ "net/http/pprof"

// Cmd the command of the hare app
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

type mockBlockProvider struct {
}

func (mbp *mockBlockProvider) HandleValidatedLayer(ctx context.Context, validatedLayer types.LayerID, layer []types.BlockID) {
}

func (mbp *mockBlockProvider) LayerBlockIds(types.LayerID) ([]types.BlockID, error) {
	return buildSet(), nil
}

func (mbp *mockBlockProvider) RecordCoinflip(ctx context.Context, layerID types.LayerID, coinflip bool) {
}

// HareApp represents an Hare application
type HareApp struct {
	*cmdp.BaseApp
	p2p     p2p.Service
	oracle  *oracleClient
	sgn     hare.Signer
	ha      *hare.Hare
	clock   *timesync.TimeClock
	updater *monitoring.MemoryUpdater
	monitor *monitoring.Monitor
}

// IsSynced returns true always as we assume the node is synced
func IsSynced(context.Context) bool {
	return true
}

// NewHareApp returns a new instance
func NewHareApp() *HareApp {
	return &HareApp{BaseApp: cmdp.NewBaseApp(), sgn: signing.NewEdSigner()}
}

// Cleanup just unregisters the oracle
func (app *HareApp) Cleanup() {
	// TODO: move to array of cleanup functions and execute all here
	app.oracle.Unregister(true, app.sgn.PublicKey().String())
}

func buildSet() []types.BlockID {
	s := make([]types.BlockID, 200, 200)

	for i := uint64(0); i < 200; i++ {
		s = append(s, types.NewExistingBlock(types.GetEffectiveGenesis()+1, util.Uint64ToBytes(i), nil).ID())
	}

	return s
}

type mockIDProvider struct {
}

func (mip *mockIDProvider) GetIdentity(edID string) (types.NodeID, error) {
	return types.NodeID{Key: edID, VRFPublicKey: []byte{}}, nil
}

type mockStateQuerier struct {
}

func (msq mockStateQuerier) IsIdentityActiveOnConsensusView(ctx context.Context, edID string, layer types.LayerID) (bool, error) {
	return true, nil
}

func validateBlocks(blocks []types.BlockID) bool {
	return true
}

// Start the app
func (app *HareApp) Start(cmd *cobra.Command, args []string) {
	log.Info("starting hare main")

	if app.Config.PprofHTTPServer {
		log.Info("starting pprof server")
		go func() {
			err := http.ListenAndServe(":6060", nil)
			if err != nil {
				log.With().Error("cannot start http server", log.Err(err))
			}
		}()
	}
	types.SetLayersPerEpoch(int32(app.Config.LayersPerEpoch))
	log.Info("initializing P2P services")
	swarm, err := p2p.New(cmdp.Ctx, app.Config.P2P, log.NewDefault("p2p_haretest"), app.Config.DataDir())
	app.p2p = swarm
	if err != nil {
		log.With().Panic("error starting p2p services", log.Err(err))
	}

	pub := app.sgn.PublicKey()

	lg1 := log.NewDefault(pub.String())
	lvl := zap.NewAtomicLevel()
	lvl.SetLevel(zapcore.DebugLevel)
	lg := lg1.SetLevel(&lvl)

	setServerAddress(app.Config.OracleServer)
	app.oracle = newClientWithWorldID(uint64(app.Config.OracleServerWorldID))
	app.oracle.Register(true, pub.String()) // todo: configure no faulty nodes
	hareOracle := newHareOracleFromClient(app.oracle)

	gTime, err := time.Parse(time.RFC3339, app.Config.GenesisTime)
	/*if err != nil {
		log.Panic("error parsing config err=%v", err)
	}*/
	//ld := time.Duration(app.Config.LayerDurationSec) * time.Second
	//app.clock = timesync.NewClock(timesync.RealClock{}, ld, gTime, lg)
	lt := make(timesync.LayerTimer)

	hareI := hare.New(app.Config.HARE, app.p2p, app.sgn, types.NodeID{Key: app.sgn.PublicKey().String(), VRFPublicKey: []byte{}}, validateBlocks, IsSynced, &mockBlockProvider{}, hareOracle, uint16(app.Config.LayersPerEpoch), &mockIDProvider{}, &mockStateQuerier{}, lt, lg)
	log.Info("starting hare service")
	app.ha = hareI
	if err = app.ha.Start(cmdp.Ctx); err != nil {
		log.With().Panic("error starting hare", log.Err(err))
	}
	if err = app.p2p.Start(cmdp.Ctx); err != nil {
		log.With().Panic("error starting p2p", log.Err(err))
	}
	if gTime.After(time.Now()) {
		log.Info("sleeping until %v", gTime)
		time.Sleep(gTime.Sub(time.Now()))
	}
	startLayer := types.GetEffectiveGenesis() + 1
	lt <- startLayer
}

func main() {
	if err := Cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
