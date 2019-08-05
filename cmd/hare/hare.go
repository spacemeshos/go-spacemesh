package main

import (
	"fmt"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/monitoring"
	"github.com/spacemeshos/go-spacemesh/oracle"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spf13/cobra"
	"net/http"
	"os"
	"runtime"
	"runtime/pprof"
	"time"
)

import _ "net/http/pprof"

// Hare cmd
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

func (mbp *mockBlockProvider) GetUnverifiedLayerBlocks(layerId types.LayerID) ([]types.BlockID, error) {
	return buildSet(), nil
}

type HareApp struct {
	*cmdp.BaseApp
	p2p     p2p.Service
	oracle  *oracle.OracleClient
	sgn     hare.Signer
	ha      *hare.Hare
	clock   *timesync.Ticker
	updater *monitoring.MemoryUpdater
	monitor *monitoring.Monitor
}

func IsSynced() bool {
	return true
}

func NewHareApp() *HareApp {
	return &HareApp{BaseApp: cmdp.NewBaseApp(), sgn: signing.NewEdSigner()}
}

func (app *HareApp) Cleanup() {
	// TODO: move to array of cleanup functions and execute all here
	app.oracle.Unregister(true, app.sgn.PublicKey().String())
}

func buildSet() []types.BlockID {
	s := make([]types.BlockID, 200, 200)

	for i := uint64(0); i < 200; i++ {
		s = append(s, types.BlockID(i))
	}

	return s
}

type mockIdProvider struct {
}

func (mip *mockIdProvider) GetIdentity(edId string) (types.NodeId, error) {
	return types.NodeId{Key: edId, VRFPublicKey: []byte{}}, nil
}

type mockStateQuerier struct {
}

func (msq mockStateQuerier) IsIdentityActiveOnConsensusView(edId string, layer types.LayerID) (bool, error) {
	return true, nil
}

func validateBlocks(blocks []types.BlockID) bool {
	return true
}

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

	if app.Config.PprofHttpServer {
		log.Info("Starting pprof server")
		go func() {
			err := http.ListenAndServe(":6060", nil)
			if err != nil {
				log.Error("cannot start http server", err)
			}
		}()
	}

	log.Info("Initializing P2P services")
	swarm, err := p2p.New(cmdp.Ctx, app.Config.P2P)
	app.p2p = swarm
	if err != nil {
		log.Panic("Error starting p2p services err=%v", err)
	}

	pub := app.sgn.PublicKey()

	lg := log.NewDefault(pub.String())

	oracle.SetServerAddress(app.Config.OracleServer)
	app.oracle = oracle.NewOracleClientWithWorldID(uint64(app.Config.OracleServerWorldId))
	app.oracle.Register(true, pub.String()) // todo: configure no faulty nodes
	hareOracle := oracle.NewHareOracleFromClient(app.oracle)

	gTime, err := time.Parse(time.RFC3339, app.Config.GenesisTime)
	if err != nil {
		log.Panic("error parsing config err=%v", err)
	}
	ld := time.Duration(app.Config.LayerDurationSec) * time.Second
	app.clock = timesync.NewTicker(timesync.RealClock{}, ld, gTime)

	app.ha = hare.New(app.Config.HARE, app.p2p, app.sgn, types.NodeId{Key: app.sgn.PublicKey().String(), VRFPublicKey: []byte{}}, validateBlocks, IsSynced, &mockBlockProvider{}, hareOracle, uint16(app.Config.LayersPerEpoch), &mockIdProvider{}, &mockStateQuerier{}, app.clock.Subscribe(), lg)
	log.Info("Starting hare service")
	err = app.ha.Start()
	if err != nil {
		log.Panic("error starting maatuf err=%v", err)
	}
	err = app.p2p.Start()
	if err != nil {
		log.Panic("error starting p2p err=%v", err)
	}
	app.clock.StartNotifying()
}

func main() {
	if err := Cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
