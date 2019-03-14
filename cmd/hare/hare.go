package main

import (
	"fmt"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/oracle"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spf13/cobra"
	"os"
	"time"
)

const defaultSetSize = 200

// Hare cmd
var Cmd = &cobra.Command{
	Use:   "hare",
	Short: "start hare",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Starting hare")
		hareApp := NewHareApp()
		defer hareApp.Cleanup()

		hareApp.Initialize(cmd)
		hareApp.Start(cmd, args)
		<-hareApp.ha.CloseChannel()
	},
}

func init() {
	cmdp.AddCommands(Cmd)
}

type mockBlockProvider struct {
	isPulled bool
}

func (mbp *mockBlockProvider) GetUnverifiedLayerBlocks(layerId mesh.LayerID) ([]mesh.BlockID, error) {
	if mbp.isPulled {
		return []mesh.BlockID{}, nil
	}

	mbp.isPulled = true
	return []mesh.BlockID{1, 2, 3}, nil
}

type HareApp struct {
	*cmdp.BaseApp
	p2p    p2p.Service
	oracle *oracle.OracleClient
	sgn    hare.Signing
	ha     *hare.Hare
	clock  *timesync.Ticker
}

func NewHareApp() *HareApp {
	return &HareApp{BaseApp: cmdp.NewBaseApp(), sgn: hare.NewMockSigning()}
}

func (app *HareApp) Cleanup() {
	// TODO: move to array of cleanup functions and execute all here
	app.oracle.Unregister(true, app.sgn.Verifier().String())
}

func buildSet() *hare.Set {
	s := hare.NewEmptySet(defaultSetSize)

	for i := uint64(0); i < defaultSetSize; i++ {
		s.Add(hare.NewValue(i))
	}

	return s
}

func (app *HareApp) Start(cmd *cobra.Command, args []string) {
	// start p2p services
	log.Info("Initializing P2P services")
	swarm, err := p2p.New(cmdp.Ctx, app.Config.P2P)
	app.p2p = swarm
	if err != nil {
		log.Error("Error starting p2p services, err: %v", err)
		panic("Error starting p2p services")
	}

	pub := app.sgn.Verifier()

	lg := log.NewDefault(pub.String())

	oracle.SetServerAddress(app.Config.OracleServer)
	app.oracle = oracle.NewOracleClientWithWorldID(uint64(app.Config.OracleServerWorldId))
	app.oracle.Register(true, pub.String()) // todo: configure no faulty nodes
	hareOracle := oracle.NewHareOracleFromClient(app.oracle)

	gTime, err := time.Parse(time.RFC3339, app.Config.GenesisTime)
	if err != nil {
		log.Error("Could not parse config GT t=%v err=%v", app.Config.GenesisTime, err)
		panic("error parsing config")
	}
	ld := time.Duration(app.Config.LayerDurationSec) * time.Second
	app.clock = timesync.NewTicker(timesync.RealClock{}, ld, gTime)

	app.ha = hare.New(app.Config.HARE, swarm, app.sgn, &mockBlockProvider{}, hareOracle, app.clock.Subscribe(), lg)
	log.Info("Starting hare service")
	app.ha.Start()
	app.p2p.Start()
	app.clock.Start()
}

func main() {
	if err := Cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
