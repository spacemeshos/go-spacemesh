// package hare is the tester executable running instances of hare consensus algorithm
package main

import (
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
	"runtime"
	"runtime/pprof"
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

func (mbp *mockBlockProvider) HandleValidatedLayer(validatedLayer types.LayerID, layer []types.BlockID) {
}

func (mbp *mockBlockProvider) LayerBlockIds(types.LayerID) ([]types.BlockID, error) {
	return buildSet(), nil
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
func IsSynced() bool {
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
		s = append(s, types.NewExistingBlock(types.GetEffectiveGenesis()+1, util.Uint64ToBytes(i)).ID())
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

func (msq mockStateQuerier) IsIdentityActiveOnConsensusView(edID string, layer types.LayerID) (bool, error) {
	return true, nil
}

func validateBlocks(blocks []types.BlockID) bool {
	return true
}

// Start the app
func (app *HareApp) Start(cmd *cobra.Command, args []string) {
	log.Info("Starting hare main")

	if app.Config.MemProfile != "" {
		log.Info("Starting mem profiling")
		f, err := os.Create(app.Config.MemProfile)
		if err != nil {
			log.Error("could not create memory profile: ", err)
		}
		defer f.Close()
		runtime.GC() // get up-to-date statistics
		if err := pprof.WriteHeapProfile(f); err != nil {
			log.Error("could not write memory profile: ", err)
		}
	}

	if app.Config.PprofHTTPServer {
		log.Info("Starting pprof server")
		go func() {
			err := http.ListenAndServe(":6060", nil)
			if err != nil {
				log.Error("cannot start http server", err)
			}
		}()
	}
	types.SetLayersPerEpoch(int32(app.Config.LayersPerEpoch))
	log.Info("Initializing P2P services")
	swarm, err := p2p.New(cmdp.Ctx, app.Config.P2P, log.NewDefault("p2p_haretest"), app.Config.DataDir())
	app.p2p = swarm
	if err != nil {
		log.Panic("Error starting p2p services err=%v", err)
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
	log.Info("Starting hare service")
	app.ha = hareI
	err = app.ha.Start()
	if err != nil {
		log.Panic("error starting maatuf err=%v", err)
	}
	err = app.p2p.Start()
	if err != nil {
		log.Panic("error starting p2p err=%v", err)
	}
	if gTime.After(time.Now()) {
		log.Info("sleeping until %v", gTime)
		time.Sleep(time.Duration(gTime.Sub(time.Now())))
	}
	startLayer := types.GetEffectiveGenesis() + 1
	lt <- types.LayerID(startLayer)
}

func main() {
	if err := Cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
